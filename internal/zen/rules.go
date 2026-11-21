package zen

import (
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// The pace of zen's offers (zen.md §6, §7): gentle, so the path takes a few
// days and nothing comes while the person is busy with something else.
const (
	SettleIn = 2 * time.Minute  // zen is on this long before it offers a step
	Between  = 15 * time.Minute // since the last offer was answered
	PerDay   = 3                // steps a day, at most
	Typing   = 20 * time.Second // hands still this long: waiting, not typing
	Away     = 5 * time.Minute  // no key or mouse this long counts as a rest
	Snooze   = 10 * time.Minute // In 10 Minutes, for a rest
)

// Moment is what zen knows of the moment, which its offers depend on. It
// holds nothing about how the person feels: only time, and what the
// session on show is doing.
type Moment struct {
	Now      time.Time
	On       time.Duration // how long zen has been on
	Answered time.Time     // when the last offer was answered; zero for none
	Today    int           // steps answered today
	NeedsYou bool          // the session on show waits on the person
	Working  bool          // its agent is working
	Idle     time.Duration // since the last key or mouse movement
	AtDesk   time.Duration // at the computer, with no pause as long as Away
}

// Night says whether t is late at night, from 23:00 to 05:00 where the
// person is.
func Night(t time.Time) bool {
	h := t.Hour()
	return h >= 23 || h < 5
}

// NightOf names the night t falls in by the date of its evening, so 00:30
// belongs to the night before: a day runs from noon to noon.
func NightOf(t time.Time) string {
	return t.Add(-12 * time.Hour).Format(time.DateOnly)
}

// MayStep says whether zen may offer a step of its path at the moment m: it
// has been on a while, the last offer was a while ago, fewer than PerDay
// steps came today, nothing waits on the person, and they are waiting on
// the agent or resting their hands rather than typing, and it is not late
// at night, when zen says only that they can rest.
func MayStep(m Moment) bool {
	return m.On >= SettleIn && m.Now.Sub(m.Answered) >= Between && m.Today < PerDay &&
		!m.NeedsYou && (m.Working || m.Idle >= Typing) && !Night(m.Now)
}

// NightDue says whether zen says, late at night, that the person can rest:
// once a night, while p asks for it, and after a step or a rest offered a
// moment before was answered.
func NightDue(p ipc.ZenProfile, m Moment, saidOn string) bool {
	return p.Night && Night(m.Now) && saidOn != NightOf(m.Now) && m.On >= SettleIn
}

// RestDue says whether zen offers a rest at the moment m, with your zen p:
// a breath to follow, Back to It or In 10 Minutes (zen.md §7). The caller
// holds back a rest put off with In 10 Minutes until Snooze is up.
func RestDue(p ipc.ZenProfile, m Moment) bool {
	// TODO(human): when is a rest due? p.Breaks is the minutes chosen (0
	// for none), and m says how long they've been at the computer and what
	// the session on show is doing.
	return false
}

// RestOffer is the rest zen offers.
func RestOffer() ipc.ZenOffer {
	return ipc.ZenOffer{ID: ipc.ZenRest, Kind: ipc.ZenKindRest,
		Text: "You’ve been at it a while. Rest your eyes and follow the dot: breathe in as it rises, out as it falls.",
		Yes:  "Back to It", No: "In 10 Minutes"}
}

// NightOffer is what zen says late at night.
func NightOffer() ipc.ZenOffer {
	return ipc.ZenOffer{ID: ipc.ZenNight, Kind: ipc.ZenKindNight,
		Text: "It’s late. Your agents can carry on without you, and rest is part of the work.",
		Yes:  "Good Night", No: "A Little Longer"}
}

// GentleNote is what the agent of each session zen shows is told, when
// your zen asks for a gentle agent (zen.md §8). It says only that zen is
// on, never how the person seems: it goes to the model's provider with the
// conversation.
func GentleNote() string {
	bin := "`\"$" + brand.EnvPrefix + "_BIN\" ctl zen"
	return "[" + brand.Name + "] The user is in zen, a calm, minimal view where their wellbeing comes first. " +
		"Until a note says zen ended: keep replies short, plain and kind; ask one question at a time; no urgency, alarm or pressure; " +
		"say plainly when something is done or stuck. If they seem tired or stressed, it is fine to suggest a pause: the work can wait. " +
		"At their word only, you can change their zen: " + bin + "` shows it and " + bin + " set KEY=VALUE` changes it."
}

// LeftNote is what each agent told of zen is told when it ends.
func LeftNote() string {
	return "[" + brand.Name + "] The user left zen; carry on as usual."
}
