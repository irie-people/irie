package statusline

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
)

// sample is shaped like the status line docs' example input.
const sample = `{
  "session_id": "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f",
  "cwd": "/home/alice/code/website",
  "model": {"id": "claude-opus-5-5", "display_name": "Opus 5.5"},
  "workspace": {"current_dir": "/home/alice/code/website", "project_dir": "/home/alice/code/website"},
  "cost": {"total_cost_usd": 1.234, "total_duration_ms": 45000, "total_api_duration_ms": 2300},
  "context_window": {"total_input_tokens": 15500, "total_output_tokens": 1200, "context_window_size": 200000,
    "used_percentage": 41, "remaining_percentage": 59, "current_usage": null},
  "rate_limits": {"five_hour": {"used_percentage": 62.5, "resets_at": 1793000000},
    "seven_day": {"used_percentage": 18, "resets_at": 1793400000}}
}`

func TestParse(t *testing.T) {
	u, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if u.Model != "claude-opus-5-5" || u.ModelName != "Opus 5.5" || u.CostUSD == nil || *u.CostUSD != 1.234 ||
		u.Context == nil || *u.Context != 41 || u.ContextSize != 200000 {
		t.Errorf("usage = %+v", u)
	}
	if u.FiveHour == nil || u.FiveHour.Used != 62.5 || !u.FiveHour.ResetsAt.Equal(time.Unix(1793000000, 0)) || u.SevenDay == nil || u.SevenDay.Used != 18 {
		t.Errorf("plan = %+v, %+v", u.FiveHour, u.SevenDay)
	}
	// Early in a session, and off a Pro or Max plan, fields are null or
	// absent.
	u, err = Parse([]byte(`{"model": {"id": "x"}, "context_window": {"used_percentage": null}, "rate_limits": {"five_hour": {}}}`))
	if err != nil || u.Context != nil || u.CostUSD != nil || u.FiveHour != nil || u.SevenDay != nil {
		t.Errorf("sparse usage = %+v, %v", u, err)
	}
	if _, err := Parse([]byte("not json")); err == nil {
		t.Error("no error for bad input")
	}
}

func TestLine(t *testing.T) {
	u, _ := Parse([]byte(sample))
	if got := Line(u); got != "$1.23 · ctx 41% · 5h 62% · 7d 18%" {
		t.Errorf("line = %q", got)
	}
	free := 0.0
	if got := Line(Usage{CostUSD: &free}); got != "" {
		t.Errorf("line for nothing to say = %q", got)
	}
}

// fakeCore records the status updates it is given.
type fakeCore struct {
	mu   sync.Mutex
	args []ipc.StatusArgs
}

func (f *fakeCore) Call(op string, raw json.RawMessage) (any, error) {
	if op != ipc.OpStatus {
		return nil, ipc.Errorf(ipc.CodeUnknownOp, "unknown op %q", op)
	}
	var a ipc.StatusArgs
	json.Unmarshal(raw, &a)
	f.mu.Lock()
	f.args = append(f.args, a)
	f.mu.Unlock()
	return nil, nil
}

func (f *fakeCore) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	return nil, nil, ipc.Errorf(ipc.CodeUnknownOp, "no")
}

func TestRunForwardsAndPrintsItsOwnLine(t *testing.T) {
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	core := &fakeCore{}
	go ipc.Serve(l, core)
	env := map[string]string{"IRIE_SESSION": "s0k3j9x2m4a7q", "IRIE_SOCKET": sock}

	var out, errOut bytes.Buffer
	if code := Run(nil, strings.NewReader(sample), &out, &errOut, func(k string) string { return env[k] }); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "$1.23 · ctx 41% · 5h 62% · 7d 18%" {
		t.Errorf("printed %q", out.String())
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if len(core.args) != 1 || core.args[0].Session != "s0k3j9x2m4a7q" || !json.Valid(core.args[0].Payload) {
		t.Errorf("the core got %+v", core.args)
	}
}

func TestRunWithoutIrieStillPrints(t *testing.T) {
	var out, errOut bytes.Buffer
	env := map[string]string{"IRIE_SESSION": "s0k3j9x2m4a7q", "IRIE_SOCKET": "/nonexistent/core.sock"}
	start := time.Now()
	if code := Run(nil, strings.NewReader(sample), &out, &errOut, func(k string) string { return env[k] }); code != 0 || out.String() == "" {
		t.Errorf("exit %d, printed %q", code, out.String())
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v without a core", d)
	}
	if code := Run([]string{"--bogus"}, strings.NewReader(""), &out, &errOut, func(string) string { return "" }); code != 2 {
		t.Errorf("bad arguments: exit %d", code)
	}
}
