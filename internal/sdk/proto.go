package sdk

import (
	"encoding/json"
	"strings"
)

// Flags are what Run adds to the command line it is given: the Agent SDK's
// CLI with JSON lines in and out, each reply streamed as it comes, and
// permission prompts sent to this program to answer (DESIGN §3.5).
var Flags = []string{
	"-p", "--input-format", "stream-json", "--output-format", "stream-json",
	"--verbose", "--include-partial-messages", "--permission-prompt-tool", "stdio",
}

// userLine is a message from the user, as the CLI reads one.
func userLine(text string) []byte {
	return jsonLine(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": text},
		"parent_tool_use_id": nil,
		"session_id":         "default",
	})
}

// interruptLine asks the CLI to stop the turn it is in.
func interruptLine(id string) []byte {
	return jsonLine(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "interrupt"}})
}

// allowLine answers a permission prompt with yes, for the input it asked
// about.
func allowLine(id string, input json.RawMessage) []byte {
	if len(input) == 0 || string(input) == "null" {
		input = json.RawMessage("{}")
	}
	return answerLine(id, map[string]any{"behavior": "allow", "updatedInput": input})
}

// denyLine answers a permission prompt with no. With stop, the turn ends
// there and the agent waits for the user.
func denyLine(id, why string, stop bool) []byte {
	r := map[string]any{"behavior": "deny", "message": why}
	if stop {
		r["interrupt"] = true
	}
	return answerLine(id, r)
}

func answerLine(id string, response map[string]any) []byte {
	return jsonLine(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": id, "response": response,
	}})
}

// refuseLine answers a control request this program does not take.
func refuseLine(id, why string) []byte {
	return jsonLine(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "error", "request_id": id, "error": why,
	}})
}

func jsonLine(v any) []byte {
	b, _ := json.Marshal(v)
	return append(b, '\n')
}

// message is one line the CLI writes, with the fields Irie reads.
type message struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Request   *request        `json:"request"`  // control_request
	Response  *response       `json:"response"` // control_response
	Event     *streamEvent    `json:"event"`    // stream_event
	Message   *apiMessage     `json:"message"`  // assistant, user
	Parent    *string         `json:"parent_tool_use_id"`
	Error     json.RawMessage `json:"error"` // system api_retry: its category

	// system init
	Model          string `json:"model"`
	PermissionMode string `json:"permissionMode"`
	// system api_retry
	Attempt    int `json:"attempt"`
	MaxRetries int `json:"max_retries"`
	// system status
	Status *string `json:"status"`

	// result
	IsError    bool              `json:"is_error"`
	Result     string            `json:"result"`
	Errors     []json.RawMessage `json:"errors"`
	CostUSD    *float64          `json:"total_cost_usd"`
	ModelUsage map[string]struct {
		ContextWindow int `json:"contextWindow"`
	} `json:"modelUsage"`
}

// sub reports whether m belongs to a subagent's run rather than the main
// conversation.
func (m message) sub() bool { return m.Parent != nil && *m.Parent != "" }

type request struct {
	Subtype     string          `json:"subtype"`
	ToolName    string          `json:"tool_name"`
	DisplayName string          `json:"display_name"`
	Input       json.RawMessage `json:"input"`
	Description string          `json:"description"`
}

type response struct {
	Subtype string `json:"subtype"`
	Error   string `json:"error"`
}

type streamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string `json:"id"`
		Usage *usage `json:"usage"`
	} `json:"message"` // message_start
	ContentBlock *struct {
		Type string `json:"type"`
	} `json:"content_block"` // content_block_start
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"` // content_block_delta
}

type usage struct {
	Input         int `json:"input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
}

type apiMessage struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"` // a string, or blocks
}

// block is one block of a message's content.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`          // tool_use
	Name      string          `json:"name"`        // tool_use
	Input     json.RawMessage `json:"input"`       // tool_use
	ToolUseID string          `json:"tool_use_id"` // tool_result
	Content   json.RawMessage `json:"content"`     // tool_result: a string, or blocks
	IsError   bool            `json:"is_error"`    // tool_result
}

// blocks reads a message's content: blocks, or a string as one text block.
func blocks(raw json.RawMessage) []block {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	json.Unmarshal(raw, &bs)
	return bs
}

// plain is the text of a tool result's content.
func plain(raw json.RawMessage) string {
	var out string
	for _, b := range blocks(raw) {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// errorText is the text of a result that failed: its errors, then its
// result, then its subtype, the order the Agent SDK's own clients read
// them in.
func (m message) errorText() string {
	var parts []string
	for _, e := range m.Errors {
		var s string
		var o struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		switch {
		case json.Unmarshal(e, &s) == nil && s != "":
			parts = append(parts, s)
		case json.Unmarshal(e, &o) == nil && (o.Message != "" || o.Error != ""):
			parts = append(parts, o.Message+o.Error)
		}
	}
	switch {
	case len(parts) > 0:
		return strings.Join(parts, "; ")
	case m.Result != "":
		return m.Result
	case m.Subtype != "" && m.Subtype != "success":
		return m.Subtype
	}
	return "the turn failed"
}
