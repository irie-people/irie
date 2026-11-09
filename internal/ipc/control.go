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
	// OpPark puts a session last in the Parked group at the end of the
	// list; OpUnpark puts it last among the rest, just above Parked.
	OpPark   = "park"
	OpUnpark = "unpark"
	// OpPin puts a session last in the Pinned group at the top of the list,
	// where no close but your own of it reaches it; OpUnpin puts it first
	// among the rest, just under Pinned.
	OpPin   = "pin"
	OpUnpin = "unpin"
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

// ParkArgs name the session to park or unpark, or to pin or unpin. It goes
// last in the group it joins, Parked or Pinned or the rest (first among the
// rest for an unpin), or with Before just before that session, where a drag
// across Parked's or Pinned's edge drops it.
type ParkArgs struct {
	Session string `json:"session"`
	Before  string `json:"before,omitempty"`
}
