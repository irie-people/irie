//go:build !noyoink

package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/yoink"
)

// The yoink plugin's part of the core (DESIGN §4.17): the yoink op, which
// the Mac app's Yoink tab, :yoink and irie yoink draw from. go build -tags
// noyoink leaves this file out, and yoink_off.go stands in.

const yoinkBuilt = true

// yoinkState is the downloads, made at the first call. Those a core before
// this one started come in with it, from their folders.
type yoinkState struct {
	once sync.Once
	sv   *yoink.Service
}

func (c *Core) yoinkService() *yoink.Service {
	c.yoink.once.Do(func() {
		c.yoink.sv = &yoink.Service{Dir: filepath.Join(c.paths.State, "yoink")}
	})
	return c.yoink.sv
}

// yoinkOp answers the yoink op. peer is the process that asks. A link is
// the person's own word, so a call from inside an agent is refused, a look
// as much as a start: a page an agent reads can't send it to fetch from a
// site, nor fill the person's Downloads, nor learn what they download. An
// agent that needs a video runs yt-dlp itself, in its own folder, where its
// person sees what it did.
func (c *Core) yoinkOp(peer int, a ipc.YoinkArgs) (ipc.YoinkResult, error) {
	cfg := c.cfg()
	if why := plugins.Why(*cfg, plugins.Yoink); why != "" {
		return ipc.YoinkResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", why)
	}
	if c.fromAgent(peer) {
		return ipc.YoinkResult{}, ipc.Errorf(ipc.CodeBadArgs, "Yoink takes links from you alone: an agent can't look at, start or stop a download through %s. An agent that needs a video runs yt-dlp itself, in its own folder.", brand.Name)
	}
	h := home()
	setup := yoink.Look(cfg.Yoink, os.Getenv("PATH"), h)
	sv := c.yoinkService()
	r := ipc.YoinkResult{Folder: setup.Folder}
	engine := func() (yoink.Engine, error) {
		e, ok := setup.Engine(host.WithoutIdentity(tmux.WithoutEnv(os.Environ(), "TMUX", "TMUX_PANE")))
		if !ok {
			return e, ipc.Errorf(ipc.CodeBadArgs, "%s", setup.Lacks())
		}
		return e, nil
	}
	switch a.Do {
	case ipc.YoinkList:
	case ipc.YoinkFormats:
		e, err := engine()
		if err != nil {
			return r, err
		}
		info, err := sv.Formats(e, a.URL)
		if err != nil {
			return r, ipc.Errorf(ipc.CodeBadArgs, "%s", yoinkSentence(err.Error()))
		}
		r.Info = &info
		var opts []string
		for _, ch := range info.Choices {
			opts = append(opts, ch.Label)
		}
		r.Text = fmt.Sprintf("“%s” comes as %s.", info.Title, strings.Join(opts, ", "))
	case ipc.YoinkStart:
		e, err := engine()
		if err != nil {
			return r, err
		}
		c.log.Info("yoink start", "get", a.Get)
		d, err := sv.Start(e, setup.Folder, a.URL, a.Get)
		if err != nil {
			return r, ipc.Errorf(ipc.CodeBadArgs, "%s", yoinkSentence(err.Error()))
		}
		r.Download = &d
		what := "“" + d.Title + "”"
		if d.Get != ipc.YoinkBest {
			what += " (" + d.Label + ")"
		}
		r.Text = "Yoinking " + what + " into " + repo.Tilde(setup.Folder, h) + ". The Yoink tab shows how far it is."
	case ipc.YoinkCancel:
		d, err := sv.Cancel(a.ID)
		if err != nil {
			return r, ipc.Errorf(ipc.CodeBadArgs, "%s", yoinkSentence(err.Error()))
		}
		r.Download = &d
		r.Text = "Canceled “" + d.Title + "”: what it downloaded is gone."
		if d.State != ipc.YoinkCanceled {
			r.Text = "Canceling “" + d.Title + "”."
		}
	case ipc.YoinkClear:
		n, err := sv.Clear(a.ID)
		if err != nil {
			return r, ipc.Errorf(ipc.CodeBadArgs, "%s", yoinkSentence(err.Error()))
		}
		r.Text = fmt.Sprintf("Cleared %d from the list. Their files stay in %s.", n, repo.Tilde(setup.Folder, h))
		if n == 1 {
			r.Text = "Cleared it from the list. Its file stays where it is."
		}
	default:
		return r, ipc.Errorf(ipc.CodeBadArgs, "The yoink op takes formats, start, cancel or clear, or nothing for the downloads, not %q.", a.Do)
	}
	r.Downloads = sv.List()
	if r.Text == "" {
		r.Text = yoinkText(r, setup, h)
	}
	return r, nil
}

// yoinkText says how the downloads stand, the newest five a line each, as
// :yoink answers in a client with no Yoink tab, and what lacks to start
// one.
func yoinkText(r ipc.YoinkResult, setup yoink.Setup, h string) string {
	var b strings.Builder
	if len(r.Downloads) == 0 {
		b.WriteString("No downloads yet. `:yoink LINK` downloads a video into " + repo.Tilde(r.Folder, h) + ", and `:yoink audio LINK` its sound as an mp3.")
	}
	for i, d := range r.Downloads {
		if i == 5 {
			fmt.Fprintf(&b, "\nAnd %d more.", len(r.Downloads)-5)
			break
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(yoink.Line(d, h))
	}
	if lacks := setup.Lacks(); lacks != "" {
		b.WriteString("\n" + lacks)
	}
	return b.String()
}

// yoinkSentence is an error of the yoink package as a sentence.
func yoinkSentence(s string) string {
	s = strings.TrimSpace(s)
	r, n := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(r)) + s[n:]
	if last, _ := utf8.DecodeLastRuneInString(s); !strings.ContainsRune(".!?…", last) {
		s += "."
	}
	return s
}
