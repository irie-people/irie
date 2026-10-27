package ui

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/route"
)

// callTimeout bounds one show or completion.
const callTimeout = 10 * time.Second

// spawnTimeout bounds one spawn, which may make a worktree and check out its
// submodules first.
const spawnTimeout = 10 * time.Minute

// socketBackend sends the sidebar's requests to the core.
type socketBackend struct{ socket string }

func (b socketBackend) Spawn(a ipc.SpawnArgs) (ipc.Session, error) {
	var r ipc.SpawnResult
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpSpawn, a, &r)
	return r.Session, err
}

func (b socketBackend) Complete(text string) (ipc.CompleteResult, error) {
	var r ipc.CompleteResult
	err := ipc.Call(b.socket, callTimeout, ipc.OpComplete, ipc.CompleteArgs{Text: text}, &r)
	return r, err
}

func (b socketBackend) Show(id string, focus bool) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpShow, ipc.ShowArgs{Session: id, Focus: focus}, nil)
}

func (b socketBackend) Rename(id, name string) (ipc.Session, error) {
	var r ipc.RenameResult
	err := ipc.Call(b.socket, callTimeout, ipc.OpRename, ipc.RenameArgs{Session: id, Name: name}, &r)
	return r.Session, err
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

func (b socketBackend) Tool(name string) (ipc.Session, error) {
	var r ipc.SpawnResult
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpTool, ipc.ToolArgs{Name: name}, &r)
	return r.Session, err
}

func (b socketBackend) Route(task string) (route.Choice, error) {
	var r route.Choice
	err := ipc.Call(b.socket, spawnTimeout, ipc.OpRoute, ipc.RouteArgs{Task: task}, &r)
	return r, err
}

// Run is `irie ui`: the sidebar, fed by the core at socket.
func Run(socket string, o Options) error {
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	p := tea.NewProgram(New(socketBackend{socket}, o))
	go follow(p, socket)
	_, err := p.Run()
	return err
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
