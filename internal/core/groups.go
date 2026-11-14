package core

// Your groups (DESIGN §4.1): named sets of sessions that the sidebars draw
// together, under a header that folds them, like a browser's tab groups.
// You make one from a card or several, name it, put cards in it and take
// them out, and remove it; an agent may too, at your word (irie ctl group,
// irie ctl spawn --group), and a session an agent starts goes in that
// agent's group. Nothing else files a session in a group or takes one out:
// a state change never does.
//
// A group has no place of its own in the list's one order (order.go): a
// sidebar draws it where the first of its sessions is, with its other
// sessions just after it, in their order. So the core keeps a group's
// sessions together: a new group gathers its sessions just after the first
// of them, a session that joins a group goes last in it, and one taken out
// goes just after it. A pinned or parked session of a group waits in
// Pinned or Parked, and comes back to its group when unpinned or unparked;
// one that joins a group comes out of Pinned or Parked.
//
// The groups live in the @irie_groups option of the agents' session, as
// JSON; a session's group is in the journal (ui.grouped), as its space is,
// so a restarted core keeps both, and a closed session keeps its group for
// its reopen. A group goes when no session is in it, open or closed, or
// when you remove it. Joining, leaving and removing a group are steps of
// the history; making one is its sessions joining it, and renaming one is
// not a step. Moving a session to another space takes it out of its group,
// unless the whole group goes with it: each space has its own groups.

import (
	"cmp"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// optGroups holds the groups, as JSON: [{"id", "name"}], in the order they
// were made.
const optGroups = "@irie_groups"

// maxGroupName is the longest a group's name may be, in characters.
const maxGroupName = 40

// loadGroups reads the groups a previous core kept.
func (c *Core) loadGroups() {
	out, err := c.tmux.Run("show-options", "-qv", "-t", agentsTarget, optGroups)
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	var saved []ipc.Group
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &saved); err != nil {
		c.log.Warn("groups", "err", err)
		return
	}
	c.groups = slices.DeleteFunc(saved, func(g ipc.Group) bool { return g.ID == "" || g.Name == "" })
}

// saveGroups lets go of the groups no session is in, open or closed, and
// keeps the rest in tmux. The caller holds c.mu.
func (c *Core) saveGroups() {
	used := map[string]bool{}
	for _, x := range c.sessions {
		used[x.track.group] = true
	}
	for _, r := range c.closes.list {
		used[r.Group] = true
	}
	c.groups = slices.DeleteFunc(c.groups, func(g ipc.Group) bool { return !used[g.ID] })
	b, _ := json.Marshal(c.groups)
	if c.groups == nil {
		b = []byte("[]")
	}
	if _, err := c.tmux.Run("set-option", "-t", agentsTarget, optGroups, string(b)); err != nil {
		c.log.Warn("groups", "err", err)
	}
}

// groupIndex is where group id is among the groups, or -1.
func (c *Core) groupIndex(id string) int {
	if id == "" {
		return -1
	}
	return slices.IndexFunc(c.groups, func(g ipc.Group) bool { return g.ID == id })
}

// groupNamed is where the group of that name is, in any case, or -1.
func (c *Core) groupNamed(name string) int {
	name = c.groupName(name, "\x00")
	return slices.IndexFunc(c.groups, func(g ipc.Group) bool { return strings.EqualFold(g.Name, name) })
}

// groupOf is the group x is in, as its row says it: "" when it is in none,
// or its group is gone. The caller holds c.mu.
func (c *Core) groupOf(x *session) string {
	if x == nil || c.groupIndex(x.track.group) < 0 {
		return ""
	}
	return x.track.group
}

// groupNameOf is group id's name, or "" when it is gone. The caller holds
// c.mu.
func (c *Core) groupNameOf(id string) string {
	if i := c.groupIndex(id); i >= 0 {
		return c.groups[i].Name
	}
	return ""
}

// setGroup puts x in group id ("" for none), and reports whether that moved
// it. The caller holds c.mu.
func (c *Core) setGroup(x *session, id, source string) bool {
	if c.groupOf(x) == id {
		return false
	}
	c.record(x, event.Event{Source: source, Kind: event.UIGrouped, Data: map[string]any{"group": id}})
	return true
}

// ensureGroup makes group id again, named name, should it be gone, as undo
// does for a group that went with its last session. The caller holds c.mu.
func (c *Core) ensureGroup(id, name string) {
	if id != "" && c.groupIndex(id) < 0 {
		c.groups = append(c.groups, ipc.Group{ID: id, Name: c.groupName(name, id)})
	}
}

// newGroup makes a group named name, or a name of its own, and returns its
// id. The caller holds c.mu.
func (c *Core) newGroup(name string) string {
	g := ipc.Group{ID: newGroupID(), Name: c.groupName(name, "")}
	c.groups = append(c.groups, g)
	return g.ID
}

// groupName makes name fit to name a group other than id: one line, cut to
// maxGroupName, and numbered when another group has it in any case; a name
// of its own, "Group 2", when empty. The caller holds c.mu.
func (c *Core) groupName(name, id string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(name); len(r) > maxGroupName {
		name = strings.TrimSpace(string(r[:maxGroupName]))
	}
	if id == "\x00" {
		return name
	}
	taken := func(n string) bool {
		return slices.ContainsFunc(c.groups, func(g ipc.Group) bool { return g.ID != id && strings.EqualFold(g.Name, n) })
	}
	if name == "" {
		for i := len(c.groups) + 1; ; i++ {
			if n := fmt.Sprintf("Group %d", i); !taken(n) {
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

// newGroupID is a group's id: "g" and 12 random base32 characters.
func newGroupID() string {
	var b [12]byte
	rand.Read(b[:])
	out := []byte("g")
	for _, x := range b {
		out = append(out, crockford[int(x)%len(crockford)])
	}
	return string(out)
}

// inRest reports whether x is drawn among the rest of the list, in its
// group if it has one: neither pinned nor parked.
func inRest(x *session) bool { return x != nil && !x.track.pinned && !x.track.parked }

// lastIn is the last session of group id in the order that the group
// draws, one neither pinned nor parked, leaving out those in skip; "" when
// there is none. The caller holds c.mu.
func (c *Core) lastIn(id string, skip map[string]bool) string {
	last := ""
	for _, s := range c.fullOrder() {
		if x := c.sessions[s]; x != nil && !skip[s] && inRest(x) && c.groupOf(x) == id {
			last = s
		}
	}
	return last
}

// gather puts the sessions ids, which are in the list's order, together:
// just before session before; else just after session after; else where
// the first of them is. The caller holds c.mu.
func (c *Core) gather(ids []string, after, before string) {
	if len(ids) == 0 {
		return
	}
	order := c.fullOrder()
	moving := map[string]bool{}
	for _, id := range ids {
		moving[id] = true
	}
	var rest []string
	at := -1
	for _, s := range order {
		if s == ids[0] && at < 0 {
			at = len(rest)
		}
		if !moving[s] {
			rest = append(rest, s)
		}
	}
	switch {
	case before != "" && slices.Contains(rest, before):
		at = slices.Index(rest, before)
	case after != "" && slices.Contains(rest, after):
		at = slices.Index(rest, after) + 1
	case at < 0:
		at = len(rest)
	}
	next := slices.Concat(rest[:at], ids, rest[at:])
	if !slices.Equal(next, order) {
		c.setOrder(next)
	}
}

// groupable are this machine's open sessions among ids, each once, in the
// list's order. The caller holds c.mu.
func (c *Core) groupable(ids []string) []*session {
	pos := map[string]int{}
	for i, s := range c.fullOrder() {
		pos[s] = i
	}
	var out []*session
	for _, id := range ids {
		if x := c.sessions[id]; x != nil && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	slices.SortStableFunc(out, func(a, b *session) int { return cmp.Compare(pos[a.id], pos[b.id]) })
	return out
}

// groupStep is one session's step of the history that a group op took:
// the groups it left and joined, and the places it left and went to.
type groupStep struct {
	id   string
	data map[string]any
}

// ids of steps, in their order.
func stepIDs(steps []groupStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.id
	}
	return out
}

// join puts the sessions xs, in the list's order, in group id: last in it,
// or where a drag dropped them, just before session before or just after
// session after. One that joins comes out of Pinned or Parked. Those in it
// already stay where they are. It returns each step that undo needs. The
// caller holds c.mu.
func (c *Core) join(xs []*session, id, after, before string) []groupStep {
	var moving []*session
	for _, x := range xs {
		if c.groupOf(x) != id || !inRest(x) {
			moving = append(moving, x)
		}
	}
	if len(moving) == 0 {
		return nil
	}
	steps := make([]groupStep, len(moving))
	skip, was := map[string]bool{}, map[string][]string{}
	for i, x := range moving {
		skip[x.id], was[x.id] = true, c.nextOf(x.id)
		d := map[string]any{"from": c.groupOf(x), "to": id, "to_name": c.groupNameOf(id)}
		if from := c.groupOf(x); from != "" {
			d["from_name"] = c.groupNameOf(from)
		}
		for k, v := range map[string]string{"before": before, "after": after} {
			if v != "" {
				d[k] = v
			}
		}
		if x.track.pinned {
			c.setPinned(x.id, false)
			d["pinned"] = true
		}
		if x.track.parked {
			c.setParked(x.id, false)
			d["parked"] = true
		}
		steps[i] = groupStep{id: x.id, data: d}
	}
	if after == "" && before == "" {
		after = c.lastIn(id, skip)
	}
	for _, x := range moving {
		c.setGroup(x, id, event.SourceUser)
	}
	c.gather(stepIDs(steps), after, before)
	c.notePlaces(steps, was)
	return steps
}

// leave takes the sessions xs, in the list's order, out of their groups:
// each just after the rest of the group it leaves, or where a drag dropped
// them, just before session before or just after session after. A pinned
// or parked one stays where it is. It returns each step that undo needs.
// The caller holds c.mu.
func (c *Core) leave(xs []*session, after, before string) []groupStep {
	var steps []groupStep
	skip, was, from := map[string]bool{}, map[string][]string{}, map[string]string{}
	for _, x := range xs {
		g := c.groupOf(x)
		if g == "" {
			continue
		}
		skip[x.id], was[x.id], from[x.id] = true, c.nextOf(x.id), g
		d := map[string]any{"from": g, "from_name": c.groupNameOf(g), "to": ""}
		for k, v := range map[string]string{"before": before, "after": after} {
			if v != "" {
				d[k] = v
			}
		}
		steps = append(steps, groupStep{id: x.id, data: d})
	}
	if len(steps) == 0 {
		return nil
	}
	// Where each goes, worked out while the groups still have them.
	anchor := map[string]string{}
	for _, s := range steps {
		if _, ok := anchor[from[s.id]]; !ok {
			anchor[from[s.id]] = c.lastIn(from[s.id], skip)
		}
	}
	var placed []string
	for _, s := range steps {
		x := c.sessions[s.id]
		c.setGroup(x, "", event.SourceUser)
		if !inRest(x) {
			continue
		}
		if before != "" || after != "" {
			placed = append(placed, x.id)
			continue
		}
		if a := anchor[from[x.id]]; a != "" {
			c.gather([]string{x.id}, a, "")
			anchor[from[x.id]] = x.id
		}
	}
	c.gather(placed, after, before)
	c.notePlaces(steps, was)
	return steps
}

// notePlaces adds to each step, when its session moved, the place it left,
// which undo puts it back in, and the place it went to. Redo works that
// one out again (doGroup): of several steps taken together, the first
// redone would find the others' places not yet there. The caller holds
// c.mu.
func (c *Core) notePlaces(steps []groupStep, was map[string][]string) {
	for _, s := range steps {
		if now := c.nextOf(s.id); !slices.Equal(was[s.id], now) {
			s.data["was"], s.data["now"] = was[s.id], now
		}
	}
}

// replace puts session x, which a step moved into group to or out of group
// from (to ""), where the step put it: just before or after the session
// its drag dropped it by, while that one is in that group, or in none as x
// is; else last in its group, or just after the group it left. The caller
// holds c.mu.
func (c *Core) replace(x *session, to, from, before, after string) {
	if !inRest(x) {
		return
	}
	if y := c.sessions[before]; y != nil && y != x && c.groupOf(y) == to {
		c.gather([]string{x.id}, "", before)
		return
	}
	if y := c.sessions[after]; y != nil && y != x && c.groupOf(y) == to {
		c.gather([]string{x.id}, after, "")
		return
	}
	in := to
	if in == "" {
		in = from
	}
	if last := c.lastIn(in, map[string]bool{x.id: true}); last != "" {
		c.gather([]string{x.id}, last, "")
	}
}

// groupOp lists your groups, or makes, fills, empties, renames or removes
// one, and returns them as they then are. New, add, remove and ungroup are
// one step of the history, which one undo takes back.
func (c *Core) groupOp(a ipc.GroupArgs) (ipc.GroupResult, error) {
	c.mu.Lock()
	res := ipc.GroupResult{}
	var steps []groupStep
	kind := actGroup
	err := func() error {
		for _, s := range []string{a.Before, a.After} {
			if s != "" && c.sessions[s] == nil {
				return ipc.Errorf(ipc.CodeNotFound, "no session %q", s)
			}
		}
		switch a.Do {
		case ipc.GroupList:
		case ipc.GroupNew:
			xs := c.groupable(a.Sessions)
			if len(xs) == 0 {
				return ipc.Errorf(ipc.CodeBadArgs, "a new group needs one of this machine's sessions")
			}
			res.Group = c.newGroup(a.Name)
			steps = c.join(xs, res.Group, "", "")
		case ipc.GroupAdd:
			at := c.groupIndex(a.Group)
			switch {
			case a.Group != "" && at < 0:
				return ipc.Errorf(ipc.CodeNotFound, "no group %q", a.Group)
			case a.Group == "" && strings.TrimSpace(a.Name) == "":
				return ipc.Errorf(ipc.CodeBadArgs, "add needs a group, or a group's name")
			}
			xs := c.groupable(a.Sessions)
			if len(xs) == 0 {
				return ipc.Errorf(ipc.CodeBadArgs, "no session of this machine's to put in a group")
			}
			if at < 0 {
				at = c.groupNamed(a.Name)
			}
			if at < 0 {
				res.Group = c.newGroup(a.Name)
			} else {
				res.Group = c.groups[at].ID
			}
			steps = c.join(xs, res.Group, a.After, a.Before)
		case ipc.GroupRemove:
			kind = actUngroup
			steps = c.leave(c.groupable(a.Sessions), a.After, a.Before)
		case ipc.GroupRename:
			at := c.groupIndex(a.Group)
			if at < 0 {
				return ipc.Errorf(ipc.CodeNotFound, "no group %q", a.Group)
			}
			if strings.TrimSpace(a.Name) == "" {
				return ipc.Errorf(ipc.CodeBadArgs, "a group needs a name")
			}
			c.groups[at].Name = c.groupName(a.Name, a.Group)
		case ipc.GroupUngroup:
			kind = actUngroup
			at := c.groupIndex(a.Group)
			if at < 0 {
				return ipc.Errorf(ipc.CodeNotFound, "no group %q", a.Group)
			}
			// Its sessions stay where they are.
			g := c.groups[at]
			for _, id := range c.fullOrder() {
				if x := c.sessions[id]; x != nil && c.groupOf(x) == g.ID {
					c.setGroup(x, "", event.SourceUser)
					steps = append(steps, groupStep{id: x.id, data: map[string]any{"from": g.ID, "from_name": g.Name, "to": ""}})
				}
			}
			c.groups = slices.Delete(c.groups, at, at+1)
		default:
			return ipc.Errorf(ipc.CodeBadArgs, "unknown group do %q", a.Do)
		}
		return nil
	}()
	if err == nil && a.Do != ipc.GroupList {
		c.saveGroups()
		c.log.Info("group", "do", a.Do, "group", cmp.Or(res.Group, a.Group), "sessions", len(steps))
	}
	res.Groups = slices.Clone(c.groups)
	res.Sessions = stepIDs(steps)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
	byID := map[string]map[string]any{}
	for _, s := range steps {
		byID[s.id] = s.data
	}
	c.recordEach(kind, res.Sessions, func(id string) map[string]any { return byID[id] })
	return res, err
}

// Action kinds of a group op (undo.go): a session that joined a group, as
// new and add put them, and one that left its group, as remove and ungroup
// take them out. Undo puts it back in the group it left, or in none, and
// in the place it left; redo where the op put it.
const (
	actGroup   = "group"
	actUngroup = "ungroup"
)

func init() {
	registerAction(actGroup, actionKind{verb: "group",
		undo: effect{check: checkGroup("from"), do: doGroup("from")},
		redo: effect{check: checkGroup("to"), do: doGroup("to")}})
	registerAction(actUngroup, actionKind{verb: "ungroup",
		undo: effect{check: checkGroup("from"), do: doGroup("from")},
		redo: effect{check: checkGroup("to"), do: doGroup("to")}})
}

// checkGroup is what makes putting a's session in the group its data keeps
// as k uncertain: that it is closed or gone, or in that group already.
func checkGroup(k string) func(c *Core, a action) []problem {
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			if _, ok := c.closedRecord(a.Session); ok {
				return []problem{{code: "closed", question: fmt.Sprintf("%q is closed, so there is nothing to move.", c.nameOf(a.Session)), recommend: ipc.ChoiceSkip}}
			}
			return []problem{gone(a)}
		}
		if c.groupOf(x) == a.str(k) {
			where := "in no group already"
			if a.str(k) != "" {
				where = "in that group already"
			}
			return []problem{{code: "already", question: fmt.Sprintf("%q is %s.", x.name, where),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it as it is and move on",
					Detail: "Nothing needs moving, so the history moves on as if " + brand.Name + " had moved it."}},
				recommend: ipc.ChoiceSettle}}
		}
		return nil
	}
}

// doGroup puts a's session in the group its data keeps as k, made again
// should it have gone: as before the step (from), in the place it left, or
// as after it (to), where the step put it. A session that joining unpinned
// or unparked is pinned or parked again by undo, and comes out again by
// redo.
func doGroup(k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		g := a.str(k)
		c.ensureGroup(g, a.str(k+"_name"))
		back := k == "from"
		if a.flag("pinned") {
			c.setPinned(x.id, back)
		}
		if a.flag("parked") {
			c.setParked(x.id, back)
		}
		c.setGroup(x, g, event.SourceUser)
		switch _, moved := a.Data["was"]; {
		case moved && back:
			c.putBack(x.id, a.strs("was"))
		case moved:
			c.replace(x, a.str("to"), a.str("from"), a.str("before"), a.str("after"))
		}
		c.saveGroups()
		a.Name = x.name
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		c.publishHeader()
		return a, nil
	}
}

// leaveForSpace takes x, just moved to space to, out of its group when the
// group keeps a session elsewhere, so each space has its own groups, and
// returns the group it left; "" when it stays in it or has none. moved are
// the sessions that move with it, as the group may go whole. The caller
// holds c.mu.
func (c *Core) leaveForSpace(x *session, to string, moved map[string]bool) string {
	g := c.groupOf(x)
	if g == "" {
		return ""
	}
	for _, y := range c.sessions {
		if y != x && !moved[y.id] && c.groupOf(y) == g && c.spaceOf(y) != to {
			c.setGroup(x, "", event.SourceUser)
			return g
		}
	}
	return ""
}

// placeInGroup puts session id, just spawned by a, in its group: a.Group,
// a group's id or name, made when no group has that name; else the group
// of the session whose agent started it, or for a jot that ui.new_tabs
// puts beside the card selected, that card's group, while that session is
// in the new one's space. It records no step: a new session is not a move
// of yours. The caller holds c.mu.
func (c *Core) placeInGroup(id string, a ipc.SpawnArgs) {
	x := c.sessions[id]
	if x == nil {
		return
	}
	g := ""
	switch {
	case a.Group != "":
		if at := c.groupIndex(a.Group); at >= 0 {
			g = a.Group
		} else if at := c.groupNamed(a.Group); at >= 0 {
			g = c.groups[at].ID
		} else if strings.TrimSpace(a.Group) != "" {
			g = c.newGroup(a.Group)
		}
	case !a.Jot || c.cfg().UI.NewTabs == "next":
		if from := c.sessions[cmp.Or(a.After, a.Parent, a.Continues)]; from != nil && from != x && c.spaceOf(from) == c.spaceOf(x) {
			g = c.groupOf(from)
		}
	}
	if g == "" {
		return
	}
	c.record(x, event.Event{Source: event.SourceIrie, Kind: event.UIGrouped, Data: map[string]any{"group": g}})
	c.saveGroups()
}
