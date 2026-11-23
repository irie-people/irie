package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// The list's layout outlives its tmux server, as when the Mac restarts:
// the spaces and the groups are back as the core starts again, and each
// lost session, once reopened, is back in its place, its space and its
// group, pinned as before and in its colour, whatever order reopen brings
// them back in.
func TestALostSessionComesBackInItsPlaceSpaceAndGroup(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma", "delta")
	other := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Gigbot"}).Space
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["beta"], id["gamma"]}, Space: other}, nil); err != nil {
		t.Fatal(err)
	}
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["beta"], id["gamma"]}}).Group
	if err := c.call(ipc.OpReorder, ipc.ReorderArgs{Session: id["delta"], Before: id["alpha"]}, nil); err != nil {
		t.Fatal(err)
	}
	c.pin(id["alpha"], true)
	before := c.list()

	if _, err := c.TryTmux("kill-server"); err != nil {
		t.Fatal(err)
	}
	c.up()
	if ls := c.list(); len(ls) != 0 {
		t.Fatalf("rows after the restart = %+v", ls)
	}
	if r := c.spaces(ipc.SpacesArgs{}); !slices.Contains(r.Spaces, ipc.Space{ID: other, Name: "Gigbot"}) {
		t.Errorf("spaces after the restart = %+v, want Gigbot back", r.Spaces)
	}
	if r := c.group(ipc.GroupArgs{}); !slices.Equal(groupNames(r), []string{"Steak"}) {
		t.Errorf("groups after the restart = %v, want Steak back", groupNames(r))
	}
	for _, n := range []string{"gamma", "alpha", "delta", "beta"} {
		if _, err := c.reopen(id[n]); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := names(c.list()), names(before); !slices.Equal(got, want) {
		t.Errorf("order after reopen = %v, want %v as before the restart", got, want)
	}
	for _, b := range before {
		a, ok := c.row(b.ID)
		if !ok || a.Space != b.Space || a.Group != b.Group || a.Pinned != b.Pinned || a.Hue != b.Hue {
			t.Errorf("%s came back as space %q, group %q, pinned %v, hue %d; want %q, %q, %v, %d",
				b.Name, a.Space, a.Group, a.Pinned, a.Hue, b.Space, b.Group, b.Pinned, b.Hue)
		}
	}
	if r, _ := c.row(id["beta"]); r.Space != other || r.Group != g {
		t.Errorf("beta = space %q, group %q; want %q, %q", r.Space, r.Group, other, g)
	}
}

// tmux stays the one the core goes by: the copy keeps what tmux holds, and
// fills in only what a new server lacks.
func TestTheLayoutsCopyFollowsTmux(t *testing.T) {
	c := setup(t)
	c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Gigbot"})
	copyOf := func() map[string]string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(c.paths.State, layoutFile))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := copyOf(); !strings.Contains(m[optSpaces], `"Gigbot"`) {
		t.Errorf("copy = %v, want the new space in it", m)
	}
	// An option changed in tmux behind the core's back wins over the copy
	// when the core starts again, and the copy takes it.
	c.Tmux(t, "set-option", "-t", agentsTarget, optSpaces, `[{"id":"main","name":"Main"},{"id":"pwriting00000","name":"Writing"}]`)
	c.restartCore()
	if got := spaceNames(c.spaces(ipc.SpacesArgs{})); !slices.Equal(got, []string{"Main", "Writing"}) {
		t.Errorf("spaces = %v, want tmux's", got)
	}
	if m := copyOf(); !strings.Contains(m[optSpaces], `"Writing"`) || strings.Contains(m[optSpaces], `"Gigbot"`) {
		t.Errorf("copy = %v, want tmux's spaces", m)
	}
}
