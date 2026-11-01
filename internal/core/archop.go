package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/arch"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// The arch op (DESIGN §4.11) shows every arch agent the way any client draws
// him: his name and work from his file (internal/arch), and from the table
// below what he does now, what a person can do with him, and his settings
// with their values. So a client draws a new arch agent with no change of
// its own: an arch agent is his file, his work in the core, and his row
// here.

// archNow is what an arch agent does now, as the op says it.
type archNow struct {
	on      bool
	status  string
	actions []ipc.ArchAction
}

// archEntry is an arch agent's row: his settings, in the order to show
// them (their keys are under arch.<name>), and what he does now. now holds
// no lock when it is called.
type archEntry struct {
	settings []ipc.ArchSetting
	now      func(c *Core) archNow
}

// archTable has a row for each arch agent the core runs.
var archTable = map[string]archEntry{
	"mikael": {
		settings: []ipc.ArchSetting{
			{Key: "arch.mikael.on", Kind: ipc.ArchBool, Label: "Mikael goes with every session"},
			{Key: "arch.mikael.door", Kind: ipc.ArchBool, Label: "He weighs calls that can’t be taken back", Help: "A small model weighs each such call in his name and stops one that would do evil, telling the agent why."},
			{Key: "arch.mikael.good", Kind: ipc.ArchLines, Label: "The good you want to see in the world", Help: "In your own words: every agent he goes with hears them."},
		},
		now: (*Core).mikaelNow,
	},
	"gabriel": {
		settings: []ipc.ArchSetting{
			{Key: "arch.gabriel.on", Kind: ipc.ArchBool, Label: "Gabriel gives your focus the machine first"},
			{Key: "arch.gabriel.outside", Kind: ipc.ArchBool, Label: "Slow your heavy apps outside Irie too"},
			{Key: "arch.gabriel.away", Kind: ipc.ArchDuration, Label: "You’re away after", Help: "With no keyboard or mouse this long, nothing is held back."},
			{Key: "arch.gabriel.word", Kind: ipc.ArchDuration, Label: "A session you message stays in your focus for"},
			{Key: "arch.gabriel.boost", Kind: ipc.ArchDuration, Label: "A boost lasts", Help: "Your focus first at once, busy or not; it changes only what runs first, never a clock or a fan."},
			{Key: "arch.gabriel.heavy", Kind: ipc.ArchNumber, Label: "An app outside Irie is heavy from (percent of a CPU)"},
			{Key: "arch.gabriel.spare", Kind: ipc.ArchLines, Label: "Apps Gabriel never slows", Help: "Calls, music, video and recording; an app that plays sound or holds a call is spared too."},
		},
		now: (*Core).gabrielNow,
	},
}

// archList is the arch op: every arch agent, by rank.
func (c *Core) archList() ipc.ArchResult {
	tree := config.Tree(*c.cfg())
	r := ipc.ArchResult{Agents: []ipc.ArchAgent{}}
	for _, a := range arch.All() {
		ag := ipc.ArchAgent{Name: a.Name, Nickname: a.Nickname, Icon: a.Icon, Title: a.Title, About: a.Description}
		if e, ok := archTable[a.Name]; ok {
			n := e.now(c)
			ag.On, ag.Status, ag.Actions = n.on, n.status, n.actions
			for _, s := range e.settings {
				s.Value = settingValue(tree, s.Key)
				ag.Settings = append(ag.Settings, s)
			}
		}
		r.Agents = append(r.Agents, ag)
	}
	return r
}

// settingValue is the value of a dotted config key in tree, as JSON.
func settingValue(tree map[string]any, key string) json.RawMessage {
	var v any = tree
	for _, k := range strings.Split(key, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func (c *Core) mikaelNow() archNow {
	m := c.cfg().Arch.Mikael
	n := archNow{on: m.On}
	switch {
	case !m.On:
		n.status = "Off: sessions start without him."
	case m.Door:
		n.status = "With every Claude Code session, weighing the calls that can’t be taken back."
	default:
		n.status = "With every Claude Code session; his door is off."
	}
	// Talking with him starts a session that runs as him (+mikael).
	if _, ok := c.cfg().Profiles["mikael"]; ok {
		args, _ := json.Marshal(ipc.SpawnArgs{Task: "Hello, Mikael. Say who you are in a line, and ask what I would like your help with.", Profile: "mikael", Name: "mikael"})
		n.actions = append(n.actions, ipc.ArchAction{Label: "Talk with Mikael", Op: ipc.OpSpawn, Args: args, Done: "Mikael is starting in a session of his own."})
	}
	return n
}

func (c *Core) gabrielNow() archNow {
	g := c.cfg().Arch.Gabriel
	on := g.On && proc.CanBackground && !gabrielOff()
	c.mu.Lock()
	st := c.gab
	held := 0
	for id, x := range c.sessions {
		if st.holding && x.demoted && x.track.priority == 0 && !st.focus[id] {
			held++
		}
	}
	c.mu.Unlock()
	n := archNow{on: on}
	boosting := time.Now().Before(st.boostUntil)
	switch {
	case !on:
		n.status = "Off: nothing is held back for your focus."
	case !st.present && !st.idleRead.IsZero():
		n.status = "You’re away, so nothing is held back."
	case st.holding:
		var what []string
		if held > 0 {
			what = append(what, counted(held, "session"))
		}
		if len(st.outside) > 0 {
			what = append(what, counted(len(st.outside), "app"))
		}
		focus := "your focus"
		if st.frontOK && st.front.Name != "" {
			focus = st.front.Name
		}
		if len(what) == 0 {
			n.status = "Giving " + focus + " the machine first; nothing needs holding back."
		} else {
			n.status = "Holding back " + strings.Join(what, " and ") + " for " + focus + "."
		}
	default:
		n.status = "Room to spare, so nothing is held back."
	}
	if boosting {
		n.status += " Boosting until " + st.boostUntil.Local().Format("15:04") + "."
	}
	if on {
		if boosting {
			n.actions = []ipc.ArchAction{{Label: "Stop the boost", Op: ipc.OpGabriel, Args: json.RawMessage(`{"stop":true}`), Done: "The boost is over."}}
		} else {
			n.actions = []ipc.ArchAction{{Label: "Boost for " + spokenFor(g.Boost), Op: ipc.OpGabriel, Args: json.RawMessage(`{"boost":true}`), Done: "Boosting your focus: the rest runs on the efficiency cores."}}
		}
	}
	return n
}

// counted is n things in words: "1 session", "3 sessions".
func counted(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// spokenFor is a duration in words, such as "1 hour" or "30 minutes".
func spokenFor(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m == 0:
		return counted(h, "hour")
	case h > 0:
		return fmt.Sprintf("%s %s", counted(h, "hour"), counted(m, "minute"))
	}
	return counted(int(d.Minutes()), "minute")
}
