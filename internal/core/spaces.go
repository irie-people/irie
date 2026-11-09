package core

// Spaces (DESIGN §4.1): named sets of sessions that a sidebar shows one at
// a time, like a browser's spaces, each with its own Pinned and Parked.
// Every session is in one: the first, unless you put it in another, or it
// was started from a session in another. The spaces live in the
// @irie_spaces option of the agents' session, as JSON, in their order; a
// session's space is in the journal (ui.spaced), as its park is, so a
// restarted core keeps both, and a closed session keeps its space for its
// reopen. A session whose space is gone is in the first. Moving sessions
// to another space is one step of the history; making, renaming, moving
// and removing a space are not.

import (
	"cmp"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// optSpaces holds the spaces, as JSON: [{"id", "name"}], in their order.
const optSpaces = "@irie_spaces"

// firstSpace is the space every Irie starts with.
var firstSpace = ipc.Space{ID: "main", Name: "Main"}

// maxSpaceName is the longest a space's name may be, in characters.
const maxSpaceName = 40

// loadSpaces reads the spaces a previous core kept.
func (c *Core) loadSpaces() {
	c.spaces = []ipc.Space{firstSpace}
	out, err := c.tmux.Run("show-options", "-qv", "-t", agentsTarget, optSpaces)
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	var saved []ipc.Space
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &saved); err != nil {
		c.log.Warn("spaces", "err", err)
		return
	}
	saved = slices.DeleteFunc(saved, func(s ipc.Space) bool { return s.ID == "" || s.Name == "" })
	if len(saved) > 0 {
		c.spaces = saved
	}
}

// saveSpaces keeps the spaces in tmux. The caller holds c.mu.
func (c *Core) saveSpaces() {
	b, _ := json.Marshal(c.spaces)
	if _, err := c.tmux.Run("set-option", "-t", agentsTarget, optSpaces, string(b)); err != nil {
		c.log.Warn("spaces", "err", err)
	}
}

// spaceIndex is where space id is among the spaces, or -1.
func (c *Core) spaceIndex(id string) int {
	return slices.IndexFunc(c.spaces, func(s ipc.Space) bool { return s.ID == id })
}

// spaceKey is space id as a row says it: "" for the first space, or for one
// that is gone. The caller holds c.mu.
func (c *Core) spaceKey(id string) string {
	if c.spaceIndex(id) <= 0 {
		return ""
	}
	return id
}

// spaceOf is the space x is in, as its row says it. The caller holds c.mu.
func (c *Core) spaceOf(x *session) string {
	if x == nil {
		return ""
	}
	return c.spaceKey(x.track.space)
}

// putInSpace puts x in space id ("" for the first), and reports whether
// that moved it. The caller holds c.mu.
func (c *Core) putInSpace(x *session, id string) bool {
	id = c.spaceKey(id)
	if c.spaceOf(x) == id {
		return false
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.UISpaced, Data: map[string]any{"space": id}})
	return true
}

// spacesOp lists the spaces, or makes, renames, moves or removes one, and
// returns them as they then are.
func (c *Core) spacesOp(a ipc.SpacesArgs) (ipc.SpacesResult, error) {
	c.mu.Lock()
	defer func() {
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
	}()
	res := ipc.SpacesResult{}
	at := c.spaceIndex(a.Space)
	if a.Do != ipc.SpacesList && a.Do != ipc.SpacesNew && at < 0 {
		return res, ipc.Errorf(ipc.CodeNotFound, "no space %q", a.Space)
	}
	switch a.Do {
	case ipc.SpacesList:
	case ipc.SpacesNew:
		s := ipc.Space{ID: newSpaceID(), Name: c.spaceName(a.Name, "")}
		c.spaces = append(c.spaces, s)
		res.Space = s.ID
	case ipc.SpacesRename:
		if strings.TrimSpace(a.Name) == "" {
			return res, ipc.Errorf(ipc.CodeBadArgs, "a space needs a name")
		}
		c.spaces[at].Name = c.spaceName(a.Name, a.Space)
	case ipc.SpacesMove:
		if a.Before == a.Space {
			break
		}
		if a.Before != "" && c.spaceIndex(a.Before) < 0 {
			return res, ipc.Errorf(ipc.CodeNotFound, "no space %q", a.Before)
		}
		s := c.spaces[at]
		c.spaces = slices.Delete(c.spaces, at, at+1)
		to := len(c.spaces)
		if a.Before != "" {
			to = c.spaceIndex(a.Before)
		}
		c.spaces = slices.Insert(c.spaces, to, s)
	case ipc.SpacesRemove:
		if len(c.spaces) == 1 {
			return res, ipc.Errorf(ipc.CodeBadArgs, "the last space stays")
		}
		// Its sessions are in the first space from now on, which rows say
		// by themselves: a session whose space is gone is in the first.
		c.spaces = slices.Delete(c.spaces, at, at+1)
	default:
		return res, ipc.Errorf(ipc.CodeBadArgs, "unknown spaces do %q", a.Do)
	}
	if a.Do != ipc.SpacesList {
		c.saveSpaces()
		c.log.Info("spaces", "do", a.Do, "space", cmp.Or(res.Space, a.Space))
	}
	res.Spaces = slices.Clone(c.spaces)
	return res, nil
}

// spaceName makes name fit to name a space other than id: one line, cut
// to maxSpaceName, and numbered when another space has it in any case; a
// name of its own, "Space 2", when empty. The caller holds c.mu.
func (c *Core) spaceName(name, id string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(name); len(r) > maxSpaceName {
		name = strings.TrimSpace(string(r[:maxSpaceName]))
	}
	taken := func(n string) bool {
		return slices.ContainsFunc(c.spaces, func(s ipc.Space) bool { return s.ID != id && strings.EqualFold(s.Name, n) })
	}
	if name == "" {
		for i := len(c.spaces) + 1; ; i++ {
			if n := fmt.Sprintf("Space %d", i); !taken(n) {
				return n
			}
		}
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !taken(n) {
			return n
		}
	}
}

// newSpaceID is a space's id: "p" and 12 random base32 characters.
func newSpaceID() string {
	var b [12]byte
	rand.Read(b[:])
	out := []byte("p")
	for _, x := range b {
		out = append(out, crockford[int(x)%len(crockford)])
	}
	return string(out)
}

// spaceOp puts the sessions a names in a space, as one step of the history,
// and returns those it moved. Another machine's sessions stay where they
// are, in that machine's own group.
func (c *Core) spaceOp(a ipc.SpaceArgs) (ipc.SpaceResult, error) {
	c.mu.Lock()
	if a.Space != "" && c.spaceIndex(a.Space) < 0 {
		c.mu.Unlock()
		return ipc.SpaceResult{}, ipc.Errorf(ipc.CodeNotFound, "no space %q", a.Space)
	}
	to := c.spaceKey(a.Space)
	from := map[string]string{}
	var moved []string
	for _, id := range a.Sessions {
		x := c.sessions[id]
		if x == nil || slices.Contains(moved, id) {
			continue
		}
		was := c.spaceOf(x)
		if c.putInSpace(x, to) {
			moved = append(moved, id)
			from[id] = was
		}
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.recordEach(actSpace, moved, func(id string) map[string]any {
		return map[string]any{"from": from[id], "to": to}
	})
	return ipc.SpaceResult{Sessions: moved}, nil
}

// actSpace is the action kind of a move to another space (undo.go): undo
// puts the session back in the space it left, and redo in the one it went
// to; when that space is gone, in the first.
const actSpace = "space"

func init() {
	registerAction(actSpace, actionKind{verb: "move",
		label: func(a action) string { return label("move", a.Name) + " to another space" },
		undo:  effect{check: checkSpace("from"), do: doSpace("from")},
		redo:  effect{check: checkSpace("to"), do: doSpace("to")}})
}

// checkSpace is what makes putting a's session in the space its data keeps
// as k uncertain: that it is closed or gone, or in that space already.
func checkSpace(k string) func(c *Core, a action) []problem {
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			if _, ok := c.closedRecord(a.Session); ok {
				return []problem{{code: "closed", question: fmt.Sprintf("%q is closed, so there is nothing to move.", c.nameOf(a.Session)), recommend: ipc.ChoiceSkip}}
			}
			return []problem{gone(a)}
		}
		if c.spaceOf(x) == c.spaceKey(a.str(k)) {
			return []problem{{code: "already", question: fmt.Sprintf("%q is in that space already.", x.name),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it as it is and move on",
					Detail: "Nothing needs moving, so the history moves on as if Irie had moved it."}},
				recommend: ipc.ChoiceSettle}}
		}
		return nil
	}
}

// doSpace puts a's session in the space its data keeps as k.
func doSpace(k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		c.putInSpace(x, a.str(k))
		a.Name = x.name
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		return a, nil
	}
}

// placeInSpace puts session id, just spawned by a, in its space: a.Space,
// else the space of the session that started it. The caller holds c.mu.
func (c *Core) placeInSpace(id string, a ipc.SpawnArgs) {
	x := c.sessions[id]
	if x == nil {
		return
	}
	space := a.Space
	if space == "" {
		space = c.spaceOf(c.sessions[cmp.Or(a.After, a.Parent, a.Continues)])
	}
	if space = c.spaceKey(space); space != "" {
		c.record(x, event.Event{Source: event.SourceIrie, Kind: event.UISpaced, Data: map[string]any{"space": space}})
	}
}
