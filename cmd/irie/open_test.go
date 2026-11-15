package main

import "testing"

// TestSplitFilePos: a trailing :LINE or :LINE:COL comes off a file name
// unless the name as given exists.
func TestSplitFilePos(t *testing.T) {
	exists := func(p string) bool { return p == "/home/alice/a:7.txt" || p == "/home/alice/b:2:3" }
	for _, c := range []struct {
		in        string
		file      string
		line, col int
	}{
		{"/home/alice/main.go", "/home/alice/main.go", 0, 0},
		{"/home/alice/main.go:12", "/home/alice/main.go", 12, 0},
		{"/home/alice/main.go:12:5", "/home/alice/main.go", 12, 5},
		{"/home/alice/a:7.txt", "/home/alice/a:7.txt", 0, 0},
		{"/home/alice/b:2:3", "/home/alice/b:2:3", 0, 0},
		{"/home/alice/b:2:3:9", "/home/alice/b:2", 3, 9},
		{"/home/alice/a:7.txt:4", "/home/alice/a:7.txt", 4, 0},
		{"/home/alice/main.go:x", "/home/alice/main.go:x", 0, 0},
		{"/home/alice/main.go:12:x", "/home/alice/main.go:12:x", 0, 0},
		{"/home/alice/main.go:0", "/home/alice/main.go:0", 0, 0},
		{"/home/alice/main.go:-3", "/home/alice/main.go:-3", 0, 0},
		{"/home/alice/main.go:", "/home/alice/main.go:", 0, 0},
	} {
		file, line, col := splitFilePos(c.in, exists)
		if file != c.file || line != c.line || col != c.col {
			t.Errorf("%q = %q, %d, %d; want %q, %d, %d", c.in, file, line, col, c.file, c.line, c.col)
		}
	}
}
