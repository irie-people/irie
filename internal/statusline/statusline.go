// Package statusline is `irie statusline`, the status line command Irie
// gives Claude Code sessions (DESIGN §6.3). Claude Code runs it with the
// session's status as JSON on stdin; it forwards that to the core, which
// learns the session's cost, context and the plan's use from it, then runs
// the user's own status line on the same input, or prints a short line of
// its own. When Irie is down the user's status line still runs.
package statusline

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/irie-hq/irie/internal/ipc"
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

// Line is Irie's own status line, for a user who has none: what the session
// has cost, how full its context is and how much of the plan is used, such
// as "$1.23 · ctx 41% · 5h 62% · 7d 18%".
func Line(u Usage) string {
	var parts []string
	if u.CostUSD != nil && *u.CostUSD >= 0.005 {
		parts = append(parts, fmt.Sprintf("$%.2f", *u.CostUSD))
	}
	if u.Context != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", *u.Context))
	}
	if u.FiveHour != nil {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", u.FiveHour.Used))
	}
	if u.SevenDay != nil {
		parts = append(parts, fmt.Sprintf("7d %.0f%%", u.SevenDay.Used))
	}
	return strings.Join(parts, " · ")
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
	forward(raw, getenv)
	if then != "" {
		err := execShell(then, raw)
		fmt.Fprintln(stderr, "irie statusline:", err)
		return 1
	}
	if u, err := Parse(raw); err == nil {
		fmt.Fprint(stdout, Line(u))
	}
	return 0
}

// forward gives the core one status of the session the environment names.
// It never fails: without Irie the status line goes on.
func forward(raw []byte, getenv func(string) string) {
	session, socket := getenv("IRIE_SESSION"), getenv("IRIE_SOCKET")
	if session == "" || socket == "" || !json.Valid(raw) {
		return
	}
	c, err := ipc.Dial(socket, connectTimeout)
	if err != nil {
		return
	}
	defer c.Close()
	c.Timeout = replyTimeout
	c.Call(ipc.OpStatus, ipc.StatusArgs{Session: session, Payload: raw}, nil)
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
