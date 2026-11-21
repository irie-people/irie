package zen

import (
	"cmp"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/irie-people/irie/internal/ipc"
)

//go:embed path.toml
var pathFile string

// MaxStepText is how long a step may be, in characters: a bubble at the
// foot of the terminal holds a sentence or two.
const MaxStepText = 300

// Step is one step of zen's path (path.toml).
type Step struct {
	ID   string `toml:"id"`
	Text string `toml:"text"`
	Yes  string `toml:"yes"`  // the first button's words, for a lesson or a show
	Show string `toml:"show"` // what the first button opens: ipc.ZenShowHistory
	Link string `toml:"link"` // a page the text names
	// A change: rests every Breaks minutes, or text Grow points larger.
	Breaks int    `toml:"breaks"`
	Grow   int    `toml:"grow"`
	Keep   string `toml:"keep"` // what the change on trial asks
	Why    string `toml:"why"`  // the kept change's version's why
}

// Change says whether the step changes your zen, rather than teaching.
func (s Step) Change() bool { return s.Breaks != 0 || s.Grow != 0 }

// Apply is p with the step's change, in a window whose text is windowText
// points (13 when the app hasn't said).
func (s Step) Apply(p ipc.ZenProfile, windowText int) ipc.ZenProfile {
	if s.Breaks != 0 {
		p.Breaks = s.Breaks
	}
	if s.Grow != 0 {
		from := cmp.Or(p.TextSize, windowText, 13)
		p.TextSize = min(max(from+s.Grow, MinText), MaxText)
	}
	return p
}

// Offer is the step as zen offers it.
func (s Step) Offer() ipc.ZenOffer {
	o := ipc.ZenOffer{ID: s.ID, Text: s.Text, Show: s.Show, Link: s.Link}
	switch {
	case s.Change():
		o.Kind, o.Yes, o.No = ipc.ZenKindTry, "Try It", "Not Now"
	case s.Show != "":
		o.Kind, o.Yes, o.No = ipc.ZenKindLearn, cmp.Or(s.Yes, "Show Me"), "Not Now"
	default:
		o.Kind, o.Yes = ipc.ZenKindLearn, cmp.Or(s.Yes, "Got It")
	}
	return o
}

// KeepOffer is what zen asks while the step's change is on trial.
func (s Step) KeepOffer() ipc.ZenOffer {
	return ipc.ZenOffer{ID: s.ID, Kind: ipc.ZenKindKeep, Text: s.Keep, Yes: "Keep", No: "Go Back"}
}

var path = mustPath(pathFile)

func mustPath(s string) []Step {
	steps, err := parsePath(s)
	if err != nil {
		panic(err)
	}
	return steps
}

func parsePath(s string) ([]Step, error) {
	var f struct {
		Step []Step `toml:"step"`
	}
	md, err := toml.Decode(s, &f)
	if err != nil {
		return nil, fmt.Errorf("path.toml: %w", err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("path.toml: unknown key %s", keys[0])
	}
	seen := map[string]bool{}
	for _, st := range f.Step {
		switch {
		case st.ID == "" || strings.ContainsAny(st.ID, " \t\n") || st.ID == ipc.ZenRest || st.ID == ipc.ZenNight:
			return nil, fmt.Errorf("path.toml: step %q: an id is one word, and not %q or %q", st.ID, ipc.ZenRest, ipc.ZenNight)
		case seen[st.ID]:
			return nil, fmt.Errorf("path.toml: step %q comes twice", st.ID)
		case st.Text == "" || len([]rune(st.Text)) > MaxStepText:
			return nil, fmt.Errorf("path.toml: step %q: its text must be 1 to %d characters", st.ID, MaxStepText)
		case st.Change() && (st.Keep == "" || st.Why == ""):
			return nil, fmt.Errorf("path.toml: step %q changes your zen, so it needs keep and why", st.ID)
		case st.Breaks != 0 && (st.Breaks < MinBreaks || st.Breaks > MaxBreaks):
			return nil, fmt.Errorf("path.toml: step %q: breaks must be %d to %d minutes", st.ID, MinBreaks, MaxBreaks)
		case st.Show != "" && st.Show != ipc.ZenShowHistory:
			return nil, fmt.Errorf("path.toml: step %q: show must be %q", st.ID, ipc.ZenShowHistory)
		case st.Link != "" && !strings.HasPrefix(st.Link, "https://"):
			return nil, fmt.Errorf("path.toml: step %q: a link is https", st.ID)
		}
		seen[st.ID] = true
	}
	return f.Step, nil
}

// Path is zen's path, in order.
func Path() []Step { return append([]Step(nil), path...) }

// StepByID is the step with that id.
func StepByID(id string) (Step, bool) {
	for _, s := range path {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// How a step was answered, Answered's How.
const (
	Done  = "done"  // read, kept, or tried and gone back from: never offered again
	Later = "later" // Not Now: offered again after the fresh steps, once
)

// Answered is how a step was answered, and when.
type Answered struct {
	How   string    `json:"how"`
	At    time.Time `json:"at"`
	Times int       `json:"times,omitempty"` // how often Not Now
}

// Progress is how far along the path the person is: each step answered,
// by id. It records nothing else.
type Progress map[string]Answered

// Today is how many steps were answered on now's day.
func (g Progress) Today(now time.Time) int {
	y, m, d := now.Date()
	n := 0
	for _, a := range g {
		if ay, am, ad := a.At.In(now.Location()).Date(); ay == y && am == m && ad == d {
			n++
		}
	}
	return n
}

// Next is the step to offer next for your zen p: the first not yet
// answered, then those put off once, in order. A change that would change
// nothing is passed over, and so is a step put off twice: there is none
// when every step is done or dropped.
func Next(p ipc.ZenProfile, windowText int, g Progress) (Step, bool) {
	for _, again := range []bool{false, true} {
		for _, s := range path {
			a, ok := g[s.ID]
			fresh := !ok
			putOff := ok && a.How == Later && a.Times < 2
			if again && putOff || !again && fresh {
				if s.Change() && s.Apply(p, windowText) == p {
					continue
				}
				return s, true
			}
		}
	}
	return Step{}, false
}
