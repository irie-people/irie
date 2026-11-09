package ipc

// Spaces (DESIGN §4.1): named sets of sessions that a sidebar shows one at
// a time, each with its own Pinned and Parked, like a browser's spaces.
// Every session is in one: the first, unless it was put in another. A row
// says its space as Space, "" for the first.
const (
	// OpSpaces lists the spaces, and makes, renames, moves or removes one.
	OpSpaces = "spaces"
	// OpSpace puts sessions in a space, as one step of the history.
	OpSpace = "space"
)

// What OpSpaces does, SpacesArgs' Do.
const (
	SpacesList   = ""
	SpacesNew    = "new"
	SpacesRename = "rename"
	SpacesMove   = "move"
	SpacesRemove = "remove"
)

// Space is one space: its id, and its name, which no other space has in
// any case.
type Space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SpacesArgs say what to do with the spaces. New makes a space named Name
// (a name of its own when empty), last; rename names Space Name; move puts
// Space just before Before, or last; remove removes Space, whose sessions
// go to the first space. The last space never goes.
type SpacesArgs struct {
	Do     string `json:"do,omitempty"`
	Space  string `json:"space,omitempty"`
	Name   string `json:"name,omitempty"`
	Before string `json:"before,omitempty"`
}

// SpacesResult is the spaces in their order, and the space new made.
type SpacesResult struct {
	Spaces []Space `json:"spaces"`
	Space  string  `json:"space,omitempty"`
}

// SpaceArgs put Sessions in Space ("" for the first).
type SpaceArgs struct {
	Sessions []string `json:"sessions"`
	Space    string   `json:"space"`
}

// SpaceResult lists the sessions a space op moved.
type SpaceResult struct {
	Sessions []string `json:"sessions"`
}
