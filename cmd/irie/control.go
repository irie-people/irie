package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/rebuild"
)

// controlUsage is the part of ctlUsage for what the user does to a session
// as a whole and to its place in the list (DESIGN §4.3).
const controlUsage = `  ` + brand.Slug + ` ctl freeze SESSION|--all          stop a session's whole process tree at
                                         once, or every session's
  ` + brand.Slug + ` ctl thaw SESSION|--all            continue it
  ` + brand.Slug + ` ctl promote SESSION [REASON...]   put a session at the top of the list
                                         and give its process tree the machine
                                         first; the others run in the background
  ` + brand.Slug + ` ctl unpromote SESSION             put it back among the rest
  ` + brand.Slug + ` ctl reorder [--before OTHER] SESSION
                                         put a session just before another in
                                         the list, or last
  ` + brand.Slug + ` ctl park [SESSION]                put a session out of the way, last in
                                         the Parked group at the end of the
                                         list: by id or name, or with none the
                                         one this runs in
  ` + brand.Slug + ` ctl unpark [SESSION]              bring it back, last in the list, just
                                         above Parked
  ` + brand.Slug + ` ctl pin [SESSION]                 keep a session at hand, last in the
                                         Pinned group at the top, where no
                                         close but your own of it reaches it
  ` + brand.Slug + ` ctl unpin [SESSION]               put it back, first among the rest
  ` + brand.Slug + ` ctl group NAME [SESSION...]       put sessions in the group of that name,
                                         made when none has it, which the
                                         sidebars draw together: by id or name,
                                         or with none the one this runs in. A
                                         path, Work/Sidebar, makes groups inside
                                         groups; --list lists them by path
  ` + brand.Slug + ` ctl ungroup [SESSION...]          take sessions out of their groups, each
                                         into the group around its group
  ` + brand.Slug + ` ctl nest GROUP [INTO]             put a group, with what it holds, inside
                                         another, by id, name or path, or with
                                         no INTO back at the top of the list
`

// ctlFreeze is `irie ctl freeze` (on) and `irie ctl thaw`.
func ctlFreeze(args []string, on bool) error {
	name, op := "thaw", ipc.OpThaw
	if on {
		name, op = "freeze", ipc.OpFreeze
	}
	fs := flag.NewFlagSet("ctl "+name, flag.ContinueOnError)
	all := fs.Bool("all", false, "every session")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *all == (fs.NArg() == 1) || fs.NArg() > 1 {
		return fmt.Errorf("usage: "+brand.Slug+" ctl %s SESSION|--all", name)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	// What waking each costs, for those whose prompt cache has run out.
	var before ipc.ListResult
	if !on {
		ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &before)
	}
	var r ipc.FreezeResult
	if err := ipc.Call(sock, 10*time.Second, op, ipc.FreezeArgs{Session: fs.Arg(0), All: *all}, &r); err != nil {
		return err
	}
	if *all {
		fmt.Println(strings.Join(r.Sessions, "\n"))
	}
	for _, s := range before.Sessions {
		thawed := slices.Contains(r.Sessions, s.ID) || len(r.Sessions) == 0 && (s.ID == fs.Arg(0) || strings.EqualFold(s.Name, fs.Arg(0)))
		if c := s.Cold; c != nil && s.Frozen != nil && thawed {
			fmt.Fprintf(os.Stderr, "%s: its prompt cache has run out, so its next turn writes %s tokens into it again, ~%s\n",
				s.Name, rebuild.Tokens(c.Tokens), rebuild.Dollars(c.USD))
		}
	}
	return nil
}

// ctlPromote is `irie ctl promote` (level 1) and `irie ctl unpromote`.
func ctlPromote(args []string, level int) error {
	if len(args) == 0 || level == 0 && len(args) != 1 {
		if level == 0 {
			return errors.New("usage: " + brand.Slug + " ctl unpromote SESSION")
		}
		return errors.New("usage: " + brand.Slug + " ctl promote SESSION [REASON...]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	a := ipc.PriorityArgs{Session: args[0], Level: level, Reason: strings.Join(args[1:], " ")}
	return ipc.Call(sock, 10*time.Second, ipc.OpPriority, a, nil)
}

func ctlReorder(args []string) error {
	fs := flag.NewFlagSet("ctl reorder", flag.ContinueOnError)
	before := fs.String("before", "", "the session to put it before; last when unset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: " + brand.Slug + " ctl reorder [--before OTHER] SESSION")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	return ipc.Call(sock, 10*time.Second, ipc.OpReorder, ipc.ReorderArgs{Session: fs.Arg(0), Before: *before}, nil)
}

// ctlPark is `irie ctl park` (on) and `irie ctl unpark`. It finds the
// session as ctl close does: by id or name, or with none the one it runs
// in.
func ctlPark(args []string, on bool) error {
	name, op := "unpark", ipc.OpUnpark
	if on {
		name, op = "park", ipc.OpPark
	}
	return ctlPlace(name, op, args)
}

// ctlPin is `irie ctl pin` (on) and `irie ctl unpin`, which find their
// session as ctl park does.
func ctlPin(args []string, on bool) error {
	name, op := "unpin", ipc.OpUnpin
	if on {
		name, op = "pin", ipc.OpPin
	}
	return ctlPlace(name, op, args)
}

// ctlGroup is `irie ctl group NAME [SESSION...]`: the sessions go in the
// group of that name, made when none has it, as one step of the history.
// With no session it groups the one it runs in; --list lists the groups.
func ctlGroup(args []string) error {
	sock, err := socket()
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "--list" {
		var r ipc.GroupResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpGroup, ipc.GroupArgs{}, &r); err != nil {
			return err
		}
		// Each by its path, a group's groups just after it.
		var walk func(parent string)
		walk = func(parent string) {
			for _, g := range r.Groups {
				if g.Parent == parent {
					fmt.Printf("%s\t%s\n", g.ID, ipc.GroupTitle(r.Groups, g.ID, "/"))
					walk(g.ID)
				}
			}
		}
		walk("")
		return nil
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return errors.New("usage: " + brand.Slug + " ctl group NAME [SESSION...] | --list")
	}
	ids, err := groupTargets(sock, "group", args[1:])
	if err != nil {
		return err
	}
	var r ipc.GroupResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpGroup, ipc.GroupArgs{Do: ipc.GroupAdd, Name: args[0], Sessions: ids}, &r); err != nil {
		return err
	}
	fmt.Printf("in %s: %d moved\n", ipc.GroupTitle(r.Groups, r.Group, "/"), len(r.Sessions))
	return nil
}

// ctlNest is `irie ctl nest GROUP [INTO]`: the group, with the sessions and
// groups it holds, goes last inside group INTO, or back at the top of the
// list, as one step of the history.
func ctlNest(args []string) error {
	if len(args) < 1 || len(args) > 2 || strings.TrimSpace(args[0]) == "" {
		return errors.New("usage: " + brand.Slug + " ctl nest GROUP [INTO]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	// The group by its id, so that what this prints is the group that
	// moved, numbered should its new group have its name already.
	var l ipc.GroupResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpGroup, ipc.GroupArgs{}, &l); err != nil {
		return err
	}
	id := args[0]
	if i := ipc.FindGroup(l.Groups, id); i >= 0 {
		id = l.Groups[i].ID
	}
	a := ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{id}}
	if len(args) == 2 {
		a.Into = args[1]
	}
	var r ipc.GroupResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpGroup, a, &r); err != nil {
		return err
	}
	if r.Steps == 0 {
		fmt.Println("there already: " + ipc.GroupTitle(r.Groups, id, "/"))
		return nil
	}
	fmt.Println(ipc.GroupTitle(r.Groups, id, "/"))
	return nil
}

// ctlUngroup is `irie ctl ungroup [SESSION...]`: the sessions come out of
// their groups, as one step of the history.
func ctlUngroup(args []string) error {
	sock, err := socket()
	if err != nil {
		return err
	}
	ids, err := groupTargets(sock, "ungroup", args)
	if err != nil {
		return err
	}
	return ipc.Call(sock, 10*time.Second, ipc.OpGroup, ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: ids}, nil)
}

// groupTargets are the sessions args name, by id or name, or with none the
// one this runs in.
func groupTargets(sock, verb string, args []string) ([]string, error) {
	var l ipc.ListResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		id, err := sessionTarget(verb, nil, os.Getenv(brand.Env("SESSION")), l.Sessions)
		return []string{id}, err
	}
	ids := make([]string, 0, len(args))
	for _, a := range args {
		id, err := sessionTarget(verb, []string{a}, "", l.Sessions)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ctlPlace runs op, park, unpark, pin or unpin, on the session args name.
func ctlPlace(name, op string, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: "+brand.Slug+" ctl %s [SESSION]", name)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var l ipc.ListResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l); err != nil {
		return err
	}
	id, err := sessionTarget(name, args, os.Getenv(brand.Env("SESSION")), l.Sessions)
	if err != nil {
		return err
	}
	return ipc.Call(sock, 10*time.Second, op, ipc.ParkArgs{Session: id}, nil)
}
