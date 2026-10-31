package localmodel

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// installDirs are where Ollama's installers put the binary: Homebrew on
// Apple silicon and on Intel Macs, the link the macOS app offers, the Linux
// script, and the macOS app itself.
var installDirs = []string{
	"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin",
	"/Applications/Ollama.app/Contents/Resources",
}

// ErrNoOllama means the ollama binary was not found.
var ErrNoOllama = errors.New("ollama not found")

// FindOllama returns the ollama binary: the configured path, else ollama
// on PATH, else the usual install places (a GUI launch may lack the user's
// PATH).
func FindOllama(configured string) (string, error) {
	if configured != "" {
		if isExec(configured) {
			return configured, nil
		}
		return "", errors.New("local.ollama " + configured + " is not an executable")
	}
	if p, err := exec.LookPath("ollama"); err == nil {
		return p, nil
	}
	for _, dir := range installDirs {
		if p := filepath.Join(dir, "ollama"); isExec(p) {
			return p, nil
		}
	}
	return "", ErrNoOllama
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// Install is how to install Ollama on this system.
type Install struct {
	Argv []string // a command setup may run with the user's yes; nil when there is none
	Hint string   // what to tell the user
}

// HowToInstall returns Ollama's official install route for goos: on Linux
// its install script; on macOS its app, or Homebrew's formula when brew is
// there, which installs the command line and server that Irie runs.
func HowToInstall(goos string, lookPath func(string) (string, error)) Install {
	switch goos {
	case "linux":
		return Install{
			Argv: []string{"sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh"},
			Hint: "Ollama's install script: curl -fsSL https://ollama.com/install.sh | sh",
		}
	case "darwin":
		if brew, err := lookPath("brew"); err == nil {
			return Install{
				Argv: []string{brew, "install", "ollama"},
				Hint: "Homebrew's ollama formula: brew install ollama (or the app from https://ollama.com/download)",
			}
		}
		return Install{Hint: "download Ollama from https://ollama.com/download, open the app once, then run `irie setup local` again"}
	}
	return Install{Hint: "install Ollama from https://ollama.com/download, then run `irie setup local` again"}
}

// HowToInstallHere is HowToInstall for this system.
func HowToInstallHere() Install { return HowToInstall(runtime.GOOS, exec.LookPath) }
