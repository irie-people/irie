package harness

import (
	"regexp"
	"testing"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUIDIsVersion4(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		u := NewUUID()
		if !uuidV4.MatchString(u) {
			t.Fatalf("%q is not a v4 UUID", u)
		}
		if seen[u] {
			t.Fatalf("%q repeated", u)
		}
		seen[u] = true
	}
}
