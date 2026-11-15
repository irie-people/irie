// Command windows is Irie's Windows wrapper, irie.exe (docs/WINDOWS.md).
// Irie runs inside WSL 2, Windows' own Linux, as it runs on any Linux:
// `irie setup` sets WSL up and installs Irie in it, and every other
// command runs Irie's own there, from this console. Its logic is in
// internal/wsl; this file runs Windows' programs for it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/version"
	"github.com/irie-people/irie/internal/wsl"
)

const usage = brand.Slug + ".exe runs " + brand.Name + ` inside WSL 2, Windows' own Linux.

  ` + brand.Slug + ` setup [--check] [--yes] [--distro NAME]
                 set this computer up: WSL, a Linux to run in (Ubuntu
                 unless you name one), ` + brand.Name + ` in it, and the Start menu,
                 each after a yes. --check only says how far it is
  ` + brand.Slug + ` up          start ` + brand.Name + ` in WSL without opening it
  ` + brand.Slug + `             open ` + brand.Name + `; every other command is ` + brand.Name + `'s own, run in WSL
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout)) }

func run(args []string, in io.Reader, out io.Writer) int {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, brand.Slug+".exe is "+brand.Name+"'s Windows wrapper; on macOS and Linux, run "+brand.Slug+" itself.")
		return 2
	}
	m, e := machine{}, wsl.EnvFromOS()
	if len(args) > 0 {
		switch args[0] {
		case "setup":
			return setup(m, e, args[1:], in, out)
		case "help", "-h", "--help", "version", "--version":
			// The wrapper's own words, then Irie's from WSL when it is set up.
			if args[0] == "version" || args[0] == "--version" {
				fmt.Fprintln(out, brand.Slug+".exe "+version.String()+" (the Windows wrapper)")
			} else {
				fmt.Fprint(out, usage+"\n")
			}
			d, ok := distro(m, e)
			if !ok {
				return 0
			}
			code, err := m.Run("wsl.exe", wsl.Command(d.Name, args[:1])...)
			if err != nil || code == notFound {
				return 0
			}
			return code
		}
	}
	d, ok := distro(m, e)
	if !ok {
		fmt.Fprintln(out, brand.Name+" is not set up on this computer yet.")
		if code := wsl.Setup(m, e, in, out, false, false); code != 0 {
			return code
		}
		d = wsl.Look(m, e).Distro
	}
	code, err := m.Run("wsl.exe", wsl.Command(d.Name, args)...)
	if err == nil && code == notFound {
		fmt.Fprintln(out, brand.Name+" is not installed in "+d.Name+" yet.")
		if code := wsl.Setup(m, e, in, out, false, false); code != 0 {
			return code
		}
		code, err = m.Run("wsl.exe", wsl.Command(d.Name, args)...)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, brand.Slug+": "+err.Error())
		return 1
	}
	return code
}

// notFound is how sh exits when there is no such program: Irie is not
// installed in the distribution yet.
const notFound = 127

// distro is the distribution Irie runs in, from one quick question to WSL;
// ok is false when there is none yet on WSL 2. The full look waits for a
// problem.
func distro(m machine, e wsl.Env) (wsl.Distro, bool) {
	if !e.HasWSL() {
		return wsl.Distro{}, false
	}
	list, _ := m.Output("wsl.exe", "--list", "--verbose")
	d, ok := wsl.Choose(wsl.ParseList(list), e.Distro)
	return d, ok && d.Version == 2
}

func setup(m machine, e wsl.Env, args []string, in io.Reader, out io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	check := fs.Bool("check", false, "only say how far this computer is set up")
	yes := fs.Bool("yes", false, "take every offer without asking")
	distro := fs.String("distro", "", "the WSL distribution to run in, such as Ubuntu-24.04")
	if fs.Parse(args) != nil || fs.NArg() > 0 {
		return 2
	}
	if *distro != "" && !*check {
		if err := wsl.SaveDistro(e, *distro); err != nil {
			fmt.Fprintln(os.Stderr, brand.Slug+": "+err.Error())
			return 1
		}
	}
	if *distro != "" {
		e.Distro = *distro
	}
	return wsl.Setup(m, e, in, out, *check, *yes)
}

// machine runs Windows' programs.
type machine struct{}

// env is the programs' environment: wsl.exe writes UTF-8 with WSL_UTF8=1,
// which wsl.Decode reads either way.
func env() []string { return append(os.Environ(), "WSL_UTF8=1") }

func (machine) Output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env()
	b, err := cmd.Output()
	return wsl.Decode(b), err
}

func (machine) Run(name string, args ...string) (int, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Ctrl+C reaches every program in the console: WSL hands it to Linux,
	// and the wrapper waits for it rather than leave it running alone.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	case err != nil:
		return -1, err
	}
	return 0, nil
}
