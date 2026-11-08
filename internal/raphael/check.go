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
	Now   time.Time
	// Thorough asks Homebrew what it would clean up too, which can take
	// minutes: for a look the person asked for.
	Thorough bool
}

// Quick is the part of a look that runs no tool: what runs hot, memory,
// and the disk. The core makes it at every sample.
func Quick(in Input, m Memory, d Disk) (fs []Finding, fine []string) {
	hot := hotFindings(in.Hot, in.Owner, in.UID, in.Home)
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

// Slow is the rest of a look: the caches, what starts by itself and
// macOS's protections. It runs tools, in the background state, so it takes
// a while: du alone may take a minute over big caches.
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
