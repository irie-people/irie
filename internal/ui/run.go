package ui

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/ipc"
)

// callTimeout bounds one show or completion.
const callTimeout = 10 * time.Second

// spawnTimeout bounds one spawn, which may make a worktree and check out its
// submodules first.
const spawnTimeout = 10 * time.Minute

// socketBackend sends the sidebar's requests to the core.
type socketBackend struct{ socket string }

// Input waits as long as a spawn: what was typed may start a task, or
// reopen or move a session.
func (b socketBackend) Input(a ipc.InputArgs) (ipc.InputResult, error) {
	var r ipc.InputResult
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpInput, a, &r)
	return r, err
}

func (b socketBackend) Complete(text string) (ipc.CompleteResult, error) {
	var r ipc.CompleteResult
	err := ipc.Call(b.socket, callTimeout, ipc.OpComplete, ipc.CompleteArgs{Text: text}, &r)
	return r, err
}

func (b socketBackend) Show(id string, focus bool) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpShow, ipc.ShowArgs{Session: id, Focus: focus}, nil)
}

// Move waits as long as a spawn: a move waits its turn behind one.
func (b socketBackend) Move(vendorID, profile string) (ipc.Session, error) {
	var r ipc.MoveResult
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpMove, ipc.MoveArgs{VendorID: vendorID, Profile: profile}, &r)
	return r.Session, err
}

func (b socketBackend) Rate(id, verdict string) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpRate, ipc.RateArgs{Session: id, Verdict: verdict}, nil)
}

func (b socketBackend) Close(id string) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpClose, ipc.CloseArgs{Session: id}, nil)
}

// Undo and Redo wait as long as a spawn: redo may reopen a session, which
// waits for its old agent to end.
func (b socketBackend) Undo(a ipc.UndoArgs) (ipc.UndoResult, error) {
	var r ipc.UndoResult
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpUndo, a, &r)
	return r, err
}

func (b socketBackend) Redo(a ipc.UndoArgs) (ipc.UndoResult, error) {
	var r ipc.UndoResult
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpRedo, a, &r)
	return r, err
}

// Run is `irie ui`: the sidebar, fed by the core at socket.
func Run(socket string, o Options) error {
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	p := tea.NewProgram(New(socketBackend{socket}, o))
	go follow(p, socket)
	go watch(p, socket)
	_, err := p.Run()
	return err
}

// pingMsg is a ping of Raphael's watch (DESIGN §2.4, §4.11), which the
// sidebar answers from its own loop.
type pingMsg struct {
	n      uint64
	answer func(n uint64)
}

// watch asks the core to watch the sidebar, as it watches the Mac app: the
// core pings it once a second on a connection of its own, and the
// program's loop answers each, so a sidebar whose loop is stuck goes
// unanswered, and Raphael force quits it for its supervisor to start it
// again. When the connection ends it asks again; a core from before the
// watch refuses, and is asked again now and then.
func watch(p *tea.Program, socket string) {
	for {
		wait := 2 * time.Second
		if c, err := ipc.Dial(socket, time.Second); err == nil {
			c.Timeout = 5 * time.Second
			if frames, err := c.Subscribe(ipc.SubscribeArgs{View: ipc.ViewWatch, PID: os.Getpid()}); err == nil {
				answer := func(n uint64) { c.Send(ipc.Pong{Pong: n}) }
				for f := range frames {
					if f.T == ipc.FramePing && f.Ping > 0 {
						// Send waits for the loop to take it, as the
						// answer does.
						p.Send(pingMsg{n: f.Ping, answer: answer})
					}
				}
			} else {
				wait = time.Minute
			}
			c.Close()
		}
		time.Sleep(wait)
	}
}

// follow feeds the core's frames to p. While the core cannot be reached
// the header says so, and it retries every second.
func follow(p *tea.Program, socket string) {
	for {
		if c, err := ipc.Dial(socket, time.Second); err == nil {
			if frames, err := c.Subscribe(ipc.SubscribeArgs{}); err == nil {
				for f := range frames {
					p.Send(FrameMsg{Frame: f})
				}
			}
			c.Close()
		}
		p.Send(OfflineMsg{})
		time.Sleep(time.Second)
	}
}
