package core

import (
	"slices"
	"testing"
)

func TestPluginDirsReadsTheCommandLine(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want []string
	}{
		{[]string{"claude", "--settings", "s.json", "--plugin-dir", "/a", "--session-id", "x", "task"}, []string{"/a"}},
		{[]string{"claude", "--plugin-dir", "/a", "--plugin-dir", "/b"}, []string{"/a", "/b"}},
		{[]string{"claude", "--plugin-dir"}, nil},
		{[]string{"claude", "task with --plugin-dir in it"}, nil},
		{nil, nil},
	} {
		if got := pluginDirs(c.argv); !slices.Equal(got, c.want) {
			t.Errorf("pluginDirs(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
}
