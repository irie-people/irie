package variorum

import (
	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// The words of where things stand.
const (
	notInstalled = "Variorum isn't installed: it's free from " + Site + ", for macOS 26 or later on Apple silicon."
	noneKept     = brand.Name + " keeps none of Variorum's setups yet. In " + copyWhere + ", choose Claude Code, copy its setup and " + connect + "; then the same with Any MCP client."
	noSessions   = brand.Name + " keeps Any MCP client's setup, for the Variorum tab, but not Claude Code's, for writing sessions: in " + copyWhere + ", choose Claude Code, copy its setup and " + connect + "."
	noTab        = brand.Name + " keeps Claude Code's setup, for writing sessions, but not Any MCP client's, for the Variorum tab: in " + copyWhere + ", choose Any MCP client, copy its setup and " + connect + "."
	ready        = "Ready: " + brand.Name + " keeps both of Variorum's setups. Writing sessions get Variorum's door when they start, and the Variorum tab reads what you share with " + brand.Slug + "."
	forgot       = "Forgot both of Variorum's setups."
)

// check is Check of a kept setup's program; nil when none is kept.
func (s *Setup) check() error {
	if s == nil {
		return nil
	}
	return Check(s.Command)
}

// copyAgain is what fixes a kept setup that fails its checks.
func copyAgain(kind string) string {
	return "Copy " + client(kind) + " setup again in " + copyWhere + " and " + connect + "."
}

// client is whose setup kind is, as Variorum's menu names it.
func client(kind string) string {
	if kind == Sessions {
		return "Claude Code's"
	}
	return "Any MCP client's"
}

// Kept says which setup Connect kept.
func Kept(kind string) string {
	if kind == Sessions {
		return "Kept Claude Code's setup, for writing sessions."
	}
	return "Kept Any MCP client's setup, for the Variorum tab."
}

// Status says where Irie's part of Variorum stands, from its files alone:
// whether Variorum is installed (Variorum.app in an Applications folder, or
// a kept setup whose program passes the checks), which setups are kept,
// and whether their programs pass the checks. It calls nothing: the
// writing plugin's check and the variorum op both read it.
func Status(data string) ipc.VariorumResult {
	st, err := Load(data)
	r := ipc.VariorumResult{Sessions: st.Sessions != nil, Tab: st.Tab != nil, Installed: Installed()}
	if err != nil {
		r.State, r.Text = ipc.VariorumNeeds, err.Error()
		return r
	}
	installed, failed := r.Installed, ""
	for _, k := range []struct {
		kind string
		s    *Setup
	}{{Sessions, st.Sessions}, {Tab, st.Tab}} {
		switch err := k.s.check(); {
		case err != nil && failed == "":
			failed = err.Error() + " " + copyAgain(k.kind)
		case err == nil && k.s != nil:
			// A setup whose program passes is Variorum, wherever it is.
			installed = true
		}
	}
	r.Installed = installed
	switch {
	case !installed:
		r.State, r.Text = ipc.VariorumNeeds, notInstalled
	case failed != "":
		r.State, r.Text = ipc.VariorumNeeds, failed
	case st.Sessions == nil && st.Tab == nil:
		r.State, r.Text = ipc.VariorumSetup, noneKept
	case st.Sessions == nil:
		r.State, r.Text = ipc.VariorumSetup, noSessions
	case st.Tab == nil:
		r.State, r.Text = ipc.VariorumSetup, noTab
	default:
		r.State, r.Text = ipc.VariorumReady, ready
	}
	return r
}

// Connect keeps a setup as Variorum copied it, once its program passes
// the checks, and answers where things then stand, with which setup it
// kept, and that kind; or what was wrong with it, and nothing kept.
func Connect(data, setup string) (ipc.VariorumResult, string) {
	kind, s, err := Parse(setup)
	if err == nil {
		err = Check(s.Command)
	}
	if err == nil {
		if err = Save(data, kind, s); err != nil {
			err = &saveError{err}
		}
	}
	r := Status(data)
	if err != nil {
		r.Error = err.Error()
		return r, ""
	}
	r.Text = Kept(kind) + " " + r.Text
	return r, kind
}

type saveError struct{ err error }

func (e *saveError) Error() string {
	return brand.Name + " couldn't keep the setup: " + e.err.Error()
}

// ForgetAll deletes both setups, and answers where things then stand.
func ForgetAll(data string) ipc.VariorumResult {
	err := Forget(data)
	r := Status(data)
	if err != nil {
		r.Error = brand.Name + " couldn't forget Variorum's setups: " + err.Error()
		return r
	}
	r.Text = forgot + " " + r.Text
	return r
}
