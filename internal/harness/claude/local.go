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
// for a model ID it does not recognise it would assume 200K. context 0 means
// unknown.
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
		if context >= minAutoCompactWindow {
			env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = strconv.Itoa(context)
		}
	}
	return env
}
