package remote

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/irie-people/irie/internal/filetx"
)

const (
	devicesFile = "devices.json"
	pairingFile = "pairing.json"
	machineFile = "machine.json"

	// CodeLife is how long a pairing code works unless asked otherwise.
	CodeLife = 10 * time.Minute
	// codeTries is how many wrong codes end a code: a guess has 5 chances
	// in a million.
	codeTries = 5
	// seenEvery is how often a device's last use is written down.
	seenEvery = 10 * time.Minute
)

// Device is a phone paired with this machine. Only a hash of its token is
// kept, so the file alone lets no one in.
type Device struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Hash    string    `json:"hash"` // SHA-256 of its token, in hex
	Created time.Time `json:"created"`
	Seen    time.Time `json:"seen,omitzero"`
}

// pairing is the code a phone pairs with, while one is open.
type pairing struct {
	Hash    string    `json:"hash"` // SHA-256 of the code, in hex
	Expires time.Time `json:"expires"`
	Tries   int       `json:"tries"`
}

// Store keeps the phones paired with this machine and the open pairing
// code, in Irie's state folder. `irie remote pair` writes the code; the
// gateway reads it, so the two need no other channel.
type Store struct {
	Dir string
	Now func() time.Time

	mu      sync.Mutex
	devices []Device
	modTime time.Time
	seen    map[string]time.Time // last use not yet written, by device id
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// Pairing errors, with the words a phone shows.
var (
	ErrNoCode   = &PairError{Code: "expired", Msg: "That code has expired or was used. On the machine, run `irie remote pair` for a new one."}
	ErrBadCode  = &PairError{Code: "bad_code", Msg: "That code isn't right. Check it on the machine and try again."}
	ErrTooMany  = &PairError{Code: "too_many", Msg: "Too many wrong codes, so that one no longer works. On the machine, run `irie remote pair` for a new one."}
	ErrUnpaired = &PairError{Code: "unpaired", Msg: "This phone isn't paired with this machine any more. Pair it again with a code from `irie remote pair`."}
)

// PairError is a refused pairing or token, in words for the phone.
type PairError struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func (e *PairError) Error() string { return e.Msg }

// NewCode opens a pairing code for life, replacing any open one, and
// returns it: six digits.
func (s *Store) NewCode(life time.Duration) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	p := pairing{Hash: hash(code), Expires: s.now().Add(life)}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	return code, filetx.WriteFile(s.path(pairingFile), data, 0o600)
}

// Pair takes a code from a phone. A right one pairs the phone, closes the
// code and returns the phone's token; a wrong one counts against the code.
func (s *Store) Pair(code, name string) (token string, d Device, err error) {
	code = strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, code)
	// The file changes whatever the outcome: a wrong code counts.
	var refused error
	err = filetx.Update(s.path(pairingFile), 0o600, func(old []byte) ([]byte, error) {
		var p pairing
		switch {
		case len(old) == 0 || json.Unmarshal(old, &p) != nil || p.Hash == "" || !s.now().Before(p.Expires):
			refused = ErrNoCode
		case subtle.ConstantTimeCompare([]byte(hash(code)), []byte(p.Hash)) != 1:
			if p.Tries++; p.Tries < codeTries {
				refused = ErrBadCode
				return json.Marshal(p)
			}
			refused = ErrTooMany
		}
		return []byte("{}"), nil
	})
	if err == nil {
		err = refused
	}
	if err != nil {
		return "", Device{}, err
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token = base64.RawURLEncoding.EncodeToString(raw)
	id := make([]byte, 4)
	rand.Read(id)
	d = Device{ID: "d" + hex.EncodeToString(id), Name: deviceName(name), Hash: hash(token), Created: s.now().UTC()}
	if err := s.change(func(ds []Device) []Device { return append(ds, d) }); err != nil {
		return "", Device{}, err
	}
	return token, d, nil
}

// deviceName is what the phone calls itself, made one short line.
func deviceName(name string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40])
	}
	if name == "" {
		name = "Phone"
	}
	return name
}

// Device finds the phone a token belongs to.
func (s *Store) Device(token string) (Device, error) {
	if token == "" {
		return Device{}, ErrUnpaired
	}
	h := hash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Device{}, err
	}
	for _, d := range s.devices {
		if subtle.ConstantTimeCompare([]byte(d.Hash), []byte(h)) == 1 {
			return d, nil
		}
	}
	return Device{}, ErrUnpaired
}

// Saw notes that a phone was used now, written down at most every
// seenEvery.
func (s *Store) Saw(d Device) {
	now := s.now()
	s.mu.Lock()
	if s.seen == nil {
		s.seen = map[string]time.Time{}
	}
	if now.Sub(d.Seen) < seenEvery || now.Sub(s.seen[d.ID]) < seenEvery {
		s.mu.Unlock()
		return
	}
	s.seen[d.ID] = now
	s.mu.Unlock()
	s.change(func(ds []Device) []Device {
		for i := range ds {
			if ds[i].ID == d.ID {
				ds[i].Seen = now.UTC()
			}
		}
		return ds
	})
}

// Devices lists the paired phones, oldest first.
func (s *Store) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return slices.Clone(s.devices), nil
}

// Forget unpairs the phones whose id or name is key, or every phone for
// "all", and returns those it forgot.
func (s *Store) Forget(key string) ([]Device, error) {
	var gone []Device
	err := s.change(func(ds []Device) []Device {
		return slices.DeleteFunc(ds, func(d Device) bool {
			if key == "all" || d.ID == key || strings.EqualFold(d.Name, key) {
				gone = append(gone, d)
				return true
			}
			return false
		})
	})
	return gone, err
}

// load reads the devices when the file changed. The caller holds s.mu.
func (s *Store) load() error {
	fi, err := os.Stat(s.path(devicesFile))
	if errors.Is(err, fs.ErrNotExist) {
		s.devices, s.modTime = nil, time.Time{}
		return nil
	}
	if err != nil {
		return err
	}
	if fi.ModTime().Equal(s.modTime) && s.devices != nil {
		return nil
	}
	data, err := os.ReadFile(s.path(devicesFile))
	if err != nil {
		return err
	}
	var ds []Device
	if err := json.Unmarshal(data, &ds); err != nil {
		return fmt.Errorf("%s: %w", s.path(devicesFile), err)
	}
	s.devices, s.modTime = ds, fi.ModTime()
	return nil
}

// change edits the devices file in one transaction.
func (s *Store) change(f func([]Device) []Device) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	err := filetx.Update(s.path(devicesFile), 0o600, func(old []byte) ([]byte, error) {
		var ds []Device
		if len(old) > 0 {
			if err := json.Unmarshal(old, &ds); err != nil {
				return nil, fmt.Errorf("%s: %w", s.path(devicesFile), err)
			}
		}
		ds = f(ds)
		if ds == nil {
			ds = []Device{}
		}
		return json.MarshalIndent(ds, "", "  ")
	})
	s.mu.Lock()
	s.modTime = time.Time{} // read it again
	s.mu.Unlock()
	return err
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// MachineID is this machine's id for the phone, made once: phones tell
// machines apart by it, whatever address they reach one at.
func MachineID(dir string) (string, error) {
	path := filepath.Join(dir, machineFile)
	var m struct {
		ID string `json:"id"`
	}
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &m) == nil && m.ID != "" {
		return m.ID, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	err := filetx.Update(path, 0o600, func(old []byte) ([]byte, error) {
		if json.Unmarshal(old, &m) == nil && m.ID != "" {
			return old, nil
		}
		b := make([]byte, 4)
		rand.Read(b)
		m.ID = "m" + hex.EncodeToString(b)
		return json.Marshal(m)
	})
	return m.ID, err
}
