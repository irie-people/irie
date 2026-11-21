package config

import (
	"strings"
	"testing"
)

func TestMaterializeNamesTheModelThatMakesCards(t *testing.T) {
	if c := Default(); c.Materialize.Model != "" {
		t.Errorf("materialize.model defaults to %q; want the best Ollama has", c.Materialize.Model)
	}
	c, err := Parse([]byte("[materialize]\nmodel = \"bonsai-2:9b\"\n"), "config.toml")
	if err != nil || c.Materialize.Model != "bonsai-2:9b" {
		t.Fatalf("model %q, %v", c.Materialize.Model, err)
	}
	if _, err := Parse([]byte("[materialize]\nmodel = \"bonsai 2\"\n"), "config.toml"); err == nil || !strings.Contains(err.Error(), "materialize.model") {
		t.Errorf("a name with a space: %v; want it refused, naming the key", err)
	}
}
