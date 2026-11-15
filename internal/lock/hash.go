package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// The most of a skill a lock reads to hash it: a skill is a few files, and
// one with a whole program beside it is hashed by its names and sizes.
const (
	skillFilesMost = 4096
	skillBytesMost = 16 << 20
)

// Hash is "sha256:" and the hex of b's SHA-256.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var (
	fileMu     sync.Mutex
	fileHashes = map[string]string{} // by path, size and modification time
)

// FileHash is Hash of the file at path, read once for each size and time
// of change it has, as the irie binary is; "" for no path.
func FileHash(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", path, fi.Size(), fi.ModTime().UnixNano())
	fileMu.Lock()
	h, ok := fileHashes[key]
	fileMu.Unlock()
	if ok {
		return h, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	h = "sha256:" + hex.EncodeToString(sum.Sum(nil))
	fileMu.Lock()
	fileHashes[key] = h
	fileMu.Unlock()
	return h, nil
}

// errTooBig stops a walk of a skill too big to read whole.
var errTooBig = errors.New("too big")

// DirHash is the hash of a folder's files, a skill's: each one's path in
// the folder, its size and its bytes, in the order of their paths. The
// folder may be a link, as installed skills often are; links inside it are
// hashed by where they point. One past skillFilesMost files or
// skillBytesMost bytes is hashed by its paths and sizes alone, and says
// so with "names:" after "sha256:".
func DirHash(dir string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	type file struct {
		rel  string
		size int64
	}
	var files []file
	var total int64
	big := false
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			return nil // a link that leads nowhere, or to a folder
		}
		files = append(files, file{filepath.ToSlash(rel), fi.Size()})
		total += fi.Size()
		if len(files) > skillFilesMost || total > skillBytesMost {
			big = true
			return errTooBig
		}
		return nil
	})
	if err != nil && !errors.Is(err, errTooBig) {
		return "", err
	}
	slices.SortFunc(files, func(a, b file) int {
		switch {
		case a.rel < b.rel:
			return -1
		case a.rel > b.rel:
			return 1
		}
		return 0
	})
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%d\x00", f.rel, f.size)
		if big {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.rel)))
		if err != nil {
			return "", err
		}
		h.Write(b)
	}
	prefix := "sha256:"
	if big {
		prefix += "names:"
	}
	return prefix + hex.EncodeToString(h.Sum(nil)), nil
}
