//go:build !nojammin

package jammin

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/ipc"
)

// Text says what plays on each source, a line each, as :jammin answers in
// a client with no Jammin tab.
func Text(all []ipc.JamSource) string {
	var lines []string
	for _, st := range all {
		lines = append(lines, Line(st))
	}
	return strings.Join(lines, "\n")
}

// Line is one source in a line: what plays on it, or what it lacks.
func Line(st ipc.JamSource) string {
	switch st.State {
	case ipc.JamReady:
	case ipc.JamLinking:
		return st.Title + ": signing in, on Spotify's page."
	case ipc.JamOff:
		return st.Title + ": off."
	default:
		return st.Title + ": " + st.Problem
	}
	n := st.Now
	if n == nil || n.Title == "" {
		return st.Title + ": nothing plays."
	}
	what := "playing"
	if !n.Playing {
		what = "paused on"
	}
	line := fmt.Sprintf("%s: %s %s", st.Title, what, item(n))
	if d := n.Device; d != nil && d.Name != "" && !d.Here {
		line += " on " + d.Name
	}
	return line + "."
}

// item is what plays in words: “Title” by its artists.
func item(n *ipc.JamNow) string {
	s := "“" + n.Title + "”"
	if len(n.Artists) > 0 {
		s += " by " + strings.Join(n.Artists, ", ")
	}
	return s
}

// didText says what a control did, by the source's look after it.
func didText(a ipc.JamminArgs, st ipc.JamSource) string {
	switch a.Do {
	case ipc.JamPause:
		return "Paused " + st.Title + "."
	case ipc.JamSeek:
		return fmt.Sprintf("%s: from %d:%02d.", st.Title, a.Position/60000, a.Position/1000%60)
	case ipc.JamVolume:
		return fmt.Sprintf("%s: volume %d.", st.Title, *a.Volume)
	case ipc.JamShuffle:
		if *a.On {
			return st.Title + ": shuffle on."
		}
		return st.Title + ": shuffle off."
	case ipc.JamRepeat:
		return st.Title + ": repeat " + a.Repeat + "."
	case ipc.JamMove:
		return st.Title + ": moved to the device you picked."
	}
	if n := st.Now; n != nil && n.Title != "" && a.Do != ipc.JamToggle {
		return st.Title + ": " + item(n) + "."
	}
	return Line(st)
}

// itemsText says what lists, search or devices found, in a few lines.
func itemsText(title, do string, r ipc.JamminResult) string {
	var b strings.Builder
	switch do {
	case ipc.JamLists:
		fmt.Fprintf(&b, "%s: %d playlists.", title, len(r.Lists))
		for i, it := range r.Lists {
			if i == 10 {
				fmt.Fprintf(&b, "\nAnd %d more.", len(r.Lists)-10)
				break
			}
			b.WriteString("\n" + it.Title)
		}
	case ipc.JamSearch:
		if len(r.Found) == 0 {
			return title + ": nothing found."
		}
		fmt.Fprintf(&b, "%s found %d.", title, len(r.Found))
		for i, it := range r.Found {
			if i == 10 {
				fmt.Fprintf(&b, "\nAnd %d more.", len(r.Found)-10)
				break
			}
			b.WriteString("\n" + it.Kind + ": " + it.Title)
			if it.By != "" {
				b.WriteString(" by " + it.By)
			}
		}
	case ipc.JamDevices:
		if len(r.Devices) == 0 {
			return title + ": no device of yours is open. Open Spotify on one."
		}
		fmt.Fprintf(&b, "%s: %d devices.", title, len(r.Devices))
		for _, d := range r.Devices {
			b.WriteString("\n" + d.Name)
			if d.Active {
				b.WriteString(" (playing)")
			}
		}
	}
	return b.String()
}

// sentence is s as a sentence: a capital first, and a stop last.
func sentence(s string) string {
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
