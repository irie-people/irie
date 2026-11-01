// Package statusline is `irie statusline`, the status line command Irie
// gives Claude Code sessions (DESIGN §6.3). Claude Code runs it with the
// session's status as JSON on stdin; it forwards that to the core, which
// learns the session's cost, context and the plan's use from it and answers
// with the session's state, then runs the user's own status line on the
// same input, or prints a short line of its own. When Irie is down the
// user's status line still runs.
package statusline

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/irie-people/irie/internal/ipc"
)

// Window is one window of a plan's use, such as the five hours.
type Window = ipc.Window

// Usage is what a status update says about a session and its plan.
type Usage struct {
	Model       string   `json:"model,omitempty"`        // model.id
	ModelName   string   `json:"model_name,omitempty"`   // model.display_name
	CostUSD     *float64 `json:"cost_usd,omitempty"`     // cost.total_cost_usd, at list price
	Context     *float64 `json:"context,omitempty"`      // context_window.used_percentage
	ContextSize int      `json:"context_size,omitempty"` // context_window.context_window_size
	FiveHour    *Window  `json:"five_hour,omitempty"`    // rate_limits.five_hour, on a Pro or Max plan
	SevenDay    *Window  `json:"seven_day,omitempty"`    // rate_limits.seven_day
}

// Parse reads the fields Irie uses from Claude Code's status line JSON.
// Absent or null fields stay unset.
func Parse(raw []byte) (Usage, error) {
	var s struct {
		Model *struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Cost *struct {
			TotalCostUSD *float64 `json:"total_cost_usd"`
		} `json:"cost"`
		ContextWindow *struct {
			UsedPercentage    *float64 `json:"used_percentage"`
			ContextWindowSize int      `json:"context_window_size"`
		} `json:"context_window"`
		RateLimits *struct {
			FiveHour *window `json:"five_hour"`
			SevenDay *window `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Usage{}, fmt.Errorf("status line: %w", err)
	}
	var u Usage
	if s.Model != nil {
		u.Model, u.ModelName = s.Model.ID, s.Model.DisplayName
	}
	if s.Cost != nil {
		u.CostUSD = s.Cost.TotalCostUSD
	}
	if s.ContextWindow != nil {
		u.Context, u.ContextSize = s.ContextWindow.UsedPercentage, s.ContextWindow.ContextWindowSize
	}
	if s.RateLimits != nil {
		u.FiveHour, u.SevenDay = s.RateLimits.FiveHour.get(), s.RateLimits.SevenDay.get()
	}
	return u, nil
}

type window struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       float64  `json:"resets_at"` // Unix seconds
}

func (w *window) get() *Window {
	if w == nil || w.UsedPercentage == nil {
		return nil
	}
	out := &Window{Used: *w.UsedPercentage}
	if w.ResetsAt > 0 {
		sec, frac := math.Modf(w.ResetsAt)
		out.ResetsAt = time.Unix(int64(sec), int64(frac*1e9)).UTC()
	}
	return out
}

// Line is Irie's own status line, for a user who has none: the session's
// state when the core gave it, then what the session has cost, how full its
// context is and how much of the plan is used, such as
// "✓ done · $1.23 · ctx ▰▰▰▱▱▱▱▱ 41% · 5h 62% · 7d 18%". The state has the
// sidebar's marks and colours (DESIGN §5.3); the rest is faint until a
// share runs high. While the session works, or waits on its helpers, the
// state and the faint parts take its own colour instead, the one its card
// has in the Mac app. Parts
// drop from the end to fit in width columns; 0 is no limit.
func Line(u Usage, st *ipc.StatusReply, width int) string {
	var parts []seg
	quiet := faint
	if st != nil {
		own := rgb(st.Color)
		if own != "" {
			quiet = own
		}
		if s := state(*st, own); s.width > 0 {
			parts = append(parts, s)
		}
	}
	if u.CostUSD != nil && *u.CostUSD >= 0.005 {
		var s seg
		s.add(quiet, fmt.Sprintf("$%.2f", *u.CostUSD))
		parts = append(parts, s)
	}
	if u.Context != nil {
		parts = append(parts, share("ctx", *u.Context, true, quiet))
	}
	if u.FiveHour != nil {
		parts = append(parts, share("5h", u.FiveHour.Used, false, quiet))
	}
	if u.SevenDay != nil {
		parts = append(parts, share("7d", u.SevenDay.Used, false, quiet))
	}
	for width > 0 && len(parts) > 1 && lineWidth(parts) > width {
		parts = parts[:len(parts)-1]
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(paint(quiet, sep))
		}
		b.WriteString(p.text)
	}
	return b.String()
}

// state leads the line with the session's state, working in own, the
// session's colour, when it has one. A turn that ended while its helpers or
// teammates still work is not done: the line says what it waits on, as its
// card does, in its colour too. A parked session is out of the way, so its
// turn that is over is quiet and not to verify, as the sidebars count it.
func state(r ipc.StatusReply, own string) seg {
	var s seg
	n := len(r.Subagents)
	switch r.State {
	case ipc.StateNeedsYou:
		who := ""
		for _, sa := range r.Subagents {
			if sa.NeedsYou {
				who = cmp.Or(sa.Type, "a helper") + " "
				break
			}
		}
		s.add(red, "● "+who+"needs you")
	case ipc.StateStalled:
		s.add(red, "● stalled")
	case ipc.StateStarting:
		s.add("", "◌ starting")
	case ipc.StateWorking:
		if n == 0 {
			s.add(own, "◐ working")
		} else {
			s.add(own, "◐ working + "+helpers(n))
		}
	case ipc.StateDone, ipc.StateIdle:
		// What it waits on: a core from before Subs sends only the helpers.
		words := ""
		if r.Subs != nil {
			words = r.Subs.Words()
		} else if n > 0 {
			words = helpers(n)
		}
		switch {
		case words == "" && r.Parked:
			s.add(faint, "✓ parked")
		case words == "":
			s.add(green, "✓ to verify")
		case kinds(r.Subagents) != "":
			s.add(own, "◉ waiting on "+words+": "+kinds(r.Subagents))
		default:
			s.add(own, "◉ waiting on "+words)
		}
	}
	return s
}

func helpers(n int) string {
	if n == 1 {
		return "1 helper"
	}
	return fmt.Sprintf("%d helpers", n)
}

// kinds names the helpers' kinds, oldest first, such as "Explore ×2, dev".
func kinds(subs []ipc.Subagent) string {
	var names []string
	count := map[string]int{}
	for _, sa := range subs {
		if sa.Type == "" {
			continue
		}
		if count[sa.Type] == 0 {
			names = append(names, sa.Type)
		}
		count[sa.Type]++
	}
	var out []string
	for i, name := range names {
		if i == maxKinds {
			return strings.Join(out, ", ") + fmt.Sprintf(" and %d more", len(names)-maxKinds)
		}
		if count[name] > 1 {
			name += fmt.Sprintf(" ×%d", count[name])
		}
		out = append(out, name)
	}
	return strings.Join(out, ", ")
}

// maxKinds is how many kinds of helper the line names.
const maxKinds = 3

// share is a percentage with its label, such as "5h 62%"; the context's
// has a bar of eight cells as well. It is drawn in quiet, and turns yellow
// from 70% and red from 90%.
func share(label string, pct float64, bar bool, quiet string) seg {
	style := quiet
	switch {
	case pct >= 90:
		style = red
	case pct >= 70:
		style = yellow
	}
	var s seg
	s.add(quiet, label+" ")
	if bar {
		full := int(math.Round(min(max(pct, 0), 100) / 100 * cells))
		s.add(style, strings.Repeat("▰", full))
		s.add(quiet, strings.Repeat("▱", cells-full)+" ")
	}
	s.add(style, fmt.Sprintf("%.0f%%", pct))
	return s
}

const cells = 8

// SGR styles. The colours are the terminal's own, as the sidebar's are;
// only a working session's own colour is not (rgb).
const (
	faint  = "2"
	red    = "31"
	green  = "32"
	yellow = "33"
)

const sep = " · "

// rgb is the SGR style of a colour given as "#rrggbb", and "" for anything
// else.
func rgb(hex string) string {
	if len(hex) != 7 || hex[0] != '#' {
		return ""
	}
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("38;2;%d;%d;%d", v>>16, v>>8&0xFF, v&0xFF)
}

// seg is one part of the line: its text with its styles, and the columns
// it takes.
type seg struct {
	text  string
	width int
}

// add appends s in a style; "" is the terminal's own.
func (p *seg) add(style, s string) {
	p.width += utf8.RuneCountInString(s)
	p.text += paint(style, s)
}

func paint(style, s string) string {
	if style == "" || s == "" {
		return s
	}
	return "\x1b[" + style + "m" + s + "\x1b[0m"
}

func lineWidth(parts []seg) int {
	w := 0
	for i, p := range parts {
		if i > 0 {
			w += utf8.RuneCountInString(sep)
		}
		w += p.width
	}
	return w
}

// Timeouts for forwarding a status to the core: Claude Code cancels a
// status line that is still running when the next update comes, and
// updates come about every 300 ms while a turn runs.
const (
	connectTimeout = 100 * time.Millisecond
	replyTimeout   = 300 * time.Millisecond
)

// maxInput is the most Run reads from stdin.
const maxInput = 4 << 20

// Run is `irie statusline [--then COMMAND]`. It returns the exit code;
// with --then it does not return when it can run COMMAND, which takes its
// place, so Claude Code's signals and exit code are COMMAND's own.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	then := ""
	switch {
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--then":
		then = args[1]
	default:
		fmt.Fprintln(stderr, "usage: irie statusline [--then COMMAND]")
		return 2
	}
	raw, _ := io.ReadAll(io.LimitReader(stdin, maxInput))
	st := forward(raw, getenv)
	if then != "" {
		err := execShell(then, raw)
		fmt.Fprintln(stderr, "irie statusline:", err)
		return 1
	}
	if u, err := Parse(raw); err == nil {
		// Claude Code gives the terminal's width in COLUMNS.
		width, _ := strconv.Atoi(getenv("COLUMNS"))
		fmt.Fprint(stdout, Line(u, st, width))
	}
	return 0
}

// forward gives the core one status of the session the environment names,
// and returns the core's answer: the session's state and helpers. It never
// fails: without Irie the status line goes on, with no state.
func forward(raw []byte, getenv func(string) string) *ipc.StatusReply {
	session, socket := getenv("IRIE_SESSION"), getenv("IRIE_SOCKET")
	if session == "" || socket == "" || !json.Valid(raw) {
		return nil
	}
	c, err := ipc.Dial(socket, connectTimeout)
	if err != nil {
		return nil
	}
	defer c.Close()
	c.Timeout = replyTimeout
	var r ipc.StatusReply
	if c.Call(ipc.OpStatus, ipc.StatusArgs{Session: session, Payload: raw}, &r) != nil {
		return nil
	}
	return &r
}

// execShell replaces this process with `sh -c command`, whose stdin holds
// input: a file already unlinked, so nothing is left behind.
func execShell(command string, input []byte) error {
	f, err := os.CreateTemp("", "irie-status-")
	if err != nil {
		return err
	}
	os.Remove(f.Name())
	if _, err := f.Write(input); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := unix.Dup2(int(f.Fd()), 0); err != nil {
		return err
	}
	return syscall.Exec("/bin/sh", []string{"sh", "-c", command}, os.Environ())
}
