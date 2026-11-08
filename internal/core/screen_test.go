package core

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestPromptOptionsReadsTheMenuAtTheBottom(t *testing.T) {
	opt := func(key, label string, selected bool) ipc.ScreenOption {
		return ipc.ScreenOption{Key: key, Label: label, Selected: selected}
	}
	for _, tc := range []struct {
		name   string
		screen string
		want   []ipc.ScreenOption
	}{
		{"a permission in a box", `
● Bash(rm -rf build)
╭──────────────────────────────────────────────────────╮
│ Bash command                                         │
│                                                      │
│   rm -rf build                                       │
│   Remove the build directory                         │
│                                                      │
│ Do you want to proceed?                              │
│ ❯ 1. Yes                                             │
│   2. Yes, and don't ask again for rm commands        │
│   3. No, and tell Claude what to do differently (esc)│
╰──────────────────────────────────────────────────────╯`,
			[]ipc.ScreenOption{opt("1", "Yes", true), opt("2", "Yes, and don't ask again for rm commands", false), opt("3", "No, and tell Claude what to do differently (esc)", false)}},
		{"a question whose choices have descriptions", `
 ☐ Colour

Which colour should the button be?

❯ 1. Green
     Matches the palette's accent
  2. Red
     Stands out, but red means it needs you
  3. Type something.

Enter to select · ↑/↓ to navigate · Esc to cancel`,
			[]ipc.ScreenOption{opt("1", "Green", true), opt("2", "Red", false), opt("3", "Type something.", false)}},
		{"a plan, the second choice selected", `
Would you like to proceed?

  1. Yes, and auto-accept edits
› 2. Yes, and manually approve edits
  3. No, keep planning`,
			[]ipc.ScreenOption{opt("1", "Yes, and auto-accept edits", false), opt("2", "Yes, and manually approve edits", true), opt("3", "No, keep planning", false)}},
		{"a numbered list in the reply, far above the bottom", `
The plan:
1. Read the parser
2. Add the tests
` + strings.Repeat("\nmore of the reply", 14) + `
> `,
			[]ipc.ScreenOption{}},
		{"choices that never reach 1", `
  2. Yes, and don't ask again
  3. No`,
			[]ipc.ScreenOption{}},
		{"an empty screen", ``, []ipc.ScreenOption{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := promptOptions(screenLines(tc.screen))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("promptOptions =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestScreenLinesDropsWhatIsNotWritten(t *testing.T) {
	got := screenLines("one   \n\ttwo\t\n\nPane is dead (status 0, Fri)\n\n\n")
	if want := []string{"one", "\ttwo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("screenLines = %q, want %q", got, want)
	}
}

func TestAPromptIsReadAndAnsweredWithAKey(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,permission,stop")
	c.waitState(s.ID, ipc.StateNeedsYou)

	var r ipc.ScreenResult
	testutil.Eventually(t, 5*time.Second, "the screen never showed the menu", func() bool {
		if err := c.call(ipc.OpScreen, ipc.ScreenArgs{Session: s.ID}, &r); err != nil {
			t.Fatal(err)
		}
		return len(r.Options) == 3
	})
	if r.Options[0] != (ipc.ScreenOption{Key: "1", Label: "Yes", Selected: true}) || r.Options[2].Label != "No, and tell Claude what to do differently" {
		t.Errorf("options = %+v", r.Options)
	}
	if !strings.Contains(strings.Join(r.Lines, "\n"), "Do you want to proceed?") {
		t.Errorf("lines = %q", r.Lines)
	}

	// The fake agent takes a line; Claude Code takes the digit alone.
	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: s.ID, Keys: []string{"1", "Enter"}}, nil); err != nil {
		t.Fatal(err)
	}
	c.waitState(s.ID, ipc.StateDone)
	// No prompt, no choices, though the menu is still on the screen.
	if err := c.call(ipc.OpScreen, ipc.ScreenArgs{Session: s.ID}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Options) != 0 || len(r.Lines) == 0 {
		t.Errorf("after the answer: options %+v, %d lines", r.Options, len(r.Lines))
	}
}

func TestTheScreenStreamsWhatTheTerminalShowsAsItChanges(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,permission,stop")
	c.waitState(s.ID, ipc.StateNeedsYou)

	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{Session: s.ID, Screen: true})
	if err != nil {
		t.Fatal(err)
	}
	// lines is the terminal as a client keeps it, frame by frame.
	var lines []string
	next := func(what string, ok func(ipc.Screen) bool) ipc.Screen {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case f, open := <-frames:
				if !open {
					t.Fatalf("%s: the subscription ended", what)
				}
				if f.T != ipc.FrameScreen || f.Screen == nil {
					continue
				}
				sc := *f.Screen
				if sc.Session != s.ID || sc.Cols == 0 || sc.Rows == 0 {
					t.Fatalf("%s: frame %+v", what, sc)
				}
				switch {
				case sc.Reset:
					if len(sc.Lines) != sc.Rows || len(sc.Set) != 0 {
						t.Fatalf("%s: a reset of %d lines for %d rows: %+v", what, len(sc.Lines), sc.Rows, sc)
					}
					lines = slices.Clone(sc.Lines)
				case lines == nil:
					t.Fatalf("%s: the first frame was no reset: %+v", what, sc)
				default:
					if sc.Lines != nil {
						t.Fatalf("%s: a later frame carried every line: %+v", what, sc)
					}
					for _, l := range sc.Set {
						lines[l.N] = l.Text
					}
				}
				if ok(sc) {
					return sc
				}
			case <-deadline:
				t.Fatalf("no screen frame: %s", what)
			}
		}
	}
	shows := func(text string) bool { return strings.Contains(plainText(strings.Join(lines, "\n")), text) }
	menu := next("the menu", func(sc ipc.Screen) bool { return len(sc.Options) == 3 && shows("Do you want to proceed?") })
	if menu.Options[0] != (ipc.ScreenOption{Key: "1", Label: "Yes", Selected: true}) {
		t.Errorf("options = %+v", menu.Options)
	}

	// The answer, pressed as keys, shows soon, and the choices go.
	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: s.ID, Keys: []string{"1", "Enter"}}, nil); err != nil {
		t.Fatal(err)
	}
	c.waitState(s.ID, ipc.StateDone)
	if after := next("the answer", func(sc ipc.Screen) bool { return len(sc.Options) == 0 }); after.Reset {
		t.Errorf("the size never changed, yet the frame was a reset: %+v", after)
	}

	// Nobody watches once the subscription ends, and nothing reads it.
	cl.Close()
	reads := func() uint64 {
		var p ipc.PerfResult
		if err := c.call(ipc.OpPerf, nil, &p); err != nil {
			t.Fatal(err)
		}
		return p.Counts["screen.read"]
	}
	testutil.Eventually(t, 10*time.Second, "the screen was still read with nobody watching", func() bool {
		before := reads()
		time.Sleep(3 * screenEvery)
		return reads() == before
	})
}

func TestParseScreenReadsTheSizeTheCursorAndEveryRow(t *testing.T) {
	sc, ok := parseScreen("5 3 1 2 1\n\x1b[1mhi\x1b[0m   \nthere\n")
	if !ok || sc.Cols != 5 || sc.Rows != 3 || sc.Cursor == nil || *sc.Cursor != (ipc.ScreenCursor{X: 1, Y: 2}) {
		t.Fatalf("parseScreen = %+v, %v", sc, ok)
	}
	if want := []string{"\x1b[1mhi\x1b[0m   ", "there", ""}; !reflect.DeepEqual(sc.Lines, want) {
		t.Errorf("lines = %q, want %q", sc.Lines, want)
	}
	if sc, _ := parseScreen("5 2 0 0 0\none\ntwo\nthree\n"); sc.Cursor != nil || len(sc.Lines) != 2 {
		t.Errorf("a hidden cursor and a line too many: %+v", sc)
	}
	for _, out := range []string{"", "5 3\nhi\n", "5 x 0 0 1\nhi\n", "0 3 0 0 1\n"} {
		if _, ok := parseScreen(out); ok {
			t.Errorf("parseScreen(%q) took it", out)
		}
	}
}

func TestPlainTextIsWhatAPersonReads(t *testing.T) {
	in := "\x1b[38;5;174m❯\x1b[39m \x1b[48;2;1;2;3mgo\x1b[0m \x1b]8;;https://example.com\x07link\x1b]8;;\x07 \x1b(Bok"
	if got := plainText(in); got != "❯ go link ok" {
		t.Errorf("plainText = %q", got)
	}
}

func TestKeysTypesTextWithoutEnter(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,ask,stop")
	c.waitState(s.ID, ipc.StateNeedsYou)

	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: s.ID, Text: "green, like the accent"}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if row, _ := c.row(s.ID); row.State != ipc.StateNeedsYou {
		t.Fatalf("text alone answered the question: state %s", row.State)
	}
	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: s.ID, Keys: []string{"Enter"}}, nil); err != nil {
		t.Fatal(err)
	}
	c.waitState(s.ID, ipc.StateDone)
}

func TestKeysRefusesWhatIsNotAKey(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,stop")
	c.waitState(s.ID, ipc.StateDone)

	for _, a := range []ipc.KeysArgs{
		{Session: s.ID},
		{Session: s.ID, Keys: []string{"C-c"}},
		{Session: s.ID, Keys: []string{";"}},
		{Session: s.ID, Keys: []string{"Enter; kill-server"}},
		{Session: s.ID, Text: "two\nlines"},
		{Session: s.ID, Text: "bell\a"},
		{Session: s.ID, Keys: make([]string, maxKeys+1)},
	} {
		var e *ipc.Error
		if err := c.call(ipc.OpKeys, a, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadArgs {
			t.Errorf("keys %+v: err = %v, want bad_args", a, err)
		}
	}
	var e *ipc.Error
	if err := c.call(ipc.OpKeys, ipc.KeysArgs{Session: "s000000000000", Keys: []string{"1"}}, nil); !errors.As(err, &e) || e.Code != ipc.CodeNotFound {
		t.Errorf("keys to no session: err = %v, want not_found", err)
	}
	if err := c.call(ipc.OpScreen, ipc.ScreenArgs{Session: "s000000000000"}, nil); !errors.As(err, &e) || e.Code != ipc.CodeNotFound {
		t.Errorf("screen of no session: err = %v, want not_found", err)
	}
}

func TestPairOpensACodeTheGatewayTakes(t *testing.T) {
	c := setup(t)
	var r ipc.PairResult
	if err := c.call(ipc.OpPair, ipc.PairArgs{Minutes: 60}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Code) != 6 || r.Name == "" || r.On || time.Until(r.Until) < 59*time.Minute || len(r.Fingerprint) != 23 {
		t.Errorf("pair = %+v", r)
	}
	if len(r.URLs) > 0 && (!strings.HasPrefix(r.Link, "irie://pair?") || !strings.Contains(r.Link, "c="+r.Code)) {
		t.Errorf("link %q", r.Link)
	}
	for _, u := range r.URLs {
		if !strings.HasPrefix(u, "http://") || !strings.HasSuffix(u, ":7386/") {
			t.Errorf("setup URL %q", u)
		}
	}
	var e *ipc.Error
	if err := c.call(ipc.OpPair, ipc.PairArgs{Minutes: -1}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadArgs {
		t.Errorf("a negative life: err = %v", err)
	}
}
