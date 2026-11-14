package claude

// The user's default model. Claude Code's /model <name> switches a
// session's model and also saves the name as the default for new sessions,
// in the model key of the user's settings.json (model-config's "Setting
// your model"). When Irie switches a session at a usage limit (DESIGN
// §6.4), it puts the user's own default back. A session keeps the model it
// switched to when the key changes under it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/irie-people/irie/internal/filetx"
)

// modelKey is the key of settings.json that holds the default model.
const modelKey = "model"

// userSettings is the user's settings file in Claude Code's config folder.
func userSettings(dir string) string { return filepath.Join(dir, "settings.json") }

// SavedModel is the default model the user's settings in config folder dir
// hold, as written: a JSON value such as "fable[1m]", or nil when they
// hold none or there is no file.
func SavedModel(dir string) (json.RawMessage, error) {
	b, err := os.ReadFile(userSettings(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	members, err := topMembers(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", userSettings(dir), err)
	}
	if m, ok := lastMember(members, modelKey); ok {
		return slices.Clone(json.RawMessage(b[m.value:m.end])), nil
	}
	return nil, nil
}

// RestoreModel puts the default model back to was, nil for none, in the
// user's settings in config folder dir, when they now name one of names,
// as /model saved it. It changes the model key alone, as written, and
// keeps the rest of the file byte for byte and a link to it. put says it
// wrote; settled says there is nothing to wait for: the settings name
// neither was nor one of names, as when someone else changed them.
func RestoreModel(dir string, was json.RawMessage, names []string) (put, settled bool, err error) {
	path := userSettings(dir)
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, was == nil, nil
	case err != nil:
		return false, true, err
	}
	members, err := topMembers(b)
	if err != nil {
		return false, true, fmt.Errorf("%s: %v", path, err)
	}
	m, ok := lastMember(members, modelKey)
	var now string
	switch {
	case !ok && was == nil, ok && bytes.Equal(b[m.value:m.end], was):
		// Not saved yet.
		return false, false, nil
	case !ok, json.Unmarshal(b[m.value:m.end], &now) != nil, !slices.Contains(names, now):
		return false, true, nil
	}
	var out []byte
	if was == nil {
		out = without(b, members, slices.Index(members, m))
	} else {
		out = slices.Concat(b[:m.value], was, b[m.end:])
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false, true, err
	}
	// Claude Code takes no lock of Irie's on its settings, so none is left
	// beside them.
	if err := filetx.Replace(path, out, fi.Mode().Perm()); err != nil {
		return false, true, err
	}
	return true, true, nil
}

// member is where a top-level member of a JSON object lies in its text:
// from after what comes before it (the object's brace, or the previous
// member's value), its key, its value, and the end of its value.
type member struct {
	key                  string
	from, at, value, end int
}

// topMembers finds the top-level members of the JSON object b, in order.
func topMembers(b []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var out []member
	// What comes before each member ends here: More looks past the blanks
	// after it.
	from := int(dec.InputOffset())
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		keyEnd := int(dec.InputOffset())
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		out = append(out, member{key: key, from: from, at: skip(b, from, ", \t\r\n"), value: skip(b, keyEnd, ": \t\r\n"), end: end})
		from = end
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("not a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("more after the JSON object")
	}
	return out, nil
}

// lastMember is the member named key that a reader of b takes: the last.
func lastMember(members []member, key string) (member, bool) {
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].key == key {
			return members[i], true
		}
	}
	return member{}, false
}

// without is b with member i of its top-level members taken out, with the
// comma that joined it to the others.
func without(b []byte, members []member, i int) []byte {
	m := members[i]
	switch {
	case i > 0:
		// From the previous value's end: its comma goes.
		return slices.Concat(b[:m.from], b[m.end:])
	case len(members) > 1:
		// The first of several: up to the next key.
		return slices.Concat(b[:m.at], b[members[1].at:])
	}
	return slices.Concat(b[:m.from], b[m.end:])
}

// skip is the first offset from i in b of a byte not in set.
func skip(b []byte, i int, set string) int {
	for i < len(b) && bytes.IndexByte([]byte(set), b[i]) >= 0 {
		i++
	}
	return i
}
