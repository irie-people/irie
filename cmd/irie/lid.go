package main

// The lid (DESIGN §4.12): `irie setup lid` lets Irie keep a MacBook awake
// with its lid closed while sessions work, `irie lid` says how that stands,
// and `irie lid check` is the launchd agent's look for a hold no core keeps.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/lid"
	"github.com/irie-people/irie/internal/paths"
)

const lidUsage = `usage: irie lid [check [--file PATH]]

  irie lid                whether Irie keeps this Mac awake with its lid
                          closed now, and why
  irie lid check          turn the Mac's sleep back on if no core has kept it
                          off for a while; the launchd agent runs it
  irie setup lid [--remove]
                          let Irie keep the Mac awake with its lid closed
                          while sessions work; asks for your password once
`

func lidCmd(args []string) error {
	if len(args) > 0 && args[0] == "check" {
		fs := flag.NewFlagSet("lid check", flag.ContinueOnError)
		file := fs.String("file", "", "the core's lid.json; the state dir's by default")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := *file
		if path == "" {
			p, err := paths.FromEnv()
			if err != nil {
				return err
			}
			path = p.LidFile()
		}
		did, err := lid.Check(lid.Mac{}, path, time.Now())
		if did != "" {
			fmt.Println("irie lid check: turned the Mac's sleep back on:", did)
		}
		return err
	}
	if len(args) > 0 {
		if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
			fmt.Print(lidUsage)
			return nil
		}
		fmt.Fprint(os.Stderr, lidUsage)
		return exitCode{2, errors.New("unknown lid command " + strconv.Quote(args[0]))}
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	lidStatus(os.Stdout, cfg.Lid, p, lid.Mac{}, time.Now())
	return nil
}

// lidStatus is `irie lid`: the setting, whether it is set up, what the core
// last did and the Mac as it is now.
func lidStatus(w io.Writer, n config.Lid, p paths.Paths, m lid.Mac, now time.Time) {
	switch n.Awake {
	case config.LidWork:
		fmt.Fprintf(w, "lid.awake = work: with its lid closed, the Mac stays awake while sessions work, and %v after.\n", n.Grace)
	case config.LidAlways:
		fmt.Fprintln(w, "lid.awake = always: with its lid closed, the Mac stays awake while Irie runs.")
	default:
		fmt.Fprintln(w, "lid.awake = off: the Mac sleeps when its lid closes, as macOS decides.")
	}
	if !lid.Supported() {
		fmt.Fprintln(w, "This is not a Mac: Irie leaves its sleep alone.")
		return
	}
	home, _ := os.UserHomeDir()
	_, agentErr := os.Stat(lid.AgentPath(home))
	switch ok := m.Allowed(); {
	case !ok:
		fmt.Fprintln(w, "Not set up: run irie setup lid, which asks for your password once.")
	case agentErr != nil:
		fmt.Fprintln(w, "Set up, but the check that turns sleep back on if no core does is missing: run irie setup lid again.")
	default:
		fmt.Fprintln(w, "Set up.")
	}
	s, at, err := lid.Read(p.LidFile())
	switch {
	case err != nil:
		fmt.Fprintln(w, "lid.json:", err)
	case s.Off:
		fmt.Fprintf(w, "Now: Irie keeps the Mac awake, since %s (%s); last word %s ago.\n", s.Since.Local().Format("Jan 2 15:04"), s.Why, now.Sub(at).Round(time.Second))
	case !s.Since.IsZero():
		fmt.Fprintf(w, "Now: the Mac sleeps as usual, since %s (%s).\n", s.Since.Local().Format("Jan 2 15:04"), s.Why)
	default:
		fmt.Fprintln(w, "Now: the Mac sleeps as usual; Irie has not kept it awake.")
	}
	var facts []string
	if closed, err := m.Closed(); err == nil {
		facts = append(facts, map[bool]string{true: "its lid is closed", false: "its lid is open"}[closed])
	}
	if pw, err := m.Power(); err == nil {
		switch {
		case pw.Percent < 0:
			facts = append(facts, "on power")
		case pw.Battery:
			facts = append(facts, fmt.Sprintf("on battery at %d%%", pw.Percent))
		default:
			facts = append(facts, fmt.Sprintf("plugged in at %d%%", pw.Percent))
		}
	}
	if h, err := m.Heat(); err == nil {
		facts = append(facts, "heat "+[]string{"nominal", "moderate", "heavy", "trapping", "sleeping"}[min(max(h, 0), 4)])
	}
	if off, err := m.IsOff(); err == nil && off && !s.Off {
		facts = append(facts, "sleep is off, but not by Irie")
	}
	if len(facts) > 0 {
		fmt.Fprintf(w, "The Mac: %s.\n", strings.Join(facts, "; "))
	}
}

// setupLid is `irie setup lid`: the sudoers rule, written as root through
// one password prompt (sudo in a terminal, else macOS's own dialog), and
// the launchd agent. --remove takes both away again.
func setupLid(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("setup lid", flag.ContinueOnError)
	remove := fs.Bool("remove", false, "take the rule and the launchd agent away again")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: irie setup lid [--remove]")
	}
	if !lid.Supported() {
		return errors.New("setup lid: a lid is kept open only on a Mac")
	}
	if os.Geteuid() == 0 {
		return errors.New("setup lid: run it as yourself, not as root: it asks for your password itself")
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	m := lid.Mac{}
	agent := lid.AgentPath(home)
	uid := strconv.Itoa(os.Getuid())
	if *remove {
		// Sleep the core turned off goes back on while the rule still allows it.
		if s, _, _ := lid.Read(p.LidFile()); s.Off {
			if err := m.SleepOff(false); err != nil {
				return fmt.Errorf("setup lid: turn sleep back on: %w", err)
			}
			lid.Write(p.LidFile(), lid.State{Off: false, Since: time.Now(), Why: "irie setup lid --remove"})
		}
		exec.Command("/bin/launchctl", "bootout", "gui/"+uid+"/"+lid.Label).Run()
		if err := os.Remove(agent); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := os.Stat(lid.Rule); err == nil {
			if err := asRoot(lid.RemoveScript(), "Irie wants to remove the rule that let it keep this Mac awake with its lid closed.", stdin); err != nil {
				return fmt.Errorf("setup lid: remove %s: %w", lid.Rule, err)
			}
		}
		fmt.Fprintln(stdout, "Done: the Mac sleeps when its lid closes again, whatever Irie is doing.")
		return nil
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	text, err := lid.RuleText(u.Username)
	if err != nil {
		return err
	}
	if !m.Allowed() {
		script, err := lid.InstallScript(text)
		if err != nil {
			return err
		}
		if err := asRoot(script, "Irie wants to keep this Mac awake with its lid closed while agents work. It adds a rule that lets you turn sleep off and on without a password.", stdin); err != nil {
			return fmt.Errorf("setup lid: install %s: %w", lid.Rule, err)
		}
		if !m.Allowed() {
			return fmt.Errorf("setup lid: %s is in place, but sudo does not take it", lid.Rule)
		}
	}
	// The check runs the installed irie, which outlives a build in a
	// worktree.
	bin := filepath.Join(p.Data, "bin", "irie")
	if _, err := os.Stat(bin); err != nil {
		if bin, err = os.Executable(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(agent), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(agent, []byte(lid.AgentPlist(bin, p.LidFile())), 0o644); err != nil {
		return err
	}
	exec.Command("/bin/launchctl", "bootout", "gui/"+uid+"/"+lid.Label).Run()
	if out, err := exec.Command("/bin/launchctl", "bootstrap", "gui/"+uid, agent).CombinedOutput(); err != nil {
		return fmt.Errorf("setup lid: launchctl bootstrap %s: %v: %s", agent, err, strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(stdout, `Done: with its lid closed, this Mac now stays awake while Irie's sessions work.
  The rule:  %s lets you run pmset -a disablesleep 1 or 0 without a password.
  The check: %s turns sleep back on if no core does.
Undo it with irie setup lid --remove.
`, strings.TrimPrefix(lid.Rule, "/private"), agent)
	return nil
}

// asRoot runs script as root, through sudo's prompt in a terminal or macOS's
// password dialog, which says why.
func asRoot(script, why string, stdin io.Reader) error {
	if isTerminal(stdin) {
		cmd := exec.Command("/usr/bin/sudo", "/bin/sh", "-c", script)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	out, err := exec.Command("/usr/bin/osascript", "-e", "do shell script "+quote(script)+" with prompt "+quote(why)+" with administrator privileges").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
