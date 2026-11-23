package terms

import (
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/config"
)

func TestNoServerOnThisComputerWhileModelsAreOffHere(t *testing.T) {
	cfg := config.Default()
	if got := Servers(cfg); !slices.Equal(got, []string{"http://127.0.0.1:11435", "http://127.0.0.1:11434"}) {
		t.Errorf("servers = %v; want Irie's own, then Ollama's usual port", got)
	}
	cfg.Local.Here = false
	if got := Servers(cfg); len(got) != 0 {
		t.Errorf("with local.here off, servers = %v; want none", got)
	}
	cfg.Local.URL = "http://gpu.example.com:11434"
	if got := Servers(cfg); !slices.Equal(got, []string{cfg.Local.URL}) {
		t.Errorf("with local.here off and a server elsewhere, servers = %v; want it", got)
	}
}
