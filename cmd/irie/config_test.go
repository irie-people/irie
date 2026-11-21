package main

import (
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
)

// TestAConfigValueReachesTheCoreAsItReadsIt: `config set` hands the running
// core each value as JSON, and the core's config op reads a duration only
// as text, such as "30s", never as json.Marshal's nanoseconds.
func TestAConfigValueReachesTheCoreAsItReadsIt(t *testing.T) {
	for _, c := range []struct {
		key, text string
		want      any
	}{
		{"arch.raguel.every", "168h", 168 * time.Hour},
		{"spawn.close_delay", "45s", 45 * time.Second},
		{"arch.raguel.on", "false", false},
		{"arch.raguel.folder", "~/Documents/Council", "~/Documents/Council"},
	} {
		v, err := config.ParseText(c.key, c.text)
		if err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		raw, err := configValue(v)
		if err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if got, err := config.ParseValue(c.key, raw); err != nil || got != c.want {
			t.Errorf("%s %s: the core reads %s as %v, %v; want %v", c.key, c.text, raw, got, err, c.want)
		}
	}
}
