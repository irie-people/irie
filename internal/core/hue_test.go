package core

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/ui"
)

// hueCore is a core with no tmux behind it, enough for giveHues: keeping
// the colours fails, and the failure goes to a discarded log.
func hueCore() *Core {
	return &Core{
		sessions: map[string]*session{}, hues: map[string]int{},
		tmux: &tmux.Runner{Bin: "/usr/bin/false"}, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// add lists a new session last, as a spawn does, and returns the rows with
// their colours.
func (c *Core) add(rows []ipc.Session, repo string) []ipc.Session {
	id := fmt.Sprintf("s%012d", len(rows))
	c.sessions[id] = &session{id: id, repo: repo}
	rows = append(rows, ipc.Session{ID: id, Repo: repo})
	c.giveHues(rows)
	return rows
}

func hues(rows []ipc.Session) []int {
	var out []int
	for _, r := range rows {
		out = append(out, r.Hue)
	}
	return out
}

func TestCardsSideBySideTakeColoursNearOpposite(t *testing.T) {
	c := hueCore()
	var rows []ipc.Session
	for range 7 {
		rows = c.add(rows, "/home/alice/code/irie")
	}
	// Each card sits 150° or more round the wheel from the one above it,
	// but where that would repeat the card above that, and the seventh
	// card brings the last colour in.
	if got, want := hues(rows), []int{1, 5, 2, 6, 3, 1, 4}; !slices.Equal(got, want) {
		t.Errorf("colours %v, want %v", got, want)
	}
	// A card keeps its colour wherever it goes.
	slices.Reverse(rows)
	for i := range rows {
		rows[i].Hue = 0
	}
	c.giveHues(rows)
	if got, want := hues(rows), []int{4, 1, 3, 6, 2, 5, 1}; !slices.Equal(got, want) {
		t.Errorf("after the list turned round, colours %v, want %v", got, want)
	}
}

func TestEveryCardStandsApartFromTheCardsAboveIt(t *testing.T) {
	c := hueCore()
	repos := []string{"/home/alice/code/irie", "/home/alice/code/bigup", ""}
	var rows []ipc.Session
	for i := range 60 {
		rows = c.add(rows, repos[i*7%11%3])
	}
	seen := map[int]bool{}
	for i, r := range rows {
		seen[r.Hue] = true
		if r.Hue < 1 || r.Hue > ui.AgentColors {
			t.Fatalf("card %d has colour %d", i, r.Hue)
		}
		if i > 0 && ui.HuesApart(r.Hue, rows[i-1].Hue) < apart {
			t.Errorf("card %d: colour %d is %.0f° from the card above's %d", i, r.Hue, ui.HuesApart(r.Hue, rows[i-1].Hue), rows[i-1].Hue)
		}
		if i > 1 && r.Hue == rows[i-2].Hue {
			t.Errorf("card %d: colour %d, as the card two above has", i, r.Hue)
		}
		for j := i - 1; j >= 0; j-- {
			if rows[j].Repo == r.Repo {
				if rows[j].Hue == r.Hue {
					t.Errorf("card %d: colour %d, as the card above it in %q has", i, r.Hue, r.Repo)
				}
				break
			}
		}
	}
	if len(seen) != ui.AgentColors {
		t.Errorf("colours used: %v, want all %d", seen, ui.AgentColors)
	}
}

// TestAParkedCardTakesItsColourAmongTheParked: the List view draws the
// parked cards last, so a parked card's neighbours are the cards there.
func TestAParkedCardTakesItsColourAmongTheParked(t *testing.T) {
	c := hueCore()
	var rows []ipc.Session
	for range 3 {
		rows = c.add(rows, "")
	}
	// A card parked before it had a colour sits under the last card.
	id := "s000000000003"
	c.sessions[id] = &session{id: id}
	rows = slices.Insert(rows, 1, ipc.Session{ID: id, Parked: true})
	c.giveHues(rows)
	if got := rows[1].Hue; ui.HuesApart(got, rows[3].Hue) < apart || got == rows[2].Hue {
		t.Errorf("the parked card has %d under %v", got, hues(rows))
	}
}

func TestColoursHoldThroughARestart(t *testing.T) {
	c := setup(t)
	for _, n := range []string{"alpha", "beta", "gamma"} {
		c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n})
	}
	rows := c.list()
	if got := hues(rows); len(got) != 3 || slices.Contains(got, 0) {
		t.Fatalf("colours %v, want one for each session", got)
	}
	for i := 1; i < len(rows); i++ {
		if d := ui.HuesApart(rows[i].Hue, rows[i-1].Hue); d < apart {
			t.Errorf("%s's colour is %.0f° from %s's", rows[i].Name, d, rows[i-1].Name)
		}
	}
	c.restartCore()
	if got, want := hues(c.list()), hues(rows); !slices.Equal(got, want) {
		t.Errorf("after a restart, colours %v, want %v", got, want)
	}
}
