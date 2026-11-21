package config

import "strings"

// Materialize is how Irie makes materialized cards: cards a local model
// makes once, which Irie keeps and makes again only when they are out of
// date (docs/proposals/materialized-cards.md).
type Materialize struct {
	// Model is the local model that makes cards, such as "bonsai-2:9b",
	// from Ollama on this computer; "" = the best Ollama has for chat that
	// fits, at most 16B parameters and 10 GB. A card is small, so a small
	// model makes it well and leaves the memory to your own work.
	Model string `toml:"model"`
}

func checkMaterialize(m Materialize, bad func(string, ...any)) {
	if strings.ContainsAny(m.Model, " \t\n\"'") || len(m.Model) > 200 {
		bad("materialize.model = %q: must be a model's name as Ollama lists it, such as bonsai-2:9b, or empty for the best it has", m.Model)
	}
}
