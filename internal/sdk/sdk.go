// Package sdk is `irie sdk`, the pane program of a session on the Agent
// SDK (DESIGN §3.5). It runs Claude Code's agent loop as the Agent SDK's
// CLI, `claude -p`, with JSON lines in and out; it shows the conversation
// in its terminal, sends what is typed there, by a person or by the core,
// as the next message, and asks there when the agent wants permission.
//
// Such a session is paid as Agent SDK use: from the API key's credits,
// which may cover the Agent SDK where they do not cover Claude Code, as a
// Claude plan's monthly API credits do.
package sdk

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
)

// Main is `irie sdk [--first FILE] -- CLAUDE [ARGS...]`.
func Main(args []string) int {
	fs := flag.NewFlagSet("sdk", flag.ContinueOnError)
	first := fs.String("first", "", "a file whose text is the first message")
	if fs.Parse(args) != nil || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: irie sdk [--first FILE] -- CLAUDE [ARGS...]")
		return 2
	}
	var text string
	if *first != "" {
		b, err := os.ReadFile(*first)
		if err != nil {
			fmt.Fprintln(os.Stderr, "irie sdk:", err)
			return 1
		}
		text = string(b)
	}
	return Run(Options{Argv: fs.Args(), First: text, In: os.Stdin, Out: os.Stdout, Getenv: os.Getenv})
}

// Options is what Run needs.
type Options struct {
	Argv   []string            // the CLI's command line, to which Run adds Flags
	First  string              // the first message; "" waits for the user's
	In     io.Reader           // the terminal's keys
	Out    io.Writer           // the terminal
	Getenv func(string) string // finds Irie's socket and the session, to report its cost
	Width  int                 // columns when Out is not a terminal; 0 = 80
}

// endWait is how long the CLI has to end after its input ends or a signal
// asks it to, before it is killed.
const endWait = 10 * time.Second

// Run runs the conversation until the CLI exits, and returns its exit code.
func Run(o Options) int {
	if o.Getenv == nil {
		o.Getenv = func(string) string { return "" }
	}
	width := cmp.Or(o.Width, 80)
	fd := -1
	if f, ok := o.In.(*os.File); ok && columns(int(f.Fd())) > 0 {
		fd = int(f.Fd())
		width = columns(fd)
	}

	inR, inW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(o.Out, "irie sdk:", err)
		return 1
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(o.Out, "irie sdk:", err)
		return 1
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(o.Out, "irie sdk:", err)
		return 1
	}
	cmd := exec.Command(o.Argv[0], append(slices.Clone(o.Argv[1:]), Flags...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(o.Out, "irie sdk: %v\r\n", err)
		return 127
	}
	inR.Close()
	outW.Close()
	errW.Close()

	if fd >= 0 {
		if restore, err := rawMode(fd); err == nil {
			defer restore()
		}
		// Bracketed paste: the core types each message as a paste, and a
		// paste's new lines stay in the message.
		fmt.Fprint(o.Out, "\x1b[?2004h")
		defer fmt.Fprint(o.Out, "\x1b[?2004l")
	}

	writes := make(chan []byte, 256)
	go func() {
		for b := range writes {
			inW.Write(b)
		}
		inW.Close()
	}()
	lines := make(chan message, 64)
	go readMessages(outR, lines)
	errs := make(chan string, 16)
	go readLines(errR, errs)
	keys := make(chan []key, 16)
	go readKeys(o.In, keys)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGWINCH)
	defer signal.Stop(sigs)

	u := &ui{out: o.Out, v: newView(width), writes: writes, getenv: o.Getenv}
	u.render([]string{bold + "Irie" + reset + dim + " · Agent SDK session" + reset})
	if o.First != "" {
		u.submit(o.First)
	}
	var kill <-chan time.Time
	for {
		select {
		case m, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			u.message(m)
		case l, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			u.render([]string{u.v.faint("  " + l)})
		case ks, ok := <-keys:
			if !ok {
				// The terminal is gone: nobody can type any more.
				keys = nil
				u.end()
				continue
			}
			for _, k := range ks {
				u.key(k)
			}
			u.render(nil)
		case s := <-sigs:
			if s == syscall.SIGWINCH {
				if fd >= 0 {
					if w := columns(fd); w > 0 {
						u.v.width = max(w, 20)
					}
				}
				u.render(nil)
				continue
			}
			u.end()
			if kill == nil {
				kill = time.After(endWait)
			}
		case <-kill:
			cmd.Process.Kill()
		case err := <-done:
			// What the CLI wrote before it ended still shows.
			drain(u, lines, errs)
			code := exitCode(err)
			end := "  · the session ended"
			if code != 0 {
				end += fmt.Sprintf(" (exit %d)", code)
			}
			u.v.line = ""
			u.ask, u.ended = nil, true
			u.render([]string{u.v.faint(end)})
			return code
		}
	}
}

// drain shows what is left in lines and errs, for a moment at most.
func drain(u *ui, lines <-chan message, errs <-chan string) {
	stop := time.After(2 * time.Second)
	for lines != nil || errs != nil {
		select {
		case m, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			u.message(m)
		case l, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			u.render([]string{u.v.faint("  " + l)})
		case <-stop:
			return
		}
	}
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		if err != nil {
			return 1
		}
		return 0
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}

// readMessages decodes the CLI's lines, one JSON value each, until it
// closes its output.
func readMessages(r io.ReadCloser, out chan<- message) {
	defer close(out)
	defer r.Close()
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var m message
			if json.Unmarshal(line, &m) == nil && m.Type != "" {
				out <- m
			}
		}
		if err != nil {
			return
		}
	}
}

// readLines sends the lines of r, the CLI's errors, until it closes.
func readLines(r io.ReadCloser, out chan<- string) {
	defer close(out)
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		if l := strings.TrimRight(sc.Text(), " \r"); l != "" {
			out <- l
		}
	}
}

// readKeys sends the keys the terminal sends until it closes.
func readKeys(r io.Reader, out chan<- []key) {
	defer close(out)
	var kr keyReader
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if ks := kr.feed(buf[:n]); len(ks) > 0 {
				out <- ks
			}
		}
		if err != nil {
			return
		}
	}
}

// ui is the terminal: the log, which scrolls up, and the area at its
// bottom, which holds the reply's line in progress and then the line being
// typed, or the question that waits for an answer.
type ui struct {
	out    io.Writer
	v      *view
	writes chan<- []byte
	getenv func(string) string

	typed    string    // what is typed and not sent
	busy     bool      // a turn runs
	stopping bool      // the user asked the turn to stop
	since    time.Time // when it began
	ask      *ask      // a permission prompt waiting for an answer
	hint     string    // a note for the prompt row, until the next key
	ctrlC    time.Time // when Ctrl+C last found nothing to stop
	ending   bool      // the user ended the session: its input is closed
	ended    bool      // the CLI exited
	rows     int       // rows the area took when last drawn
	asked    int       // control requests sent, to name the next
}

// ask is a permission prompt the CLI sent.
type ask struct {
	id     string
	tool   string
	title  string
	input  json.RawMessage
	choice int // 0 yes, 1 no
}

// Words the agent gets back.
const (
	noteDenied = "The user said no to this. Stop, and wait for them to say how to go on."
	noteForm   = "This session cannot show a question form. Ask the user in your reply instead, then end your turn so they can answer."
)

// send queues a line for the CLI's input; nothing goes once the session
// ends.
func (u *ui) send(b []byte) {
	if !u.ending {
		u.writes <- b
	}
}

// submit sends text as the user's next message and shows it in the log.
func (u *ui) submit(text string) {
	text = strings.TrimRight(text, " \t\r\n")
	if strings.TrimSpace(text) == "" || u.ending {
		return
	}
	shown := wrap(text, u.v.width-2)
	const most = 12
	var lines []string
	for i, l := range shown {
		if i == most {
			lines = append(lines, u.v.faint("  … "+plural(len(shown)-most, "more line")))
			break
		}
		prefix := "  "
		if i == 0 {
			prefix = "› "
		}
		lines = append(lines, bold+prefix+l+reset)
	}
	u.render(append([]string{""}, lines...))
	u.send(userLine(text))
	u.begin()
}

// begin marks a turn as running.
func (u *ui) begin() {
	if !u.busy {
		u.busy, u.since = true, time.Now()
	}
}

// message shows one line from the CLI, and answers it when it asks.
func (u *ui) message(m message) {
	switch m.Type {
	case "control_request":
		u.control(m)
	case "control_cancel_request":
		if u.ask != nil && u.ask.id == m.RequestID {
			u.ask = nil
			u.render([]string{u.v.faint("  · the question was withdrawn")})
		}
	case "control_response":
		if m.Response != nil && m.Response.Subtype == "error" {
			u.render([]string{"  ! " + m.Response.Error})
		}
	case "result":
		took := time.Since(u.since)
		if u.stopping && m.IsError {
			// A turn the user stopped ends as an error, whose text is the
			// CLI's own diagnostic.
			m.IsError, m.Errors, m.Result, m.Subtype = false, nil, "", "success"
			u.v.stopped = true
		}
		u.busy, u.stopping = false, false
		u.render(u.v.result(m, took))
		u.report()
	default:
		if m.Type == "system" && m.Subtype == "init" {
			// A message sent while the last turn ran starts a turn of its
			// own.
			u.begin()
			defer u.report()
		}
		u.render(u.v.add(m))
	}
}

// control answers the CLI's control requests: permission prompts are asked
// in the terminal, except for a question form, which this program cannot
// show, so the agent asks in its reply instead.
func (u *ui) control(m message) {
	r := m.Request
	if r == nil {
		return
	}
	switch {
	case r.Subtype != "can_use_tool":
		u.send(refuseLine(m.RequestID, "irie sdk does not take "+r.Subtype))
	case r.ToolName == "AskUserQuestion":
		u.send(denyLine(m.RequestID, noteForm, false))
		u.render([]string{u.v.faint("  · asked the agent to put its question in its reply")})
	default:
		u.ask = &ask{id: m.RequestID, tool: r.ToolName, title: cmp.Or(r.DisplayName, r.ToolName), input: r.Input}
		var lines []string
		for _, l := range details(r.ToolName, r.Input) {
			lines = append(lines, u.v.faint(l))
		}
		u.render(lines)
	}
}

// answer answers the permission prompt.
func (u *ui) answer(yes bool) {
	a := u.ask
	u.ask = nil
	if yes {
		u.send(allowLine(a.id, a.input))
		u.render([]string{u.v.faint("  · allowed")})
		return
	}
	u.send(denyLine(a.id, noteDenied, true))
	u.render([]string{u.v.faint("  · not allowed")})
}

// interrupt asks the CLI to stop the turn.
func (u *ui) interrupt() {
	u.asked++
	u.stopping = true
	u.send(interruptLine(fmt.Sprintf("req_%d_irie", u.asked)))
	u.render([]string{u.v.faint("  · stopping…")})
}

// end ends the session: its input closes, and the CLI ends once its turn
// does.
func (u *ui) end() {
	if u.ending {
		return
	}
	u.ending = true
	close(u.writes)
	u.render([]string{u.v.faint("  · ending the session…")})
}

// key acts on one key.
func (u *ui) key(k key) {
	u.hint = ""
	if u.ask != nil {
		switch {
		case k.kind == keyRune && strings.ContainsAny(k.text, "1yY") && len(k.text) == 1:
			u.answer(true)
		case k.kind == keyRune && strings.ContainsAny(k.text, "2nN") && len(k.text) == 1:
			u.answer(false)
		case k.kind == keyUp:
			u.ask.choice = 0
		case k.kind == keyDown:
			u.ask.choice = 1
		case k.kind == keyEnter:
			u.answer(u.ask.choice == 0)
		case k.kind == keyEscape:
			u.answer(false)
		case k.kind == keyInterrupt:
			u.interrupt()
		}
		return
	}
	switch k.kind {
	case keyRune, keyPaste:
		u.typed += strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(k.text)
	case keyEnter:
		text := u.typed
		u.typed = ""
		u.submit(text)
	case keyBackspace:
		if _, n := utf8.DecodeLastRuneInString(u.typed); n > 0 {
			u.typed = u.typed[:len(u.typed)-n]
		}
	case keyClearLine:
		u.typed = ""
	case keyDeleteWord:
		t := strings.TrimRight(u.typed, " \n")
		u.typed = t[:strings.LastIndexAny(t, " \n")+1]
	case keyEscape:
		if u.busy {
			u.interrupt()
		}
	case keyInterrupt:
		switch {
		case u.busy:
			u.interrupt()
		case u.typed != "":
			u.typed = ""
		case time.Since(u.ctrlC) < 2*time.Second:
			u.end()
		default:
			u.ctrlC = time.Now()
			u.hint = "Ctrl+C again ends the session"
		}
	case keyEnd:
		if u.typed == "" {
			u.end()
		}
	}
}

// render prints lines to the log, above the area, and draws the area again.
// The area's rows each fit in one row of the terminal, so it can be found
// and cleared again by its count.
func (u *ui) render(lines []string) {
	var b strings.Builder
	b.WriteString("\r")
	if u.rows > 1 {
		fmt.Fprintf(&b, "\x1b[%dA", u.rows-1)
	}
	b.WriteString("\x1b[J")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\r\n")
	}
	rows, back := u.area()
	b.WriteString(strings.Join(rows, "\r\n"))
	if back > 0 {
		fmt.Fprintf(&b, "\x1b[%dD", back)
	}
	u.rows = len(rows)
	io.WriteString(u.out, b.String())
}

// area is the bottom of the screen, as rows, and how many columns the
// cursor goes back from the end of the last, to sit where typing goes.
func (u *ui) area() ([]string, int) {
	var rows []string
	if u.v.line != "" {
		rows = append(rows, u.v.line)
	}
	if u.ended {
		return rows, 0
	}
	if a := u.ask; a != nil {
		rows = append(rows, bold+"? "+reset+u.v.fit("Allow "+a.title+": "+summary(a.tool, a.input)))
		for i, c := range []string{"Yes", "No"} {
			mark := "  "
			if i == a.choice {
				mark = "❯ "
			}
			rows = append(rows, fmt.Sprintf("%s%d. %s", mark, i+1, c))
		}
		return append(rows, u.v.faint("Enter picks · Esc says no")), 0
	}
	if u.ending {
		return append(rows, u.v.faint("  · ending…")), 0
	}
	prompt := "› "
	if u.busy {
		prompt = "… "
	}
	if u.typed == "" {
		hint := cmp.Or(u.hint, "type a message · Ctrl+D ends the session")
		if u.busy && u.hint == "" {
			hint = "working · Esc stops it · what you type goes next"
		}
		hint = fitTo(hint, u.v.width-1-lipgloss.Width(prompt))
		return append(rows, prompt+dim+hint+reset), lipgloss.Width(hint)
	}
	// The end of what is typed, as much as fits.
	shown := strings.ReplaceAll(u.typed, "\n", "↵ ")
	room := u.v.width - 1 - lipgloss.Width(prompt)
	for lipgloss.Width(shown) > room {
		_, n := utf8.DecodeRuneInString(shown)
		shown = shown[n:]
	}
	return append(rows, prompt+shown), 0
}

// report tells the core what the session has cost and how full its
// context is, as the status line of a Claude Code session would (DESIGN
// §6.3). Without Irie it does nothing.
func (u *ui) report() {
	session, socket := u.getenv("IRIE_SESSION"), u.getenv("IRIE_SOCKET")
	if session == "" || socket == "" {
		return
	}
	payload := statusPayload(u.v)
	go func() {
		c, err := ipc.Dial(socket, 200*time.Millisecond)
		if err != nil {
			return
		}
		defer c.Close()
		c.Timeout = time.Second
		var r ipc.StatusReply
		c.Call(ipc.OpStatus, ipc.StatusArgs{Session: session, Payload: payload}, &r)
	}()
}

// statusPayload is what the view knows of the session's model, cost and
// context, in the fields of Claude Code's status line JSON that Irie reads.
func statusPayload(v *view) []byte {
	p := map[string]any{}
	if v.model != "" {
		p["model"] = map[string]any{"id": v.model, "display_name": v.model}
	}
	if v.costed {
		p["cost"] = map[string]any{"total_cost_usd": v.cost}
	}
	if v.window > 0 && v.context > 0 {
		p["context_window"] = map[string]any{
			"used_percentage":     min(100, 100*float64(v.context)/float64(v.window)),
			"context_window_size": v.window,
		}
	}
	b, _ := json.Marshal(p)
	return b
}
