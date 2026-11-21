//go:build !nojammin

package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/jammin"
)

// The jammin plugin's part of the core (docs/proposals/jammin.md): the
// jammin op, which the Mac app's Jammin tab, :jammin and irie jammin draw
// from. go build -tags nojammin leaves this file out, and jammin_off.go
// stands in.

const jamminBuilt = true

// jamminState is the music service, made at the first call.
type jamminState struct {
	once sync.Once
	sv   *jammin.Service
}

// jamminWait is how long one call may take: the first word to the Music
// app waits while macOS asks the person whether Irie may control it.
const jamminWait = 150 * time.Second

func (c *Core) jamminService() *jammin.Service {
	c.jammin.once.Do(func() {
		dir := filepath.Join(c.paths.State, "jammin")
		c.jammin.sv = &jammin.Service{
			Dir:       dir,
			Store:     jammin.DefaultStore(filepath.Join(dir, "keys")),
			Config:    func() config.Jammin { return c.cfg().Jammin },
			Configure: func(sets []ipc.ConfigSet) error { return c.configure(c.paths.ConfigFile(), sets) },
		}
	})
	return c.jammin.sv
}

// jamminResume, as the core starts, takes up a track of the music files
// that played on while the core restarted, so the next plays when it
// ends. It asks no app or service anything.
func (c *Core) jamminResume() {
	if c.cfg().JamminOn() {
		c.jamminService().Resume()
	}
}

// jamminLive is how each source stood at the core's last look, for the
// plugin's parts: it asks no source anything.
func (c *Core) jamminLive() []ipc.JamSource {
	return c.jamminService().Last()
}

// jamminOp answers the jammin op. peer is the process that asks. What a
// person listens to is theirs, and Spotify's Developer Policy forbids
// giving its content to an AI model, so a call from inside an agent's pane
// is refused, a look as much as a play.
func (c *Core) jamminOp(peer int, a ipc.JamminArgs) (ipc.JamminResult, error) {
	if !c.cfg().JamminOn() {
		return ipc.JamminResult{}, ipc.Errorf(ipc.CodeBadArgs, "Jammin is off, as its plugin is: `%s plugins on jammin` turns it on.", brand.Slug)
	}
	if c.fromAgent(peer) {
		return ipc.JamminResult{}, ipc.Errorf(ipc.CodeBadArgs, "Jammin is the person's alone: an agent can't see or play their music through %s. What they listen to is theirs, and Spotify forbids giving its content to an AI model.", brand.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), jamminWait)
	defer cancel()
	r, err := c.jamminService().Do(ctx, a)
	if err != nil {
		var ie *ipc.Error
		if errors.As(err, &ie) {
			return ipc.JamminResult{}, err
		}
		return ipc.JamminResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", jamminSentence(err.Error()))
	}
	return r, nil
}

// jamminSentence is an error of the jammin package as a sentence.
func jamminSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(r)) + s[n:]
	if last, _ := utf8.DecodeLastRuneInString(s); !strings.ContainsRune(".!?…", last) {
		s += "."
	}
	return s
}
