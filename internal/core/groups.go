package core

// Your groups (DESIGN §4.1): named sets of sessions that the sidebars draw
// together, under a header that folds them, like a browser's tab groups,
// and that hold other groups, as folders hold folders, up to
// ipc.MaxGroupDepth deep. You make one from a card or several, or around a
// group, name it, put cards and groups in it and take them out, and remove
// it; an agent may too, at your word (irie ctl group, irie ctl nest, irie
// ctl spawn --group), and a session an agent starts goes in that agent's
// group. Nothing else files a session in a group or takes one out: a state
// change never does.
//
// A group has no place of its own in the list's one order (order.go): a
// sidebar draws it where the first of its sessions is, its groups'
// sessions included, with the rest of them just after it, in their order,
// and the groups inside it where the first of theirs is. So the core keeps
// a group's sessions together whatever changes the order (together), which
// changes nothing a sidebar draws: a new group forms where the first of
// what it holds is, a session or group that joins a group goes last in it
// or where a drag dropped it, and one taken out goes just after the group
// it left. A pinned or parked session of a group waits in Pinned or Parked,
// and comes back last in its group when unpinned or unparked, or first when
// promoted, so the group stays where it is; one that joins a group comes
// out of Pinned or Parked.
//
// The groups live in the @irie_groups option of the agents' session, as
// JSON; a session's group is in the journal (ui.grouped), as its space is,
// so a restarted core keeps both, and a closed session keeps its group for
// its reopen. A group goes when no session is in it or in a group inside
// it, open or closed, or when you remove it. Joining, leaving, moving and
// removing a group are steps of the history; making one is what it holds
// joining it, and renaming one is not a step. Moving a session to another
// space takes it out of its group, unless the whole group goes with it,
// which then comes out of a group around it that stays: each space has its
// own groups.

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

// optGroups holds the groups, as JSON: [{"id", "name", "parent"}], in the
// order they were made.
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
	c.fixGroups()
}

// fixGroups puts at the top of the list a group whose group is gone, or
// is inside it, as an option edited by hand could have it. The caller
// holds c.mu, or the core is starting.
func (c *Core) fixGroups() {
	for i := range c.groups {
		if p := c.groups[i].Parent; p != "" && (c.groupIndex(p) < 0 || c.within(p, c.groups[i].ID)) {
			c.groups[i].Parent = ""
		}
	}
}

// saveGroups lets go of the groups no session is in, open or closed, nor
// in a group inside them, and keeps the rest in tmux. The caller holds
// c.mu.
func (c *Core) saveGroups() {
	used := map[string]bool{}
	mark := func(g string) {
		for range len(c.groups) + 1 {
			if g == "" || used[g] {
				return
			}
			used[g] = true
			g = c.parentOf(g)
		}
	}
	for _, x := range c.sessions {
		mark(x.track.group)
	}
	for _, r := range c.closes.list {
		mark(r.Group)
	}
	c.groups = slices.DeleteFunc(c.groups, func(g ipc.Group) bool { return !used[g.ID] })
	c.fixGroups()
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

// findGroup is where the group ref names is, by id, name or path
// (ipc.FindGroup), or -1.
func (c *Core) findGroup(ref string) int { return ipc.FindGroup(c.groups, ref) }

// findGroupIn is findGroup among the groups of space, or -1: a group of
// another space's sessions is not one a session of this space can join.
// The caller holds c.mu.
func (c *Core) findGroupIn(ref, space string) int {
	var in []ipc.Group
	var at []int
	for i, g := range c.groups {
		if c.groupIn(g.ID, space, nil) {
			in, at = append(in, g), append(at, i)
		}
	}
	if j := ipc.FindGroup(in, ref); j >= 0 {
		return at[j]
	}
	return -1
}

// groupIn reports whether group id can hold a session of space: none of
// its open sessions, nor those of the groups inside it, but those in
// except, is in another space, as each space has its own groups. The
// caller holds c.mu.
func (c *Core) groupIn(id, space string, except map[string]bool) bool {
	if id == "" {
		return true
	}
	for _, y := range c.sessions {
		if !except[y.id] && c.within(c.groupOf(y), id) && c.spaceOf(y) != space {
			return false
		}
	}
	return true
}

// oneSpace refuses sessions xs of more than one space, or, for a group id
// to join, of a space other than its sessions': each space has its own
// groups. The caller holds c.mu.
func (c *Core) oneSpace(xs []*session, id string) error {
	if len(xs) == 0 {
		return nil
	}
	space, moving := c.spaceOf(xs[0]), map[string]bool{}
	for _, x := range xs {
		moving[x.id] = true
		if c.spaceOf(x) != space {
			return ipc.Errorf(ipc.CodeBadArgs, "%q and %q are in different spaces, and a group is in one", xs[0].name, x.name)
		}
	}
	if id != "" && !c.groupIn(id, space, moving) {
		return ipc.Errorf(ipc.CodeBadArgs, "the group %q is in another space than %q", c.groupNameOf(id), xs[0].name)
	}
	return nil
}

// parentOf is the group group id is in: "" at the top of the list, or
// when id is gone.
func (c *Core) parentOf(id string) string {
	if i := c.groupIndex(id); i >= 0 {
		return c.groups[i].Parent
	}
	return ""
}

// within reports whether group id is group anc or inside it, at any depth.
// Every group is within the top of the list, anc "", and a session in no
// group is within the top alone.
func (c *Core) within(id, anc string) bool {
	if anc == "" {
		return true
	}
	for range len(c.groups) + 1 {
		if id == "" {
			return false
		}
		if id == anc {
			return true
		}
		id = c.parentOf(id)
	}
	return false
}

// pathOf is group id and the groups around it, the outermost first; none
// for "" or a group that is gone.
func (c *Core) pathOf(id string) []string {
	var out []string
	for _, g := range ipc.GroupPath(c.groups, id) {
		out = append(out, g.ID)
	}
	return out
}

// chainOf is group id and the groups around it as they are now, the
// outermost first: what a step keeps to make them again (restoreGroups).
func (c *Core) chainOf(id string) []ipc.Group { return ipc.GroupPath(c.groups, id) }

// depthOf is how many groups deep group id is: 1 at the top, 0 for "".
func (c *Core) depthOf(id string) int { return len(c.pathOf(id)) }

// heightOf is how many groups deep the groups inside group id go below
// it: 0 when it holds none.
func (c *Core) heightOf(id string) int {
	h, d := 0, c.depthOf(id)
	for _, g := range c.groups {
		if g.ID != id && c.within(g.ID, id) {
			h = max(h, c.depthOf(g.ID)-d)
		}
	}
	return h
}

// around is the innermost group that holds all of locs, each a group or
// "" for the top of the list: "" when only the top holds them all.
func (c *Core) around(locs []string) string {
	if len(locs) == 0 {
		return ""
	}
	common := c.pathOf(locs[0])
	for _, l := range locs[1:] {
		p, n := c.pathOf(l), 0
		for n < len(common) && n < len(p) && common[n] == p[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 {
		return ""
	}
	return common[len(common)-1]
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

// ensureGroup makes group id again, named name, at the top of the list,
// should it be gone, as undo does for a step kept before groups held
// groups. The caller holds c.mu.
func (c *Core) ensureGroup(id, name string) {
	if id != "" && c.groupIndex(id) < 0 {
		c.groups = append(c.groups, ipc.Group{ID: id, Name: c.groupName(name, id, "")})
	}
}

// restoreGroups makes the groups of shot, the outermost first, as a step
// kept them: each in the group shot says, made again with its id and name
// should it be gone. The caller holds c.mu.
func (c *Core) restoreGroups(shot []ipc.Group) {
	for _, g := range shot {
		if g.ID == "" {
			continue
		}
		p := g.Parent
		if p != "" && (c.groupIndex(p) < 0 || c.within(p, g.ID)) {
			p = ""
		}
		if i := c.groupIndex(g.ID); i >= 0 {
			if c.groups[i].Parent != p {
				c.groups[i].Parent = p
				c.groups[i].Name = c.groupName(c.groups[i].Name, g.ID, p)
			}
			continue
		}
		c.groups = append(c.groups, ipc.Group{ID: g.ID, Name: c.groupName(g.Name, g.ID, p), Parent: p})
	}
}

// newGroup makes a group in group parent, named name or a name of its own,
// and returns its id. The caller holds c.mu.
func (c *Core) newGroup(name, parent string) string {
	g := ipc.Group{ID: newGroupID(), Name: c.groupName(name, "", parent), Parent: parent}
	c.groups = append(c.groups, g)
	return g.ID
}

// cleanGroupName is name on one line, its spaces as one, cut to
// maxGroupName.
func cleanGroupName(name string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(name); len(r) > maxGroupName {
		name = strings.TrimSpace(string(r[:maxGroupName]))
	}
	return name
}

// groupName makes name fit to name group id in group parent: one line, cut
// to maxGroupName, and numbered when another group beside it has it in any
// case, as files in a folder are; a name of its own, "Group 2", when
// empty. The caller holds c.mu.
func (c *Core) groupName(name, id, parent string) string {
	name = cleanGroupName(name)
	taken := func(n string) bool {
		return slices.ContainsFunc(c.groups, func(g ipc.Group) bool {
			return g.ID != id && g.Parent == parent && strings.EqualFold(g.Name, n)
		})
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

// groupFor is the group ref names among those of space (findGroupIn),
// made when none has it: a path such as "Work/Irie" makes each group of it
// that is not there yet, inside the one before it. "" when ref names
// nothing, or a group of another space by its id. The caller holds c.mu.
func (c *Core) groupFor(ref, space string) (string, error) {
	if i := c.findGroupIn(ref, space); i >= 0 {
		return c.groups[i].ID, nil
	}
	if c.groupIndex(strings.TrimSpace(ref)) >= 0 {
		return "", nil
	}
	words := ipc.GroupWords(ref)
	if len(words) > ipc.MaxGroupDepth {
		return "", ipc.Errorf(ipc.CodeBadArgs, "groups go %d deep at most", ipc.MaxGroupDepth)
	}
	parent := ""
	for _, w := range words {
		w = cleanGroupName(w)
		i := slices.IndexFunc(c.groups, func(g ipc.Group) bool {
			return g.Parent == parent && strings.EqualFold(g.Name, w) && c.groupIn(g.ID, space, nil)
		})
		if i >= 0 {
			parent = c.groups[i].ID
			continue
		}
		parent = c.newGroup(w, parent)
	}
	return parent, nil
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

// blockOf are the sessions of group id drawn in the rest of the list,
// those of the groups inside it too, in the list's order, leaving out
// those in skip; for "", every session in the rest. The caller holds c.mu.
func (c *Core) blockOf(id string, skip map[string]bool) []string {
	var out []string
	for _, s := range c.fullOrder() {
		if x := c.sessions[s]; x != nil && !skip[s] && inRest(x) && c.within(c.groupOf(x), id) {
			out = append(out, s)
		}
	}
	return out
}

// firstIn and lastIn are the first and the last session within group id,
// its groups' too, in the order the group draws, ones neither pinned nor
// parked, leaving out those in skip; "" when there is none, or no group.
// The caller holds c.mu.
func (c *Core) firstIn(id string, skip map[string]bool) string {
	if b := c.blockOf(id, skip); id != "" && len(b) > 0 {
		return b[0]
	}
	return ""
}

func (c *Core) lastIn(id string, skip map[string]bool) string {
	if b := c.blockOf(id, skip); id != "" && len(b) > 0 {
		return b[len(b)-1]
	}
	return ""
}

// childOf is what of group into's holds session sid: "" when sid is in
// into itself, else the group inside into that sid is in, at any depth;
// false when sid is not within into. The caller holds c.mu.
func (c *Core) childOf(into, sid string) (string, bool) {
	x := c.sessions[sid]
	if x == nil {
		return "", false
	}
	g := c.groupOf(x)
	if g == into {
		return "", true
	}
	for range len(c.groups) + 1 {
		if g == "" {
			return "", false
		}
		if p := c.parentOf(g); p == into {
			return g, true
		} else {
			g = p
		}
	}
	return "", false
}

// spot is where in rest, the order without what moves, what goes in group
// into goes: just before what of into's holds session before (the session
// itself, or the group inside into that holds it), or just after what
// holds session after; else last in into, just after the last of its
// sessions in rest; else, when into has none there, at first, where the
// first of what moves was, or last. The caller holds c.mu.
func (c *Core) spot(rest []string, into, before, after string, first int) int {
	span := func(sid string) (lo, hi int, ok bool) {
		g, ok := c.childOf(into, sid)
		if !ok {
			return 0, 0, false
		}
		if g == "" {
			i := slices.Index(rest, sid)
			return i, i, i >= 0
		}
		lo, hi = -1, -1
		for i, s := range rest {
			if x := c.sessions[s]; x != nil && inRest(x) && c.within(c.groupOf(x), g) {
				if lo < 0 {
					lo = i
				}
				hi = i
			}
		}
		return lo, hi, lo >= 0
	}
	if before != "" {
		if lo, _, ok := span(before); ok {
			return lo
		}
	}
	if after != "" {
		if _, hi, ok := span(after); ok {
			return hi + 1
		}
	}
	last := -1
	for i, s := range rest {
		if x := c.sessions[s]; x != nil && inRest(x) && c.within(c.groupOf(x), into) {
			last = i
		}
	}
	switch {
	case last >= 0:
		return last + 1
	case first >= 0:
		return first
	}
	return len(rest)
}

// putIn puts the sessions ids, in their order, together in group into's
// part of the order, where spot says. The caller holds c.mu.
func (c *Core) putIn(ids []string, into, before, after string) {
	if len(ids) == 0 {
		return
	}
	order := c.fullOrder()
	moving := map[string]bool{}
	for _, id := range ids {
		moving[id] = true
	}
	var rest []string
	first := -1
	for _, s := range order {
		if moving[s] {
			if first < 0 {
				first = len(rest)
			}
			continue
		}
		rest = append(rest, s)
	}
	at := c.spot(rest, into, before, after, first)
	if next := slices.Concat(rest[:at], ids, rest[at:]); !slices.Equal(next, order) {
		c.setOrder(next)
	}
}

// putBlock puts the sessions ids, in their order, together just before the
// first of next that the order has without them, or last when next names
// none; where they are when none of next is left. The caller holds c.mu.
func (c *Core) putBlock(ids, next []string) {
	if len(ids) == 0 {
		return
	}
	order := c.fullOrder()
	rest := slices.DeleteFunc(slices.Clone(order), func(s string) bool { return slices.Contains(ids, s) })
	at := -1
	if len(next) == 0 {
		at = len(rest)
	}
	for _, n := range next {
		if i := slices.Index(rest, n); i >= 0 {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	if moved := slices.Concat(rest[:at], ids, rest[at:]); !slices.Equal(moved, order) {
		c.setOrder(moved)
	}
}

// together is ids with each of your groups' sessions in the rest of the
// list kept together, as the sidebars draw them: each group where the
// first of its sessions is, those of the groups inside it included, with
// the rest of them just after it, and the groups inside it where the first
// of theirs is. Only sessions of a group apart from the rest of it move,
// to where a sidebar draws them anyway, so a move worked out on the order
// lands where it shows and a group never splits; pinned, parked and closed
// sessions keep their places. setOrder runs every order through it. The
// caller holds c.mu.
func (c *Core) together(ids []string) []string {
	if len(c.groups) == 0 {
		return ids
	}
	var slots []int
	kids := map[string][]string{}
	seen := map[string]bool{}
	for i, s := range ids {
		x := c.sessions[s]
		if x == nil || !inRest(x) {
			continue
		}
		slots = append(slots, i)
		parent := ""
		for _, g := range c.pathOf(c.groupOf(x)) {
			if !seen[g] {
				seen[g] = true
				kids[parent] = append(kids[parent], "\x00"+g)
			}
			parent = g
		}
		kids[parent] = append(kids[parent], s)
	}
	if len(seen) == 0 {
		return ids
	}
	flat := make([]string, 0, len(slots))
	var walk func(string)
	walk = func(p string) {
		for _, k := range kids[p] {
			if g, ok := strings.CutPrefix(k, "\x00"); ok {
				walk(g)
			} else {
				flat = append(flat, k)
			}
		}
	}
	walk("")
	out := slices.Clone(ids)
	for j, i := range slots {
		out[i] = flat[j]
	}
	return out
}

// tidyOrder keeps the order together (together), as a change of a
// session's group needs that changes no place itself. The caller holds
// c.mu.
func (c *Core) tidyOrder() {
	if order := c.fullOrder(); !slices.Equal(c.together(order), order) {
		c.setOrder(order)
	}
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

// groupsIn are the groups refs name (findGroup), each once, without one
// inside another of them, which goes with it: in the order the list draws
// them, those it draws nowhere last. The caller holds c.mu.
func (c *Core) groupsIn(refs []string) ([]string, error) {
	var out []string
	for _, r := range refs {
		i := c.findGroup(r)
		if i < 0 {
			return nil, ipc.Errorf(ipc.CodeNotFound, "no group %q", r)
		}
		if !slices.Contains(out, c.groups[i].ID) {
			out = append(out, c.groups[i].ID)
		}
	}
	out = slices.DeleteFunc(out, func(g string) bool {
		return slices.ContainsFunc(out, func(o string) bool { return o != g && c.within(g, o) })
	})
	pos := map[string]int{}
	for i, s := range c.fullOrder() {
		if x := c.sessions[s]; x != nil && inRest(x) {
			for _, g := range c.pathOf(c.groupOf(x)) {
				if _, ok := pos[g]; !ok {
					pos[g] = i
				}
			}
		}
	}
	at := func(g string) int {
		if p, ok := pos[g]; ok {
			return p
		}
		return len(c.order) + 1 + c.groupIndex(g)
	}
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(at(a), at(b)) })
	return out, nil
}

// spaceOfGroup is the space of group id's open sessions, those of the
// groups inside it too: the first one's; false when it has none. The
// caller holds c.mu.
func (c *Core) spaceOfGroup(id string) (string, bool) {
	for _, s := range c.fullOrder() {
		if x := c.sessions[s]; x != nil && c.within(c.groupOf(x), id) {
			return c.spaceOf(x), true
		}
	}
	return "", false
}

// groupStep is one step of the history that a group op took: a session's,
// that joined or left a group (actGroup, actUngroup), or a group's, that
// went into another group or out of one (actNest), kept by the first of
// its sessions. Its data are what undo and redo need.
type groupStep struct {
	kind string
	id   string
	data map[string]any
}

// stepIDs are the sessions whose group steps changed, in the steps' order.
func stepIDs(steps []groupStep) []string {
	var out []string
	for _, s := range steps {
		if s.kind == actGroup || s.kind == actUngroup {
			out = append(out, s.id)
		}
	}
	return out
}

// shotOf is groups as a step keeps them, which state.json gives back as
// it is.
func shotOf(groups []ipc.Group) []any {
	out := make([]any, len(groups))
	for i, g := range groups {
		out[i] = map[string]any{"id": g.ID, "name": g.Name, "parent": g.Parent}
	}
	return out
}

// shot are the groups a's data keep as k (shotOf).
func (a action) shot(k string) []ipc.Group {
	v, _ := a.Data[k].([]any)
	var out []ipc.Group
	for _, e := range v {
		m, _ := e.(map[string]any)
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		parent, _ := m["parent"].(string)
		if id != "" {
			out = append(out, ipc.Group{ID: id, Name: name, Parent: parent})
		}
	}
	return out
}

// moveData is what a session's step keeps: the groups it left and joined,
// with the groups around them; notePlaces adds where it was and went.
func (c *Core) moveData(from, to string) map[string]any {
	d := map[string]any{"from": from, "to": to, "to_name": c.groupNameOf(to),
		"from_groups": shotOf(c.chainOf(from)), "to_groups": shotOf(c.chainOf(to))}
	if from != "" {
		d["from_name"] = c.groupNameOf(from)
	}
	return d
}

// join puts the sessions xs, in the list's order, in group id, "" for the
// top of the list: last in it, or where a drag dropped them, just before
// what of id's holds session before or just after what holds session
// after. One that joins comes out of Pinned or Parked. Those in it
// already stay where they are. It returns each step that undo needs. The
// caller holds c.mu.
func (c *Core) join(xs []*session, id, after, before string) []groupStep {
	var moving, staying []*session
	for _, x := range xs {
		if c.groupOf(x) != id || !inRest(x) {
			moving = append(moving, x)
		} else if before != "" || after != "" {
			staying = append(staying, x)
		}
	}
	// One in it already that a drag dropped elsewhere in it goes there, as
	// a reorder.
	var steps []groupStep
	for _, x := range staying {
		from := c.nextOf(x.id)
		c.putIn([]string{x.id}, id, before, after)
		if to := c.nextOf(x.id); !slices.Equal(from, to) {
			steps = append(steps, groupStep{kind: actReorder, id: x.id, data: map[string]any{"from": from, "to": to}})
			before, after = "", x.id
		}
	}
	if len(moving) == 0 {
		return steps
	}
	kind := actGroup
	if id == "" {
		kind = actUngroup
	}
	joined := make([]groupStep, len(moving))
	was := map[string][]string{}
	var ids []string
	for i, x := range moving {
		was[x.id] = c.nextOf(x.id)
		d := c.moveData(c.groupOf(x), id)
		if x.track.pinned {
			c.setPinned(x.id, false)
			d["pinned"] = true
		}
		if x.track.parked {
			c.setParked(x.id, false)
			d["parked"] = true
		}
		joined[i] = groupStep{kind: kind, id: x.id, data: d}
		ids = append(ids, x.id)
	}
	for _, x := range moving {
		c.setGroup(x, id, event.SourceUser)
	}
	c.putIn(ids, id, before, after)
	c.notePlaces(joined, was)
	return append(steps, joined...)
}

// leave takes the sessions xs, in the list's order, out of their groups,
// each into the group around its group, or none at the top: just after
// the rest of the group it leaves, or where a drag dropped them, just
// before session before or just after session after. A pinned or parked
// one stays where it is. It returns each step that undo needs. The caller
// holds c.mu.
func (c *Core) leave(xs []*session, after, before string) []groupStep {
	var steps []groupStep
	var froms []string
	skip, was, from := map[string]bool{}, map[string][]string{}, map[string]string{}
	for _, x := range xs {
		g := c.groupOf(x)
		if g == "" {
			continue
		}
		skip[x.id], was[x.id], from[x.id] = true, c.nextOf(x.id), g
		if !slices.Contains(froms, g) {
			froms = append(froms, g)
		}
		steps = append(steps, groupStep{kind: actUngroup, id: x.id, data: c.moveData(g, c.parentOf(g))})
	}
	if len(steps) == 0 {
		return nil
	}
	// Where each goes, worked out while the groups still have them: just
	// after the rest of the group it leaves.
	anchor := map[string]string{}
	for _, g := range froms {
		if b := c.blockOf(g, skip); len(b) > 0 {
			anchor[g] = b[len(b)-1]
		}
	}
	for _, s := range steps {
		c.setGroup(c.sessions[s.id], c.parentOf(from[s.id]), event.SourceUser)
	}
	var placed []string
	for _, g := range froms {
		var ids []string
		for _, s := range steps {
			if from[s.id] == g && inRest(c.sessions[s.id]) {
				ids = append(ids, s.id)
			}
		}
		switch {
		case before != "" || after != "":
			placed = append(placed, ids...)
		case anchor[g] != "":
			c.putIn(ids, c.parentOf(g), "", anchor[g])
		}
	}
	c.gather(placed, after, before)
	c.notePlaces(steps, was)
	return steps
}

// nest puts the groups gs, in the list's order, in group into, "" for the
// top of the list, each with its sessions: last in it, or just before what
// of into's holds session before or just after what holds session after,
// one after another. A group whose name another group in into has is
// numbered. It returns each step that undo needs: one for each group with
// an open session, which the step is kept by. The caller holds c.mu.
func (c *Core) nest(gs []string, into, before, after string) []groupStep {
	var steps []groupStep
	for _, g := range gs {
		i := c.groupIndex(g)
		if i < 0 {
			continue
		}
		from := c.groups[i].Parent
		if from == into && before == "" && after == "" {
			continue
		}
		ids := c.blockOf(g, nil)
		var was []string
		if len(ids) > 0 {
			was = slices.DeleteFunc(c.nextOf(ids[len(ids)-1]), func(s string) bool { return slices.Contains(ids, s) })
		}
		d := map[string]any{"group": g, "group_name": c.groups[i].Name, "from": from, "to": into,
			"from_name": c.groupNameOf(from), "to_name": c.groupNameOf(into),
			"from_groups": shotOf(append(c.chainOf(from), c.groups[i])), "was": was}
		for k, v := range map[string]string{"before": before, "after": after} {
			if v != "" {
				d[k] = v
			}
		}
		if from != into {
			c.groups[i].Parent = into
			c.groups[i].Name = c.groupName(c.groups[i].Name, g, into)
		}
		d["to_groups"] = shotOf(append(c.chainOf(into), c.groups[i]))
		c.putIn(ids, into, before, after)
		if len(ids) > 0 {
			before, after = "", ids[len(ids)-1]
		}
		anchor := ""
		if len(ids) > 0 {
			anchor = ids[0]
		} else {
			for _, s := range c.fullOrder() {
				if x := c.sessions[s]; x != nil && c.within(c.groupOf(x), g) {
					anchor = s
					break
				}
			}
		}
		if anchor != "" {
			steps = append(steps, groupStep{kind: actNest, id: anchor, data: d})
		}
	}
	return steps
}

// notePlaces adds to each session's step, when its session moved, the
// place it left and the place it went to, each as the sessions after it
// (nextOf). Undo takes a batch's steps back last first, so each finds the
// ones after it back in their places already; redo takes them again first
// first, so the place each went to leaves out the batch's sessions after
// it, which are not back there yet. The caller holds c.mu.
func (c *Core) notePlaces(steps []groupStep, was map[string][]string) {
	order := c.fullOrder()
	for i, s := range steps {
		if s.kind == actNest || s.kind == actReorder || slices.Equal(was[s.id], c.nextOf(s.id)) {
			continue
		}
		later := map[string]bool{}
		for _, t := range steps[i+1:] {
			later[t.id] = true
		}
		now := []string{}
		for _, id := range order[slices.Index(order, s.id)+1:] {
			if len(now) == placeAnchors {
				break
			}
			if !later[id] {
				now = append(now, id)
			}
		}
		s.data["was"], s.data["now"] = was[s.id], now
	}
}

// fits reports whether groups gs, with the groups inside them, still fit
// ipc.MaxGroupDepth once in a group depth deep.
func (c *Core) fits(gs []string, depth int) error {
	need := depth
	for _, g := range gs {
		need = max(need, depth+1+c.heightOf(g))
	}
	if need > ipc.MaxGroupDepth {
		return ipc.Errorf(ipc.CodeBadArgs, "groups go %d deep at most", ipc.MaxGroupDepth)
	}
	return nil
}

// renameInHistory gives group id's name to the steps of the history that
// keep it, the groups around them included, so that an undo or redo that
// makes the group again names it as it is named now. The caller holds
// c.mu.
func (c *Core) renameInHistory(id, name string) {
	changed := false
	for _, list := range [][]action{c.steps.hist.Undo, c.steps.hist.Redo} {
		for i := range list {
			a := &list[i]
			if a.Data == nil {
				continue
			}
			for _, k := range []string{"from", "to", "group"} {
				if a.str(k) == id {
					a.Data[k+"_name"] = name
					changed = true
				}
			}
			for _, k := range []string{"from_groups", "to_groups", "group_groups", "nest_groups"} {
				shot, _ := a.Data[k].([]any)
				for _, e := range shot {
					if m, ok := e.(map[string]any); ok && m["id"] == id {
						m["name"] = name
						changed = true
					}
				}
			}
			// A label that names the group names it as it is now.
			if k, ok := actionKinds[a.Kind]; ok && a.Kind == actNest {
				a.Label = k.labelOf(*a)
			}
		}
	}
	if changed {
		c.saveState()
	}
}

// groupOp lists your groups, or makes, fills, empties, renames, moves or
// removes one, and returns them as they then are. New, add, remove,
// ungroup and move are one step of the history, which one undo takes
// back.
func (c *Core) groupOp(a ipc.GroupArgs) (ipc.GroupResult, error) {
	c.mu.Lock()
	res := ipc.GroupResult{}
	var steps []groupStep
	label := ""
	if a.Do != ipc.GroupList {
		c.tidyOrder()
	}
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
			gs, err := c.groupsIn(a.Groups)
			if err != nil {
				return err
			}
			if len(xs) == 0 && len(gs) == 0 {
				return ipc.Errorf(ipc.CodeBadArgs, "a new group needs one of this machine's sessions, or a group")
			}
			if err := c.oneSpace(xs, ""); err != nil {
				return err
			}
			for _, g := range gs {
				if s, ok := c.spaceOfGroup(g); ok && len(xs) > 0 && s != c.spaceOf(xs[0]) {
					return ipc.Errorf(ipc.CodeBadArgs, "%q is in another space: each space has its own groups", c.groupNameOf(g))
				}
			}
			// It goes in the innermost group that holds all of what it
			// holds, as a new folder of files picked in a folder goes in
			// that folder.
			var locs []string
			for _, x := range xs {
				locs = append(locs, c.groupOf(x))
			}
			for _, g := range gs {
				locs = append(locs, c.parentOf(g))
			}
			parent := c.around(locs)
			if err := c.fits(gs, c.depthOf(parent)+1); err != nil {
				return err
			}
			res.Group = c.newGroup(a.Name, parent)
			// It forms where the first of what it holds is (tidyOrder).
			steps = append(c.nest(gs, res.Group, "", ""), c.joinInPlace(xs, res.Group)...)
			if len(gs) > 0 {
				label = fmt.Sprintf("group %q", c.groupNameOf(res.Group))
			}
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
			if at >= 0 {
				res.Group = c.groups[at].ID
			} else {
				// By name or path, among the groups of their space.
				g, err := c.groupFor(a.Name, c.spaceOf(xs[0]))
				if err != nil {
					return err
				}
				if g == "" {
					return ipc.Errorf(ipc.CodeBadArgs, "the group %q is in another space than %q", a.Name, xs[0].name)
				}
				res.Group = g
			}
			if err := c.oneSpace(xs, res.Group); err != nil {
				return err
			}
			steps = c.join(xs, res.Group, a.After, a.Before)
		case ipc.GroupRemove:
			steps = c.leave(c.groupable(a.Sessions), a.After, a.Before)
		case ipc.GroupRename:
			at := c.groupIndex(a.Group)
			if at < 0 {
				return ipc.Errorf(ipc.CodeNotFound, "no group %q", a.Group)
			}
			if strings.TrimSpace(a.Name) == "" {
				return ipc.Errorf(ipc.CodeBadArgs, "a group needs a name")
			}
			c.groups[at].Name = c.groupName(a.Name, a.Group, c.groups[at].Parent)
			c.renameInHistory(a.Group, c.groups[at].Name)
		case ipc.GroupUngroup:
			at := c.groupIndex(a.Group)
			if at < 0 {
				return ipc.Errorf(ipc.CodeNotFound, "no group %q", a.Group)
			}
			// What it held goes in the group around it, where it is.
			g := c.groups[at]
			label = fmt.Sprintf("ungroup %q", g.Name)
			for _, id := range c.fullOrder() {
				if x := c.sessions[id]; x != nil && c.groupOf(x) == g.ID {
					d := c.moveData(g.ID, g.Parent)
					c.setGroup(x, g.Parent, event.SourceUser)
					steps = append(steps, groupStep{kind: actUngroup, id: x.id, data: d})
				}
			}
			var kids []string
			for _, k := range c.groups {
				if k.Parent == g.ID {
					kids = append(kids, k.ID)
				}
			}
			if kids, _ = c.groupsIn(kids); len(kids) > 0 {
				steps = append(steps, c.nestInPlace(kids, g.Parent)...)
			}
			c.groups = slices.DeleteFunc(c.groups, func(k ipc.Group) bool { return k.ID == g.ID })
		case ipc.GroupMove:
			xs := c.groupable(a.Sessions)
			gs, err := c.groupsIn(a.Groups)
			if err != nil {
				return err
			}
			// Into by name or path, among the groups of what moves' space
			// first.
			into := a.Into
			if into != "" {
				i := -1
				if len(xs) > 0 {
					i = c.findGroupIn(into, c.spaceOf(xs[0]))
				} else if len(gs) > 0 {
					if sp, ok := c.spaceOfGroup(gs[0]); ok {
						i = c.findGroupIn(into, sp)
					}
				}
				if i < 0 {
					i = c.findGroup(into)
				}
				if i < 0 {
					return ipc.Errorf(ipc.CodeNotFound, "no group %q", into)
				}
				into = c.groups[i].ID
			}
			if len(xs) == 0 && len(gs) == 0 {
				return ipc.Errorf(ipc.CodeBadArgs, "move needs one of this machine's sessions, or a group")
			}
			for _, g := range gs {
				if c.within(into, g) {
					return ipc.Errorf(ipc.CodeBadArgs, "%q can't go inside itself", c.groupNameOf(g))
				}
				if s, ok := c.spaceOfGroup(g); ok && into != "" {
					if t, ok := c.spaceOfGroup(into); ok && s != t {
						return ipc.Errorf(ipc.CodeBadArgs, "%q is in another space: each space has its own groups", c.groupNameOf(into))
					}
				}
			}
			if err := c.fits(gs, c.depthOf(into)); err != nil {
				return err
			}
			if err := c.oneSpace(xs, into); err != nil {
				return err
			}
			res.Group = into
			steps = c.nest(gs, into, a.Before, a.After)
			before, after := a.Before, a.After
			if len(gs) > 0 {
				if b := c.blockOf(gs[len(gs)-1], nil); len(b) > 0 {
					before, after = "", b[len(b)-1]
				}
				label = fmt.Sprintf("move %q", c.groupNameOf(gs[0]))
				if len(gs) > 1 || len(xs) > 0 {
					label = fmt.Sprintf("move %d groups and sessions", len(gs)+len(xs))
				}
			}
			steps = append(steps, c.join(xs, into, after, before)...)
		default:
			return ipc.Errorf(ipc.CodeBadArgs, "unknown group do %q", a.Do)
		}
		return nil
	}()
	if err == nil && a.Do != ipc.GroupList {
		c.tidyOrder()
		c.saveGroups()
		c.log.Info("group", "do", a.Do, "group", cmp.Or(res.Group, a.Group), "steps", len(steps))
	}
	res.Groups = slices.Clone(c.groups)
	res.Sessions = stepIDs(steps)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
	if a.Do == ipc.GroupRename {
		// Its name in the history's labels.
		c.publishHistory()
	}
	taken := make([]takenStep, len(steps))
	for i, s := range steps {
		if label != "" && len(steps) > 1 {
			s.data["batch_label"] = label
		}
		taken[i] = takenStep{kind: s.kind, session: s.id, data: s.data}
	}
	c.recordTaken(taken)
	return res, err
}

// joinInPlace puts the sessions xs in group id where they are, which then
// come together where the first of the group's sessions is, as a new
// group's do: each comes out of Pinned or Parked. It returns each step
// that undo needs. The caller holds c.mu.
func (c *Core) joinInPlace(xs []*session, id string) []groupStep {
	var steps []groupStep
	was := map[string][]string{}
	for _, x := range xs {
		was[x.id] = c.nextOf(x.id)
		d := c.moveData(c.groupOf(x), id)
		if x.track.pinned {
			c.setPinned(x.id, false)
			d["pinned"] = true
		}
		if x.track.parked {
			c.setParked(x.id, false)
			d["parked"] = true
		}
		c.setGroup(x, id, event.SourceUser)
		steps = append(steps, groupStep{kind: actGroup, id: x.id, data: d})
	}
	c.tidyOrder()
	c.notePlaces(steps, was)
	return steps
}

// nestInPlace puts the groups gs in group into where they are, as
// ungroup does with the groups of the group it removes. It returns each
// step that undo needs. The caller holds c.mu.
func (c *Core) nestInPlace(gs []string, into string) []groupStep {
	var steps []groupStep
	for _, g := range gs {
		i := c.groupIndex(g)
		ids := c.blockOf(g, nil)
		anchor := ""
		if len(ids) > 0 {
			anchor = ids[0]
		}
		from := c.groups[i].Parent
		d := map[string]any{"group": g, "group_name": c.groups[i].Name, "from": from, "to": into,
			"from_name": c.groupNameOf(from), "to_name": c.groupNameOf(into),
			"from_groups": shotOf(append(c.chainOf(from), c.groups[i]))}
		c.groups[i].Parent = into
		c.groups[i].Name = c.groupName(c.groups[i].Name, g, into)
		d["to_groups"] = shotOf(append(c.chainOf(into), c.groups[i]))
		if anchor != "" {
			steps = append(steps, groupStep{kind: actNest, id: anchor, data: d})
		}
	}
	return steps
}

// Action kinds of a group op (undo.go): a session that joined a group, as
// new and add put them, and one that left its group, as remove and ungroup
// take them out; and a group that went into another group, or out of one,
// as move and a new group around groups put them. Undo puts it back in the
// group it left, made again should it be gone, and in the place it left;
// redo where the op put it.
const (
	actGroup   = "group"
	actUngroup = "ungroup"
	actNest    = "nest"
)

func init() {
	registerAction(actGroup, actionKind{verb: "group",
		undo: effect{check: checkGroup("from"), do: doGroup("from")},
		redo: effect{check: checkGroup("to"), do: doGroup("to")}})
	registerAction(actUngroup, actionKind{verb: "ungroup",
		undo: effect{check: checkGroup("from"), do: doGroup("from")},
		redo: effect{check: checkGroup("to"), do: doGroup("to")}})
	registerAction(actNest, actionKind{verb: "move", label: nestLabel,
		undo: effect{check: checkNest, do: doNest("from")},
		redo: effect{check: checkNest, do: doNest("to")}})
}

// nestLabel is what the header shows for a group's step: `move "Irie"
// into "Work"`, or `move "Irie" to the top`.
func nestLabel(a action) string {
	if a.str("to") == "" {
		return fmt.Sprintf("move %q to the top", a.str("group_name"))
	}
	return fmt.Sprintf("move %q into %q", a.str("group_name"), a.str("to_name"))
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
		back := k == "from"
		if g := a.str(k); g != "" && c.groupIndex(g) >= 0 && !c.groupIn(g, c.spaceOf(x), map[string]bool{x.id: true}) {
			return []problem{{code: "space", question: fmt.Sprintf("The group %q is in another space than %q now.", c.groupNameOf(g), x.name), recommend: ipc.ChoiceSkip}}
		}
		if c.groupOf(x) == a.str(k) && (!a.flag("pinned") || x.track.pinned == back) && (!a.flag("parked") || x.track.parked == back) {
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
// with the groups around it should they have gone, and in its place then:
// before the step (from) or after it (to). A session that joining unpinned
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
		c.tidyOrder()
		g := a.str(k)
		if shot := a.shot(k + "_groups"); len(shot) > 0 {
			c.restoreGroups(shot)
		} else {
			c.ensureGroup(g, a.str(k+"_name"))
		}
		back := k == "from"
		if a.flag("pinned") {
			c.setPinned(x.id, back)
		}
		if a.flag("parked") {
			c.setParked(x.id, back)
		}
		c.setGroup(x, g, event.SourceUser)
		place := "now"
		if back {
			place = "was"
		}
		if _, moved := a.Data[place]; moved {
			c.putBack(x.id, a.strs(place))
		}
		c.tidyOrder()
		c.saveGroups()
		a.Name = x.name
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		c.publishHeader()
		return a, nil
	}
}

// checkNest is what makes moving a's group back or again uncertain: that
// the group is gone, with every session it held.
func checkNest(c *Core, a action) []problem {
	if c.groupIndex(a.str("group")) < 0 {
		return []problem{{code: "gone", question: fmt.Sprintf("The group %q is gone, so there is nothing to move.", a.str("group_name")), recommend: ipc.ChoiceSkip}}
	}
	return nil
}

// doNest puts a's group in the group its data keeps as k, made again with
// the groups around it should they have gone: as before the step (from),
// its sessions back in the place they left, or as after it (to), where the
// step put them.
func doNest(k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		g := a.str("group")
		if c.groupIndex(g) < 0 {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no group %q", a.str("group_name"))
		}
		c.tidyOrder()
		c.restoreGroups(a.shot(k + "_groups"))
		ids := c.blockOf(g, nil)
		_, placed := a.Data["was"]
		switch {
		case k == "from" && placed:
			c.putBlock(ids, a.strs("was"))
		case k == "to" && placed:
			c.putIn(ids, a.str("to"), a.str("before"), a.str("after"))
		}
		c.tidyOrder()
		c.saveGroups()
		if x := c.sessions[a.Session]; x != nil {
			a.Name = x.name
		}
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		c.publishHeader()
		return a, nil
	}
}

// leaveForSpace takes x, just moved to space to, out of its group, unless
// the group goes whole, every session in it or in a group inside it moved
// with x or there already: each space has its own groups. The outermost
// group around x that goes whole comes out of a group around it that
// stays, to the top of the list in its new space. It returns the group x
// left, "" when it stays in it or has none, and the group that came out of
// the one around it, as it was before; moved are the sessions that move
// with x. The caller holds c.mu.
func (c *Core) leaveForSpace(x *session, to string, moved map[string]bool) (string, ipc.Group) {
	g := c.groupOf(x)
	if g == "" {
		return "", ipc.Group{}
	}
	whole := func(a string) bool {
		for _, y := range c.sessions {
			if y != x && !moved[y.id] && c.within(c.groupOf(y), a) && c.spaceOf(y) != to {
				return false
			}
		}
		return true
	}
	if !whole(g) {
		c.setGroup(x, "", event.SourceUser)
		return g, ipc.Group{}
	}
	top := g
	for p := c.parentOf(top); p != "" && whole(p); p = c.parentOf(top) {
		top = p
	}
	i := c.groupIndex(top)
	if c.groups[i].Parent == "" {
		return "", ipc.Group{}
	}
	out := c.groups[i]
	c.groups[i].Parent = ""
	c.groups[i].Name = c.groupName(out.Name, top, "")
	return "", out
}

// placeInGroup puts session id, just spawned by a, in its group: a.Group,
// a group's id, name or path among those of its space, made when no group
// has it (groupFor); else the group of the session whose agent started
// it, or for a jot that ui.new_tabs puts beside the card selected, that
// card's group, while that session is in the new one's space. It goes
// last in the group, until placeNew puts it elsewhere in it. It records no
// step: a new session is not a move of yours. The caller holds c.mu.
func (c *Core) placeInGroup(id string, a ipc.SpawnArgs) {
	x := c.sessions[id]
	if x == nil {
		return
	}
	g, space := "", c.spaceOf(x)
	switch {
	case a.Group != "":
		var err error
		if g, err = c.groupFor(a.Group, space); err != nil {
			c.log.Warn("spawn: group", "group", a.Group, "err", err)
		}
	case !a.Jot || c.cfg().UI.NewTabs == "next":
		if from := c.sessions[cmp.Or(a.After, a.Parent, a.Continues)]; from != nil && from != x && c.spaceOf(from) == space {
			g = c.groupOf(from)
		}
	}
	if g == "" {
		return
	}
	c.record(x, event.Event{Source: event.SourceIrie, Kind: event.UIGrouped, Data: map[string]any{"group": g}})
	c.saveGroups()
	c.setOrder(c.fullOrder())
}
