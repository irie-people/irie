package core

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
)

func (c *coreEnv) chainDo(a ipc.ChainArgs) ipc.ChainResult {
	c.t.Helper()
	var r ipc.ChainResult
	if err := c.call(ipc.OpChain, a, &r); err != nil {
		c.t.Fatalf("chain %+v: %v", a, err)
	}
	return r
}

// chainLog is the whole chain, newest first.
func (c *coreEnv) chainLog() ipc.ChainResult {
	c.t.Helper()
	return c.chainDo(ipc.ChainArgs{Do: ipc.ChainLog, Limit: 1000})
}

// versionFile is where the chain keeps the version named.
func (c *coreEnv) versionFile(name string) string {
	return filepath.Join(c.paths.Chain(), "objects", strings.TrimPrefix(name, chain.Prefix)+".json")
}

func whats(r ipc.ChainResult) []string {
	var out []string
	for _, e := range r.Versions {
		out = append(out, e.What)
	}
	return out
}

func TestEachChangeToTheOrganisationIsOneVersion(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma")
	// The chain began with the core, which had no sessions yet; starting
	// one changes no organisation of yours, so it adds none.
	r := c.chainLog()
	if r.Problem != "" || len(r.Versions) != 1 || r.Versions[0].What != "the chain begins" || r.Versions[0].Cards != 0 || r.Versions[0].Parent != "" {
		t.Fatalf("at the start: %+v", r)
	}
	act := func(want string, do func(), or ...string) ipc.ChainEntry {
		t.Helper()
		was := len(r.Versions)
		do()
		r = c.chainLog()
		if len(r.Versions) != was+1 || r.Problem != "" {
			t.Fatalf("after %s: versions %q, problem %q", want, whats(r), r.Problem)
		}
		if w := r.Versions[0].What; w != want && !slices.Contains(or, w) {
			t.Errorf("version %q, want %q", w, want)
		}
		if r.Versions[0].Parent != r.Versions[1].Name {
			t.Errorf("after %s: the version names %s before it, not %s", want, r.Versions[0].Parent, r.Versions[1].Name)
		}
		return r.Versions[0]
	}
	first := act(`reorder "gamma"`, func() {
		if err := c.call(ipc.OpReorder, ipc.ReorderArgs{Session: id["gamma"], Before: id["alpha"]}, nil); err != nil {
			t.Fatal(err)
		}
	})
	if first.Cards != 3 || first.Groups != 0 || first.Spaces != 1 {
		t.Errorf("first version holds %+v", first)
	}
	parked := act(`park "alpha"`, func() { c.park(id["alpha"], true) })
	if parked.Parked != 1 {
		t.Errorf("parked = %d", parked.Parked)
	}
	// Undoing it gives back the organisation the reorder left, which has
	// the same name whichever version holds it; the versions' own names
	// differ, as each names the one before.
	back := act(`undo park "alpha"`, func() { c.step(dirUndo) })
	if back.State != first.State || back.Name == first.Name || parked.State == first.State {
		t.Errorf("states: first %s, parked %s, back %s", chain.Short(first.State), chain.Short(parked.State), chain.Short(back.State))
	}
	act(`park "alpha"`, func() { c.park(id["alpha"], true) })
	act(`unpark "alpha"`, func() { c.park(id["alpha"], false) })
	act(`pin "beta"`, func() { c.pin(id["beta"], true) })
	act(`unpin "beta"`, func() { c.pin(id["beta"], false) })
	act(`rename "delta"`, func() {
		if _, err := c.rename(id["gamma"], "delta"); err != nil {
			t.Fatal(err)
		}
	})
	var g string
	act(`group "alpha" and "beta"`, func() {
		g = c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"], id["beta"]}}).Group
	}, `group "beta" and "alpha"`)
	act(`rename a group "Irie"`, func() { c.group(ipc.GroupArgs{Do: ipc.GroupRename, Group: g, Name: "Irie"}) })
	var sp string
	act(`new space "Work"`, func() { sp = c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Work"}).Space })
	act(`rename a space "Play"`, func() { c.spaces(ipc.SpacesArgs{Do: ipc.SpacesRename, Space: sp, Name: "Play"}) })
	act(`move "delta" to another space`, func() {
		if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["gamma"]}, Space: sp}, nil); err != nil {
			t.Fatal(err)
		}
	})
	act(`undo move "delta" to another space`, func() { c.step(dirUndo) })
	act(`redo move "delta" to another space`, func() { c.step(dirRedo) })
	act(`undo move "delta" to another space`, func() { c.step(dirUndo) })
	// An undo of several steps at once is one version.
	act(`undo group "beta" and "alpha"`, func() { c.step(dirUndo) }, `undo group "alpha" and "beta"`)

	// What changes nothing a version keeps adds none: a boost is a step
	// of the history, but no organisation, and a park of a parked session
	// is no step at all.
	n := len(r.Versions)
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id["beta"], Level: 1}, nil); err != nil {
		t.Fatal(err)
	}
	c.park(id["beta"], true)
	c.park(id["beta"], true)
	if r = c.chainLog(); len(r.Versions) != n+1 {
		t.Errorf("a boost and a second park made versions: %q", whats(r)[:3])
	}

	// A version's name is the SHA-512 of its canonical bytes, which any
	// tool can check.
	show := c.chainDo(ipc.ChainArgs{Do: ipc.ChainShow, Version: chain.Short(r.Head)})
	sum := sha512.Sum512([]byte(show.Raw))
	if show.Version == nil || chain.Prefix+hex.EncodeToString(sum[:]) != r.Head || show.Version.Name != r.Head {
		t.Errorf("show %s: %+v", chain.Short(r.Head), show.Version)
	}
	if canon, err := chain.Canonical(json.RawMessage(show.Raw)); err != nil || !bytes.Equal(canon, []byte(show.Raw)) {
		t.Errorf("the bytes kept are not canonical: %s", show.Raw)
	}
	if h := c.chainDo(ipc.ChainArgs{Do: ipc.ChainHead}); h.Head != r.Head || h.Count != len(r.Versions) || h.Dir != c.paths.Chain() {
		t.Errorf("head %+v, want %s of %d", h, r.Head, len(r.Versions))
	}

	// A restart writes no version of its own: the chain has begun.
	c.restartCore()
	if again := c.chainLog(); again.Head != r.Head || len(again.Versions) != len(r.Versions) {
		t.Errorf("after a restart: %q", whats(again)[:2])
	}
}

// A version holds ids and the names the user gave, and nothing of who
// they are or of the machine it runs on.
func TestAVersionHoldsNoIdentity(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha")
	c.pin(id["alpha"], true)
	r := c.chainLog()
	var avoid []string
	if h, err := os.Hostname(); err == nil {
		avoid = append(avoid, strings.Split(h, ".")[0])
	}
	if u, err := user.Current(); err == nil {
		avoid = append(avoid, u.Username, u.HomeDir)
	}
	avoid = append(avoid, os.Getenv("HOME"), c.paths.State)
	for _, e := range r.Versions {
		b, err := os.ReadFile(c.versionFile(e.Name))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range avoid {
			if len(s) > 2 && bytes.Contains(bytes.ToLower(b), bytes.ToLower([]byte(s))) {
				t.Errorf("version %s holds %q: %s", chain.Short(e.Name), s, b)
			}
		}
	}
	if !strings.Contains(c.chainDo(ipc.ChainArgs{Do: ipc.ChainShow, Version: "head"}).Raw, `"name":"alpha"`) {
		t.Error("the version lacks the name the card was given")
	}
}

func TestRestoreBringsAVersionBackAsOneStep(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma")
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"], id["beta"]}}).Group
	c.pin(id["gamma"], true)
	sp := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Work"}).Space
	then := c.chainDo(ipc.ChainArgs{Do: ipc.ChainHead}).Head
	at := func() map[string]ipc.Session {
		out := map[string]ipc.Session{}
		for _, s := range c.list() {
			out[s.ID] = s
		}
		return out
	}

	// Everything changes, and a session starts that the version knows
	// nothing of; one it holds closes.
	c.group(ipc.GroupArgs{Do: ipc.GroupUngroup, Group: g})
	c.pin(id["gamma"], false)
	if _, err := c.rename(id["alpha"], "omega"); err != nil {
		t.Fatal(err)
	}
	c.spaces(ipc.SpacesArgs{Do: ipc.SpacesRename, Space: sp, Name: "Play"})
	delta := c.spawnNamed("delta")["delta"]
	c.park(delta, true)
	c.closeSession(id["beta"])
	changed := at()

	res := c.chainDo(ipc.ChainArgs{Do: ipc.ChainRestore, Version: chain.Short(then)})
	x := res.Restored
	if x == nil || x.Cards != 2 || x.Closed != 1 || x.Kept != 1 || x.Label != "restore "+chain.Short(then) {
		t.Fatalf("restored %+v", x)
	}
	rows := at()
	if a := rows[id["alpha"]]; a.Name != "alpha" || a.Group != g {
		t.Errorf("alpha came back as %+v", a)
	}
	if gm := rows[id["gamma"]]; !gm.Pinned {
		t.Errorf("gamma is not pinned again: %+v", gm)
	}
	if d := rows[delta]; !d.Parked || d.Name != "delta" {
		t.Errorf("the session the version does not name changed: %+v", d)
	}
	if _, open := rows[id["beta"]]; open {
		t.Error("restore reopened a closed session")
	}
	if names := spaceNames(c.spaces(ipc.SpacesArgs{})); !slices.Equal(names, []string{"Main", "Work"}) {
		t.Errorf("spaces %v", names)
	}
	if h := c.history(); h.Undo[len(h.Undo)-1].Label != x.Label || h.Undo[len(h.Undo)-1].Session != "" {
		t.Errorf("history %v", labels(h.Undo))
	}
	// The restore kept the organisation as it stood first, then its own,
	// which is the version it brought back.
	log := c.chainLog()
	if w := whats(log)[:2]; !slices.Equal(w, []string{x.Label, "as it stood before a restore"}) {
		t.Errorf("versions %q", w)
	}
	if h := c.history(); h.Undo[len(h.Undo)-1].Data["from"] != log.Versions[1].Name || h.Undo[len(h.Undo)-1].Data["to"] != then {
		t.Errorf("the step keeps %v", h.Undo[len(h.Undo)-1].Data)
	}

	// One undo takes it all back, and redo brings the version back again.
	if u := c.step(dirUndo); u.Label != x.Label {
		t.Errorf("undo %q", u.Label)
	}
	rows = at()
	for _, sid := range []string{id["alpha"], id["gamma"], delta} {
		a, b := rows[sid], changed[sid]
		if a.Name != b.Name || a.Group != b.Group || a.Pinned != b.Pinned || a.Parked != b.Parked || a.Space != b.Space || a.Order != b.Order {
			t.Errorf("after undo %s is %+v, was %+v", sid, a, b)
		}
	}
	if names := spaceNames(c.spaces(ipc.SpacesArgs{})); !slices.Equal(names, []string{"Main", "Play"}) {
		t.Errorf("spaces after undo %v", names)
	}
	c.step(dirRedo)
	if a := at()[id["alpha"]]; a.Name != "alpha" || a.Group != g {
		t.Errorf("after redo alpha is %+v", a)
	}
	if w := whats(c.chainLog())[:2]; !slices.Equal(w, []string{"redo " + x.Label, "undo " + x.Label}) {
		t.Errorf("versions %q", w)
	}
	// Restoring what is there already changes nothing, and is no step.
	if again := c.chainDo(ipc.ChainArgs{Do: ipc.ChainRestore, Version: then}); again.Restored == nil || again.Restored.Label != "" {
		t.Errorf("a second restore: %+v", again.Restored)
	}
	if err := c.call(ipc.OpChain, ipc.ChainArgs{Do: ipc.ChainRestore, Version: "000000"}, nil); err == nil {
		t.Error("a version that is not there was restored")
	}
}

func TestATamperedOrCutShortVersionIsCaught(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta")
	c.park(id["alpha"], true)
	c.pin(id["beta"], true)
	c.park(id["alpha"], false)
	log := c.chainLog()
	if len(log.Versions) != 4 {
		t.Fatalf("versions %q", whats(log))
	}
	mid := log.Versions[2].Name // park "alpha"
	path := c.versionFile(mid)
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"tampered":  bytes.Replace(good, []byte(`"parked":true`), []byte(`"pinned":true`), 1),
		"cut short": good[:len(good)/2],
	} {
		if err := os.WriteFile(path, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		r := c.chainLog()
		if len(r.Versions) != 2 || !strings.Contains(r.Problem, chain.Short(mid)) || !strings.Contains(r.Problem, "changed after it was written") {
			t.Errorf("%s: log %q, problem %q", name, whats(r), r.Problem)
		}
		for _, do := range []string{ipc.ChainShow, ipc.ChainRestore} {
			if err := c.call(ipc.OpChain, ipc.ChainArgs{Do: do, Version: mid}, nil); err == nil || !strings.Contains(err.Error(), "changed after it was written") {
				t.Errorf("%s: %s %v", name, do, err)
			}
		}
	}
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}

	// The newest version cut short is found as the core starts, and said;
	// the next version still names it, so the log stops there and says so.
	head := log.Head
	hb, _ := os.ReadFile(c.versionFile(head))
	os.WriteFile(c.versionFile(head), hb[:10], 0o600)
	c.restartCore()
	if r := c.chainDo(ipc.ChainArgs{}); !strings.Contains(r.Problem, chain.Short(head)) {
		t.Errorf("status after a restart: %+v", r)
	}
	c.pin(id["beta"], false)
	r := c.chainLog()
	if len(r.Versions) != 1 || r.Versions[0].Parent != head || !strings.Contains(r.Problem, chain.Short(head)) {
		t.Errorf("after a newest cut short: %q, problem %q", whats(r), r.Problem)
	}

	// A HEAD that names no version goes aside, and the chain starts again.
	os.WriteFile(filepath.Join(c.paths.Chain(), "HEAD"), []byte("sha512:nonsense\n"), 0o600)
	c.restartCore()
	if r := c.chainDo(ipc.ChainArgs{}); !strings.Contains(r.Problem, "HEAD.bad") || r.Head == head {
		t.Errorf("status with a bad HEAD: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(c.paths.Chain(), "HEAD.bad")); err != nil {
		t.Error(err)
	}
	c.pin(id["beta"], true)
	if r := c.chainLog(); !slices.Equal(whats(r), []string{`pin "beta"`, "the chain begins"}) || r.Versions[1].Parent != "" {
		t.Errorf("after a bad HEAD: %+v", r.Versions)
	}
}

// A version that can't be written is said, and the action goes ahead
// all the same; the next one that can be written clears it.
func TestAFailedWriteNeverStopsTheAction(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a folder of mode 0500")
	}
	c := setup(t)
	id := c.spawnNamed("alpha")
	objects := filepath.Join(c.paths.Chain(), "objects")
	c.chainLog()
	if err := os.Chmod(objects, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(objects, 0o700) })
	c.park(id["alpha"], true)
	if s, ok := c.row(id["alpha"]); !ok || !s.Parked {
		t.Errorf("the park did not happen: %+v", s)
	}
	if r := c.chainDo(ipc.ChainArgs{}); !strings.Contains(r.Problem, "could not be written") {
		t.Errorf("status %+v", r)
	}
	os.Chmod(objects, 0o700)
	c.park(id["alpha"], false)
	if r := c.chainLog(); r.Problem != "" || r.Versions[0].What != `unpark "alpha"` {
		t.Errorf("after the folder came back: %q, %q", whats(r), r.Problem)
	}
}

// The canonical bytes of an organisation depend on it alone: not on the
// order a decoder met its keys, nor on space between them.
func TestTheSameOrganisationAlwaysHasTheSameName(t *testing.T) {
	org := ipc.ChainOrg{
		Spaces: []ipc.Space{{ID: "main", Name: "Main"}, {ID: "pabc", Name: "Work"}},
		Groups: []ipc.Group{{ID: "gone", Name: "Steak"}, {ID: "gtwo", Name: "Irie", Parent: "gone"}},
		Cards:  []ipc.ChainCard{{ID: "s38fyj3p0c4fr", Name: "api fix", Pinned: true}, {ID: "s38fyaba87zvs", Space: "pabc", Group: "gtwo"}},
	}
	a, err := chain.Canonical(org)
	if err != nil {
		t.Fatal(err)
	}
	var shuffled ipc.ChainOrg
	in := `{ "cards": [ {"group":"","pinned":true,"name":"api fix","id":"s38fyj3p0c4fr"},
	  {"space":"pabc","id":"s38fyaba87zvs","group":"gtwo","parked":false} ],
	  "groups": [{"name":"Steak","id":"gone"},{"parent":"gone","name":"Irie","id":"gtwo"}],
	  "spaces": [{"name":"Main","id":"main"},{"name":"Work","id":"pabc"}] }`
	if err := json.Unmarshal([]byte(in), &shuffled); err != nil {
		t.Fatal(err)
	}
	b, err := chain.Canonical(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) || chain.Name(a) != chain.Name(b) {
		t.Errorf("\n%s\n%s", a, b)
	}
	if bytes.ContainsAny(a, "\n\t") || bytes.Contains(a, []byte(`": `)) || bytes.Contains(a, []byte(`, "`)) {
		t.Errorf("whitespace between the tokens: %s", a)
	}
	// A version holds whole numbers only.
	v := ipc.ChainVersion{Format: ipc.ChainFormat, At: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli(), What: ipc.ChainWhat{Do: chainBegin}, Org: org}
	if _, err := chain.Canonical(v); err != nil {
		t.Error(err)
	}
}

// BenchmarkNoteOrg is what an action that changes the organisation adds to
// its own path, with the 40 sessions of perf_test.go: a copy of the
// organisation, queued for the writer.
func BenchmarkNoteOrg(b *testing.B) {
	c := benchCore(b, benchSessions)
	c.spaces = []ipc.Space{firstSpace}
	c.mu.Lock()
	defer c.mu.Unlock()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		c.noteOrg(ipc.ChainWhat{Do: actPark, Sessions: []string{"s000000000001"}})
		if i%1000 == 999 {
			c.chain.mu.Lock()
			c.chain.queue = nil
			c.chain.mu.Unlock()
		}
	}
}

// BenchmarkWriteVersion is what the writer does for each, off the action's
// path: the canonical bytes, their SHA-512, and the file.
func BenchmarkWriteVersion(b *testing.B) {
	c := benchCore(b, benchSessions)
	c.spaces = []ipc.Space{firstSpace}
	c.mu.Lock()
	org := c.orgNow()
	c.mu.Unlock()
	store := chain.Store{Dir: b.TempDir()}
	parent := ""
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		org.Cards[0].Parked = i%2 == 0
		name, _, err := writeVersion(store, orgNote{at: time.Now(), what: ipc.ChainWhat{Do: actPark}, org: org}, parent, nil)
		if err != nil {
			b.Fatal(err)
		}
		parent = name
	}
}
