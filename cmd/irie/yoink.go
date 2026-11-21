//go:build !noyoink

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/yoink"
)

// irie yoink: the yoink plugin's downloads (DESIGN §4.17), in a terminal.
// The core does the work, so a download shows in the Mac app's Yoink tab
// too, and runs on if the core restarts. go build -tags noyoink leaves this
// file out, and yoink_off.go stands in.

// yoinkUsage is the yoink plugin's lines in the usage.
const yoinkUsage = `  ` + brand.Slug + ` yoink [--audio] [--res N] LINK
                          download a video, or its sound as an mp3, into
                          ~/Downloads (` + brand.Slug + ` yoink help)
`

const yoinkHelp = `usage:
  ` + brand.Slug + ` yoink [--audio] [--res N] [--detach] LINK
                       download the video at LINK into yoink.folder,
                       ~/Downloads unless config.toml says otherwise: the
                       best there is, at most N lines tall with --res (720,
                       1080, 4k), or its sound alone as an mp3 with --audio.
                       It says how far it is on standard error, and prints
                       the file's path on standard output once it is there.
                       Ctrl-C cancels it; --detach starts it and leaves it
                       to run, in the Yoink tab
  ` + brand.Slug + ` yoink formats LINK   what LINK offers, with about how big each is
  ` + brand.Slug + ` yoink [list]         the downloads, newest first
  ` + brand.Slug + ` yoink cancel ID      stop a download, and remove what it wrote
  ` + brand.Slug + ` yoink clear [ID]     forget the finished downloads, or one; their
                       files stay
yt-dlp downloads, at the release yoink.version pins, through uvx, or the
yt-dlp yoink.ytdlp names; ffmpeg merges a video with its sound and makes
mp3s. A link is http or https. Sites you sign in to, such as Instagram,
come later.
`

func init() {
	pluginCmds["yoink"] = pluginCmd{run: func(args []string) error { return yoinkCmd(args, os.Stdout, os.Stderr) }}
}

// yoinkLook is how long a start may take: the core looks at the link
// first, and the first look also fetches the pinned yt-dlp.
const yoinkLook = 3 * time.Minute

// yoinkCall asks the core's yoink op.
func yoinkCall(a ipc.YoinkArgs, wait time.Duration) (ipc.YoinkResult, error) {
	_, p, err := load()
	if err != nil {
		return ipc.YoinkResult{}, err
	}
	var r ipc.YoinkResult
	err = ipc.Call(p.Socket(), wait, ipc.OpYoink, a, &r)
	return r, err
}

func yoinkCmd(args []string, stdout, stderr io.Writer) error {
	cfg, _, err := load()
	if err != nil {
		return err
	}
	if why := plugins.Why(cfg, plugins.Yoink); why != "" {
		return errors.New(why)
	}
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") && !strings.Contains(args[0], ".") {
		sub, args = args[0], args[1:]
	}
	h, _ := os.UserHomeDir()
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		sub = "help"
	}
	switch sub {
	case "help":
		fmt.Fprint(stdout, yoinkHelp)
		return nil
	case "", "list":
		if sub == "list" || len(args) == 0 {
			r, err := yoinkCall(ipc.YoinkArgs{}, 10*time.Second)
			if err != nil {
				return err
			}
			if len(r.Downloads) == 0 {
				fmt.Fprintln(stdout, r.Text)
			}
			for _, d := range r.Downloads {
				fmt.Fprintf(stdout, "%s  %s\n", d.ID, yoink.Line(d, h))
			}
			return nil
		}
		return yoinkGet(args, stdout, stderr)
	case "formats":
		if len(args) != 1 {
			return errors.New("usage: " + brand.Slug + " yoink formats LINK")
		}
		r, err := yoinkCall(ipc.YoinkArgs{Do: ipc.YoinkFormats, URL: args[0]}, yoinkLook)
		if err != nil {
			return err
		}
		writeChoices(stdout, *r.Info)
		return nil
	case "cancel", "clear":
		id := ""
		switch {
		case len(args) == 1:
			id = args[0]
		case len(args) > 1 || sub == "cancel":
			return fmt.Errorf("usage: %s yoink %s ID (%s yoink list names them)", brand.Slug, sub, brand.Slug)
		}
		do := map[string]string{"cancel": ipc.YoinkCancel, "clear": ipc.YoinkClear}[sub]
		r, err := yoinkCall(ipc.YoinkArgs{Do: do, ID: id}, 15*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, r.Text)
		return nil
	}
	return fmt.Errorf("unknown yoink command %q\n\n%s", sub, yoinkHelp)
}

// yoinkGet downloads a link: it starts the download, says how far it is
// until it ends, and prints the file's path.
func yoinkGet(args []string, stdout, stderr io.Writer) error {
	a := ipc.YoinkArgs{Do: ipc.YoinkStart}
	detach := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch {
		case !strings.HasPrefix(arg, "-"):
			if a.URL != "" {
				return errors.New(brand.Slug + " yoink takes one link at a time")
			}
			a.URL = arg
			if !strings.Contains(arg, "://") {
				a.URL = "https://" + arg
			}
		case name == "audio" || name == "mp3":
			a.Get = ipc.YoinkAudio
		case name == "detach" || name == "d":
			detach = true
		case name == "res" || name == "r":
			if !hasValue {
				if i+1 == len(args) {
					return errors.New("--res takes a height, such as --res 720")
				}
				i++
				value = args[i]
			}
			a.Get = value
		default:
			return fmt.Errorf("unknown flag %s\n\n%s", arg, yoinkHelp)
		}
	}
	if a.URL == "" {
		return errors.New("usage: " + brand.Slug + " yoink [--audio] [--res N] LINK")
	}
	tty := isTTY(stderr)
	if tty {
		fmt.Fprintln(stderr, "Looking at the link…")
	}
	// Ctrl-C cancels the download, as it would one run here: while the
	// core looks at the link too, once the download it starts is there.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	type started struct {
		r   ipc.YoinkResult
		err error
	}
	done := make(chan started, 1)
	go func() {
		r, err := yoinkCall(a, yoinkLook)
		done <- started{r, err}
	}()
	var st started
	select {
	case st = <-done:
	case <-stop:
		fmt.Fprintln(stderr, "Stopping…")
		st = <-done
		if st.err != nil {
			return exitCode{code: 130}
		}
		c, err := yoinkCall(ipc.YoinkArgs{Do: ipc.YoinkCancel, ID: st.r.Download.ID}, 15*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintln(stderr, c.Text)
		return exitCode{code: 130}
	}
	if st.err != nil {
		return st.err
	}
	d := *st.r.Download
	what := "“" + d.Title + "”"
	if d.Get != ipc.YoinkBest {
		what += " (" + d.Label + ")"
	}
	h, _ := os.UserHomeDir()
	fmt.Fprintf(stderr, "Yoinking %s into %s.\n", what, repo.Tilde(st.r.Folder, h))
	if detach {
		fmt.Fprintf(stderr, "It runs on, in the Yoink tab too: %s yoink cancel %s stops it.\n", brand.Slug, d.ID)
		return nil
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	shown := ""
	for {
		select {
		case <-stop:
			if tty {
				fmt.Fprint(stderr, "\r\033[K")
			}
			c, err := yoinkCall(ipc.YoinkArgs{Do: ipc.YoinkCancel, ID: d.ID}, 15*time.Second)
			if err != nil {
				return err
			}
			fmt.Fprintln(stderr, c.Text)
			return exitCode{code: 130}
		case <-tick.C:
		}
		l, err := yoinkCall(ipc.YoinkArgs{}, 10*time.Second)
		if err != nil {
			// The core restarts for an install now and then: the download
			// runs on, and the next look finds it.
			continue
		}
		for _, x := range l.Downloads {
			if x.ID == d.ID {
				d = x
			}
		}
		switch d.State {
		case ipc.YoinkDone:
			if tty {
				fmt.Fprint(stderr, "\r\033[K")
			}
			fmt.Fprintln(stdout, d.File)
			return nil
		case ipc.YoinkFailed:
			if tty {
				fmt.Fprint(stderr, "\r\033[K")
			}
			return errors.New(d.Error)
		case ipc.YoinkCanceled:
			if tty {
				fmt.Fprint(stderr, "\r\033[K")
			}
			return errors.New("it was canceled")
		}
		now := d.Step
		if d.State == ipc.YoinkDownloading {
			now += ": " + yoink.Progress(d)
		}
		switch {
		case tty:
			fmt.Fprint(stderr, "\r\033[K"+now)
		case d.Step != shown:
			// Not a terminal: a line each time the step changes.
			fmt.Fprintln(stderr, now)
		}
		shown = d.Step
	}
}

// writeChoices prints what a link offers, a choice a line, with the flag
// that takes it.
func writeChoices(w io.Writer, info ipc.YoinkInfo) {
	head := info.Title
	var about []string
	if info.Site != "" {
		about = append(about, info.Site)
	}
	if info.Uploader != "" {
		about = append(about, info.Uploader)
	}
	if info.Duration > 0 {
		s := int(info.Duration)
		about = append(about, fmt.Sprintf("%d:%02d", s/60, s%60))
	}
	if len(about) > 0 {
		head += " (" + strings.Join(about, ", ") + ")"
	}
	fmt.Fprintln(w, head)
	for _, c := range info.Choices {
		flag := ""
		switch {
		case c.Get == ipc.YoinkAudio:
			flag = "--audio"
		case c.Get != ipc.YoinkBest:
			flag = "--res " + strings.TrimSuffix(c.Get, "p")
		}
		size := ""
		if c.Size > 0 {
			size = "about " + yoink.Size(c.Size)
		}
		fmt.Fprintf(w, "  %-18s %-14s %s\n", c.Label, size, flag)
	}
}

// isTTY says whether w is a terminal, where a line of progress can be
// written over.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
