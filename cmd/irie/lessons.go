package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/lessons"
)

// lessonsUsage is the part of ctlUsage for the lessons (DESIGN §4.11).
const lessonsUsage = `  ` + brand.Slug + ` ctl lessons [evidence|draft|keep N...|no N...|drop N...] [--days N] [--journal] [--model NAME] [--json]
                                         lessons from what your agents fail at
                                         most: those every session hears, and
                                         the latest draft's waiting for your
                                         word. evidence ranks what failed over
                                         N days (14), with no model; draft has
                                         a local model draft up to five from
                                         it, and waits; keep N has every
                                         session hear lesson N as it starts, no
                                         N turns it down, drop N stops kept
                                         lesson N. --journal reads the journal
                                         alone, without the transcripts
`

const lessonsUsageLine = "usage: " + brand.Slug + " ctl lessons [evidence|draft|keep N...|no N...|drop N...] [--days N] [--journal] [--model NAME] [--json]"

// ctlLessons is the lessons from what agents fail at most (DESIGN §4.11).
func ctlLessons(args []string) error {
	fs := flag.NewFlagSet("ctl lessons", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	days := fs.Int("days", 0, "with evidence or draft: how many days of what failed to read (14)")
	journalOnly := fs.Bool("journal", false, "with evidence or draft: read the journal alone, without the transcripts")
	model := fs.String("model", "", "with draft: the local model to draft with, by its name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var words []string
	for fs.NArg() > 0 {
		words = append(words, fs.Arg(0))
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	verb := ""
	if len(words) > 0 {
		verb, words = words[0], words[1:]
	}
	a := ipc.LessonsArgs{Do: verb, Days: *days, Journal: *journalOnly, Model: *model}
	timeout := 10 * time.Second
	switch verb {
	case ipc.LessonsReport:
	case ipc.LessonsEvidence:
		// A month of transcripts is a few GB to read.
		timeout = 5 * time.Minute
	case ipc.LessonsDraft:
		// Typing it is the OK the Arch Agents tab asks for.
		a.OK = true
	case ipc.LessonsKeep, ipc.LessonsNo, ipc.LessonsDrop:
		for _, w := range words {
			n, err := strconv.Atoi(strings.TrimPrefix(w, "#"))
			if err != nil {
				return fmt.Errorf("%q is not a lesson's number\n%s", w, lessonsUsageLine)
			}
			a.Lessons = append(a.Lessons, n)
		}
		words = nil
		if len(a.Lessons) == 0 {
			return errors.New(lessonsUsageLine)
		}
		// Typing keep is the OK to every session hearing the lessons.
		a.OK = verb == ipc.LessonsKeep
	default:
		return errors.New(lessonsUsageLine)
	}
	reads := verb == ipc.LessonsEvidence || verb == ipc.LessonsDraft
	if len(words) > 0 || !reads && (*days != 0 || *journalOnly) || *model != "" && verb != ipc.LessonsDraft {
		return errors.New(lessonsUsageLine)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.LessonsResult
	if err := ipc.Call(sock, timeout, ipc.OpLessons, a, &r); err != nil {
		return err
	}
	if verb == ipc.LessonsDraft {
		// The draft runs in the core, in the background: wait for it.
		for deadline := time.Now().Add(20 * time.Minute); r.Drafting; {
			if time.Now().After(deadline) {
				return errors.New("the draft is still under way after 20 minutes: its lessons show in `" + brand.Slug + " ctl lessons` when it is done")
			}
			time.Sleep(2 * time.Second)
			if err := ipc.Call(sock, timeout, ipc.OpLessons, ipc.LessonsArgs{}, &r); err != nil {
				return err
			}
		}
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	switch verb {
	case ipc.LessonsEvidence:
		lessons.PrintEvidence(os.Stdout, *r.Evidence)
	case ipc.LessonsKeep:
		fmt.Println("Kept: every session hears it from its next start.")
	case ipc.LessonsNo:
		fmt.Println(brand.Raffi + " lets it go.")
	case ipc.LessonsDrop:
		fmt.Println("Dropped: sessions stop hearing it from their next start.")
	default:
		lessons.Print(os.Stdout, r.Book, r.Drafting)
	}
	return nil
}
