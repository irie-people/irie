package claude

import "strconv"

// minAutoCompactWindow is the smallest auto-compact window Claude Code
// takes (CLAUDE_CODE_AUTO_COMPACT_WINDOW).
const minAutoCompactWindow = 100000

// LocalEnv points Claude Code at model on the Ollama server at url, the way
// Ollama's own launcher does (DESIGN §6.1). Every model alias, the
// subagents and the background tasks run on that model, so no request asks
// it for a Claude model it does not have; the token is a placeholder and an
// API key in the user's environment is blanked, so nothing goes to
// Anthropic. CLAUDE_CODE_MAX_CONTEXT_TOKENS tells Claude Code the context
// the server gives, so it compacts before the server would cut the prompt;
// for a model ID it does not recognise it would assume 200K. Its reply cap,
// CLAUDE_CODE_MAX_OUTPUT_TOKENS, is maxOutput's. context 0 means unknown.
func LocalEnv(model, url string, context int) map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       url,
		"ANTHROPIC_AUTH_TOKEN":                     "ollama",
		"ANTHROPIC_API_KEY":                        "",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           model,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            model,
		"ANTHROPIC_DEFAULT_FABLE_MODEL":            model,
		"CLAUDE_CODE_SUBAGENT_MODEL":               model,
		"CLAUDE_CODE_AUTO_MODE_SERVER":             "0",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	if context > 0 {
		env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(context)
		if n := maxOutput(context); n > 0 {
			env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = strconv.Itoa(n)
		}
		if context >= minAutoCompactWindow {
			env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = strconv.Itoa(context)
		}
	}
	return env
}

// keptFree is the room Claude Code keeps free besides a reply before it
// compacts, and replyCounted the most of a reply it counts: it keeps
// min(reply cap, 20000) + 13000 tokens free (DESIGN §6.1, measured on
// Claude Code 2.1.330). defaultReply is its reply cap for a model ID it
// does not know (its env-vars page).
const (
	keptFree     = 13000
	replyCounted = 20000
	defaultReply = 32000
)

// maxOutput is the most a local model may write in one reply at the
// context its server gives, or 0 for Claude Code's own default. For a model
// ID it does not know, that default is 32,000 tokens, so at a 64k context
// Claude Code keeps half the window free for one reply, and a session in a
// worktree of Irie, which starts at about 24k, has about 8k left to work in
// before it compacts. A lower cap leaves more room, but cuts a long reply
// short (a large Write, or long thinking), and Claude Code then asks the
// model to go on where it stopped.
func maxOutput(context int) int {
	// An eighth of the window: 8,192 at min_context's 64k, so Claude Code
	// keeps 21k free rather than 33k, and its own default from 256k on,
	// where that is no more than an eighth. Never below 4,096, room for a
	// reply's thinking and a tool call.
	n := max(context/8, 4096)
	if n >= defaultReply {
		return 0
	}
	return n
}
