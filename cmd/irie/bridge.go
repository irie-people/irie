package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/bridge"
	"github.com/irie-people/irie/internal/version"
)

// bridgeCmd is `irie bridge`, the Windows app's way into Irie inside WSL
// (docs/WINDOWS.md §5): the core's socket and the slot's terminal, as
// streams over its standard input and output, which the app reads through
// wsl.exe. It starts the core when none answers, as the Mac app does with
// the irie inside it.
func bridgeCmd(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: " + brand.Slug + " bridge")
	}
	out, err := takeStdout()
	if err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		// A config that does not read still leaves the core's socket where
		// it always is; the app shows the core's own words about the file.
		fmt.Fprintln(os.Stderr, brand.Slug+" bridge:", err)
	}
	err = bridge.Serve(os.Stdin, out, bridge.Options{
		Socket:  p.Socket(),
		Version: version.String(),
		Up:      func() error { return up(nil) },
		Env:     os.Environ(),
		Log:     os.Stderr,
	})
	if errors.Is(err, io.EOF) {
		// The app closed its end: it quit, or wsl.exe did.
		return nil
	}
	return err
}

// takeStdout gives the bridge standard output for itself, and points file
// descriptor 1 at standard error, so nothing else this process or the
// programs it starts print, such as `irie up`'s lines, lands in the
// stream.
func takeStdout() (*os.File, error) {
	fd, err := unix.Dup(1)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	if err := unix.Dup2(2, 1); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "bridge"), nil
}
