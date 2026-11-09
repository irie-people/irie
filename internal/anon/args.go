package anon

import (
	"errors"
	"flag"
	"io"
)

// Args are the arguments of `irie anon mcp` that serve o, after the irie
// binary.
func Args(o ServerOptions) []string {
	args := []string{"anon", "mcp", "--dir", o.Dirs.Root, "--compartment", o.Compartment}
	if o.Search != "" {
		args = append(args, "--search", o.Search)
	}
	if o.Evidence != "" {
		args = append(args, "--evidence", o.Evidence)
	}
	return args
}

// ParseArgs reads the arguments of `irie anon mcp`.
func ParseArgs(args []string, errOut io.Writer) (ServerOptions, error) {
	fs := flag.NewFlagSet("irie anon mcp", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var o ServerOptions
	fs.StringVar(&o.Dirs.Root, "dir", "", "anon's folder, <data>/anon")
	fs.StringVar(&o.Compartment, "compartment", "", "the session's compartment")
	fs.StringVar(&o.Search, "search", "", "Irie's SearXNG, such as http://127.0.0.1:8888")
	fs.StringVar(&o.Evidence, "evidence", "", "the folder fetch's save writes to")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 || o.Dirs.Root == "" || o.Compartment == "" {
		return o, errors.New("usage: irie anon mcp --dir DIR --compartment NAME [--search URL] [--evidence DIR]")
	}
	return o, CheckName(o.Compartment)
}
