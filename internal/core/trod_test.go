package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/worktree"
)

// trodSection is a [trod] section that works around the clock with
// more's keys. Unless more says otherwise it steps in at once, so a test
// needs no idle Mac, and leaves the Mac's sleep alone.
func trodSection(more string) string {
	b := "\n[trod]\non = true\nstart = \"00:00\"\nend = \"00:00\"\n"
	if !strings.Contains(more, "away =") {
		b += "away = \"0s\"\n"
	}
	if !strings.Contains(more, "keep_awake =") {
		b += "keep_awake = false\n"
	}
	return b + more
}

func trodSetup(t *testing.T, more string) *coreEnv {
	t.Helper()
	return setupWith(t, agentConfig{extra: trodSection(more)})
}

// trodSends are the session's night.sent events.
func (c *coreEnv) trodSends(id string) []event.Event {
	var out []event.Event
	for _, e := range c.events(id) {
		if e.Kind == event.TrodSent {
			out = append(out, e)
		}
	}
	return out
}

func (c *coreEnv) trodReport() ipc.TrodResult {
	c.t.Helper()
	var r ipc.TrodResult
	if err := c.call(ipc.OpTrod, nil, &r); err != nil {
		c.t.Fatalf("night: %v", err)
	}
	return r
}

// typedTo is what was typed to a session's agent, as its conversation
// shows it while the transcript does not have it yet.
func (c *coreEnv) typedTo(id string) []string {
	c.t.Helper()
	var cv ipc.Conversation
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: id}, &cv); err != nil {
		c.t.Fatal(err)
	}
	var out []string
	for _, it := range cv.Pending {
		out = append(out, it.Text)
	}
	return out
}

// hasRulesAndGuidelines reports whether a note carries the default rules
// and guidelines and what to leave for the user.
func hasRulesAndGuidelines(note string) bool {
	for _, want := range append(append(config.TrodRules(), config.TrodGuidelines()...), "Leave for the user only what needs them") {
		if !strings.Contains(note, want) {
			return false
		}
	}
	return true
}

func TestTheTrodBotTellsAnAgentThatAsksToDecideItself(t *testing.T) {
	c := trodSetup(t, "")
	s := c.spawn("prompt,stop:ask")
	row := c.waitState(s.ID, ipc.StateDone)

	// The turn went on once, with the note, and then ended.
	said := c.sentOn(s.ID)
	if len(said) != 1 || !strings.HasPrefix(said[0], "[Irie] The trod bot: the user is away and can’t answer you.\nDon’t wait for them") ||
		!hasRulesAndGuidelines(said[0]) {
		t.Errorf("the trod bot said %q", said)
	}
	var kinds []string
	for _, e := range c.events(s.ID) {
		if e.Kind == event.TrodSent || e.Kind == event.TurnEnded {
			kinds = append(kinds, e.Kind)
		}
	}
	if !slices.Equal(kinds, []string{event.TrodSent, event.TurnEnded}) {
		t.Errorf("events = %v", kinds)
	}
	if sent := c.trodSends(s.ID); len(sent) != 1 || sent[0].Str("reason") != "question" || sent[0].Str("how") != "stop" ||
		sent[0].Str("message") != "It works. Shall I land it?" || sent[0].Source != event.SourceIrie {
		t.Errorf("night.sent = %+v", sent)
	}
	if row.Trod == nil || row.Trod.Sent != 1 || row.Trod.Reason != "question" {
		t.Errorf("row's night = %+v", row.Trod)
	}
	r := c.trodReport()
	if !r.On || !r.In || !r.Works || len(r.Sessions) != 1 || r.Sessions[0].ID != s.ID || len(r.Sessions[0].Sent) != 1 ||
		r.Sessions[0].Sent[0].How != "stop" || !slices.Equal(r.Rules, config.TrodRules()) {
		t.Errorf("report = %+v", r)
	}
}

func TestTheTrodBotTurnsAQuestionAway(t *testing.T) {
	c := trodSetup(t, "rules = [\"Never touch production.\"]\nguidelines = []\n")
	s := c.spawn("prompt,ask,stop")
	c.waitState(s.ID, ipc.StateDone)

	// The agent was told why, and asked no one.
	var reasons []string
	for _, l := range c.agentLog(s.ID) {
		if l["t"] == "denied" {
			r, _ := l["reason"].(string)
			reasons = append(reasons, r)
		}
	}
	if len(reasons) != 1 || !strings.HasPrefix(reasons[0], "[Irie] The trod bot: the user is away and can’t answer you.") ||
		!strings.Contains(reasons[0], "- Never touch production.") || strings.Contains(reasons[0], "guidelines") ||
		strings.Contains(reasons[0], config.TrodRules()[0]) {
		t.Errorf("denied with %q", reasons)
	}
	// The question's call never ran, so it never needed the user.
	for _, e := range c.events(s.ID) {
		if e.Kind == event.InputNeeded || e.Kind == event.ToolStarted && e.Str("tool") == "AskUserQuestion" {
			t.Errorf("recorded %s %v", e.Kind, e.Data)
		}
	}
	if sent := c.trodSends(s.ID); len(sent) != 1 || sent[0].Str("how") != "ask" || sent[0].Str("question") != "Which colour should the button be?" {
		t.Errorf("night.sent = %+v", sent)
	}
}

func TestAQuestionAskedAgainWaitsForTheUser(t *testing.T) {
	c := trodSetup(t, "")
	// It asks again at once, having done nothing since.
	again := c.spawn("prompt,ask,ask,stop")
	// It works between its questions, so the second is a question of its own.
	worked := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,ask,tool:Bash,ask,stop", Name: "worked"})
	row := c.waitState(again.ID, ipc.StateNeedsYou)
	if row.Reason != "question" {
		t.Errorf("row = %+v", row)
	}
	c.waitState(worked.ID, ipc.StateDone)
	if n := len(c.trodSends(again.ID)); n != 1 {
		t.Errorf("again: sent on %d times", n)
	}
	if n := len(c.trodSends(worked.ID)); n != 2 {
		t.Errorf("worked: sent on %d times", n)
	}
	// It ends its turn asking again too: that waits for the user as well.
	ends := c.spawn("prompt,stop:asks")
	c.waitState(ends.ID, ipc.StateDone)
	time.Sleep(trodEvery*time.Second + 2*time.Second)
	if n := len(c.trodSends(ends.ID)); n != 1 {
		t.Errorf("ends: sent on %d times", n)
	}
	if r := c.trodReport(); !slices.ContainsFunc(r.Sessions, func(s ipc.TrodSession) bool { return s.ID == ends.ID && s.Left == "asks you again" }) {
		t.Errorf("report = %+v", r.Sessions)
	}
}

func TestTheTrodBotIsOffUntilYouTurnItOn(t *testing.T) {
	c := setup(t)
	ask := c.spawn("prompt,ask")
	stop := c.spawn("prompt,stop:ask")
	c.waitState(ask.ID, ipc.StateNeedsYou)
	c.waitState(stop.ID, ipc.StateDone)
	if said := c.sentOn(stop.ID); len(said) != 0 {
		t.Errorf("sent on with %q", said)
	}
	if r := c.trodReport(); r.On || r.Works || len(r.Sessions) != 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestTheTrodBotTriesATurnAgainAfterAnAPIError(t *testing.T) {
	c := trodSetup(t, "retry = \"1s\"\n")
	s := c.spawn("prompt,stopfail:server_error")
	c.waitState(s.ID, ipc.StateNeedsYou)
	// Within a sweep, it types the agent a message, which goes on as a turn.
	testutil.Eventually(t, 2*trodEvery*time.Second, "the trod bot never sent it on", func() bool {
		return len(c.trodSends(s.ID)) > 0
	})
	c.waitState(s.ID, ipc.StateDone)
	sent := c.trodSends(s.ID)
	if len(sent) != 1 || sent[0].Str("reason") != "error" || sent[0].Str("how") != "typed" || sent[0].Str("error") != "server_error" {
		t.Errorf("night.sent = %+v", sent)
	}
	typed := c.typedTo(s.ID)
	if len(typed) != 1 || !strings.HasPrefix(typed[0], "[Irie] The trod bot: your last turn stopped on an API error (server_error). The user is away, so carry on") ||
		strings.Contains(typed[0], "\n") || !hasRulesAndGuidelines(typed[0]) {
		t.Errorf("typed %q", typed)
	}
	// The typed message is the shift's, not the user's: it came in as a
	// prompt, and its turn ended with nothing left, so nothing more is sent.
	var kinds []string
	for _, e := range c.events(s.ID) {
		switch e.Kind {
		case event.TurnFailed, event.TrodSent, event.PromptSubmitted, event.TurnEnded:
			kinds = append(kinds, e.Kind)
		}
	}
	if want := []string{event.PromptSubmitted, event.TurnFailed, event.TrodSent, event.PromptSubmitted, event.TurnEnded}; !slices.Equal(kinds, want) {
		t.Errorf("events = %v, want %v", kinds, want)
	}
}

func TestTheTrodBotSendsAJotOnUntilItsWorkLands(t *testing.T) {
	// The dog only watches, so what is sent is the shift's.
	c, repo := dogSetup(t, "nudges = 0\n"+trodSection("retry = \"1s\"\nnudges = 2\n"))
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop", Repo: repo})
	c.waitState(s.ID, ipc.StateDone)
	testutil.Eventually(t, 4*trodEvery*time.Second, "the trod bot never sent it on twice", func() bool {
		return len(c.trodSends(s.ID)) == 2
	})
	c.waitState(s.ID, ipc.StateDone)
	for _, e := range c.trodSends(s.ID) {
		if e.Str("reason") != "loose" || e.Str("how") != "typed" {
			t.Errorf("night.sent = %+v", e)
		}
	}
	typed := c.typedTo(s.ID)
	if len(typed) == 0 || !strings.HasPrefix(typed[0], "[Irie] The trod bot: your task is not through yet. Not committed in "+s.Worktree+" (branch "+s.Branch+"): notes.md.") {
		t.Errorf("typed %q", typed)
	}
	// Two is the most a night: the report says so, and nothing more comes.
	time.Sleep(trodEvery*time.Second + 2*time.Second)
	if n := len(c.trodSends(s.ID)); n != 2 {
		t.Errorf("sent on %d times", n)
	}
	r := c.trodReport()
	if len(r.Sessions) != 1 || r.Sessions[0].Left != "sent on 2 times, the most a night" || len(r.Sessions[0].Sent) != 2 {
		t.Errorf("report = %+v", r.Sessions)
	}
	if row, _ := c.row(s.ID); row.Loose == nil || row.Trod == nil || row.Trod.Sent != 2 || row.Trod.Reason != "loose" {
		t.Errorf("row = %+v, %+v", row.Loose, row.Trod)
	}
}

func TestTheTrodBotLeavesWhatYouPutAside(t *testing.T) {
	c := trodSetup(t, "retry = \"1s\"\n")
	parked := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stopfail:overloaded", Name: "parked"})
	frozen := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stopfail:overloaded", Name: "frozen"})
	login := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stopfail:authentication_failed", Name: "login"})
	asks := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,permission", Name: "asks"})
	for _, s := range []ipc.Session{parked, frozen, login, asks} {
		c.waitState(s.ID, ipc.StateNeedsYou)
	}
	if err := c.call(ipc.OpPark, ipc.ParkArgs{Session: parked.ID}, nil); err != nil {
		t.Fatal(err)
	}
	c.freeze(frozen.ID, true)
	time.Sleep(trodEvery*time.Second + 2*time.Second)
	for _, s := range []ipc.Session{parked, frozen, login, asks} {
		if sent := c.trodSends(s.ID); len(sent) != 0 {
			t.Errorf("%s: sent on %+v", s.Name, sent)
		}
	}
	// In its hours, the report says what waits for the user.
	left := map[string]string{}
	for _, s := range c.trodReport().Sessions {
		left[s.Name] = s.Left
	}
	if left["login"] != "its turn failed: authentication_failed" || !strings.Contains(left["asks"], "rm -rf build") || len(left) != 2 {
		t.Errorf("left = %q", left)
	}
}

func TestTheTrodBotKeepsWithinTheWeeksAllotment(t *testing.T) {
	// No status line of the user's own runs after Irie's.
	t.Setenv("HOME", t.TempDir())
	fake := testutil.BuildFakeAgent(t)
	// The status line of +fake's sessions says the week is 95% used, and
	// that of +low's, once the week has reset, 10%.
	low := fmt.Sprintf("\n[profiles.low]\ncommand = %q\nmodel = \"claude-opus-5-5\"\nenv = { IRIE_FAKE_WEEK = \"10\" }\n", fake)
	c := setupWith(t, agentConfig{statusLine: true, env: map[string]string{"IRIE_FAKE_WEEK": "95"}, extra: trodSection("retry = \"1s\"\n") + low})
	working := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,status:0.5,sleep:45s,stop", Name: "working"})
	failed := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stopfail:server_error", Name: "failed"})
	c.waitState(failed.ID, ipc.StateNeedsYou)

	// Past the 90% it may use, it freezes what would still spend the week,
	// and sends nothing on.
	row := c.waitState(working.ID, ipc.StateFrozen)
	if !strings.HasPrefix(row.Detail, "Frozen by the trod bot: the week's plan use is at 95%, the most it may use is 90%") {
		t.Errorf("detail = %q", row.Detail)
	}
	if r := c.trodReport(); !r.Capped || r.Week == nil || *r.Week != 95 || r.MaxPlanUse != 90 {
		t.Errorf("report = %+v", r)
	}
	time.Sleep(trodEvery*time.Second + 2*time.Second)
	if sent := c.trodSends(failed.ID); len(sent) != 0 {
		t.Errorf("sent on past the cap: %+v", sent)
	}
	if row, _ := c.row(failed.ID); row.State != ipc.StateNeedsYou {
		t.Errorf("failed: %s", row.State)
	}
	for _, e := range c.events(working.ID) {
		if e.Kind == event.FreezeOn && (e.Source != event.SourceIrie || e.Str("by") != "trod") {
			t.Errorf("freeze.on = %+v", e)
		}
	}

	// Once the week is back under it, it thaws what it froze, and sends on
	// what is due.
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt,status:0.1,stop", Profile: "low", Name: "low"})
	c.waitState(working.ID, ipc.StateWorking)
	testutil.Eventually(t, 2*trodEvery*time.Second, "the trod bot never sent the failed turn on", func() bool {
		return len(c.trodSends(failed.ID)) == 1
	})
	var kinds []string
	for _, e := range c.events(working.ID) {
		if e.Kind == event.FreezeOn || e.Kind == event.FreezeOff {
			kinds = append(kinds, e.Kind+" "+e.Str("by"))
		}
	}
	if !slices.Equal(kinds, []string{"freeze.on trod", "freeze.off trod"}) {
		t.Errorf("freezes = %v", kinds)
	}
}

// fakeTool puts a script on the PATH Irie runs with, in front of the
// system's tools.
func fakeTool(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestTheTrodBotWaitsWhileYouAreAtTheMac(t *testing.T) {
	bin := t.TempDir()
	idle := filepath.Join(t.TempDir(), "idle")
	os.WriteFile(idle, []byte("0"), 0o600)
	// ioreg's line, as macOS prints it, in nanoseconds.
	fakeTool(t, bin, "ioreg", fmt.Sprintf("printf '    | |     \"HIDIdleTime\" = %%s\\n' \"$(cat %s)\"\n", idle))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := trodSetup(t, "away = \"15m\"\n")
	s := c.spawn("prompt,stop:ask")
	c.waitState(s.ID, ipc.StateDone)
	if said := c.sentOn(s.ID); len(said) != 0 {
		t.Errorf("sent on while you were at the Mac: %q", said)
	}
	if r := c.trodReport(); !r.In || r.Away || r.Works || r.IdleSeconds == nil || *r.IdleSeconds != 0 {
		t.Errorf("report = %+v", r)
	}
	// Once you have been away for 15 minutes, its question is answered.
	os.WriteFile(idle, []byte(strconv.FormatInt(int64(16*time.Minute), 10)), 0o600)
	testutil.Eventually(t, 2*trodEvery*time.Second, "the trod bot never answered", func() bool {
		return len(c.trodSends(s.ID)) > 0
	})
	c.waitState(s.ID, ipc.StateDone)
	if sent := c.trodSends(s.ID); len(sent) != 1 || sent[0].Str("reason") != "question" || sent[0].Str("how") != "typed" {
		t.Errorf("night.sent = %+v", sent)
	}
	if typed := c.typedTo(s.ID); len(typed) != 1 || !strings.HasPrefix(typed[0], "[Irie] The trod bot: you asked the user something, but they are away and can’t answer.") {
		t.Errorf("typed %q", typed)
	}
	if r := c.trodReport(); !r.Works || r.IdleSeconds == nil || *r.IdleSeconds != 16*60 {
		t.Errorf("report = %+v", r)
	}
}

func TestTheTrodBotKeepsTheMacAwakeWhileThereIsWork(t *testing.T) {
	bin := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "caffeinate.pid")
	// It stays while the process -w names lives, as caffeinate does.
	fakeTool(t, bin, "caffeinate", fmt.Sprintf("echo \"$$ $*\" > %s\nwhile kill -0 \"$3\" 2>/dev/null; do sleep 0.2; done\n", pidFile))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := trodSetup(t, "keep_awake = true\n")
	s := c.spawn("prompt,sleep:15s,stop")
	c.waitState(s.ID, ipc.StateWorking)
	var pid int
	testutil.Eventually(t, 2*trodEvery*time.Second, "the Mac was never kept awake", func() bool {
		b, err := os.ReadFile(pidFile)
		f := strings.Fields(string(b))
		if err != nil || len(f) != 4 || f[1] != "-i" || f[2] != "-w" {
			return false
		}
		pid, _ = strconv.Atoi(f[0])
		return syscall.Kill(pid, 0) == nil
	})
	if r := c.trodReport(); !r.Awake {
		t.Errorf("report = %+v", r)
	}
	// Once nothing is left, the Mac may sleep.
	c.waitState(s.ID, ipc.StateDone)
	testutil.Eventually(t, 2*trodEvery*time.Second, "the Mac was kept awake with nothing left", func() bool {
		return syscall.Kill(pid, 0) != nil
	})
}

// trodCore is a core with a config file's settings and no tmux, for the
// trod bot's choices.
func trodCore(t *testing.T, file string) *Core {
	t.Helper()
	cfg, err := config.Parse([]byte(file), "test.toml")
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{sessions: map[string]*session{}}
	c.useConfig(cfg)
	return c
}

// stopped is a Claude Code session whose turn ended or failed at, by the
// events given, oldest first.
func stopped(id string, at time.Time, evs ...event.Event) *session {
	x := &session{id: id, name: id, harness: "claude-code"}
	x.track.apply(event.Event{Kind: event.SpawnRequested, TS: event.Stamp(at.Add(-time.Hour))})
	x.track.apply(event.Event{Kind: event.SessionStarted, TS: event.Stamp(at.Add(-time.Hour))})
	x.track.apply(event.Event{Kind: event.PromptSubmitted, TS: event.Stamp(at.Add(-time.Hour))})
	for _, e := range evs {
		if e.TS == "" {
			e.TS = event.Stamp(at)
		}
		if e.Agent == "" {
			e.Agent = event.Main
		}
		x.track.apply(e)
	}
	return x
}

func failed(err string) event.Event {
	return event.Event{Kind: event.TurnFailed, Data: map[string]any{"error": err}}
}

func TestTheTrodBotsChoices(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	now := time.Now()
	c := trodCore(t, trodSection("retry = \"10m\"\nnudges = 3\nwithin = \"24h\"\n"))
	s := c.shiftAt(now)
	due := func(x *session) (string, bool) {
		d, work := c.trodNext(x, now, s, time.Time{})
		if d == nil {
			return "", work
		}
		return d.reason, work
	}
	check := func(name string, x *session, wantReason string, wantWork bool) {
		t.Helper()
		if r, w := due(x); r != wantReason || w != wantWork {
			t.Errorf("%s: %q, %v; want %q, %v", name, r, w, wantReason, wantWork)
		}
	}
	ended := event.Event{Kind: event.TurnEnded, Data: map[string]any{}}
	asks := event.Event{Kind: event.TurnEnded, Data: map[string]any{"asks": true}}

	// An API error that may pass is tried again once retry has passed.
	check("overloaded, just now", stopped("a", now, failed("overloaded")), "", true)
	check("overloaded, 11m ago", stopped("b", now.Add(-11*time.Minute), failed("overloaded")), "error", true)
	// What only the user can mend waits for them.
	check("billing", stopped("c", now.Add(-time.Hour), failed("billing_error")), "", false)
	// A question is answered at once, but only once.
	check("asks", stopped("d", now, asks), "question", true)
	answered := stopped("e", now, asks)
	answered.track.apply(event.Event{Kind: event.TrodSent, TS: event.Stamp(now), Data: map[string]any{"reason": "question", "how": "typed"}})
	answered.track.apply(event.Event{Kind: event.PromptSubmitted, TS: event.Stamp(now)})
	answered.track.apply(asks)
	check("asks again", answered, "", false)
	// A finished turn with nothing left is through.
	check("done", stopped("f", now.Add(-time.Hour), ended), "", false)
	// A jot that is not through waits for retry, then is sent on.
	loose := stopped("g", now.Add(-time.Minute), ended)
	loose.dog.loose = []looseIn{{Loose: worktree.Loose{Dirty: 1, Files: []string{"notes.md"}}}}
	check("loose, a minute ago", loose, "", true)
	loose.track.stopped = now.Add(-time.Hour)
	check("loose, an hour ago", loose, "loose", true)
	// Three times a night at most; the Mac may sleep then.
	for range 3 {
		loose.track.apply(event.Event{Kind: event.TrodSent, TS: event.Stamp(now), Data: map[string]any{"reason": "loose", "how": "typed"}})
	}
	check("loose, sent on three times", loose, "", false)
	// One that stopped long ago is not kept going.
	check("stale", stopped("h", now.Add(-25*time.Hour), failed("server_error")), "", false)
	// Waiting on work of its own in the background, it will wake.
	bg := stopped("i", now.Add(-time.Hour), event.Event{Kind: event.TurnEnded, Data: map[string]any{"waiting": 1}})
	check("background", bg, "", true)
	// Parked, frozen or delegated: not the shift's.
	parked := stopped("j", now.Add(-time.Hour), failed("server_error"), event.Event{Kind: event.UIParked})
	check("parked", parked, "", false)
	frozen := stopped("k", now.Add(-time.Hour), failed("server_error"), event.Event{Kind: event.FreezeOn})
	check("frozen", frozen, "", false)
	delegated := stopped("l", now.Add(-time.Hour), failed("server_error"))
	delegated.out = &outcome{parent: "s000000000001"}
	check("delegated", delegated, "", false)
	other := stopped("m", now.Add(-time.Hour), failed("server_error"))
	other.harness = "generic"
	check("generic", other, "", false)

	// At a usage limit, it waits while the plan is at its limit: Claude
	// Code carries the task on at the reset itself.
	limit := stopped("n", now.Add(-2*time.Hour), failed("rate_limit"))
	c.plan = ipc.Plan{FiveHour: &ipc.Window{Used: 100, ResetsAt: now.Add(time.Hour)}}
	check("limit before the reset", limit, "", true)
	// After the reset, it gives Claude Code retry to carry on, then steps in.
	c.plan = ipc.Plan{FiveHour: &ipc.Window{Used: 100, ResetsAt: now.Add(-5 * time.Minute)}}
	check("limit just reset", limit, "", true)
	c.plan = ipc.Plan{FiveHour: &ipc.Window{Used: 100, ResetsAt: now.Add(-11 * time.Minute)}}
	check("limit reset 11m ago", limit, "limit", true)
	// Unless Claude Code's own continue is off: then its terminal asks the user.
	os.WriteFile(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json"), []byte(`{"autoContinueAtUsageLimit": false}`), 0o600)
	check("limit with Claude Code's continue off", limit, "", false)

	// Past the plan's use it may spend, nothing more is sent on; what
	// would spend on its own is what the week's cap freezes.
	c.plan = ipc.Plan{FiveHour: &ipc.Window{Used: 95, ResetsAt: now.Add(time.Hour)}}
	if either, week, _ := c.overPlan(now, 90); !either || week {
		t.Errorf("five hours at 95%%: over %v, week %v", either, week)
	}
	c.plan = ipc.Plan{SevenDay: &ipc.Window{Used: 91, ResetsAt: now.Add(48 * time.Hour)}}
	if either, week, used := c.overPlan(now, 90); !either || !week || used != 91 {
		t.Errorf("week at 91%%: over %v, week %v, %v", either, week, used)
	}
	// A window whose reset has passed is empty.
	c.plan = ipc.Plan{SevenDay: &ipc.Window{Used: 99, ResetsAt: now.Add(-time.Minute)}}
	if either, _, _ := c.overPlan(now, 90); either {
		t.Error("a week that reset is over")
	}
	works := stopped("p", now)
	works.track.apply(event.Event{Kind: event.ToolStarted, TS: event.Stamp(now), Agent: event.Main, Data: map[string]any{"tool": "Bash"}})
	for _, x := range []struct {
		name  string
		x     *session
		spend bool
	}{{"works", works, true}, {"background", bg, true}, {"limit", limit, false}, {"overloaded", stopped("q", now, failed("overloaded")), false}, {"done", stopped("r", now, ended), false}} {
		if got := c.spendsAlone(x.x, now); got != x.spend {
			t.Errorf("%s spends alone: %v", x.name, got)
		}
	}
	os.Remove(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json"))
	if !c.spendsAlone(limit, now) {
		t.Error("limit: Claude Code carries it on at the reset")
	}

	// Questions off: a question waits for the user.
	off := trodCore(t, trodSection("questions = false\n"))
	if d, _ := off.trodNext(stopped("o", now, asks), now, off.shiftAt(now), time.Time{}); d != nil {
		t.Errorf("questions off: %+v", d)
	}
}

func TestTheTrodBotKeepsItsHoursAndYourRules(t *testing.T) {
	// Out of its hours, or off, it does nothing at all.
	for _, night := range []string{"[trod]\non = false\n", "[trod]\non = true\nstart = \"1:00\"\nend = \"1:01\"\n"} {
		c := trodCore(t, night)
		s := c.shiftAt(time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local))
		if s.in || s.works() {
			t.Errorf("%q: %+v", night, s)
		}
	}
	// The note says until when, and carries your rules and guidelines.
	c := trodCore(t, "[trod]\non = true\naway = \"0s\"\nrules = [\"Never touch production.\", \"Keep to the docs folder.\"]\nguidelines = [\"Prefer small commits.\"]\n")
	s := c.shiftAt(time.Date(2026, 10, 31, 23, 30, 0, 0, time.Local))
	if !s.works() || s.until.Hour() != 7 {
		t.Fatalf("shift = %+v", s)
	}
	note := trodNote(s, trodLimit, trodTyped, "", true)
	want := "[Irie] The trod bot: the usage limit that stopped you has reset. The user is away until 07:00, so carry on with your task from where you stopped. " +
		"The user’s rules for the night, which you must follow: 1. Never touch production. 2. Keep to the docs folder. " +
		"Their guidelines, for when you decide: 1. Prefer small commits. " +
		"Leave for the user only what needs them: a login or password, an approval in their browser or on their phone, or a permission prompt, and say in one line what you left them and why. If nothing is left that you can do, say so in one line and stop."
	if note != want {
		t.Errorf("note:\n%s\nwant:\n%s", note, want)
	}
	if lines := strings.Split(trodNote(s, trodQuestion, trodAtStop, "", false), "\n"); len(lines) != 8 || lines[3] != "- Never touch production." || lines[6] != "- Prefer small commits." {
		t.Errorf("lines = %q", lines)
	}
}

// TestTheTrodBotsFakesAreSound: the stand-ins the tests put on PATH run.
func TestTheTrodBotsFakesAreSound(t *testing.T) {
	bin := t.TempDir()
	fakeTool(t, bin, "ioreg", "printf '    | |     \"HIDIdleTime\" = 61644736250\\n'\n")
	out, err := exec.Command(filepath.Join(bin, "ioreg")).Output()
	if err != nil || !hidIdle.Match(out) {
		t.Fatalf("%q, %v", out, err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if d, ok := machineIdle(); !ok || d != 61644736250 {
		t.Errorf("idle = %v, %v", d, ok)
	}
}
