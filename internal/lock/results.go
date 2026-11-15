package lock

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
)

// ResultsFile is where the core keeps what the checks found, in its state
// folder, so that the marks outlast a restart.
const ResultsFile = "lock-check.json"

// keepFound is how long what a check found for a version stays once no
// check asks about it again.
const keepFound = 30 * 24 * time.Hour

// Results are what the checks found: each package version they asked
// about, with the advisories that affect it, the newest check's first.
type Results struct {
	Checked  time.Time       `json:"checked,omitzero"` // when the last check ran
	OSV      string          `json:"osv,omitempty"`    // where it asked
	Packages []ipc.LockFound `json:"packages"`
}

// Key names a package version, as Results keep it.
func Key(p ipc.LockPackage) string { return p.Ecosystem + "\x00" + p.Name + "\x00" + p.Version }

// KeyFound is Key for what a check found.
func KeyFound(f ipc.LockFound) string { return f.Ecosystem + "\x00" + f.Name + "\x00" + f.Version }

// LoadResults reads the results in path; none when there is no file.
func LoadResults(path string) (Results, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Results{}, nil
	}
	if err != nil {
		return Results{}, err
	}
	var r Results
	if err := json.Unmarshal(b, &r); err != nil {
		return Results{}, err
	}
	return r, nil
}

// Save writes r to path, as one transaction.
func (r Results) Save(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, append(b, '\n'), 0o600)
}

// By is the results by Key.
func (r Results) By() map[string]ipc.LockFound {
	out := make(map[string]ipc.LockFound, len(r.Packages))
	for _, f := range r.Packages {
		out[KeyFound(f)] = f
	}
	return out
}

// Merge puts what a check found at now in place of what was known of the
// same versions, and keeps the rest for keepFound after its own check.
func (r Results) Merge(found []ipc.LockFound, osv string, now time.Time) Results {
	out := Results{Checked: now, OSV: osv}
	seen := map[string]bool{}
	for _, f := range found {
		if !seen[KeyFound(f)] {
			seen[KeyFound(f)] = true
			f.Sessions = nil
			out.Packages = append(out.Packages, f)
		}
	}
	for _, f := range r.Packages {
		if !seen[KeyFound(f)] && now.Sub(f.Checked) < keepFound {
			seen[KeyFound(f)] = true
			out.Packages = append(out.Packages, f)
		}
	}
	return out
}

// Mark gives each package what a check found for it, in by: whether it
// was checked, how many advisories affect it, how many no version fixes
// yet, and the version that fixes the rest. A package by does not hold
// keeps what it says already, as a peer's row does from its own core's
// check.
func Mark(pkgs []ipc.LockPackage, by map[string]ipc.LockFound) []ipc.LockPackage {
	out := slices.Clone(pkgs)
	for i, p := range out {
		f, ok := by[Key(p)]
		if !ok {
			continue
		}
		p.Checked, p.Advisories, p.Unfixed, p.Fixed = true, len(f.Advisories), 0, ""
		for _, a := range f.Advisories {
			switch {
			case a.Fixed == "":
				p.Unfixed++
			case p.Fixed == "" || Compare(a.Fixed, p.Fixed) > 0:
				p.Fixed = a.Fixed
			}
		}
		out[i] = p
	}
	return out
}

// Row is what a row says of a lock's packages, marked: nil for none.
func Row(pkgs []ipc.LockPackage) *ipc.LockRow {
	if len(pkgs) == 0 {
		return nil
	}
	r := &ipc.LockRow{Packages: pkgs}
	var words []string
	for _, p := range pkgs {
		if p.Advisories > 0 {
			r.Advisories += p.Advisories
			words = append(words, Words(p))
		}
	}
	r.Words = strings.Join(words, " · ")
	return r
}
