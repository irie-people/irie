package ipc

// Ops for what the user does to a session's whole process tree and to its
// place in the list (DESIGN §2.4, §4.3). They are served when hello's
// features name them.
const (
	// OpFreeze stops a session's whole process tree at once, or every
	// session's; OpThaw continues it.
	OpFreeze = "freeze"
	OpThaw   = "thaw"
	// OpPriority promotes a session (level 1) or puts it back (level 0).
	OpPriority = "priority"
	// OpReorder puts a session elsewhere in the list. Move, its near
	// namesake, brings in a session from outside Irie.
	OpReorder = "reorder"
	// OpPark puts a session in the Parked group at the end of the list;
	// OpUnpark brings it back to its place.
	OpPark   = "park"
	OpUnpark = "unpark"
)

// StateFrozen is the state of a frozen session (DESIGN §5.3). It comes
// after failed and exited, and before every other state.
const StateFrozen = "frozen"

// FreezeArgs name the session to freeze or thaw, or All for every one.
type FreezeArgs struct {
	Session string `json:"session,omitempty"`
	All     bool   `json:"all,omitempty"`
}

// FreezeResult lists the sessions that were frozen or thawed.
type FreezeResult struct {
	Sessions []string `json:"sessions"`
}

// PriorityArgs set a session's priority level: 1 promotes it, which puts it
// at the top of the list and gives its process tree the machine first; 0
// puts it back among the rest.
type PriorityArgs struct {
	Session string `json:"session"`
	Level   int    `json:"level"`
	Reason  string `json:"reason,omitempty"`
}

// ReorderArgs put Session just before Before in the list, or last when
// Before is empty.
type ReorderArgs struct {
	Session string `json:"session"`
	Before  string `json:"before,omitempty"`
}

// ParkArgs name the session to park or unpark.
type ParkArgs struct {
	Session string `json:"session"`
}
