package core

// The agents' colours (docs/MAC.md). While a session works, the Mac app
// washes its card in a colour of its own, and Irie's status line draws its
// state in the same colour (status.go). The core gives each session one of
// the ui.AgentColors the first time it lists it, from the top of the list
// down, and the session keeps it. So that cards side by side stand apart,
// it is a colour well round the wheel from the card above, and not the
// colour of the card above that or of the one above in its repo; of those,
// one the fewest sessions have, then the one most opposite the card above.
// Cards next to each other so come out near opposite on the wheel. The
// colours live in a user option of the agents' tmux session, as the order
// does, so a restarted core keeps them, and a closed session keeps its
// colour for its reopen.

import (
	"slices"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ui"
)

// optHues holds the sessions' colours: "id:n" pairs, space-separated.
const optHues = "@irie_hues"

// apart is how far round the wheel, in degrees, a card's colour must sit
// from the colour of the card above it: two steps of the six, or the two
// either side of the gap the needs red keeps.
const apart = 100

// loadHues reads the colours a previous core gave.
func (c *Core) loadHues() {
	out, err := c.tmux.Run("show-options", "-qv", "-t", agentsTarget, optHues)
	if err != nil {
		c.log.Warn("hues", "err", err)
		return
	}
	for _, f := range strings.Fields(out) {
		id, v, _ := strings.Cut(f, ":")
		if n, err := strconv.Atoi(v); err == nil && sessionID.MatchString(id) && n >= 1 && n <= ui.AgentColors {
			c.hues[id] = n
		}
	}
}

// giveHues gives a colour to each session of rows that has none, from the
// top of the list down, and puts every row's colour on it. rows are in the
// list's order. The caller holds c.mu.
func (c *Core) giveHues(rows []ipc.Session) {
	// The cards as the List view draws them: the parked ones last.
	shown := make([]int, 0, len(rows))
	for _, parked := range []bool{false, true} {
		for i := range rows {
			if rows[i].Parked == parked {
				shown = append(shown, i)
			}
		}
	}
	used := map[int]int{}
	for i := range rows {
		if x := c.sessions[rows[i].ID]; x != nil {
			if x.hue == 0 {
				x.hue = c.hues[x.id]
			}
			if x.hue != 0 {
				used[x.hue]++
			}
		}
	}
	given := false
	for k, i := range shown {
		x := c.sessions[rows[i].ID]
		if x == nil {
			continue
		}
		if x.hue == 0 {
			var above []int
			repo := 0
			for j := k - 1; j >= 0 && (len(above) < 2 || repo == 0); j-- {
				r := rows[shown[j]]
				if r.Hue == 0 {
					continue
				}
				if len(above) < 2 {
					above = append(above, r.Hue)
				}
				if repo == 0 && r.Repo == rows[i].Repo {
					repo = r.Hue
				}
			}
			x.hue = pickHue(above, repo, used)
			used[x.hue]++
			c.hues[x.id] = x.hue
			given = true
		}
		rows[i].Hue = x.hue
	}
	if given {
		c.saveHues()
	}
}

// pickHue is the colour for a card under cards with the colours above, the
// nearest first, and under repo's, the nearest card above it in its own
// repo (0 for none), where used counts the colours the sessions have.
func pickHue(above []int, repo int, used map[int]int) int {
	best, bestKey := 0, []int(nil)
	for n := 1; n <= ui.AgentColors; n++ {
		var near, same, opposite int
		if len(above) > 0 {
			opposite = int(ui.HuesApart(n, above[0]))
			if opposite < apart {
				near = 1
			}
		}
		if len(above) > 1 && n == above[1] {
			same++
		}
		if n == repo {
			same++
		}
		// Smallest first: too near the card above, the same as the card
		// above that or the one above in its repo, how many have it, and
		// then the most opposite the card above.
		key := []int{near, same, used[n], -opposite}
		if bestKey == nil || slices.Compare(key, bestKey) < 0 {
			best, bestKey = n, key
		}
	}
	return best
}

// saveHues keeps the colours in tmux: those of the live sessions, and of
// the closed ones that reopen or the order still knows. The caller holds
// c.mu.
func (c *Core) saveHues() {
	keep := map[string]bool{}
	for _, r := range c.closes.list {
		keep[r.ID] = true
	}
	for _, id := range c.order {
		keep[id] = true
	}
	var pairs []string
	for id, n := range c.hues {
		if c.sessions[id] == nil && !keep[id] {
			delete(c.hues, id)
			continue
		}
		pairs = append(pairs, id+":"+strconv.Itoa(n))
	}
	slices.Sort(pairs)
	if _, err := c.tmux.Run("set-option", "-t", agentsTarget, optHues, strings.Join(pairs, " ")); err != nil {
		c.log.Warn("hues", "err", err)
	}
}
