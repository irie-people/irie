package proc

import (
	"path/filepath"
	"strings"
)

// Holder is a process that holds a folder: its working directory, its
// program or a file it has open is in it.
type Holder struct {
	PID  int
	Name string // the command's name, as the system gives it
	What string // cwd, txt (its program) or the descriptor of an open file
	Path string // what it holds there
}

// Holding returns, for each of dirs, the processes whose working directory,
// program or open files are in it, from one look over the machine's
// processes, those of other users as far as the system shows them. A dir
// that no process holds has no entry. Each dir is matched as given and as
// its symlinks resolve, since the system names files by their real paths.
func Holding(dirs []string) (map[string][]Holder, error) {
	all, err := holders()
	if err != nil {
		return nil, err
	}
	return holding(all, dirs), nil
}

// holding is Holding over the holders all.
func holding(all []Holder, dirs []string) map[string][]Holder {
	type form struct{ prefix, dir string }
	var forms []form
	for _, d := range dirs {
		clean := filepath.Clean(d)
		forms = append(forms, form{clean, d})
		if real, err := filepath.EvalSymlinks(clean); err == nil && real != clean {
			forms = append(forms, form{real, d})
		}
	}
	// A process in one of two nested dirs holds both, and a dir matched in
	// both its forms counts the process once.
	out := map[string][]Holder{}
	for _, h := range all {
		p := filepath.Clean(h.Path)
		seen := map[string]bool{}
		for _, f := range forms {
			if !seen[f.dir] && (p == f.prefix || strings.HasPrefix(p, f.prefix+string(filepath.Separator))) {
				seen[f.dir] = true
				out[f.dir] = append(out[f.dir], h)
			}
		}
	}
	return out
}
