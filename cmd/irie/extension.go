//go:build !noextension

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/extension"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/repo"
)

// The browser extension plugin's commands (docs/proposals/browser-
// extension.md): irie setup extension, irie extension and irie terms.
// go build -tags noextension leaves them out.

const (
	// extensionUsage is the plugin's lines in the usage.
	extensionUsage = `  ` + brand.Slug + ` terms [ask|chat] ADDRESS|FILE [QUESTION]
                          a site's terms of service in plain words, and answers
                          to questions about them, by ` + brand.Name + `'s own code
                          (` + brand.Slug + ` terms help)
  ` + brand.Slug + ` setup extension [--remove]
                          ` + brand.Name + ` in your own browser: write the browser
                          extension, and let Chrome, Brave, Edge, Arc and
                          Firefox reach ` + brand.Name + ` from it
`
	extensionServerUsage = `  ` + brand.Slug + ` extension host       the browser extension's way into ` + brand.Name + `, which
                            the browser starts (native messaging)
`
)

func init() {
	pluginCmds["extension"] = pluginCmd{run: func(args []string) error { return extensionCmd(args, os.Stdin, os.Stdout) }}
	pluginSetups["extension"] = setupExtension
}

const extensionHelp = `usage: ` + brand.Slug + ` extension [status]   how the browser extension stands here
       ` + brand.Slug + ` extension path       the folder a browser loads it from
       ` + brand.Slug + ` extension host       the extension's way into ` + brand.Name + `: the browser
                                starts it, with the extension's address
`

func extensionCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	sub := "status"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	_, p, err := load()
	if err != nil && sub != "host" {
		return err
	}
	switch sub {
	case "host":
		// The browser runs this with the extension's address (Chrome's)
		// or its manifest and ID (Firefox's); stdin and stdout are the
		// browser's, and nothing else may be written to stdout.
		var log *slog.Logger
		var logf *os.File
		if filepath.IsAbs(p.State) && os.MkdirAll(p.State, 0o700) == nil {
			logf, err = os.OpenFile(filepath.Join(p.State, "extension.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		}
		if logf != nil && err == nil {
			defer logf.Close()
			log = slog.New(slog.NewTextHandler(logf, nil)).With("pid", os.Getpid())
			log.Info("host", "from", strings.Join(args, " "))
		}
		return extension.Serve(context.Background(), stdin, stdout, extension.Options{
			Paths: p,
			Load: func() (config.Config, error) {
				cfg, _, err := load()
				return cfg, err
			},
			Why:     func(cfg config.Config, part string) string { return plugins.Why(cfg, part) },
			Servers: modelServers,
			Log:     log,
		})
	case "path":
		fmt.Fprintln(stdout, extension.Folder(p))
		return nil
	case "status":
		h, _ := os.UserHomeDir()
		s := extension.Look(p, h)
		if s.Written == "" {
			fmt.Fprintf(stdout, "Not set up yet: `%s setup extension` writes it.\n", brand.Slug)
			return nil
		}
		fmt.Fprintf(stdout, "The extension (%s) is in %s.\n", s.Written, repo.Tilde(s.Folder, h))
		if len(s.Browsers) > 0 {
			fmt.Fprintf(stdout, "%s can reach %s from it.\n", strings.Join(s.Browsers, ", "), brand.Name)
		}
		if seen := extension.Seen(s, time.Now()); seen != "" {
			fmt.Fprintf(stdout, "Last used in %s.\n", seen)
		} else {
			fmt.Fprintln(stdout, "No browser has loaded it yet.")
		}
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, extensionHelp)
		return nil
	}
	return errors.New(strings.TrimSpace(extensionHelp))
}

// setupExtension is irie setup extension: it writes the extension and the
// host's files, and says the one step left, which only the person can
// take in their browser.
func setupExtension(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("setup extension", flag.ContinueOnError)
	remove := fs.Bool("remove", false, "take out what setup wrote")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if *remove {
		if err := extension.Remove(p, h); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Taken out. Remove the extension in your browser's extensions page too.")
		return nil
	}
	if !cfg.Plugins.Extension {
		edits, err := plugins.Edits("extension", "", true)
		if err != nil {
			return err
		}
		if err := setConfig(p, edits); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Turned the browser extension plugin on.")
	}
	bin, err := self()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	done, err := extension.Setup(p, h, bin)
	if len(done) == 0 {
		return err
	}
	if err != nil {
		fmt.Fprintln(stdout, "Some browsers couldn't be set up:", err)
	}
	folder := repo.Tilde(extension.Folder(p), h)
	fmt.Fprintf(stdout, "Ready for %s. One step is yours, once, in each browser:\n\n", strings.Join(done, ", "))
	fmt.Fprintf(stdout, "  Chrome, Brave, Edge or Arc: open chrome://extensions, turn on Developer\n  mode, choose Load unpacked and pick %s\n  (in the folder picker, ⌘⇧G takes the path).\n", folder)
	for _, b := range done {
		if b == "Firefox" {
			fmt.Fprintf(stdout, "  Firefox: open about:debugging#/runtime/this-firefox, choose Load Temporary\n  Add-on and pick manifest.json in %s; Firefox forgets it when it quits.\n", folder)
		}
	}
	fmt.Fprintf(stdout, "\nThen press %s's button in the toolbar on any site to read its terms.\n", brand.Name)
	return nil
}
