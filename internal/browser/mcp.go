package browser

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
)

// An MCP server: JSON-RPC 2.0 over stdio, one message per line (MCP
// 2025-11-25, "Transports"), with the initialize handshake of the revisions
// up to 2025-11-25 and the tools of "Server features: Tools". Claude Code's
// newer MCP runtime first asks a stdio server for revision 2026-07-28 with
// server/discover; an error that revision does not define, such as method
// not found, keeps it on the handshake, which both of its runtimes speak.

// protocolVersions are the revisions this server speaks, newest first.
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC error codes (JSON-RPC 2.0 §5.1).
const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
	call        func(ctx context.Context, args json.RawMessage) toolResult
}

type content struct {
	Type     string `json:"type"` // text | image
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// toolResult is a tool's answer. A failure the agent can act on is a
// result with isError, not a JSON-RPC error.
type toolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

func textResult(s string) toolResult {
	return toolResult{Content: []content{{Type: "text", Text: s}}}
}

func errorResult(format string, args ...any) toolResult {
	r := textResult(fmt.Sprintf(format, args...))
	r.IsError = true
	return r
}

type server struct {
	name, version, instructions string
	tools                       []tool

	out io.Writer
	wmu sync.Mutex // one message at a time
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// serve answers the messages on in until it ends or ctx is done. Tool calls
// run concurrently, so a ping is answered during a slow page; serve returns
// once every call has answered.
func (s *server) serve(ctx context.Context, in io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var calls sync.WaitGroup
	defer calls.Wait()
	lines := make(chan []byte)
	var readErr error
	go func() {
		defer close(lines)
		r := bufio.NewReader(in)
		for {
			line, err := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					readErr = err
				}
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-lines:
			if !ok {
				return readErr
			}
			s.handle(ctx, line, &calls)
		}
	}
}

func (s *server) handle(ctx context.Context, line []byte, calls *sync.WaitGroup) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(json.RawMessage("null"), nil, &rpcError{codeParse, "parse error: " + err.Error()})
		return
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return // a notification, such as notifications/initialized: nothing to answer
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		v := protocolVersions[0]
		if slices.Contains(protocolVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		s.reply(req.ID, map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
			"instructions":    s.instructions,
		}, nil)
	case "ping":
		s.reply(req.ID, struct{}{}, nil)
	case "tools/list":
		s.reply(req.ID, map[string]any{"tools": s.tools}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.reply(req.ID, nil, &rpcError{codeInvalidParams, "invalid params: " + err.Error()})
			return
		}
		i := slices.IndexFunc(s.tools, func(t tool) bool { return t.Name == p.Name })
		if i < 0 {
			s.reply(req.ID, nil, &rpcError{codeInvalidParams, "unknown tool: " + p.Name})
			return
		}
		t := s.tools[i]
		calls.Add(1)
		go func() {
			defer calls.Done()
			s.reply(req.ID, run(ctx, t, p.Arguments), nil)
		}()
	default:
		s.reply(req.ID, nil, &rpcError{codeMethodNotFound, "method not found: " + req.Method})
	}
}

// run calls a tool, turning a panic into an error result.
func run(ctx context.Context, t tool, args json.RawMessage) (r toolResult) {
	defer func() {
		if p := recover(); p != nil {
			r = errorResult("%s failed: %v", t.Name, p)
		}
	}()
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return t.call(ctx, args)
}

func (s *server) reply(id json.RawMessage, result any, e *rpcError) {
	b, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: e})
	if err != nil {
		b, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{-32603, err.Error()}})
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.out.Write(append(b, '\n'))
}
