//go:build !nochat

package chat

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
)

// The agents' drafts, kept on disk because the core restarts at every
// install and a draft must wait for its person through that. A draft is
// the agent's own words, where they go, and the session that wrote them:
// nothing anyone else wrote.

// draftsKeep is how long a sent or discarded draft stays, so its agent can
// learn what came of it.
const draftsKeep = 7 * 24 * time.Hour

// The most drafts that wait at once, for one session and in all, so an
// agent gone wrong can't bury the person in them.
const (
	draftsPerSession = 20
	draftsWaiting    = 200
)

type drafts struct {
	path string
	now  func() time.Time

	mu     sync.Mutex
	loaded bool
	list   []ipc.ChatDraft
}

type draftsFile struct {
	Drafts []ipc.ChatDraft `json:"drafts"`
}

// load reads the file once; one that doesn't read is kept aside, so a
// broken file never stops the person's drafts from coming again.
func (d *drafts) load() error {
	if d.loaded {
		return nil
	}
	b, err := os.ReadFile(d.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		var f draftsFile
		if err := json.Unmarshal(b, &f); err != nil {
			os.Rename(d.path, d.path+".broken")
		} else {
			d.list = f.Drafts
		}
	}
	d.loaded = true
	d.prune()
	return nil
}

// prune drops the drafts that ended more than a week ago.
func (d *drafts) prune() {
	cut := d.now().Add(-draftsKeep)
	d.list = slices.DeleteFunc(d.list, func(x ipc.ChatDraft) bool {
		return x.State != ipc.DraftWaiting && x.Ended.Before(cut)
	})
}

func (d *drafts) save() error {
	d.prune()
	b, err := json.MarshalIndent(draftsFile{Drafts: d.list}, "", "\t")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(d.path, append(b, '\n'), 0o600)
}

// add keeps a new draft, waiting.
func (d *drafts) add(x ipc.ChatDraft) (ipc.ChatDraft, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(); err != nil {
		return ipc.ChatDraft{}, err
	}
	mine, all := 0, 0
	for _, y := range d.list {
		if y.State == ipc.DraftWaiting {
			all++
			if y.Session == x.Session {
				mine++
			}
		}
	}
	switch {
	case mine >= draftsPerSession:
		return ipc.ChatDraft{}, ipc.Errorf(ipc.CodeBusy, "This session already has %d drafts waiting for the person, the most it may: wait until they send or discard some.", mine)
	case all >= draftsWaiting:
		return ipc.ChatDraft{}, ipc.Errorf(ipc.CodeBusy, "%d drafts already wait for the person, the most there may be: wait until they send or discard some.", all)
	}
	x.ID, x.Made, x.State = newDraftID(), d.now(), ipc.DraftWaiting
	d.list = append(d.list, x)
	if err := d.save(); err != nil {
		d.list = d.list[:len(d.list)-1]
		return ipc.ChatDraft{}, err
	}
	return x, nil
}

// get is the draft whose ID is id.
func (d *drafts) get(id string) (ipc.ChatDraft, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(); err != nil {
		return ipc.ChatDraft{}, err
	}
	for _, x := range d.list {
		if x.ID == id {
			return x, nil
		}
	}
	return ipc.ChatDraft{}, ipc.Errorf(ipc.CodeNotFound, "No draft %s: it may have ended more than a week ago.", id)
}

// change keeps what f makes of draft id.
func (d *drafts) change(id string, f func(*ipc.ChatDraft) error) (ipc.ChatDraft, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(); err != nil {
		return ipc.ChatDraft{}, err
	}
	i := slices.IndexFunc(d.list, func(x ipc.ChatDraft) bool { return x.ID == id })
	if i < 0 {
		return ipc.ChatDraft{}, ipc.Errorf(ipc.CodeNotFound, "No draft %s: it may have ended more than a week ago.", id)
	}
	was := d.list[i]
	if err := f(&d.list[i]); err != nil {
		d.list[i] = was
		return ipc.ChatDraft{}, err
	}
	if err := d.save(); err != nil {
		d.list[i] = was
		return ipc.ChatDraft{}, err
	}
	return d.list[i], nil
}

// all are the drafts, those waiting first, oldest first, then those that
// ended, newest first; only session's when it isn't "".
func (d *drafts) all(session string) []ipc.ChatDraft {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(); err != nil {
		return []ipc.ChatDraft{}
	}
	out := []ipc.ChatDraft{}
	var ended []ipc.ChatDraft
	for _, x := range d.list {
		if session != "" && x.Session != session {
			continue
		}
		if x.State == ipc.DraftWaiting {
			out = append(out, x)
		} else {
			ended = append(ended, x)
		}
	}
	slices.SortStableFunc(out, func(a, b ipc.ChatDraft) int { return a.Made.Compare(b.Made) })
	slices.SortStableFunc(ended, func(a, b ipc.ChatDraft) int { return b.Ended.Compare(a.Ended) })
	return append(out, ended...)
}

// forget drops every draft to account, as it is removed.
func (d *drafts) forget(account string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.load(); err != nil {
		return err
	}
	n := len(d.list)
	d.list = slices.DeleteFunc(d.list, func(x ipc.ChatDraft) bool { return x.Account == account })
	if len(d.list) == n {
		return nil
	}
	return d.save()
}

// newDraftID is a draft's ID: d and ten letters and digits, short enough to
// type after irie chat send.
func newDraftID() string {
	const abc = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 10)
	rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return fmt.Sprintf("d%s", b)
}
