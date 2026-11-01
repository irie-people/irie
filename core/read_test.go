package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// view is a list as a client would show it: a running session, a frozen
// one, a parked one that is promoted, one that has ended, and two outside.
func view() View {
	return View{
		Home: "/home/alice",
		Sessions: []Session{
			{ID: "s1", Name: "fix login", State: StateWorking, Branch: "irie/fix-login"},
			{ID: "s2", Name: "parser", State: StateFrozen},
			{ID: "s3", Name: "docs", State: StateIdle, Parked: true, Priority: 1},
			{ID: "s4", Name: "old", State: StateExited},
		},
		Outside: []OutsideSession{
			{VendorID: "v-1", Name: "api", Cwd: "/home/alice/code/api", Movable: true},
			{VendorID: "v-2", ID: "bg02", Cwd: "/home/alice/code/web", Why: "it is busy"},
		},
	}
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Each command comes to the op it names, on the session that was selected.
func TestReadTurnsWordsIntoOps(t *testing.T) {
	for _, c := range []struct {
		text, session, outside string
		op                     string
		args                   any
	}{
		{":rename Login fix", "s1", "", OpRename, RenameArgs{Session: "s1", Name: "Login fix"}},
		{":rate good", "s1", "", OpRate, RateArgs{Session: "s1", Verdict: Good}},
		{":close", "s4", "", OpClose, CloseArgs{Session: "s4"}},
		{":reopen", "", "", OpReopen, ReopenArgs{}},
		{":undo", "", "", OpUndo, UndoArgs{}},
		{"  :redo  ", "", "", OpRedo, UndoArgs{}},
		{":setup", "", "", OpTool, ToolArgs{Name: ToolSetup}},
		{":setup local", "", "", OpTool, ToolArgs{Name: ToolSetup}},
		{":setup switch", "", "", OpTool, ToolArgs{Name: ToolSwitch}},
		{":vpn", "", "", OpVPN, VPNArgs{}},
		{":vpn on", "s1", "", OpVPN, VPNArgs{Do: VPNOn}},
		{":vpn OFF", "", "", OpVPN, VPNArgs{Do: VPNOff}},
		{":vpn time", "", "", OpVPN, VPNArgs{Do: VPNTime}},
		{":vpn lockdown on", "", "", OpVPN, VPNArgs{Do: VPNLockdown, Args: []string{"on"}}},
		{":vpn se got", "", "", OpVPN, VPNArgs{Do: VPNPlace, Args: []string{"se", "got"}}},
		{":vpn se-got-wg-001", "", "", OpVPN, VPNArgs{Do: VPNPlace, Args: []string{"se-got-wg-001"}}},
		{":models", "", "", OpTool, ToolArgs{Name: ToolModels}},
		{":route fix the typo", "", "", OpRoute, RouteArgs{Task: "fix the typo"}},
		{":freeze", "s1", "", OpFreeze, FreezeArgs{Session: "s1"}},
		{":freeze all", "", "", OpFreeze, FreezeArgs{All: true}},
		{":thaw", "s2", "", OpThaw, FreezeArgs{Session: "s2"}},
		{":thaw all", "s1", "", OpThaw, FreezeArgs{All: true}},
		{":prio a demo at noon", "s1", "", OpPriority, PriorityArgs{Session: "s1", Level: 1, Reason: "a demo at noon"}},
		// A promoted one goes back among the rest.
		{":prio", "s3", "", OpPriority, PriorityArgs{Session: "s3", Level: 0}},
		{":park", "s1", "", OpPark, ParkArgs{Session: "s1"}},
		{":unpark", "s3", "", OpUnpark, ParkArgs{Session: "s3"}},
		{":move api", "", "", OpMove, MoveArgs{VendorID: "v-1"}},
		{":move v-1", "", "", OpMove, MoveArgs{VendorID: "v-1"}},
		{":move", "s1", "v-1", OpMove, MoveArgs{VendorID: "v-1"}},
	} {
		st, err := Read(InputArgs{Text: c.text, Session: c.session, Outside: c.outside}, view())
		if err != nil {
			t.Errorf("%q: %v", c.text, err)
			continue
		}
		if st.Op != c.op || !reflect.DeepEqual(st.Args, c.args) {
			t.Errorf("%q: %s %+v, want %s %+v", c.text, st.Op, st.Args, c.op, c.args)
		}
	}
}

// What a command can't do, it says, in words a person reads, and nothing
// runs.
func TestReadRefusesInWords(t *testing.T) {
	for _, c := range []struct {
		text, session, outside string
		code, says             string
	}{
		{":rename x", "", "", CodeBadArgs, ":rename acts on the selected session"},
		{":rename x", "v-1", "", CodeBadArgs, "select one of Irie's"},
		{":rate good", "", "", CodeBadArgs, ":rate acts on the selected session"},
		{":rate meh", "s1", "", CodeBadArgs, "Irie has no :rate"},
		{":close", "", "", CodeBadArgs, ":close acts on the selected session"},
		{":freeze", "s2", "", CodeBadArgs, "must be running; or :freeze all"},
		{":thaw", "s1", "", CodeBadArgs, "must be frozen; or :thaw all"},
		{":freeze", "s4", "", CodeBadArgs, "old has ended, so there is nothing to freeze"},
		{":park", "s3", "", CodeBadArgs, "must be not parked"},
		{":unpark", "s1", "", CodeBadArgs, "must be parked"},
		{":prio", "", "", CodeBadArgs, ":prio acts on the selected session"},
		{":move", "s1", "", CodeBadArgs, "the selected session under Outside Irie, or one you name"},
		{":move nothing-here", "", "", CodeNotFound, "No session outside Irie is called “nothing-here”"},
		{":move bg02", "", "", CodeBusy, "web can't move yet: it is busy."},
		{":bogus", "", "", CodeBadArgs, "Irie has no :bogus. The commands are :rename NAME, :move [NAME]"},
		{":pause", "s1", "", CodeBadArgs, ":undo, :redo"},
		{"hows the weather", "", "", CodeNotFound, "No agent matches “weather”."},
		{"+a +b fix it", "", "", CodeBadArgs, "Only one +profile per task."},
		{"@web", "", "", CodeBadArgs, "Empty task: type what you want done after the @ or + words."},
	} {
		_, err := Read(InputArgs{Text: c.text, Session: c.session, Outside: c.outside}, view())
		if code(err) != c.code || err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%q: %v (%s), want %s saying %q", c.text, err, code(err), c.code, c.says)
		}
	}
	// Two outside sessions with one name: the person picks.
	v := view()
	v.Outside = append(v.Outside, OutsideSession{VendorID: "v-3", Name: "api"})
	if _, err := Read(InputArgs{Text: ":move api"}, v); err == nil || !strings.Contains(err.Error(), "2 sessions outside Irie are called “api”") {
		t.Errorf(":move of a shared name: %v", err)
	}
}

// A task runs where its words say, else where the client's menus say.
func TestReadStartsATaskWhereTheWordsSay(t *testing.T) {
	menus := InputArgs{Repo: "/home/alice/scratch", InPlace: true, Profile: "haiku", Model: "sonnet"}
	for _, c := range []struct {
		text string
		want SpawnArgs
	}{
		{"fix the flaky test", SpawnArgs{Task: "fix the flaky test", Repo: "/home/alice/scratch", InPlace: true, Profile: "haiku", Model: "sonnet", Jot: true}},
		{"@web fix it", SpawnArgs{Task: "fix it", Repo: "web", Profile: "haiku", Model: "sonnet", Jot: true}},
		{"+auto @~/code/x! fix it", SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x", InPlace: true, Profile: "auto", Model: "sonnet", Jot: true}},
	} {
		in := menus
		in.Text = c.text
		st, err := Read(in, view())
		if err != nil || st.Op != OpSpawn || !reflect.DeepEqual(st.Args, c.want) {
			t.Errorf("%q: %s %+v, %v; want spawn %+v", c.text, st.Op, st.Args, err, c.want)
		}
	}
	// A task typed again with Apart is taken back from the session it went
	// to, which the step names for the words.
	st, err := Read(InputArgs{Text: "also check the docs", Apart: "s1"}, view())
	if want := (SpawnArgs{Task: "also check the docs", Jot: true, Apart: "s1"}); err != nil || st.Op != OpSpawn || !reflect.DeepEqual(st.Args, want) || st.Session == nil || st.Session.ID != "s1" {
		t.Errorf("apart: %+v, %v", st, err)
	}
	// A question about an agent answers, and starts nothing.
	st, err = Read(InputArgs{Text: "how's the login going?"}, view())
	if err != nil || st.Op != "" || st.Session == nil || st.Session.ID != "s1" || st.Answer != "fix login: working" {
		t.Errorf("hows: %+v, %v", st, err)
	}
}

// Said words each step's outcome, and tells the client what to do with it.
func TestSaidWordsWhatHappened(t *testing.T) {
	v := view()
	login, parser := &v.Sessions[0], &v.Sessions[1]
	started := Session{ID: "s9", Name: "flaky test", Branch: "irie/flaky-test", Copied: []string{".env"}, Warnings: []string{"memory is low: 2.1 GB free of 16 GB"}}
	ask := &Choice{ID: "c1", Question: "Close “fix login” while it works?", Options: []ChoiceOption{{Key: "go", Label: "Close it"}}}
	until := time.Date(2026, 10, 31, 12, 0, 8, 0, time.UTC)
	for _, c := range []struct {
		name   string
		st     Step
		result any
		err    error
		want   InputResult
	}{
		{"spawn", Step{Op: OpSpawn, Args: SpawnArgs{Task: "x"}}, SpawnResult{Session: started}, nil,
			InputResult{Did: InputSpawned, Op: OpSpawn, Session: &started,
				Text: "Started flaky test on irie/flaky-test. Memory is low: 2.1 GB free of 16 GB. Copied .env."}},
		{"added", Step{Op: OpSpawn, Args: SpawnArgs{Task: "also x", Jot: true}}, SpawnResult{Session: *login, Added: true, Until: until}, nil,
			InputResult{Did: InputAdded, Op: OpSpawn, Session: login, Until: until, Text: "Added to fix login, the task sent just before."}},
		{"apart, too late", Step{Op: OpSpawn, Args: SpawnArgs{Task: "also x", Jot: true, Apart: "s1"}, Session: login}, SpawnResult{Session: Session{ID: "s9", Name: "also x"}, Late: true}, nil,
			InputResult{Did: InputSpawned, Op: OpSpawn, Session: &Session{ID: "s9", Name: "also x"}, Text: "Started also x. fix login has it too."}},
		{"tool", Step{Op: OpTool}, &SpawnResult{Session: Session{ID: "s8", Name: "setup"}}, nil,
			InputResult{Did: InputRan, Op: OpTool, Session: &Session{ID: "s8", Name: "setup"}, Show: true, Text: "Started setup."}},
		{"rename", Step{Op: OpRename, Session: login}, RenameResult{Session: Session{ID: "s1", Name: "login-2"}}, nil,
			InputResult{Did: InputRan, Op: OpRename, Session: &Session{ID: "s1", Name: "login-2"}, Text: "Renamed fix login to login-2."}},
		{"rate", Step{Op: OpRate, Args: RateArgs{Session: "s1", Verdict: Bad}, Session: login}, nil, nil,
			InputResult{Did: InputRan, Op: OpRate, Session: login, Text: "Rated fix login bad. The router learns from it."}},
		{"close", Step{Op: OpClose, Session: login}, nil, nil,
			InputResult{Did: InputRan, Op: OpClose, Text: "Closed fix login. :reopen brings it back."}},
		{"reopen", Step{Op: OpReopen}, ReopenResult{Session: *login}, nil,
			InputResult{Did: InputRan, Op: OpReopen, Session: login, Text: "Reopened fix login."}},
		{"undo", Step{Op: OpUndo}, UndoResult{Label: `start "task-002"`, Did: DidUndo}, nil,
			InputResult{Did: InputRan, Op: OpUndo, Text: "Undid Start “task-002”."}},
		{"redo", Step{Op: OpRedo}, UndoResult{Label: `park "say \"hi\""`, Did: DidRedo}, nil,
			InputResult{Did: InputRan, Op: OpRedo, Text: "Redid Park “say \"hi\"”."}},
		{"redo settles", Step{Op: OpRedo}, UndoResult{Label: "park “docs”", Did: DidSettle}, nil,
			InputResult{Did: InputRan, Op: OpRedo, Text: "Park “docs” was that way already, so the history moved on."}},
		{"undo asks", Step{Op: OpUndo}, UndoResult{Label: "Close “fix login”", Did: DidAsk, Choice: ask}, nil,
			InputResult{Did: InputAsked, Op: OpUndo, Choice: ask, Text: "Close “fix login” while it works?"}},
		{"nothing to undo", Step{Op: OpUndo}, nil, Errorf(CodeNotFound, "nothing to undo"),
			InputResult{Did: InputAnswered, Op: OpUndo, Text: "Nothing to undo."}},
		{"route", Step{Op: OpRoute}, RouteChoice{Reason: "small: local is the cheapest per success"}, nil,
			InputResult{Did: InputAnswered, Op: OpRoute, Text: "small: local is the cheapest per success"}},
		{"freeze one", Step{Op: OpFreeze, Session: login}, FreezeResult{Sessions: []string{"s1"}}, nil,
			InputResult{Did: InputRan, Op: OpFreeze, Session: login, Text: "Froze fix login."}},
		{"thaw all", Step{Op: OpThaw}, FreezeResult{Sessions: []string{"s2", "s3"}}, nil,
			InputResult{Did: InputRan, Op: OpThaw, Text: "Thawed 2 sessions."}},
		{"freeze none", Step{Op: OpFreeze}, FreezeResult{}, nil,
			InputResult{Did: InputRan, Op: OpFreeze, Text: "Nothing to freeze."}},
		{"promote", Step{Op: OpPriority, Args: PriorityArgs{Level: 1}, Session: parser}, nil, nil,
			InputResult{Did: InputRan, Op: OpPriority, Session: parser, Text: "Promoted parser: it has the machine first."}},
		{"demote", Step{Op: OpPriority, Args: PriorityArgs{}, Session: parser}, nil, nil,
			InputResult{Did: InputRan, Op: OpPriority, Session: parser, Text: "parser is back among the rest."}},
		{"park", Step{Op: OpPark, Session: login}, nil, nil,
			InputResult{Did: InputRan, Op: OpPark, Session: login, Text: "Parked fix login."}},
		{"move", Step{Op: OpMove, Outside: &v.Outside[0]}, MoveResult{Session: Session{ID: "s7", Name: "api"}}, nil,
			InputResult{Did: InputRan, Op: OpMove, Session: &Session{ID: "s7", Name: "api"}, Text: "Moved api into Irie."}},
		{"hows", Step{Answer: "fix login: working", Session: login}, nil, nil,
			InputResult{Did: InputAnswered, Session: login, Text: "fix login: working"}},
	} {
		got, err := Said(c.st, c.result, c.err)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, %v\nwant %+v", c.name, got, err, c.want)
		}
	}
	// Any other failure is the op's own, to show as it is.
	if _, err := Said(Step{Op: OpPark}, nil, Errorf(CodeNotFound, "no session s0")); code(err) != CodeNotFound {
		t.Errorf("a failed park: %v", err)
	}
}

// A result that came over the wire reads the same as the core's own value.
func TestSaidReadsAResultFromTheWire(t *testing.T) {
	raw := []byte(`{"session":{"id":"s9","name":"flaky test","state":"starting","created":"2026-10-31T12:00:00Z"}}`)
	got, err := Said(Step{Op: OpSpawn}, rawJSON(raw), nil)
	if err != nil || got.Session == nil || got.Session.ID != "s9" || got.Text != "Started flaky test." {
		t.Errorf("from JSON: %+v, %v", got, err)
	}
}

// rawJSON is JSON as a client holds it after a call.
type rawJSON []byte

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }
