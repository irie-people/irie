package main

import "io"

// pluginCmd is a command one of Irie's plugins brings (DESIGN §4.17). A
// plugin's file adds its commands here, and a build tag that leaves the
// plugin out leaves them out too, with their code.
type pluginCmd struct {
	run func(args []string) error
	// code, when set, runs first: a command with an exit code of its own
	// says so with true.
	code func(args []string) (int, bool)
}

// pluginCmds are the plugins' commands in this build, by name.
var pluginCmds = map[string]pluginCmd{}

// pluginSetups are the plugins' irie setup words in this build, by name.
var pluginSetups = map[string]func(args []string, stdin io.Reader, stdout io.Writer) error{}
