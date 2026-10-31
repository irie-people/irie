package localmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Llama is a client of a llama-server, the server of llama.cpp, running
// one model (DESIGN §6.5). It speaks Anthropic's Messages API at
// /v1/messages, which Claude Code needs.
type Llama struct {
	URL  string       // such as http://127.0.0.1:11436
	HTTP *http.Client // nil = http.DefaultClient; every call takes a context
}

func (l Llama) get(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(l.URL, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	c := l.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, fmt.Errorf("llama-server %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

// Ready reports whether the server has loaded its model: GET /health
// answers 503 while it loads and 200 once it serves.
func (l Llama) Ready(ctx context.Context) (bool, error) {
	status, err := l.get(ctx, "/health", nil)
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

// Props is what GET /props says of the server.
type Props struct {
	Context int    // the context of a slot, in tokens
	Slots   int    // requests it serves at once
	Model   string // the model file's path
	Build   string // such as b10180-11b068d06
}

// Props is GET /props.
func (l Llama) Props(ctx context.Context) (Props, error) {
	var v struct {
		Settings struct {
			Context int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		Slots int    `json:"total_slots"`
		Model string `json:"model_path"`
		Build string `json:"build_info"`
	}
	status, err := l.get(ctx, "/props", &v)
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("llama-server /props: HTTP %d", status)
	}
	return Props{Context: v.Settings.Context, Slots: v.Slots, Model: v.Model, Build: v.Build}, err
}

// WaitReady waits until the server has loaded its model, or ctx ends, or
// gone reports that the server has exited.
func (l Llama) WaitReady(ctx context.Context, gone func() bool) error {
	for {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		ok, err := l.Ready(c)
		cancel()
		if ok {
			return nil
		}
		if gone != nil && gone() {
			return errors.New("llama-server exited")
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("llama-server did not load its model: %w", err)
			}
			return errors.New("llama-server did not load its model in time")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Build is what a llama-server binary says it is.
type Build struct {
	Number int    // llama.cpp's build number; 0 = unknown
	Prism  bool   // PrismML's build of llama.cpp, which runs Bonsai's own packings
	Text   string // its version line
}

// String is how Irie names the build.
func (b Build) String() string {
	switch {
	case b.Prism && b.Number > 0:
		return fmt.Sprintf("PrismML build %d", b.Number)
	case b.Prism:
		return "PrismML's build"
	case b.Number > 0:
		return fmt.Sprintf("build %d", b.Number)
	}
	return "an unknown build"
}

// MinRotationBuild is the first upstream llama.cpp build that runs Bonsai
// models' Q2_0 packing; older builds load it without an error and write
// nonsense.
const MinRotationBuild = 11556

var (
	buildRe   = regexp.MustCompile(`\(build (\d+)`)
	versionRe = regexp.MustCompile(`version:\s*(\d+)\b`)
)

// ParseBuild reads `llama-server --version`'s output: upstream builds say
// "version: 10180 (11b068d06)", newer ones "version: X (build N, commit C)".
// PrismML's build says so in its version or in its path.
func ParseBuild(out, bin string) Build {
	var b Build
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "version:") {
			b.Text = strings.TrimSpace(line)
			break
		}
	}
	if m := buildRe.FindStringSubmatch(out); m != nil {
		b.Number, _ = strconv.Atoi(m[1])
	} else if m := versionRe.FindStringSubmatch(out); m != nil {
		b.Number, _ = strconv.Atoi(m[1])
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	b.Prism = strings.Contains(strings.ToLower(out), "prism") || strings.Contains(strings.ToLower(bin), "prism")
	return b
}

// ReadBuild runs `bin --version`.
func ReadBuild(ctx context.Context, bin string) (Build, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return Build{}, fmt.Errorf("%s --version: %w", bin, err)
	}
	return ParseBuild(string(out), bin), nil
}

// Runs says whether build b runs model g right: a problem when it cannot,
// and a warning when Irie cannot tell.
func (b Build) Runs(g GGUF) (problem, warning string) {
	rotation, types := g.Prism()
	switch {
	case b.Prism:
	case types:
		return "it is packed in a type only PrismML's llama.cpp has; set local.llama_server to PrismML's llama-server (https://github.com/PrismML-Eng/llama.cpp)", ""
	case rotation && b.Number == 0:
		return "", fmt.Sprintf("it needs llama.cpp build %d or later, or PrismML's, and Irie cannot tell this llama-server's build; older builds write nonsense on it", MinRotationBuild)
	case rotation && b.Number < MinRotationBuild:
		return fmt.Sprintf("llama.cpp %s loads it without an error but writes nonsense: it needs build %d or later (brew upgrade llama.cpp), or PrismML's llama.cpp in local.llama_server", b, MinRotationBuild), ""
	}
	return "", ""
}

// llamaDirs are where llama.cpp's installers put llama-server: Homebrew on
// Apple silicon and on Intel Macs, and Linux packages.
var llamaDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"}

// ErrNoLlamaServer means no llama-server was found.
var ErrNoLlamaServer = errors.New("llama-server not found")

// FindLlamaServer returns the llama-server binary: the configured path,
// else llama-server on PATH, else the usual install places.
func FindLlamaServer(configured string) (string, error) {
	if configured != "" {
		if isExec(configured) {
			return configured, nil
		}
		return "", errors.New("local.llama_server " + configured + " is not an executable")
	}
	if p, err := exec.LookPath("llama-server"); err == nil {
		return p, nil
	}
	for _, dir := range llamaDirs {
		if p := filepath.Join(dir, "llama-server"); isExec(p) {
			return p, nil
		}
	}
	return "", ErrNoLlamaServer
}
