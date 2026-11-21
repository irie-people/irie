package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// readHidden reads a password or a token from the input, one line: from a
// terminal without showing it, after a prompt. It never comes in an
// argument, where ps would show it.
func readHidden(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	lines, err := readHiddenLines(stdin, stdout, prompt)
	if err != nil {
		return "", err
	}
	return lines[0], nil
}

// readHiddenLines reads a line for each prompt, as readHidden does one,
// through one reader, so a line piped in after another isn't lost to the
// first one's reading ahead. A line the input ends before is "".
func readHiddenLines(stdin io.Reader, stdout io.Writer, prompts ...string) ([]string, error) {
	f, _ := stdin.(*os.File)
	tty := isTerminal(stdin)
	r := bufio.NewReader(stdin)
	out := make([]string, len(prompts))
	for i, prompt := range prompts {
		if tty {
			fmt.Fprintf(stdout, "%s (it won't show): ", prompt)
			if err := stty(f, "-echo"); err == nil {
				defer stty(f, "echo")
			}
		}
		line, err := r.ReadString('\n')
		if tty {
			fmt.Fprintln(stdout)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		out[i] = strings.TrimRight(line, "\r\n")
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out, nil
}

func stty(tty *os.File, arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = tty
	return cmd.Run()
}
