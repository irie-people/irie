package ipc

// Your groups (DESIGN §4.1): named sets of sessions that a sidebar draws
// together under a header that folds them, like a browser's tab groups. A
// group is drawn where the first of its sessions is in the list's one
// order, with the rest of its sessions after it. A row says its group as
// Group; the header lists the groups.
const OpGroup = "group"

// What OpGroup does, GroupArgs' Do.
const (
	GroupList    = ""
	GroupNew     = "new"     // a new group of Sessions, named Name
	GroupAdd     = "add"     // Sessions into Group, or the group named Name
	GroupRemove  = "remove"  // Sessions out of their groups
	GroupRename  = "rename"  // Group named Name
	GroupUngroup = "ungroup" // Group goes; its sessions stay where they are
)

// Group is one of your groups: its id, and its name, which no other group
// has in any case.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GroupArgs say what to do with your groups. New makes a group named Name
// (a name of its own when empty) of Sessions, where the first of them is
// in the list, with the others just after it. Add puts Sessions in Group,
// or in the group named Name, made when no group has that name: last in
// it, or where a drag dropped them, just before Before or just after
// After. Remove takes Sessions out of their groups, just after the rest of
// the group, or just before Before or after After. Rename names Group
// Name, and ungroup removes Group, whose sessions stay where they are. A
// session that joins a group comes out of Pinned or Parked. New, add,
// remove and ungroup are steps of the history, which one undo takes back;
// another machine's sessions stay out of it.
type GroupArgs struct {
	Do       string   `json:"do,omitempty"`
	Group    string   `json:"group,omitempty"`
	Name     string   `json:"name,omitempty"`
	Sessions []string `json:"sessions,omitempty"`
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
