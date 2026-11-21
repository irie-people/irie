package filetx

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteOnce puts data at path in one rename, synced first, as WriteFile
// does, but takes no lock and leaves no lock file beside it: for a file
// written once under a name its contents decide, which two writers could
// only write alike, such as a version of a chain (internal/chain).
func WriteOnce(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return replace(path, data, perm)
}
