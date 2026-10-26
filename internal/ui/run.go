package ui

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

// callTimeout bounds one spawn or show.
const callTimeout = 10 * time.Second

// socketBackend sends the sidebar's requests to the core.
type socketBackend struct{ socket string }

func (b socketBackend) Spawn(a ipc.SpawnArgs) (ipc.Session, error) {
	var r ipc.SpawnResult
	err := ipc.Call(b.socket, callTimeout, ipc.OpSpawn, a, &r)
	return r.Session, err
}

func (b socketBackend) Show(id string, focus bool) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpShow, ipc.ShowArgs{Session: id, Focus: focus}, nil)
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
