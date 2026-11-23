package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// spacesUsage is the part of ctlUsage for the spaces (DESIGN §4.1).
const spacesUsage = `  ` + brand.Slug + ` ctl spaces [--json]               the spaces, in their order, and how many
                                         sessions each holds
  ` + brand.Slug + ` ctl spaces new NAME...            make a space, last
  ` + brand.Slug + ` ctl spaces rename SPACE NAME...   name a space, by its id or name
  ` + brand.Slug + ` ctl spaces move SPACE [BEFORE]    put a space just before another, or last
  ` + brand.Slug + ` ctl spaces remove SPACE           remove a space; what it holds goes to
                                         the first
  ` + brand.Slug + ` ctl space [--group GROUP]... SPACE [SESSION...]
                                         put sessions, and every session of
                                         each GROUP, in the space of that name,
                                         made when none has it, as one step of
                                         the history: by id or name, or with
                                         none the one this runs in
`

// ctlSpaces is `irie ctl spaces`: with no words it lists the spaces; new,
// rename, move and remove change them, as the Mac app's chips do. None of
// these is a step of the history (spaces.go).
func ctlSpaces(args []string) error {
	asJSON := len(args) == 1 && args[0] == "--json"
	sock, err := socket()
	if err != nil {
		return err
	}
	var cur ipc.SpacesResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpSpaces, ipc.SpacesArgs{}, &cur); err != nil {
		return err
	}
	if len(args) == 0 || asJSON {
		return listSpaces(sock, cur.Spaces, asJSON)
	}
	find := func(ref string) (string, error) {
		if i := ipc.FindSpace(cur.Spaces, ref); i >= 0 {
			return cur.Spaces[i].ID, nil
		}
		return "", fmt.Errorf("no space %q", ref)
	}
	var a ipc.SpacesArgs
	switch verb, rest := args[0], args[1:]; {
	case verb == "new" && len(rest) > 0:
		a = ipc.SpacesArgs{Do: ipc.SpacesNew, Name: strings.Join(rest, " ")}
	case verb == "rename" && len(rest) > 1:
		a = ipc.SpacesArgs{Do: ipc.SpacesRename, Name: strings.Join(rest[1:], " ")}
		a.Space, err = find(rest[0])
	case verb == "move" && (len(rest) == 1 || len(rest) == 2):
		a = ipc.SpacesArgs{Do: ipc.SpacesMove}
		if a.Space, err = find(rest[0]); err == nil && len(rest) == 2 {
			a.Before, err = find(rest[1])
		}
	case verb == "remove" && len(rest) == 1:
		a = ipc.SpacesArgs{Do: ipc.SpacesRemove}
		a.Space, err = find(rest[0])
	default:
		return errors.New("usage: " + brand.Slug + " ctl spaces [--json] | new NAME... | rename SPACE NAME... | move SPACE [BEFORE] | remove SPACE")
	}
	if err != nil {
		return err
	}
	var r ipc.SpacesResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpSpaces, a, &r); err != nil {
		return err
	}
	// A new or renamed space, with the name it got: numbered should
	// another space have it.
	id := r.Space
	if a.Do == ipc.SpacesRename {
		id = a.Space
	}
	if i := ipc.FindSpace(r.Spaces, id); id != "" && i >= 0 {
		fmt.Printf("%s\t%s\n", id, r.Spaces[i].Name)
	}
	return nil
}

// listSpaces prints spaces, with how many of this machine's sessions each
// holds.
func listSpaces(sock string, spaces []ipc.Space, asJSON bool) error {
	var l ipc.ListResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l); err != nil {
		return err
	}
	return printSpaces(os.Stdout, spaces, l.Sessions, asJSON)
}

// printSpaces prints spaces to w, with how many of rows each holds: a row
// is in the first when its space is "" or gone. A peer's rows count in
// none: they are its own.
func printSpaces(w io.Writer, spaces []ipc.Space, rows []ipc.Session, asJSON bool) error {
	count := map[string]int{}
	for _, s := range rows {
		if s.Host != "" || len(spaces) == 0 {
			continue
		}
		if i := ipc.FindSpace(spaces, s.Space); s.Space != "" && i > 0 {
			count[spaces[i].ID]++
		} else {
			count[spaces[0].ID]++
		}
	}
	if asJSON {
		type space struct {
			ipc.Space
			Sessions int `json:"sessions"`
		}
		out := make([]space, len(spaces))
		for i, s := range spaces {
			out[i] = space{s, count[s.ID]}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"spaces": out})
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SPACE\tNAME\tSESSIONS")
	for _, s := range spaces {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", s.ID, s.Name, count[s.ID])
	}
	return tw.Flush()
}

// refs is a flag that may be given more than once, each a reference.
type refs []string

func (r *refs) String() string { return strings.Join(*r, ",") }

func (r *refs) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("empty")
	}
	*r = append(*r, v)
	return nil
}

// ctlSpace is `irie ctl space [--group GROUP]... SPACE [SESSION...]`: the
// sessions, and every session of each group, go in the space of that name,
// made when none has it, as one step of the history. With no session and
// no group, the one it runs in goes.
func ctlSpace(args []string) error {
	fs := flag.NewFlagSet("ctl space", flag.ContinueOnError)
	var groups refs
	fs.Var(&groups, "group", "a group, by id, name or path, which goes with all it holds; again for more")
	if err := fs.Parse(args); err != nil {
		return err
	}
	usage := "usage: " + brand.Slug + " ctl space [--group GROUP]... SPACE [SESSION...]"
	if fs.NArg() == 0 || strings.TrimSpace(fs.Arg(0)) == "" {
		return errors.New(usage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var ids []string
	switch own := os.Getenv(brand.Env("SESSION")); {
	case fs.NArg() > 1:
		if ids, err = groupTargets(sock, "space", fs.Args()[1:]); err != nil {
			return err
		}
	case len(groups) > 0:
	case own == "":
		return errors.New(usage + ": with no SESSION or GROUP it moves the session it runs in, and it runs in none")
	default:
		ids = []string{own}
	}
	var cur ipc.SpacesResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpSpaces, ipc.SpacesArgs{}, &cur); err != nil {
		return err
	}
	at := ipc.FindSpace(cur.Spaces, fs.Arg(0))
	if at < 0 {
		if err := ipc.Call(sock, 10*time.Second, ipc.OpSpaces, ipc.SpacesArgs{Do: ipc.SpacesNew, Name: fs.Arg(0)}, &cur); err != nil {
			return err
		}
		at = ipc.FindSpace(cur.Spaces, cur.Space)
	}
	if at < 0 {
		return fmt.Errorf("no space %q", fs.Arg(0))
	}
	var r ipc.SpaceResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpSpace, ipc.SpaceArgs{Sessions: ids, Groups: groups, Space: cur.Spaces[at].ID}, &r); err != nil {
		return err
	}
	fmt.Printf("in %s: %d moved\n", cur.Spaces[at].Name, len(r.Sessions))
	return nil
}
