package peer

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/irie-people/irie/internal/filetx"
)

// File is where the peers are kept, in the state dir.
const File = "peers.json"

// Peer is one other machine's core this one is paired with.
type Peer struct {
	URL         string    `json:"url"`         // its gateway, https://host:port
	Token       string    `json:"token"`       // what its gateway issued this machine
	Fingerprint string    `json:"fingerprint"` // the SHA-256 of its Irie authority, hex
	Machine     string    `json:"machine"`     // its id, as its gateway says
	Paired      time.Time `json:"paired"`
}

type file struct {
	Peers map[string]Peer `json:"peers"`
}

// Load reads the peers kept in dir, by name; none when there is no file.
func Load(dir string) (map[string]Peer, error) {
	b, err := os.ReadFile(filepath.Join(dir, File))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Peer{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	if f.Peers == nil {
		f.Peers = map[string]Peer{}
	}
	return f.Peers, nil
}

// Names returns the peers' names in order.
func Names(ps map[string]Peer) []string { return slices.Sorted(maps.Keys(ps)) }

// Save keeps p under name in dir, replacing any peer of that name.
func Save(dir, name string, p Peer) error {
	if err := CheckName(name); err != nil {
		return err
	}
	return change(dir, func(ps map[string]Peer) { ps[name] = p })
}

// Remove forgets the peer called name; it is no error when there is none.
func Remove(dir, name string) error {
	return change(dir, func(ps map[string]Peer) { delete(ps, name) })
}

func change(dir string, f func(map[string]Peer)) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return filetx.Update(filepath.Join(dir, File), 0o600, func(old []byte) ([]byte, error) {
		var fl file
		if len(old) > 0 {
			if err := json.Unmarshal(old, &fl); err != nil {
				return nil, fmt.Errorf("%s: %w", File, err)
			}
		}
		if fl.Peers == nil {
			fl.Peers = map[string]Peer{}
		}
		f(fl.Peers)
		b, err := json.MarshalIndent(fl, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	})
}

// CheckName says whether name can name a peer: it prefixes the ids of the
// peer's sessions on this machine (Asahi/s38…), and ^name places a task
// there, so it is a word of letters, digits, - and _.
func CheckName(name string) error {
	if name == "" || len(name) > 30 || strings.ContainsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	}) {
		return fmt.Errorf("%q can't name a machine: use up to 30 letters, digits, - or _", name)
	}
	return nil
}
