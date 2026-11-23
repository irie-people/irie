package raphael

import (
	"context"
	"fmt"
	"time"
)

// Input is what a look needs from the core.
type Input struct {
	Home  string
	UID   int
	Known []string // what the person marked as theirs (arch.raphael.known)
	Owner Owner
	Procs []Proc // the last sample of every process
	Hot   []Hot  // what has run hot for long, by the sampler
	Left  []Left // what agents left running (LeftRunning)
	Now   time.Time
	// Thorough asks Homebrew what it would clean up too, which can take
	// minutes: for a look the person asked for.
	Thorough bool
	// Want is the style the person chose for each of macOS's own senders
	// of notices (alerts.*), by its id.
	Want map[string]string
	// Signed are the app's programs whose signatures say whether macOS
	// keeps what the person allows them: the app and the command.
	Signed []string
}

// Quick is the part of a look that runs no tool: what agents left running,
// what runs hot, memory, and the disk, from what the core read at the
// minute's sample.
func Quick(in Input, m Memory, d Disk) (fs []Finding, fine []string) {
	left := leftFindings(in.Left, in.Hot, in.Home)
	fs = append(fs, left...)
	if len(left) == 0 {
		fine = append(fine, "No agent has left a program running.")
	}
	hot := hotFindings(in.Hot, in.Owner, in.UID, in.Home, leftPIDs(in.Left))
	fs = append(fs, hot...)
	if len(hot) == 0 {
		fine = append(fine, "No program has run hot for long.")
	}
	if f := memoryFinding(m, in.Procs, in.Owner); f.ID != "" {
		if f.Level == OK {
			fine = append(fine, f.Text)
		} else {
			fs = append(fs, f)
		}
	}
	fs = append(fs, bigHogs(m, in.Procs, in.Owner)...)
	if f := diskFinding(d); f.ID != "" {
		if f.Level == OK {
			fine = append(fine, f.Text)
		} else {
			fs = append(fs, f)
		}
	}
	sortFindings(fs)
	return fs, fine
}

// Slow is the rest of a look: the caches, what starts by itself, macOS's
// protections and its own alerts. It runs tools, in the background state,
// so it takes a while: du alone may take a minute over big caches.
func Slow(ctx context.Context, r Runner, in Input, d Disk) (fs []Finding, fine []string) {
	caches, total := cacheFindings(ctx, r, cachesInput{Home: in.Home, Procs: in.Procs, Disk: d, Thorough: in.Thorough})
	fs = append(fs, caches...)
	if total > 0 {
		line := fmt.Sprintf("Caches: %s in all.", Size(total))
		if len(caches) == 0 {
			line = fmt.Sprintf("Caches: %s in all, none big and safe to clear.", Size(total))
		}
		fine = append(fine, line)
	}
	if ctx.Err() != nil {
		return fs, fine
	}
	startup, sfine := startupFindings(ctx, r, startupInput{Home: in.Home, UID: in.UID, Known: in.Known, Now: in.Now})
	fs = append(fs, startup...)
	fine = append(fine, sfine...)
	if ctx.Err() != nil {
		return fs, fine
	}
	protect, pfine := protectFindings(ctx, r, protectInput{Now: in.Now})
	fs = append(fs, protect...)
	fine = append(fine, pfine...)
	if ctx.Err() != nil {
		return fs, fine
	}
	alerts, afine := alertFindings(ctx, r, alertsInput{Home: in.Home, Want: in.Want, Signed: in.Signed, Now: in.Now})
	fs = append(fs, alerts...)
	fine = append(fine, afine...)
	sortFindings(fs)
	return fs, fine
}

// Merge is one report of a quick look and a slow one: what needs care
// first, then what is worth a look; what is well after.
func Merge(at time.Time, took time.Duration, quick, slow []Finding, quickFine, slowFine []string) Report {
	r := Report{At: at, Took: took, Findings: append(append([]Finding{}, quick...), slow...), Fine: append(append([]string{}, quickFine...), slowFine...)}
	sortFindings(r.Findings)
	return r
}
