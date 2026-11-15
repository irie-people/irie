package variorum

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
)

// Setups are the setups Irie keeps, in <data>/variorum/door.json, a file
// the user alone can read: the only place a key goes.
type Setups struct {
	Sessions *Setup `json:"sessions,omitempty"` // Claude Code's, for writing sessions
	Tab      *Setup `json:"tab,omitempty"`      // Any MCP client's, for the Variorum tab
}

// File is where Irie keeps the setups, under its data folder.
func File(data string) string { return filepath.Join(data, "variorum", "door.json") }

// Load reads the setups Irie keeps; none when it keeps none.
func Load(data string) (Setups, error) {
	var st Setups
	b, err := os.ReadFile(File(data))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &st)
	}
	if err != nil {
		// Not the error itself, which could quote the file.
		return Setups{}, fmt.Errorf("%s can't read %s, where it keeps Variorum's setups: `%s variorum forget` deletes it, and then you can connect again.", brand.Name, File(data), brand.Slug)
	}
	return st, nil
}

// Save keeps s as the setup of its kind, in place of any before it, and
// keeps the other kind's as it was.
func Save(data, kind string, s Setup) error {
	if kind != Sessions && kind != Tab {
		return fmt.Errorf("variorum: no kind of setup %q", kind)
	}
	path := File(data)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	// A file another wrote is the user's alone before a key goes in; filetx
	// keeps the mode of a file it replaces.
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return filetx.Update(path, 0o600, func(old []byte) ([]byte, error) {
		var st Setups
		// A file that doesn't read is replaced: it holds nothing usable.
		_ = json.Unmarshal(old, &st)
		if kind == Sessions {
			st.Sessions = &s
		} else {
			st.Tab = &s
		}
		b, err := json.MarshalIndent(st, "", "  ")
		return append(b, '\n'), err
	})
}

// Forget deletes both setups.
func Forget(data string) error {
	if err := os.Remove(File(data)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
