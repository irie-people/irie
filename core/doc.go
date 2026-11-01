// Package core is irie-core: all of Irie, for any client.
//
// Irie's core is one process, `irie core`, which owns every agent, all the
// state and every rule (DESIGN §2.1). A client, such as the Mac app, the
// terminal sidebar, `irie ctl` or one you write, draws what the core says
// and passes on what the person does, over one unix socket (DESIGN §2.4).
// It keeps no rules of its own: what the words in a task bar mean, which
// session a question is about, what a step is called and how it is undone
// are all the core's. So a new client has every feature on its first day,
// and a feature added to the core reaches every client at once.
//
// This package is that contract for Go: the wire types and op names, where
// the socket is, a client, and the language a person types, which the core
// reads with Read and words with Said. Clients in other languages speak
// the same NDJSON; docs/CORE.md describes it for them.
//
// A client does three things:
//
//   - It subscribes, and draws the rows, header, history and the sessions
//     outside Irie from the frames, as they come.
//   - It sends what the person types to the input op, as typed, with the
//     session they had selected, and shows the reply's Text as it is.
//   - It sends its gestures (a click, a drag, a key) to the ops they name:
//     show, park, freeze, priority, reorder, close, undo and the rest.
//
// For example, to ask how an agent is doing:
//
//	sock, err := core.Socket()
//	if err != nil {
//		return err
//	}
//	var r core.InputResult
//	err = core.Call(sock, 2*time.Second, core.OpInput, core.InputArgs{Text: "hows the parser?"}, &r)
//	if err != nil {
//		return err // an *Error whose message is the person's to read
//	}
//	fmt.Println(r.Text)
package core

//go:generate go test -run TestWireCoversTheProtocol -update .
