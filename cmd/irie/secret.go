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
	f, _ := stdin.(*os.File)
	if isTerminal(stdin) {
		fmt.Fprintf(stdout, "%s (it won't show): ", prompt)
		if err := stty(f, "-echo"); err == nil {
			defer func() {
				stty(f, "echo")
				fmt.Fprintln(stdout)
			}()
		}
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func stty(tty *os.File, arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = tty
	return cmd.Run()
}
