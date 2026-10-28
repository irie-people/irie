package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// controlUsage is the part of ctlUsage for what the user does to a session
// as a whole and to its place in the list (DESIGN §4.3).
const controlUsage = `  irie ctl freeze SESSION|--all          stop a session's whole process tree at
                                         once, or every session's
  irie ctl thaw SESSION|--all            continue it
  irie ctl promote SESSION [REASON...]   put a session at the top of the list
                                         and give its process tree the machine
                                         first; the others run in the background
  irie ctl unpromote SESSION             put it back among the rest
  irie ctl reorder [--before OTHER] SESSION
                                         put a session just before another in
                                         the list, or last
  irie ctl park [SESSION]                put a session out of the way, in the
                                         Parked group at the end of the list:
                                         by id or name, or with none the one
                                         this runs in
  irie ctl unpark [SESSION]              bring it back to its place
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
		return fmt.Errorf("usage: irie ctl %s SESSION|--all", name)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.FreezeResult
	if err := ipc.Call(sock, 10*time.Second, op, ipc.FreezeArgs{Session: fs.Arg(0), All: *all}, &r); err != nil {
		return err
	}
	if *all {
		fmt.Println(strings.Join(r.Sessions, "\n"))
	}
	return nil
}

// ctlPromote is `irie ctl promote` (level 1) and `irie ctl unpromote`.
func ctlPromote(args []string, level int) error {
	if len(args) == 0 || level == 0 && len(args) != 1 {
		if level == 0 {
			return errors.New("usage: irie ctl unpromote SESSION")
		}
		return errors.New("usage: irie ctl promote SESSION [REASON...]")
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
		return errors.New("usage: irie ctl reorder [--before OTHER] SESSION")
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
	if len(args) > 1 {
		return fmt.Errorf("usage: irie ctl %s [SESSION]", name)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var l ipc.ListResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l); err != nil {
		return err
	}
	id, err := sessionTarget(name, args, os.Getenv("IRIE_SESSION"), l.Sessions)
	if err != nil {
		return err
	}
	return ipc.Call(sock, 10*time.Second, op, ipc.ParkArgs{Session: id}, nil)
}
