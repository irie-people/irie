// Package mcp is the MCP server Irie's own servers share, Irie's browser
// (DESIGN §4.15) and its anonymous research tools (DESIGN §4.16): JSON-RPC
// 2.0 over stdio, one message per line (MCP 2025-11-25, "Transports"), with
// the initialize handshake of the revisions up to 2025-11-25 and the tools
// of "Server features: Tools". Claude Code's newer MCP runtime first asks a
// stdio server for revision 2026-07-28 with server/discover; an error that
// revision does not define, such as method not found, keeps it on the
// handshake, which both of its runtimes speak.
package mcp

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

// ProtocolVersions are the revisions a server speaks, newest first.
var ProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC error codes (JSON-RPC 2.0 §5.1).
const (
	CodeParse          = -32700
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
)

// Tool is one tool of a server: what tools/list says of it, and the call
// that answers it.
type Tool struct {
	Name        string                                                 `json:"name"`
	Title       string                                                 `json:"title,omitempty"`
	Description string                                                 `json:"description"`
	InputSchema map[string]any                                         `json:"inputSchema"`
	Annotations map[string]any                                         `json:"annotations,omitempty"`
	Call        func(ctx context.Context, args json.RawMessage) Result `json:"-"`
}

// Content is one part of a tool's answer.
type Content struct {
	Type     string `json:"type"` // text | image
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Result is a tool's answer. A failure the agent can act on is a result
// with IsError, not a JSON-RPC error.
type Result struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Text is an answer of text.
func Text(s string) Result {
	return Result{Content: []Content{{Type: "text", Text: s}}}
}

// Errorf is a failure, in words the agent reads.
func Errorf(format string, args ...any) Result {
	r := Text(fmt.Sprintf(format, args...))
	r.IsError = true
	return r
}

// Server is an MCP server with its tools.
type Server struct {
	Name, Version, Instructions string
	Tools                       []Tool
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

// Serve answers the messages on in, on out, until in ends or ctx is done.
// Tool calls run concurrently, so a ping is answered during a slow call;
// Serve returns once every call has answered.
func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	c := conn{Server: s, out: out}
	return c.serve(ctx, in)
}

type conn struct {
	Server
	out io.Writer
	wmu sync.Mutex // one message at a time
}

func (s *conn) serve(ctx context.Context, in io.Reader) error {
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

func (s *conn) handle(ctx context.Context, line []byte, calls *sync.WaitGroup) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(json.RawMessage("null"), nil, &rpcError{CodeParse, "parse error: " + err.Error()})
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
		v := ProtocolVersions[0]
		if slices.Contains(ProtocolVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		s.reply(req.ID, map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    s.Instructions,
		}, nil)
	case "ping":
		s.reply(req.ID, struct{}{}, nil)
	case "tools/list":
		tools := s.Tools
		if tools == nil {
			tools = []Tool{}
		}
		s.reply(req.ID, map[string]any{"tools": tools}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.reply(req.ID, nil, &rpcError{CodeInvalidParams, "invalid params: " + err.Error()})
			return
		}
		i := slices.IndexFunc(s.Tools, func(t Tool) bool { return t.Name == p.Name })
		if i < 0 {
			s.reply(req.ID, nil, &rpcError{CodeInvalidParams, "unknown tool: " + p.Name})
			return
		}
		t := s.Tools[i]
		calls.Add(1)
		go func() {
			defer calls.Done()
			s.reply(req.ID, Run(ctx, t, p.Arguments), nil)
		}()
	default:
		s.reply(req.ID, nil, &rpcError{CodeMethodNotFound, "method not found: " + req.Method})
	}
}

// Run calls a tool, turning a panic into an error result.
func Run(ctx context.Context, t Tool, args json.RawMessage) (r Result) {
	defer func() {
		if p := recover(); p != nil {
			r = Errorf("%s failed: %v", t.Name, p)
		}
	}()
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return t.Call(ctx, args)
}

func (s *conn) reply(id json.RawMessage, result any, e *rpcError) {
	b, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: e})
	if err != nil {
		b, _ = json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{-32603, err.Error()}})
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.out.Write(append(b, '\n'))
}
