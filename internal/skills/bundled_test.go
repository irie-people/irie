//go:build !noprivacy && !nowriting

package skills

import (
	"os"
	"slices"
	"testing"
)

// TestEveryFolderIsBundled: each folder of bundled/ is embedded, by
// skills.go for every build or by its plugin's file, so a new skill is
// never left out by mistake.
func TestEveryFolderIsBundled(t *testing.T) {
	entries, err := os.ReadDir("bundled")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range Bundled() {
		names = append(names, s.Name)
	}
	for _, e := range entries {
		if e.IsDir() && !slices.Contains(names, e.Name()) {
			t.Errorf("bundled/%s is not embedded: add it to skills.go's //go:embed, or to its plugin's file", e.Name())
		}
	}
}
