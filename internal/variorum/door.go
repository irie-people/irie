package variorum

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"syscall"

	"github.com/irie-people/irie/internal/brand"
)

// noSessionsSetup is what a writing session hears when Irie keeps no setup
// of Claude Code's.
const noSessionsSetup = brand.Name + " doesn't have Variorum's Claude Code setup yet: copy it in " + copyWhere + " and press Connect on " + brand.Name + "'s Variorum tab, or run `pbpaste | " + brand.Slug + " variorum connect`."

// Door is `irie variorum door`, a writing session's own MCP server: it
// becomes Variorum's door, the program Claude Code's setup names, with that
// setup's key in its environment and nothing else of the session's, so
// that every byte between the session and Variorum goes straight through.
// It writes nothing first: stdout is the session's channel to the door.
// When it can't, it tells the session why in the door's own way (Refuse)
// and returns 0.
func Door(data string, in io.Reader, out io.Writer) int {
	st, err := Load(data)
	switch {
	case err != nil:
		Refuse(in, out, err.Error())
	case st.Sessions == nil:
		Refuse(in, out, noSessionsSetup)
	default:
		s := *st.Sessions
		if err := Check(s.Command); err != nil {
			Refuse(in, out, err.Error()+" "+copyAgain(Sessions))
			return 0
		}
		err := syscall.Exec(s.Command, []string{s.Command}, Env(s.Key))
		Refuse(in, out, brand.Name+" couldn't start Variorum's door: "+err.Error()+".")
	}
	return 0
}

// The bounds of a client's opening that Refuse reads: as many messages
// before initialize as Variorum's door reads, each line at most 1 MiB.
const (
	openingFrames = 8
	openingLine   = 1 << 20
)

// Refuse answers an MCP client that Irie can't take to Variorum, as
// Variorum's own door answers one when the app is closed: it reads the
// client's opening, answering each request before initialize with "Method
// not found" (a ping with an empty result) and skipping each notification,
// as Claude Code sends server/discover first, and then answers the
// initialize with one JSON-RPC error, code -32000, whose message is why. A
// client that ends first, or overruns the bounds, hears it on its last
// message's id, or null.
func Refuse(in io.Reader, out io.Writer, why string) {
	id := opening(bufio.NewReaderSize(in, 64<<10), out)
	reply(out, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": why}})
}

// opening reads a client's messages up to its initialize, answering those
// before it, and returns the id that the refusal answers.
func opening(r *bufio.Reader, out io.Writer) any {
	for n, blank := 0, 0; ; {
		b, err := readLine(r)
		if err != nil {
			return nil
		}
		if len(bytes.TrimSpace(b)) == 0 {
			if blank++; blank > openingFrames {
				return nil
			}
			continue
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method *string         `json:"method"`
		}
		if json.Unmarshal(b, &m) != nil {
			return nil
		}
		id := echo(m.ID)
		switch {
		case n == openingFrames || m.Method == nil || *m.Method == "initialize":
			return id
		case m.ID == nil:
			// A notification, which JSON-RPC never answers.
		case id == nil:
			// An id no answer could carry: the opening's message after all.
			return nil
		case *m.Method == "ping":
			reply(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		default:
			reply(out, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "Method not found"}})
		}
		n++
	}
}

// echo is an id an answer can carry back, a string or a number, or nil.
func echo(raw json.RawMessage) any {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	switch v.(type) {
	case string:
		return v
	case float64:
		return json.Number(bytes.TrimSpace(raw))
	}
	return nil
}

func reply(out io.Writer, msg map[string]any) {
	b, err := json.Marshal(msg)
	if err == nil {
		out.Write(append(b, '\n'))
	}
}

// readLine reads one line, without its newline, of at most openingLine
// bytes.
func readLine(r *bufio.Reader) ([]byte, error) {
	var b []byte
	for {
		chunk, err := r.ReadSlice('\n')
		b = append(b, chunk...)
		if len(b) > openingLine {
			return nil, bufio.ErrTooLong
		}
		switch {
		case err == nil:
			return bytes.TrimSuffix(b, []byte("\n")), nil
		case err == bufio.ErrBufferFull:
			continue
		case err == io.EOF && len(b) > 0:
			return b, nil
		default:
			return nil, err
		}
	}
}
