package config

import (
	"os"
	"testing"
)

func TestLocalModelsAreOffHereOnlyByChoice(t *testing.T) {
	if !Default().Local.Here {
		t.Fatal("local.here is false by default; want true, so a new install loads its models as before")
	}
	name, _ := os.Hostname()
	on := Local{Here: true}
	off := Local{Here: false}
	cases := map[string]bool{
		"":                             true, // Irie's own servers
		"http://127.0.0.1:11434":       true,
		"http://localhost:11435":       true,
		"http://[::1]:11434":           true,
		"http://0.0.0.0:11434":         true,
		"http://gpu.example.com:11434": false,
		"http://192.0.2.7:11434":       false,
		"http://%zz":                   true, // does not parse
	}
	if name != "" {
		cases["http://"+name+":11434"] = true
	}
	for raw, here := range cases {
		if got := OnThisComputer(raw); raw != "" && got != here {
			t.Errorf("OnThisComputer(%q) = %v, want %v", raw, got, here)
		}
		if !on.LoadsAt(raw) {
			t.Errorf("with local.here on, LoadsAt(%q) = false; want every server", raw)
		}
		if got := off.LoadsAt(raw); got == here {
			t.Errorf("with local.here off, LoadsAt(%q) = %v; want %v", raw, got, !here)
		}
	}
	c, err := Parse([]byte("[local]\nhere = false\n"), "config.toml")
	if err != nil || c.Local.Here {
		t.Errorf("here = false read as %v, %v", c.Local.Here, err)
	}
}
