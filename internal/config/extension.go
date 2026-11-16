package config

import "strings"

// The browser extension plugin's settings (DESIGN §4.17,
// docs/proposals/browser-extension.md) live here, so the config needs none
// of the plugin's packages: a build without it (go build -tags
// noextension) still reads a config.toml that sets them.

// Extension is the browser extension plugin: Irie in your own browser.
type Extension struct {
	// Model is the local model that words Terms in short's answers when
	// you ask for one, such as "qwen3.5:9b", from Ollama on this computer;
	// "" = the best Ollama has that isn't for embeddings. Irie's own code
	// answers otherwise, and always finds the passages a model words.
	Model string `toml:"model"`
}

func checkExtension(e Extension, bad func(string, ...any)) {
	if strings.ContainsAny(e.Model, " \t\n\"'") || len(e.Model) > 200 {
		bad("extension.model = %q: must be a model's name as Ollama lists it, such as qwen3.5:9b, or empty for the best it has", e.Model)
	}
}
