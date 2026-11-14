package vpn

// Cloudflare WARP, the client of Cloudflare One (Zero Trust), through
// warp-cli: `warp-cli status` for the state, `warp-cli connect` and
// `warp-cli disconnect`, which kept their names through warp-cli's
// renaming of 2023-24. A company's WARP is enrolled in its organisation by
// its own MDM or by the person; Irie only reads and switches it, and
// never accepts Cloudflare's terms for anyone (warp-cli's --accept-tos).

import (
	"errors"
	"strings"
)

func init() {
	kinds = append(kinds, Kind{
		Name:  "warp",
		Title: "Cloudflare WARP",
		Get:   "https://developers.cloudflare.com/cloudflare-one/connections/connect-devices/warp/download-warp/",
		find: func(s Settings) (string, error) {
			return findCLI(s.CLI, []string{"warp-cli"}, []string{
				"/usr/local/bin/warp-cli",
				"/Applications/Cloudflare WARP.app/Contents/Resources/warp-cli",
				"/usr/bin/warp-cli", "/bin/warp-cli",
			})
		},
		open: func(bin string, s Settings) (VPN, error) {
			return Warp{run: newRunner(bin, "warp-cli", s.Timeout)}, nil
		},
	})
}

// Warp is Cloudflare WARP, through warp-cli.
type Warp struct{ run runner }

// Kind is Cloudflare WARP.
func (w Warp) Kind() Kind { k, _ := KindOf("warp"); return k }

// ParseWarp reads `warp-cli status`: "Status update: Connected" and, in
// some states, a "Reason:" line; older clients put the reason on the same
// line, as "Status update: Unable to connect. Reason: Registration
// Missing".
func ParseWarp(out string) (State, bool) {
	var state, reason string
	found := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Status update:"); ok {
			state, found = strings.TrimSpace(v), true
			if s, r, ok := strings.Cut(state, ". Reason:"); ok {
				state, reason = strings.TrimSpace(s), strings.TrimSpace(r)
			}
		} else if v, ok := strings.CutPrefix(line, "Reason:"); ok {
			reason = strings.TrimSpace(v)
		}
	}
	if !found {
		return State{}, false
	}
	switch strings.ToLower(state) {
	case "connected":
		return State{State: Connected}, true
	case "connecting":
		return State{State: Connecting}, true
	case "disconnecting":
		return State{State: Disconnecting}, true
	case "disconnected":
		if strings.Contains(strings.ToLower(reason), "manual") {
			reason = ""
		}
		return State{State: Disconnected, Cause: reason}, true
	case "unable", "unable to connect":
		if reason == "" {
			reason = "it can't connect"
		}
		return State{State: Error, Cause: reason}, true
	}
	cause := state
	if reason != "" {
		cause += ": " + reason
	}
	return State{State: Error, Cause: cause}, true
}

// Status is the tunnel's state now.
func (w Warp) Status() (State, error) {
	out, code, err := w.run.exec(w.run.timeout, "status")
	if err != nil {
		return State{}, err
	}
	if s, ok := ParseWarp(out); ok {
		return s, nil
	}
	return State{}, w.failed([]string{"status"}, out, code)
}

// failed is a command's error; one that stopped at Cloudflare's terms says
// how to accept them, which is the person's to do, never Irie's.
func (w Warp) failed(args []string, out string, code int) error {
	if strings.Contains(out, "Terms of Service") {
		return errors.New("Cloudflare WARP asks you to accept its terms first: open the WARP app, or run warp-cli once in a terminal")
	}
	return w.run.failed(args, out, code)
}

// Connect and Disconnect switch WARP on and off. A company may lock the
// switch, and then warp-cli says so.
func (w Warp) Connect() error { return w.do("connect", true) }

func (w Warp) Disconnect() error { return w.do("disconnect", false) }

func (w Warp) do(cmd string, long bool) error {
	limit := w.run.timeout
	if long {
		limit *= 2
	}
	out, code, err := w.run.exec(limit, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return w.failed([]string{cmd}, out, code)
	}
	return nil
}
