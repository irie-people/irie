package core

import (
	"errors"
	"reflect"
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
	if len(r.Code) != 6 || r.Name == "" || r.On || time.Until(r.Until) < 59*time.Minute {
		t.Errorf("pair = %+v", r)
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
