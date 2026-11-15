//go:build !nowriting

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/variorum"
)

// The writing plugin's commands (DESIGN §4.17): irie variorum. go build
// -tags nowriting leaves this file out. The writing plugin's lines in the
// usage.
const (
	writingUsage = `  ` + brand.Slug + ` variorum [status|connect|forget]
                          Variorum's door for writing sessions and the
                          Variorum tab: where it stands; keep a setup copied
                          in Variorum (pbpaste | ` + brand.Slug + ` variorum connect);
                          or forget both
`
	writingServerUsage = `  ` + brand.Slug + ` variorum door        Variorum's door, as a writing session's own MCP
                            server
`
)

const variorumUsage = `usage: ` + brand.Slug + ` variorum [status]   where ` + brand.Name + `'s part of Variorum stands
       ` + brand.Slug + ` variorum connect    keep a setup copied in Variorum, read from stdin:
                              pbpaste | ` + brand.Slug + ` variorum connect
       ` + brand.Slug + ` variorum forget     delete both of the setups ` + brand.Name + ` keeps
       ` + brand.Slug + ` variorum door       Variorum's door, as a writing session's own MCP
                              server; ` + brand.Name + ` runs it

` + brand.Name + ` keeps two of Variorum's setups, from Settings ▸ MCP ▸ Connect a client:
Claude Code's, for writing sessions, and Any MCP client's, for the Variorum
tab. It keeps them in its data folder, readable by you alone, and never
shows one.`

func init() {
	pluginCmds["variorum"] = pluginCmd{
		run: func(args []string) error { return variorumCmd(args, os.Stdin, os.Stdout) },
		code: func(args []string) (int, bool) {
			if len(args) > 0 && args[0] == "door" {
				return variorumDoor(args[1:], os.Stdin, os.Stdout), true
			}
			return 0, false
		},
	}
}

// maxSetup is the most of stdin that connect reads: a setup is a line, or
// a few of JSON.
const maxSetup = 64 << 10

// variorumCmd is `irie variorum`: where Irie's part of Variorum stands,
// and its setups kept or forgotten. It reads and writes Irie's files
// itself, as the core's variorum op does, so it needs no core.
func variorumCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	action := ipc.VariorumStatus
	if len(args) > 0 {
		action, args = args[0], args[1:]
	}
	if len(args) > 0 {
		return errors.New(variorumUsage)
	}
	switch action {
	case ipc.VariorumStatus, ipc.VariorumConnect, ipc.VariorumForget:
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, variorumUsage)
		return nil
	default:
		return errors.New(variorumUsage)
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	r, err := variorumAction(cfg, p, action, stdin)
	if err != nil {
		return err
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	fmt.Fprintln(stdout, r.Text)
	return nil
}

// variorumAction is the variorum op's status, connect or forget, as the
// core answers it, but for the setups' switch: they serve both of the
// writing plugin's parts, so they are here while either is on.
func variorumAction(cfg config.Config, p paths.Paths, action string, stdin io.Reader) (ipc.VariorumResult, error) {
	if why := setupsWhy(cfg); why != "" {
		r := variorum.Status(p.Data)
		r.State, r.Text = ipc.VariorumOff, why
		if action != ipc.VariorumStatus {
			r.Error = why
		}
		return r, nil
	}
	switch action {
	case ipc.VariorumConnect:
		b, err := io.ReadAll(io.LimitReader(stdin, maxSetup+1))
		if err != nil {
			return ipc.VariorumResult{}, err
		}
		if len(b) > maxSetup {
			return ipc.VariorumResult{}, errors.New("That's far longer than a setup: copy one in Variorum's Settings ▸ MCP ▸ Connect a client, then run `pbpaste | " + brand.Slug + " variorum connect`.")
		}
		r, _ := variorum.Connect(p.Data, string(b))
		return r, nil
	case ipc.VariorumForget:
		return variorum.ForgetAll(p.Data), nil
	}
	return variorum.Status(p.Data), nil
}

// setupsWhy is why Irie keeps none of Variorum's setups now, or "" while
// they serve :write sessions or the Variorum tab: they go only when both
// parts are off, and the tab's switch is the one to name then.
func setupsWhy(cfg config.Config) string {
	if plugins.Why(cfg, plugins.Variorum) == "" {
		return ""
	}
	return plugins.Why(cfg, plugins.VariorumTab)
}

// variorumDoor is `irie variorum door`, which a writing session runs as
// its own MCP server: it becomes Variorum's door, and writes nothing to
// stdout before, which is the session's channel to it. When it can't, it
// answers the session's opening with why.
func variorumDoor(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage: "+brand.Slug+" variorum door")
		return 2
	}
	cfg, p, err := load()
	switch {
	case p.Data == "":
		why := brand.Name + " can't find its data folder"
		if err != nil {
			why += ": " + err.Error()
		}
		variorum.Refuse(stdin, stdout, why+".")
		return 0
	case plugins.Why(cfg, plugins.Variorum) != "":
		// The door is a writing session's, whatever the tab's switch says.
		variorum.Refuse(stdin, stdout, plugins.Why(cfg, plugins.Variorum))
		return 0
	}
	return variorum.Door(p.Data, stdin, stdout)
}
