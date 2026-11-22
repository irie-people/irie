package core

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/keyring"
	"github.com/irie-people/irie/internal/testutil"
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

// versionFile is where the chain keeps the version named, sealed.
func (c *coreEnv) versionFile(name string) string {
	return filepath.Join(c.paths.Chain(), "objects", strings.TrimPrefix(name, chain.Prefix)+".sealed")
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
		b := []byte(c.chainDo(ipc.ChainArgs{Do: ipc.ChainShow, Version: e.Name}).Raw)
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
	flipped := bytes.Clone(good)
	flipped[len(flipped)/2] ^= 1
	for name, bad := range map[string][]byte{
		"tampered":  flipped,
		"cut short": good[:len(good)/2],
	} {
		if err := os.WriteFile(path, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		r := c.chainLog()
		if len(r.Versions) != 2 || !strings.Contains(r.Problem, chain.Short(mid)) || !strings.Contains(r.Problem, "after it was") {
			t.Errorf("%s: log %q, problem %q", name, whats(r), r.Problem)
		}
		for _, do := range []string{ipc.ChainShow, ipc.ChainRestore} {
			if err := c.call(ipc.OpChain, ipc.ChainArgs{Do: do, Version: mid}, nil); err == nil || !strings.Contains(err.Error(), "after it was") {
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

// The chain is sealed at rest: nothing on disk holds a word of what it
// keeps, and without its key nothing reads. A test's Irie has no key
// store (IRIE_KEYCHAIN=off), so the key is in the key file, and the chain
// says so. A key that is not the chain's locks it, loudly, while the
// actions go on; the right one opens it again.
func TestTheChainIsKeptInTheVault(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta")
	c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"], id["beta"]}})
	c.pin(id["alpha"], true)
	r := c.chainLog()
	if r.Vault != keyring.KindFile || len(r.Versions) != 3 {
		t.Fatalf("vault %q, versions %q", r.Vault, whats(r))
	}
	dir := c.paths.Chain()
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the chain's folder: %v %v", info, err)
	}
	for _, f := range []string{filepath.Join(dir, "key"), filepath.Join(c.paths.Keys(), "sidebar.key")} {
		if info, err := os.Stat(f); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", f, info, err)
		}
	}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(path)
		for _, word := range []string{"alpha", "beta", "Steak", "Main", ipc.ChainFormat, "pinned"} {
			if bytes.Contains(b, []byte(word)) {
				t.Errorf("%s holds %q", path, word)
			}
		}
		if filepath.Dir(path) == filepath.Join(dir, "objects") && !strings.HasSuffix(path, ".sealed") {
			t.Errorf("%s: not a sealed record", path)
		}
		return nil
	})

	// Another key: no version opens, so the chain is locked, says so and
	// writes nothing, while the action goes ahead; what it makes waits. A
	// test's Irie keeps the system's key in a file (IRIE_KEYCHAIN=off).
	keyPath := filepath.Join(c.paths.Keys(), "sidebar.key")
	good, _ := os.ReadFile(keyPath)
	wrong := []byte(strings.Repeat("ab", keyring.Size) + "\n")
	os.WriteFile(keyPath, wrong, 0o600)
	c.restartCore()
	st := c.chainDo(ipc.ChainArgs{})
	if !strings.Contains(st.Problem, "locked") || !strings.Contains(st.Problem, "Nothing on disk was changed") || !strings.Contains(st.Problem, dir) {
		t.Errorf("status under another key: %+v", st)
	}
	if err := c.call(ipc.OpChain, ipc.ChainArgs{Do: ipc.ChainLog}, nil); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Errorf("log under another key: %v", err)
	}
	before, _ := filepath.Glob(filepath.Join(dir, "objects", "*"))
	c.pin(id["alpha"], false)
	if s, ok := c.row(id["alpha"]); !ok || s.Pinned {
		t.Errorf("the unpin did not happen: %+v", s)
	}
	c.chainDo(ipc.ChainArgs{})
	if after, _ := filepath.Glob(filepath.Join(dir, "objects", "*")); len(after) != len(before) {
		t.Errorf("a locked chain wrote: %d records, then %d", len(before), len(after))
	}
	if now, _ := os.ReadFile(keyPath); !bytes.Equal(now, wrong) {
		t.Error("the key file changed while locked")
	}
	if dk, _ := os.ReadFile(filepath.Join(dir, "key")); len(dk) == 0 {
		t.Error("the sealed data key went")
	}

	// The right key back: a chain op opens it again, with no restart, and
	// what waited is written.
	os.WriteFile(keyPath, good, 0o600)
	testutil.Eventually(t, 3*chainRetry, "the chain did not open again", func() bool {
		return c.chainDo(ipc.ChainArgs{}).Problem == ""
	})
	if r := c.chainLog(); r.Problem != "" || len(r.Versions) != 4 || r.Versions[0].What != `unpin "alpha"` {
		t.Errorf("with the key back: %q, %q", whats(r), r.Problem)
	}

	// Set aside by hand, as the words say: a chain op then begins a new
	// chain, the old one left as it was.
	os.WriteFile(keyPath, wrong, 0o600)
	c.restartCore()
	if st := c.chainDo(ipc.ChainArgs{}); !strings.Contains(st.Problem, "locked") {
		t.Fatalf("not locked: %+v", st)
	}
	aside := dir + ".aside"
	if err := os.Rename(dir, aside); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 3*chainRetry, "a new chain did not begin", func() bool {
		return c.chainDo(ipc.ChainArgs{}).Problem == ""
	})
	if r := c.chainLog(); !slices.Equal(whats(r), []string{"the chain begins"}) {
		t.Errorf("a new chain: %q", whats(r))
	}
	if old, _ := filepath.Glob(filepath.Join(aside, "objects", "*.sealed")); len(old) != 4 {
		t.Errorf("the chain set aside holds %d versions", len(old))
	}
}

// A chain the first build kept, plain at the top of the chains' folder,
// moves into a folder of its own beside the other chains, and into the
// vault, as the core first starts: its versions as they were, sealed, its
// plain files and locks gone. The charters' chain beside it is
// left be.
func TestAChainKeptBeforeTheVaultMovesIntoIt(t *testing.T) {
	c := prepare(t, agentConfig{})
	top := filepath.Dir(c.paths.Chain())
	old := chain.Store{Dir: top}
	b, err := chain.Canonical(ipc.ChainVersion{Format: ipc.ChainFormat, At: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC).UnixMilli(),
		What: ipc.ChainWhat{Do: chainBegin}, Org: ipc.ChainOrg{Spaces: []ipc.Space{firstSpace}}})
	if err != nil {
		t.Fatal(err)
	}
	name, err := old.Put(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SetHead(name); err != nil {
		t.Fatal(err)
	}
	founding := filepath.Join(top, "founding", "HEAD")
	os.MkdirAll(filepath.Dir(founding), 0o700)
	os.WriteFile(founding, []byte("left be\n"), 0o600)
	c.up()
	r := c.chainLog()
	if r.Problem != "" || len(r.Versions) != 1 || r.Head != name || r.Vault != keyring.KindFile {
		t.Fatalf("after the move: %+v", r)
	}
	if got := c.chainDo(ipc.ChainArgs{Do: ipc.ChainShow, Version: "head"}).Raw; got != string(b) {
		t.Errorf("the version changed: %s", got)
	}
	left, _ := filepath.Glob(filepath.Join(top, "*"))
	for _, f := range left {
		if base := filepath.Base(f); base != "founding" && base != "sidebar" {
			t.Errorf("%s stayed at the top", f)
		}
	}
	if sealed, _ := filepath.Glob(filepath.Join(c.paths.Chain(), "objects", "*")); len(sealed) != 1 || !strings.HasSuffix(sealed[0], ".sealed") {
		t.Errorf("objects %v", sealed)
	}
	if got, _ := os.ReadFile(founding); string(got) != "left be\n" {
		t.Errorf("the founding chain was touched: %q", got)
	}
}

// memKeys is a key store in memory, as a test's Keychain: it says when it
// has no key, or can't be asked.
type memKeys struct {
	keys map[string][]byte
	away bool
}

func (m *memKeys) Kind() string { return keyring.KindKeychain }
func (m *memKeys) Find(name string) ([]byte, error) {
	if m.away {
		return nil, keyring.ErrAway
	}
	if k, ok := m.keys[name]; ok {
		return k, nil
	}
	return nil, keyring.ErrNoKey
}
func (m *memKeys) Key(name string) ([]byte, error) {
	if k, err := m.Find(name); !errors.Is(err, keyring.ErrNoKey) {
		return k, err
	}
	m.keys[name] = bytes.Repeat([]byte{7}, keyring.Size)
	return m.keys[name], nil
}
func (m *memKeys) Put(name string, key []byte) error {
	if m.away {
		return keyring.ErrAway
	}
	m.keys[name] = bytes.Clone(key)
	return nil
}

// The system's key is made only while a chain has sealed nothing; one kept
// in a file, as before there was a store, opens the chain and goes into the
// store; a store that can't be asked is away, one that lost the key reads
// as lost, and another key as wrong, each with nothing changed.
func TestTheVaultsKeyIsNeverMadeOverWhatItSealed(t *testing.T) {
	base := t.TempDir()
	keysDir, dir := filepath.Join(base, "keys"), filepath.Join(base, "chain", "sidebar")
	files := keyring.Files{Dir: keysDir}
	v, why, err := openVault(chain.Store{Dir: dir}, files, keysDir)
	if err != nil {
		t.Fatalf("first open: %d %v", why, err)
	}
	store := chain.Store{Dir: dir, Vault: v}
	name, err := store.Put([]byte(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	store.SetHead(name)
	fileKey, _ := files.Find(vaultAccount)

	// A store comes: the file's key opens the chain and moves into it.
	keys := &memKeys{keys: map[string][]byte{}}
	if _, why, err := openVault(chain.Store{Dir: dir}, keys, keysDir); err != nil {
		t.Fatalf("with a store: %d %v", why, err)
	}
	if !bytes.Equal(keys.keys[vaultAccount], fileKey) {
		t.Error("the key did not go into the store")
	}
	if _, err := os.Stat(filepath.Join(keysDir, vaultAccount+".key")); !os.IsNotExist(err) {
		t.Error("the key stayed in its file beside a store")
	}

	for what, tc := range map[string]struct {
		keys keyring.Source
		why  int
	}{
		"away":      {&memKeys{keys: map[string][]byte{vaultAccount: fileKey}, away: true}, lockAway},
		"lost":      {&memKeys{keys: map[string][]byte{}}, lockGone},
		"wrong key": {&memKeys{keys: map[string][]byte{vaultAccount: bytes.Repeat([]byte{9}, keyring.Size)}}, lockWrong},
	} {
		before, _ := os.ReadFile(filepath.Join(dir, "key"))
		if _, why, err := openVault(chain.Store{Dir: dir}, tc.keys, keysDir); err == nil || why != tc.why {
			t.Errorf("%s: %d %v, want %d", what, why, err, tc.why)
		}
		if after, _ := os.ReadFile(filepath.Join(dir, "key")); !bytes.Equal(before, after) {
			t.Errorf("%s changed the data key", what)
		}
		if m := tc.keys.(*memKeys); what == "lost" && len(m.keys) != 0 {
			t.Errorf("%s: a key was made over what the old one sealed", what)
		}
	}
	// With IRIE_KEYCHAIN=off, a chain whose key the Keychain keeps is
	// away, not lost.
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	if _, why, err := openVault(chain.Store{Dir: dir}, keyring.Files{Dir: keysDir}, keysDir); why != lockAway || !strings.Contains(lockedWords(dir, why, err), brand.Env("KEYCHAIN")+"=off") {
		t.Errorf("off: %d %v", why, err)
	}
}
