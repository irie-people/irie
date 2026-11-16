package core

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	irie "github.com/irie-people/irie/core"
	"github.com/irie-people/irie/internal/ipc"
)

// A link clicked in a terminal opens where ui.terminal_links says (docs/
// MAC.md, Links in the terminal): in the session's own panes, a page on a
// Browser card and a file of text on its Code card at the line it names;
// or in the computer's own apps, the default browser and a file's own app.
// A click that asks for the other place (⇧⌘-click) opens it there. What a
// card can't show, such as a mail address, a folder or a picture, opens in
// the computer's own app either way. The client only opens what the answer
// names, so every client opens a link alike.

// linkOp answers ipc.OpLink. A session on a peer has its files there, so
// only its addresses open here.
func (c *Core) linkOp(a ipc.LinkArgs) (ipc.LinkResult, error) {
	link := strings.TrimSpace(a.Link)
	switch {
	case link == "":
		return ipc.LinkResult{}, ipc.Errorf(ipc.CodeBadArgs, "No link: give the link that was clicked.")
	case len(link) > ipc.LinkMax:
		return ipc.LinkResult{}, ipc.Errorf(ipc.CodeBadArgs, "That link is over %d KiB long: no terminal shows one so long.", ipc.LinkMax>>10)
	}
	at := linkPlace{panes: a.Session != "" && (c.cfg().UI.TerminalLinks == "rolodex") != a.Other}
	at.home, _ = os.UserHomeDir()
	if a.Session != "" {
		c.mu.Lock()
		x := c.sessions[a.Session]
		known := x != nil
		if known {
			at.folder = cmp.Or(x.worktree, x.cwd)
		} else if p, rest, ok := c.splitPeerID(a.Session); ok {
			var row ipc.Session
			if row, known = p.rows[rest]; known {
				at.peer, at.name = p.name, row.Name
			}
		}
		c.mu.Unlock()
		if !known {
			return ipc.LinkResult{}, ipc.Errorf(ipc.CodeNotFound, "No session %q.", a.Session)
		}
	}
	return resolveLink(link, at)
}

// linkPlace is where a link was clicked.
type linkPlace struct {
	// panes: it opens in the session's panes, else in the computer's own
	// apps.
	panes bool
	// folder is the session's, which a relative path is in, and home the
	// user's, which ~ is.
	folder, home string
	// peer is the machine the session runs on, and name its name, when
	// that is not this one.
	peer, name string
}

var (
	// linkScheme is a link's scheme, as mailto: or ssh://.
	linkScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)
	// linkHost is a web address written without its scheme, as
	// github.com/alice/repo: a host whose last label is letters, maybe a
	// port, and a path. A bare name such as notes.md is a file's, though
	// md is a country's domain too. linkLocal is one of this machine's
	// own, as localhost:3000, which a browser card's address bar also
	// takes without http://.
	linkHost  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.[A-Za-z]{2,}(?::[0-9]{1,5})?/`)
	linkLocal = regexp.MustCompile(`^(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::[0-9]{1,5})?(?:/|$)`)
	// linkLines is a range of lines after a file's name, as :12-20, or a
	// line as a web page of code names it, as #L12 or #L12-L20: the first
	// line is the one to go to.
	linkLines = regexp.MustCompile(`(?::([0-9]+)-[0-9]+|#L([0-9]+)(?:-L?[0-9]+)?)$`)
)

// resolveLink says where a link opens and as what.
func resolveLink(link string, at linkPlace) (ipc.LinkResult, error) {
	page := func(u string) ipc.LinkResult {
		if at.panes {
			return ipc.LinkResult{In: ipc.LinkInCard, Kind: "browser", URL: u}
		}
		return ipc.LinkResult{In: ipc.LinkInSystem, URL: u}
	}
	system := ipc.LinkResult{In: ipc.LinkInSystem, URL: link}
	if u, err := url.Parse(link); err == nil && u.Scheme != "" {
		switch s := strings.ToLower(u.Scheme); {
		case (s == "http" || s == "https") && u.Host != "":
			return page(link), nil
		case s == "file" && (u.Host == "" || u.Host == "localhost"):
			if at.peer != "" {
				return ipc.LinkResult{}, onPeer(at)
			}
			return openFile(u.Path, at)
		case s == "file":
			return system, nil
		}
	}
	if at.peer != "" {
		// Its addresses open here; its files are on the peer.
		switch {
		case linkLocal.MatchString(link):
			return page("http://" + link), nil
		case linkScheme.MatchString(link):
			return system, nil
		case linkHost.MatchString(link):
			return page("https://" + link), nil
		}
		return ipc.LinkResult{}, onPeer(at)
	}
	// A file's name may look like a scheme, as pane.go:12 does: a link
	// with one opens in the app for its scheme unless it names a file.
	res, err := openFile(link, at)
	if err == nil || !errNotFound(err) {
		return res, err
	}
	switch {
	case linkLocal.MatchString(link):
		return page("http://" + link), nil
	case linkScheme.MatchString(link):
		return system, nil
	case linkHost.MatchString(link):
		return page("https://" + link), nil
	}
	return ipc.LinkResult{}, err
}

// onPeer is why a peer's session's file can't open here.
func onPeer(at linkPlace) error {
	return ipc.Errorf(ipc.CodeBadArgs, "That file is on %s, where %s runs: a Terminal card beside it reaches it there.", at.peer, cmp.Or(at.name, "the session"))
}

// errNotFound says whether err is a link's file that isn't there.
func errNotFound(err error) bool {
	var e *ipc.Error
	return errors.As(err, &e) && e.Code == ipc.CodeNotFound
}

// openFile says where a file's path opens: a file of text on the Code
// card, at the line it names, and anything else in its own app. A path
// is the session's folder's unless it is absolute or starts with ~ or
// $HOME, and may end in :LINE, :LINE:COL, a range of lines, or a
// sentence's last stop.
func openFile(name string, at linkPlace) (ipc.LinkResult, error) {
	for _, v := range []string{"$HOME", "${HOME}"} {
		if name == v || strings.HasPrefix(name, v+"/") {
			name = "~" + strings.TrimPrefix(name, v)
		}
	}
	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}
	file, line, col := splitLink(irie.OpenPath(name, at.folder, at.home), exists)
	// A sentence's stop, or what came after the path, that the terminal
	// took as part of it.
	for !exists(file) && line == 0 && len(file) > 1 && strings.ContainsAny(file[len(file)-1:], ".;!?#") {
		file, line, col = splitLink(file[:len(file)-1], exists)
	}
	fi, err := os.Stat(file)
	switch {
	case err != nil:
		return ipc.LinkResult{}, ipc.Errorf(ipc.CodeNotFound, "No file at %s.", shownPath(file, at))
	case fi.IsDir():
		return ipc.LinkResult{In: ipc.LinkInSystem, URL: fileURL(file)}, nil
	case !fi.Mode().IsRegular():
		return ipc.LinkResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s is not a file or a folder.", shownPath(file, at))
	case at.panes && fi.Size() <= ipc.CardFileMax && isText(file):
		return ipc.LinkResult{In: ipc.LinkInCard, Kind: "code", Path: file, Line: line, Col: col}, nil
	}
	return ipc.LinkResult{In: ipc.LinkInSystem, URL: fileURL(file)}, nil
}

// splitLink splits a trailing :LINE, :LINE:COL or range of lines off a
// file's name, unless the name as given exists.
func splitLink(name string, exists func(string) bool) (file string, line, col int) {
	if m := linkLines.FindStringSubmatchIndex(name); m != nil && !exists(name) {
		g := 2 // :12-20, else #L12
		if m[g] < 0 {
			g = 4
		}
		if n, err := strconv.Atoi(name[m[g]:m[g+1]]); err == nil && n >= 1 && exists(name[:m[0]]) {
			return name[:m[0]], n, 0
		}
	}
	return irie.SplitFilePos(name, exists)
}

// isText says whether a file is one a code card edits: UTF-8 with no NUL
// early on, as the Mac app's own test (CodeDocument.swift) has it.
func isText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ipc.CardFileMax+1))
	if err != nil || len(data) > ipc.CardFileMax {
		return false
	}
	return !bytes.Contains(data[:min(len(data), 8000)], []byte{0}) && utf8.Valid(data)
}

// fileURL is a file's path as a file:// URL.
func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// shownPath is a file's path as its message gives it: within the session's
// folder from there, and ~ for home.
func shownPath(file string, at linkPlace) string {
	if at.folder != "" {
		if rel, err := filepath.Rel(at.folder, file); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return rel
		}
	}
	if at.home != "" && (file == at.home || strings.HasPrefix(file, at.home+"/")) {
		return "~" + strings.TrimPrefix(file, at.home)
	}
	return file
}
