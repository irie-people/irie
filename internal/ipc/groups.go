package ipc

import (
	"slices"
	"strings"
)

// Your groups (DESIGN §4.1): named sets of sessions that a sidebar draws
// together under a header that folds them, like a browser's tab groups,
// and that hold other groups as folders hold folders. A group is drawn
// where the first of its sessions is in the list's one order, its groups'
// sessions included, with the rest of them after it. A row says its group
// as Group, the innermost it is in; the header lists the groups, each with
// the group it is in as Parent.
const OpGroup = "group"

// What OpGroup does, GroupArgs' Do.
const (
	GroupList    = ""
	GroupNew     = "new"     // a new group of Sessions and Groups, named Name
	GroupAdd     = "add"     // Sessions into Group, or the group named Name
	GroupRemove  = "remove"  // Sessions out of their groups, into the groups around them
	GroupRename  = "rename"  // Group named Name
	GroupUngroup = "ungroup" // Group goes; what it held goes in the group around it, where it is
	GroupMove    = "move"    // Sessions and Groups into Into, or the top of the list
)

// MaxGroupDepth is how many groups deep a session may be: a group at the
// top of the list is one deep.
const MaxGroupDepth = 8

// Group is one of your groups: its id, its name, which no other group in
// the same group has in any case, and the group it is in, "" for none.
type Group struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
}

// GroupArgs say what to do with your groups. New makes a group named Name
// (a name of its own when empty) of Sessions and of Groups, inside the
// innermost group that holds all of them, where the first of them is in
// the list, with the others just after it. Add puts Sessions in Group, or
// in the group named Name (FindGroup), made when no group has that name,
// and a path such as "Work/Irie" makes each group of it that is missing:
// last in it, or where a drag dropped them, just before Before or just
// after After. Remove takes Sessions out of their groups, each into the
// group around its group (the top of the list for a group there), just
// after the group it left, or just before Before or after After. Rename
// names Group Name, and ungroup removes Group, whose sessions and groups go
// in the group around it, staying where they are. Move puts Sessions and
// Groups in group Into, "" for the top of the list: last in it, or just
// before what holds session Before, or after what holds After, so a group
// dropped on a card goes in that card's place. A session that joins a
// group comes out of Pinned or Parked. New, add, remove, ungroup and move
// are steps of the history, which one undo takes back; another machine's
// sessions stay out of it.
type GroupArgs struct {
	Do       string   `json:"do,omitempty"`
	Group    string   `json:"group,omitempty"`
	Name     string   `json:"name,omitempty"`
	Sessions []string `json:"sessions,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Into     string   `json:"into,omitempty"`
	Before   string   `json:"before,omitempty"`
	After    string   `json:"after,omitempty"`
}

// GroupResult is your groups as they then are, the group new or add put
// sessions in, and the sessions the op moved.
type GroupResult struct {
	Groups   []Group  `json:"groups"`
	Group    string   `json:"group,omitempty"`
	Sessions []string `json:"sessions,omitempty"`
}

// GroupPath is group id and the groups around it, the outermost first;
// none when groups has no group id.
func GroupPath(groups []Group, id string) []Group {
	var out []Group
	for range len(groups) {
		i := slices.IndexFunc(groups, func(g Group) bool { return g.ID == id })
		if id == "" || i < 0 || slices.ContainsFunc(out, func(g Group) bool { return g.ID == id }) {
			break
		}
		out = append(out, groups[i])
		id = groups[i].Parent
	}
	slices.Reverse(out)
	return out
}

// GroupTitle is group id's path of names, the outermost first, joined by
// sep, as "Work / Irie".
func GroupTitle(groups []Group, id, sep string) string {
	var names []string
	for _, g := range GroupPath(groups, id) {
		names = append(names, g.Name)
	}
	return strings.Join(names, sep)
}

// GroupWords are the names a path such as "Work / Irie" holds, each on
// one line with its spaces as one, the empty ones left out.
func GroupWords(path string) []string {
	var out []string
	for _, w := range strings.Split(path, "/") {
		if w = strings.Join(strings.Fields(w), " "); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// FindGroup is where among groups the group that ref names is: by its id;
// by its name, in any case, the one nearest the top of the list first,
// then the first made; or by its path of names from the top, as
// "Work/Irie". -1 when none has it.
func FindGroup(groups []Group, ref string) int {
	ref = strings.Join(strings.Fields(ref), " ")
	if ref == "" {
		return -1
	}
	if i := slices.IndexFunc(groups, func(g Group) bool { return g.ID == ref }); i >= 0 {
		return i
	}
	best, depth := -1, 0
	for i, g := range groups {
		if strings.EqualFold(g.Name, ref) {
			if d := len(GroupPath(groups, g.ID)); best < 0 || d < depth {
				best, depth = i, d
			}
		}
	}
	if best >= 0 {
		return best
	}
	words := GroupWords(ref)
	if len(words) < 2 {
		return -1
	}
	at, parent := -1, ""
	for _, w := range words {
		at = slices.IndexFunc(groups, func(g Group) bool { return g.Parent == parent && strings.EqualFold(g.Name, w) })
		if at < 0 {
			return -1
		}
		parent = groups[at].ID
	}
	return at
}
