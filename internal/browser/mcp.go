package browser

import "github.com/irie-people/irie/internal/mcp"

// The browser's MCP server is internal/mcp's, which Irie's other servers
// share; these are its names here.
type (
	tool       = mcp.Tool
	content    = mcp.Content
	toolResult = mcp.Result
)

func textResult(s string) toolResult { return mcp.Text(s) }

func errorResult(format string, args ...any) toolResult { return mcp.Errorf(format, args...) }
