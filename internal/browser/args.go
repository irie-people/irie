package browser

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"
)

// ServerName is the connector's name in an agent's MCP config: its tools
// are mcp__irie-browser__open and so on.
const ServerName = "irie-browser"

// DefaultIdle is how long the browser waits for a call before it closes.
const DefaultIdle = 10 * time.Minute

// Args are the arguments of `irie browser` that give a server o.
func Args(o Options) []string {
	a := []string{"browser"}
	add := func(flag, v string) {
		if v != "" {
			a = append(a, flag, v)
		}
	}
	add("--path", o.Path)
	add("--profile", o.Profile)
	add("--template", o.Template)
	a = append(a, "--headless="+strconv.FormatBool(o.Headless), "--idle", o.Idle.String())
	add("--proxy", o.Proxy)
	if o.Search != DefaultSearch {
		add("--search", o.Search)
	}
	return a
}

// ParseArgs reads the arguments that follow `irie browser`. check is the
// page --check names: open it, print what an agent reads and exit.
func ParseArgs(args []string, errs io.Writer) (o Options, check string, err error) {
	fs := flag.NewFlagSet("browser", flag.ContinueOnError)
	fs.SetOutput(errs)
	fs.StringVar(&o.Path, "path", "", "the browser's executable (default: Mullvad Browser where it is installed)")
	fs.StringVar(&o.Profile, "profile", "", "the folder for the browser's profile, made fresh at each start and removed after (default: a temporary folder)")
	fs.StringVar(&o.Template, "template", "", "a clean profile, made once and copied at each start, which starts faster (default: none)")
	fs.BoolVar(&o.Headless, "headless", true, "run the browser without a window")
	fs.DurationVar(&o.Idle, "idle", DefaultIdle, "close the browser after this long without a call; 0 keeps it open")
	fs.StringVar(&o.Proxy, "proxy", "", "send the browser's traffic through this SOCKS5 proxy, socks5://host:port")
	fs.StringVar(&o.Search, "search", DefaultSearch, "the search tool's address, with %s for the query")
	fs.StringVar(&check, "check", "", "open this page, print what an agent would read there, and exit")
	if err := fs.Parse(args); err != nil {
		return o, "", err
	}
	if fs.NArg() > 0 {
		return o, "", fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if o.Proxy != "" {
		if _, _, err := ParseProxy(o.Proxy); err != nil {
			return o, "", fmt.Errorf("--proxy %s: %w", o.Proxy, err)
		}
	}
	return o, check, nil
}

// Install is how to install Mullvad Browser on this system: the command to
// run, or nil when there is none to run, and a hint for the user.
type Install struct {
	Argv []string
	Hint string
}

// HowToInstall is Mullvad Browser's install route for goos: Homebrew's cask
// on macOS when brew is found, else Mullvad's own downloads.
func HowToInstall(goos string, lookPath func(string) (string, error)) Install {
	if goos == "darwin" {
		if brew, err := lookPath("brew"); err == nil {
			return Install{Argv: []string{brew, "install", "--cask", "mullvad-browser"}, Hint: "Homebrew's mullvad-browser cask: brew install --cask mullvad-browser (or the disk image from https://mullvad.net/browser)"}
		}
	}
	return Install{Hint: "Mullvad's downloads at https://mullvad.net/browser"}
}
