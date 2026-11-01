package core

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Commands are what a task bar takes after a colon (DESIGN §4.2), as the
// error for an unknown one lists them.
const Commands = ":rename NAME, :move [NAME], :close, :reopen, :undo, :redo, :setup [local|switch], :models, :vpn [on|off|lockdown on|off|time|PLACE], :rate good|bad, :route TASK, :freeze [all], :thaw [all], :prio [REASON], :park, :unpark"

// View is what the words are read against: the rows in the list's order,
// the sessions outside Irie, and the folder that ~ means.
type View struct {
	Sessions []Session
	Outside  []OutsideSession
	Home     string
}

// A Step is what an input comes to: one op to run with its args, or, for a
// status question, only an answer.
type Step struct {
	Op   string // the op to run, or "" when Answer is all there is
	Args any    // the op's args
	// Answer is a status question's answer.
	Answer string
	// Session is the row the op acts on, or the question asked about.
	Session *Session
	// Outside is the session outside Irie that :move brings in.
	Outside *OutsideSession
}

// Read reads what a person typed, against what their client showed them,
// and says what it comes to (DESIGN §4.2, docs/CORE.md). Its errors are the
// person's to read: bad_args when the words make no sense, not_found when
// they name nothing.
func Read(in InputArgs, v View) (Step, error) {
	first, _, _ := strings.Cut(in.Text, "\n")
	if line := strings.TrimSpace(first); strings.HasPrefix(line, ":") {
		name, arg, _ := strings.Cut(strings.TrimPrefix(line, ":"), " ")
		return command(name, strings.TrimSpace(arg), in, v)
	}
	if q, ok := ParseHows(in.Text); ok {
		s := FindSession(v.Sessions, q)
		if s == nil {
			return Step{}, Errorf(CodeNotFound, "No agent matches “%s”.", q)
		}
		return Step{Answer: Hows(*s), Session: s}, nil
	}
	a, err := ParseTask(in.Text, v.Home)
	if err != nil {
		return Step{}, Errorf(CodeBadArgs, "%s", sentence(err.Error()))
	}
	a.Task = Unpaste(a.Task, in.Pastes)
	if a.Repo == "" && in.Repo != "" {
		a.Repo, a.InPlace = in.Repo, in.InPlace
	}
	if a.Profile == "" {
		a.Profile = in.Profile
	}
	if a.Tutor == "" {
		a.Tutor = in.Tutor
	}
	a.Model = in.Model
	// A typed task is a jot: one that reads as more for the jot sent just
	// before goes to that jot's agent instead (DESIGN §4.2). Apart starts
	// one that went there on its own after all.
	a.Jot, a.Apart = true, in.Apart
	st := Step{Op: OpSpawn, Args: a}
	for i := range v.Sessions {
		if in.Apart != "" && v.Sessions[i].ID == in.Apart {
			st.Session = &v.Sessions[i]
		}
	}
	return st, nil
}

// command reads a : command: its name, without the colon, and the rest of
// its line.
func command(name, arg string, in InputArgs, v View) (Step, error) {
	var sel *Session
	for i := range v.Sessions {
		if in.Session != "" && v.Sessions[i].ID == in.Session {
			sel = &v.Sessions[i]
		}
	}
	// selected is the selected row, which must be one of Irie's.
	selected := func() (*Session, error) {
		if sel == nil {
			return nil, Errorf(CodeBadArgs, ":%s acts on the selected session; select one of Irie's.", name)
		}
		return sel, nil
	}
	step := func(op string, args any, s *Session) (Step, error) {
		return Step{Op: op, Args: args, Session: s}, nil
	}
	switch {
	case name == "rename" && arg != "":
		s, err := selected()
		if err != nil {
			return Step{}, err
		}
		return step(OpRename, RenameArgs{Session: s.ID, Name: arg}, s)
	case name == "rate" && (arg == Good || arg == Bad):
		s, err := selected()
		if err != nil {
			return Step{}, err
		}
		return step(OpRate, RateArgs{Session: s.ID, Verdict: arg}, s)
	case name == "close" && arg == "":
		s, err := selected()
		if err != nil {
			return Step{}, err
		}
		return step(OpClose, CloseArgs{Session: s.ID}, s)
	case name == "reopen" && arg == "":
		return step(OpReopen, ReopenArgs{}, nil)
	case (name == OpUndo || name == OpRedo) && arg == "":
		return step(name, UndoArgs{}, nil)
	case name == "setup" && (arg == "" || arg == "local"):
		return step(OpTool, ToolArgs{Name: ToolSetup}, nil)
	case name == "setup" && arg == "switch":
		return step(OpTool, ToolArgs{Name: ToolSwitch}, nil)
	case name == "models" && arg == "":
		return step(OpTool, ToolArgs{Name: ToolModels}, nil)
	case name == "vpn":
		return step(OpVPN, vpnArgs(arg), nil)
	case name == "route" && arg != "":
		return step(OpRoute, RouteArgs{Task: arg}, nil)
	case (name == OpFreeze || name == OpThaw) && arg == "all":
		return step(name, FreezeArgs{All: true}, nil)
	case (name == OpFreeze || name == OpThaw) && arg == "":
		on := name == OpFreeze
		if sel == nil || (sel.State == StateFrozen) == on {
			what := "frozen"
			if on {
				what = "running"
			}
			return Step{}, Errorf(CodeBadArgs, ":%s takes the selected session, which must be %s; or :%s all.", name, what, name)
		}
		if on && !live(*sel) {
			return Step{}, Errorf(CodeBadArgs, "%s has ended, so there is nothing to freeze.", sel.Name)
		}
		return step(name, FreezeArgs{Session: sel.ID}, sel)
	case name == "prio":
		s, err := selected()
		if err != nil {
			return Step{}, err
		}
		level := 1
		if s.Priority > 0 {
			level = 0
		}
		return step(OpPriority, PriorityArgs{Session: s.ID, Level: level, Reason: arg}, s)
	case (name == OpPark || name == OpUnpark) && arg == "":
		on := name == OpPark
		if sel == nil || sel.Parked == on {
			what := "parked"
			if on {
				what = "not parked"
			}
			return Step{}, Errorf(CodeBadArgs, ":%s takes the selected session, which must be %s.", name, what)
		}
		return step(name, ParkArgs{Session: sel.ID}, sel)
	case name == OpMove:
		o, err := pickOutside(v.Outside, arg, in.Outside)
		if err != nil {
			return Step{}, err
		}
		if !o.Movable {
			return Step{}, Errorf(CodeBusy, "%s can't move yet: %s.", outsideName(*o), strings.TrimSuffix(cmp.Or(o.Why, "it isn't idle"), "."))
		}
		return Step{Op: OpMove, Args: MoveArgs{VendorID: o.VendorID}, Outside: o}, nil
	}
	return Step{}, Errorf(CodeBadArgs, "Irie has no :%s. The commands are %s.", name, Commands)
}

// vpnArgs reads the words after :vpn (docs/proposals/extensions.md §4.1):
// nothing for the state, on, off, time, lockdown on or off, or else a place
// such as se, se got or se-got-wg-001, which the core checks.
func vpnArgs(arg string) VPNArgs {
	words := strings.Fields(strings.ToLower(arg))
	switch {
	case len(words) == 0:
		return VPNArgs{}
	case len(words) == 1 && (words[0] == VPNOn || words[0] == VPNOff || words[0] == VPNTime):
		return VPNArgs{Do: words[0]}
	case words[0] == VPNLockdown:
		return VPNArgs{Do: VPNLockdown, Args: words[1:]}
	}
	return VPNArgs{Do: VPNPlace, Args: words}
}

// live reports whether a session's agent has not ended.
func live(s Session) bool { return s.State != StateExited && s.State != StateFailed }

// pickOutside finds the session outside Irie that :move names: by its
// vendor id, short id or name, or the selected one when it names none.
func pickOutside(outside []OutsideSession, what, selected string) (*OutsideSession, error) {
	if what == "" {
		for i, o := range outside {
			if selected != "" && (o.Key() == selected || o.VendorID == selected) {
				return &outside[i], nil
			}
		}
		return nil, Errorf(CodeBadArgs, ":move brings in the selected session under Outside Irie, or one you name.")
	}
	var named []int
	for i, o := range outside {
		switch {
		case o.VendorID == what || o.ID == what:
			return &outside[i], nil
		case o.Name == what:
			named = append(named, i)
		}
	}
	switch len(named) {
	case 0:
		return nil, Errorf(CodeNotFound, "No session outside Irie is called “%s”.", what)
	case 1:
		return &outside[named[0]], nil
	}
	return nil, Errorf(CodeBadArgs, "%d sessions outside Irie are called “%s”; select one and :move it.", len(named), what)
}

// outsideName is how a session outside Irie is called in words: its name,
// else its folder's.
func outsideName(o OutsideSession) string {
	switch {
	case o.Name != "":
		return printable(o.Name)
	case o.Cwd != "":
		return printable(filepath.Base(o.Cwd))
	}
	return o.Key()
}

// Said words what a step did, given what its op returned, or the error it
// failed with. A client shows Text as it is.
func Said(st Step, result any, err error) (InputResult, error) {
	if err != nil {
		// Nothing to undo is news, not a failure.
		var e *Error
		if (st.Op == OpUndo || st.Op == OpRedo) && errors.As(err, &e) && e.Code == CodeNotFound {
			return InputResult{Did: InputAnswered, Op: st.Op, Text: "Nothing to " + st.Op + "."}, nil
		}
		return InputResult{}, err
	}
	r := InputResult{Did: InputRan, Op: st.Op, Session: st.Session}
	name := ""
	if st.Session != nil {
		name = st.Session.Name
	}
	switch st.Op {
	case "":
		r.Did, r.Text = InputAnswered, st.Answer
	case OpSpawn:
		sr := as[SpawnResult](result)
		s := sr.Session
		r.Did, r.Session, r.Text = InputSpawned, &s, started(s)
		switch {
		case sr.Added:
			r.Did, r.Until, r.Text = InputAdded, sr.Until, "Added to "+s.Name+", the task sent just before."
		case sr.Late && name != "":
			// Taken back too late: that agent has it as well.
			r.Text += " " + name + " has it too."
		}
	case OpTool:
		s := as[SpawnResult](result).Session
		r.Session, r.Show, r.Text = &s, true, "Started "+s.Name+"."
	case OpRename:
		s := as[RenameResult](result).Session
		r.Session, r.Text = &s, "Renamed "+name+" to "+s.Name+"."
	case OpRate:
		r.Text = fmt.Sprintf("Rated %s %s. The router learns from it.", name, as[RateArgs](st.Args).Verdict)
	case OpClose:
		r.Session, r.Text = nil, "Closed "+name+". :reopen brings it back."
	case OpReopen:
		s := as[ReopenResult](result).Session
		r.Session, r.Text = &s, "Reopened "+s.Name+"."
	case OpMove:
		s := as[MoveResult](result).Session
		r.Session, r.Text = &s, "Moved "+outsideName(*st.Outside)+" into Irie."
	case OpRoute:
		r.Did, r.Text = InputAnswered, as[RouteChoice](result).Reason
	case OpVPN:
		r.Text = as[VPNResult](result).Text
		if d := as[VPNArgs](st.Args).Do; d == "" || d == VPNTime {
			r.Did = InputAnswered
		}
	case OpUndo, OpRedo:
		u := as[UndoResult](result)
		r.Text = undone(st.Op, u)
		if u.Did == DidAsk {
			r.Did, r.Choice = InputAsked, u.Choice
		}
	case OpFreeze, OpThaw:
		r.Text = froze(st, as[FreezeResult](result))
	case OpPriority:
		if as[PriorityArgs](st.Args).Level > 0 {
			r.Text = "Promoted " + name + ": it has the machine first."
		} else {
			r.Text = name + " is back among the rest."
		}
	case OpPark:
		r.Text = "Parked " + name + "."
	case OpUnpark:
		r.Text = "Unparked " + name + "."
	default:
		r.Text = "Done."
	}
	return r, nil
}

// started says that s started, where, and what it could not carry over.
func started(s Session) string {
	out := "Started " + s.Name
	if s.Branch != "" {
		out += " on " + s.Branch
	}
	out += "."
	// What did not go as asked comes first, while a short strip shows it.
	for _, w := range s.Warnings {
		out += " " + sentence(w)
	}
	if len(s.Copied) > 0 {
		out += " Copied " + strings.Join(s.Copied, ", ") + "."
	}
	return out
}

// undone says what an undo or redo did.
func undone(op string, u UndoResult) string {
	what := Title(u.Label)
	switch u.Did {
	case DidAsk:
		if u.Choice != nil {
			return u.Choice.Question
		}
		return "Irie asks about " + what + "."
	case DidUndo:
		return "Undid " + what + "."
	case DidRedo:
		return "Redid " + what + "."
	case DidSettle:
		return what + " was that way already, so the history moved on."
	case DidSkip:
		return "Took " + what + " out of the history; nothing else changed."
	}
	return "Nothing changed."
}

// Title is a step of the history as a person reads it: the core's label
// `park "api-fix"` is Park “api-fix”, as the Mac app's Edit menu says it.
func Title(label string) string {
	verb, name, ok := strings.Cut(label, " ")
	if !ok {
		return capital(label)
	}
	if strings.HasPrefix(name, `"`) {
		if q, err := strconv.Unquote(name); err == nil {
			return capital(verb) + " “" + q + "”"
		}
	}
	return capital(verb) + " " + name
}

// froze says what a freeze or thaw did.
func froze(st Step, r FreezeResult) string {
	verb := "Froze"
	if st.Op == OpThaw {
		verb = "Thawed"
	}
	if st.Session != nil {
		return verb + " " + st.Session.Name + "."
	}
	switch n := len(r.Sessions); n {
	case 0:
		return "Nothing to " + st.Op + "."
	case 1:
		return verb + " 1 session."
	default:
		return fmt.Sprintf("%s %d sessions.", verb, n)
	}
}

// as reads a result as a T: the value itself, a pointer to one, or its
// JSON, as a client that ran the op over the socket holds it.
func as[T any](v any) T {
	switch x := v.(type) {
	case T:
		return x
	case *T:
		if x != nil {
			return *x
		}
	}
	var t T
	if b, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(b, &t)
	}
	return t
}

// sentence starts s with a capital and ends it with a full stop, unless it
// ends in punctuation already.
func sentence(s string) string {
	s = capital(strings.TrimSpace(s))
	if last, _ := utf8.DecodeLastRuneInString(s); s != "" && !strings.ContainsRune(".!?…:", last) {
		s += "."
	}
	return s
}

// capital starts s with a capital letter.
func capital(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// printable replaces control characters, which would steer a terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}
