package proc

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// lsofTimeout bounds one listing of every process's files.
const lsofTimeout = 30 * time.Second

// lsofPaths are where macOS keeps lsof, and where Homebrew would.
var lsofPaths = []string{"/usr/sbin/lsof", "/usr/bin/lsof", "/opt/homebrew/bin/lsof", "/usr/local/bin/lsof"}

// holders reads every process's working directory, program and open files
// with lsof(8), the one way macOS shows them without privileges: -F gives
// a field a line, p the pid, c the command, f the descriptor (cwd, txt or
// a number) and n the name; -n and -P skip name lookups, -w its warnings.
// Another user's processes it shows only as far as the system lets it.
func holders() ([]Holder, error) {
	bin := ""
	for _, p := range lsofPaths {
		if _, err := exec.LookPath(p); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("lsof"); err != nil {
			return nil, fmt.Errorf("no lsof to read what processes hold: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), lsofTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-n", "-P", "-w", "-F", "pcfn")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("lsof gave no answer within %v", lsofTimeout)
	}
	// A listing that did not finish may lack what holds a folder, so it
	// counts for nothing.
	if err != nil {
		return nil, fmt.Errorf("lsof: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseLsof(out), nil
}

// parseLsof reads lsof's -F pcfn output: the files that have a path.
func parseLsof(out []byte) []Holder {
	var hs []Holder
	var pid int
	var name, fd string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		v := line[1:]
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(v)
			name, fd = "", ""
		case 'c':
			name = v
		case 'f':
			fd = v
		case 'n':
			if pid > 0 && strings.HasPrefix(v, "/") {
				hs = append(hs, Holder{PID: pid, Name: name, What: fd, Path: v})
			}
		}
	}
	return hs
}
