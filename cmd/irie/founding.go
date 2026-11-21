//go:build !nofounding

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

const foundingUsage = `  ` + brand.Slug + ` ctl founding [--json]             the founding documents' texts and their
                                         local chain, every version checked
  ` + brand.Slug + ` ctl founding log [-n N] [--json]  the chain's versions, newest first
`

// ctlFounding shows the founding documents and their local chain (DESIGN
// §4.17): the core records the texts this build carries on it, then reads
// back every version and checks it.
func ctlFounding(args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("ctl founding", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the answer as JSON")
	n := fs.Int("n", 20, "how many versions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	usage := errors.New("usage: " + brand.Slug + " ctl founding [--json], or log [-n N] [--json]")
	var a ipc.FoundingArgs
	switch {
	case fs.NArg() > 0:
		return usage
	case sub == "log" && *n > 0:
		a.Log = *n
	case sub != "":
		return usage
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.FoundingResult
	if err := ipc.Call(sock, 30*time.Second, ipc.OpFounding, a, &r); err != nil {
		return err
	}
	if *asJSON {
		if sub == "log" {
			return json.NewEncoder(os.Stdout).Encode(r.Versions)
		}
		type text struct {
			ID, Title, SHA512 string
		}
		out := struct {
			Chain *ipc.FoundingChain `json:"chain"`
			Texts []text             `json:"texts"`
		}{Chain: r.Chain}
		for _, d := range r.Docs {
			out.Texts = append(out.Texts, text{d.ID, d.Title, d.SHA512})
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	if sub == "log" {
		printFoundingLog(os.Stdout, r.Versions)
		return nil
	}
	printFounding(os.Stdout, r)
	return nil
}

// printFounding says how the chain checked, with ● when every version did
// and ! when one didn't, so colour never carries it alone, then each
// text's fingerprint.
func printFounding(w io.Writer, r ipc.FoundingResult) {
	home, _ := os.UserHomeDir()
	if c := r.Chain; c != nil {
		switch {
		case c.Verified:
			versions := "1 version"
			if c.Versions != 1 {
				versions = fmt.Sprintf("%d versions", c.Versions)
			}
			fmt.Fprintf(w, "● On your local chain: %s, each read back and matching its SHA-512; the newest made %s.\n", versions, c.Created)
			fmt.Fprintf(w, "  Newest: %s\n", c.Head)
		default:
			fmt.Fprintf(w, "! The local chain doesn't check: %s\n", c.Problem)
		}
		fmt.Fprintf(w, "  Kept in %s\n", repo.Tilde(c.Folder, home))
	}
	fmt.Fprintln(w, "The texts, by their SHA-512:")
	for _, d := range r.Docs {
		fmt.Fprintf(w, "  %-36s sha512:%s…\n", d.Title, d.SHA512[:min(16, len(d.SHA512))])
	}
}

// printFoundingLog lists versions, newest first: each one's short name,
// when it was made, and its texts.
func printFoundingLog(w io.Writer, vs []ipc.FoundingVersion) {
	if len(vs) == 0 {
		fmt.Fprintln(w, "Nothing is on the chain yet.")
		return
	}
	for _, v := range vs {
		fmt.Fprintf(w, "%s  %s\n", chain.Short(v.Name), v.Created)
		for _, f := range v.Files {
			file, digest, _ := strings.Cut(f, " ")
			fmt.Fprintf(w, "  %-32s %s…\n", file, digest[:min(len(chain.Prefix)+16, len(digest))])
		}
	}
}
