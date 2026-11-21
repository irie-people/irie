//go:build !nojammin

package jammin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/host"
)

// Runner runs a script of the core's own in language, JavaScript or
// AppleScript, with args as its run handler's arguments, and gives what it
// returned.
type Runner interface {
	Run(ctx context.Context, language, script string, args ...string) (string, error)
}

// scriptWait is how long one script may take: the Music app answers in a
// moment, unless macOS is asking the person whether Irie may control it.
const scriptWait = 2 * time.Minute

// script runs a script with the Service's Runner, else osascript.
func (s *Service) script(ctx context.Context, language, script string, args ...string) (string, error) {
	if s.Script != nil {
		return s.Script.Run(ctx, language, script, args...)
	}
	return Osascript{}.Run(ctx, language, script, args...)
}

// Osascript runs scripts with osascript(1), the script on its standard
// input and its words as arguments after it, so none of them is ever read
// as script.
type Osascript struct {
	Bin string // "" = /usr/bin/osascript
}

func (o Osascript) Run(ctx context.Context, language, script string, args ...string) (string, error) {
	bin := o.Bin
	if bin == "" {
		bin = "/usr/bin/osascript"
	}
	ctx, cancel := context.WithTimeout(ctx, scriptWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{"-l", language, "-"}, args...)...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = host.WithoutIdentity(os.Environ())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("osascript took longer than %s", scriptWait)
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}
