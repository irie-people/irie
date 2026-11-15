package proc

import (
	"path/filepath"
	"strconv"
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
// that no process holds has no entry.
//
// The system names a file by its own path, which may differ from a dir as
// given: through a symlink, in another case on a disk that ignores case, or
// in another Unicode normalization. So a path is in a dir when it starts
// with the dir as given or as its symlinks resolve, or when its ancestor
// as deep as the dir is the dir itself, the same file on the same device.
func Holding(dirs []string) (map[string][]Holder, error) {
	all, err := holders()
	if err != nil {
		return nil, err
	}
	return holding(all, dirs, fileKey), nil
}

// holding is Holding over the holders all, with key naming the file a path
// leads to by its device and inode.
func holding(all []Holder, dirs []string, key func(string) (fileID, bool)) map[string][]Holder {
	type target struct {
		dir   string
		forms []string // the dir as given and as its symlinks resolve
		depth int      // the components of its resolved form
		id    fileID
		known bool // its file is known
	}
	var targets []target
	for _, d := range dirs {
		t := target{dir: d, forms: []string{filepath.Clean(d)}}
		real := filepath.Clean(d)
		if r, err := filepath.EvalSymlinks(real); err == nil {
			real = r
			if r != t.forms[0] {
				t.forms = append(t.forms, r)
			}
		}
		t.depth = depth(real)
		t.id, t.known = key(real)
		targets = append(targets, t)
	}
	out := map[string][]Holder{}
	// Many paths share an ancestor, such as a framework's folder: each is
	// looked up once.
	ids := map[string]fileID{}
	lookup := func(a string) (fileID, bool) {
		if id, ok := ids[a]; ok {
			return id, id != (fileID{})
		}
		id, ok := key(a)
		if !ok && unescape(a) != a {
			id, ok = key(unescape(a))
		}
		if !ok {
			id = fileID{}
		}
		ids[a] = id
		return id, ok
	}
	for _, h := range all {
		p := filepath.Clean(h.Path)
		for _, t := range targets {
			in := false
			for _, f := range t.forms {
				if p == f || strings.HasPrefix(p, f+string(filepath.Separator)) {
					in = true
					break
				}
			}
			if !in && t.known {
				if a, ok := ancestor(p, t.depth); ok {
					id, ok := lookup(a)
					in = ok && id == t.id
				}
			}
			if in {
				out[t.dir] = append(out[t.dir], h)
			}
		}
	}
	return out
}

// fileID names a file by its device and inode.
type fileID struct{ dev, ino uint64 }

// depth counts an absolute path's components.
func depth(p string) int {
	p = strings.Trim(filepath.Clean(p), string(filepath.Separator))
	if p == "" {
		return 0
	}
	return strings.Count(p, string(filepath.Separator)) + 1
}

// ancestor is p's first n components, as a path; ok is false when p has
// fewer.
func ancestor(p string, n int) (string, bool) {
	if !filepath.IsAbs(p) || n <= 0 {
		return "", false
	}
	parts := strings.Split(strings.Trim(p, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) < n {
		return "", false
	}
	return string(filepath.Separator) + filepath.Join(parts[:n]...), true
}

// unescape undoes lsof's forms for the bytes it does not print (lsof(8)):
// \xNN, \b, \f, \n, \r, \t and ^ before a control character's letter. A
// name that merely holds a backslash is tried both ways.
func unescape(s string) string {
	if !strings.ContainsAny(s, `\^`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+3 < len(s) && s[i+1] == 'x':
			if n, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		case c == '\\' && i+1 < len(s) && strings.IndexByte("bfnrt", s[i+1]) >= 0:
			b.WriteByte(map[byte]byte{'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}[s[i+1]])
			i++
			continue
		case c == '^' && i+1 < len(s) && s[i+1] >= '@' && s[i+1] <= '_':
			b.WriteByte(s[i+1] - '@')
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
