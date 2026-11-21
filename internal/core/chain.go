package core

// The sidebar's chain (DESIGN §4.23, docs/proposals/local-chain.md). Each
// action that changes how the sessions are organised, once it has applied,
// adds a version: the whole organisation, the spaces, your groups and this
// machine's open sessions in the list's order with their pins, parks,
// spaces, groups and the names you gave them, as RFC 8785 canonical JSON
// that names the version before it, kept under the SHA-512 of its bytes
// (internal/chain) in $XDG_STATE_HOME/irie/chain. Undo, redo and restore
// add versions too, so history is never rewritten.
//
// A version holds session, group and space ids and the names you gave, and
// nothing else: no person's name, address or account, no host name, no
// machine id. Nothing of it leaves the computer.
//
// The action's own path only takes a snapshot, a copy of the organisation
// under c.mu, once state.json is written. One writer, off that path, turns
// each snapshot into a version in the order they were taken, and writes
// none when the organisation is as the newest version has it. A write that
// fails is logged and shown by `irie ctl chain`, and never blocks or takes
// back the action.

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/keyring"
)

// chainWait is how long a chain op waits for the versions still being
// written, so that it reads what the actions before it did.
const chainWait = 5 * time.Second

// What made a version besides a step of the history (ipc.ChainWhat's Do).
const (
	chainBegin       = "begin"  // the organisation as the chain found it, when it had no version yet
	chainBefore      = "before" // the organisation as it stood just before a restore, which its undo goes back to
	chainRenameGroup = "rename-group"
	chainNewSpace    = "new-space"
	chainRenameSpace = "rename-space"
	chainMoveSpace   = "move-space"
	chainRemoveSpace = "remove-space"
)

// orgKinds are the kinds of the history's steps that change the
// organisation: each adds a version. Freezing, boosting, starting, closing
// and reopening a session change no part of it that a version keeps.
var orgKinds = map[string]bool{
	actReorder: true, actPark: true, actUnpark: true, actPin: true, actUnpin: true,
	actGroup: true, actUngroup: true, actNest: true, actSpace: true, actRename: true,
	actRestore: true,
}

// orgChain is the chain as the core keeps it: its store, its newest
// version, and the snapshots still to write. Its lock is taken after c.mu,
// and held for no write.
type orgChain struct {
	mu      sync.Mutex
	store   chain.Store
	ready   bool   // the store is open: its HEAD read and checked
	head    string // the newest version's name; "" before the first
	headOrg []byte // the canonical bytes of the newest version's organisation; nil when it could not be read
	count   int    // the versions kept, as counted at the start and written since
	// kept is where the vault's key is kept (keyring.KindKeychain, and so
	// on); locked, why the vault did not open, while nothing is read or
	// written. broken says what was found wrong with what is on disk, such
	// as a version that does not match its name; failed, why the last
	// write failed. All but kept are "" while all is well.
	kept, locked, broken, failed string
	queue                        []orgNote
	held                         []orgNote     // what came while it was locked, to write once it opens
	tried                        time.Time     // when a chain op last asked a locked chain to open again
	idle                         chan struct{} // closed while nothing waits to be written
	idleClosed                   bool
	kick                         chan struct{} // a snapshot waits
	writer                       sync.Once     // starts the writer, once
}

// holdMax is how many snapshots a locked chain holds to write once it
// opens; past it, the oldest go, and the log says so.
const holdMax = 1000

// orgNote is a snapshot to write as a version: when it was taken, what
// took it, and the organisation then.
type orgNote struct {
	at   time.Time
	what ipc.ChainWhat
	org  ipc.ChainOrg
}

// lazy makes the chain's channels. The caller holds oc.mu.
func (oc *orgChain) lazy() {
	if oc.kick == nil {
		oc.kick = make(chan struct{}, 1)
		oc.idle = make(chan struct{})
		close(oc.idle)
		oc.idleClosed = true
	}
}

// push queues a snapshot for the writer, and never waits for it.
func (oc *orgChain) push(n orgNote) {
	oc.mu.Lock()
	oc.lazy()
	oc.queue = append(oc.queue, n)
	if oc.idleClosed {
		oc.idle, oc.idleClosed = make(chan struct{}), false
	}
	kick := oc.kick
	oc.mu.Unlock()
	select {
	case kick <- struct{}{}:
	default:
	}
}

// flush waits up to d for every snapshot queued so far to be written, and
// reports whether they were.
func (oc *orgChain) flush(d time.Duration) bool {
	oc.mu.Lock()
	oc.lazy()
	idle := oc.idle
	oc.mu.Unlock()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-idle:
		return true
	case <-t.C:
		return false
	}
}

// problem is what is wrong with the chain, in words, or "".
func (oc *orgChain) problem() string {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	return oc.problemLocked()
}

func (oc *orgChain) problemLocked() string {
	var ps []string
	if !oc.ready {
		ps = append(ps, "the chain is not open yet")
	}
	if oc.locked != "" {
		ps = append(ps, "the chain is locked, so nothing is read or written: "+oc.locked)
	}
	for _, p := range []string{oc.broken, oc.failed} {
		if p != "" {
			ps = append(ps, p)
		}
	}
	return strings.Join(ps, "; ")
}

// noteOrg takes a snapshot of the organisation as it is now, which what
// made, for the writer to keep as a version. The caller holds c.mu, after
// the action applied and state.json was written.
func (c *Core) noteOrg(what ipc.ChainWhat) {
	c.chain.push(orgNote{at: time.Now(), what: what, org: c.orgNow()})
}

// orgNow is the organisation as it stands: the spaces, the first first;
// your groups, in the order they were made; and this machine's open
// sessions in the list's order, each with the name you gave it, if you
// did, its pin or park, its space and its group. It holds no other name.
// The caller holds c.mu.
func (c *Core) orgNow() ipc.ChainOrg {
	o := ipc.ChainOrg{Spaces: slices.Clone(c.spaces), Groups: slices.Clone(c.groups)}
	for _, id := range c.openOrder() {
		x := c.sessions[id]
		card := ipc.ChainCard{ID: id, Pinned: x.track.pinned, Parked: x.track.parked, Space: c.spaceOf(x), Group: c.groupOf(x)}
		if x.named {
			card.Name = x.name
		}
		o.Cards = append(o.Cards, card)
	}
	return o
}

// openOrder is this machine's open sessions in the list's order, as
// sortRows puts them: those the order names first, then the rest in the
// order they started. The caller holds c.mu.
func (c *Core) openOrder() []string {
	ids := make([]string, 0, len(c.sessions))
	seen := make(map[string]bool, len(c.sessions))
	for _, id := range c.order {
		if c.sessions[id] != nil && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	var rest []*session
	for id, x := range c.sessions {
		if !seen[id] {
			rest = append(rest, x)
		}
	}
	slices.SortFunc(rest, func(a, b *session) int {
		return cmp.Or(a.created.Compare(b.created), strings.Compare(a.id, b.id))
	})
	for _, x := range rest {
		ids = append(ids, x.id)
	}
	return ids
}

// orgWhat is what of steps the user took at once changes the
// organisation, as a version says it, and false when none does.
func orgWhat(steps []takenStep) (ipc.ChainWhat, bool) {
	var w ipc.ChainWhat
	for _, s := range steps {
		if !orgKinds[s.kind] {
			continue
		}
		if w.Do == "" {
			w.Do = s.kind
		}
		w = withStep(w, s.kind, s.session, s.data)
	}
	return w, w.Do != ""
}

// startChain queues the version the chain begins with, which the writer
// keeps only while the chain has none, and opens the store beside the
// core's start: its first look at the disk need not hold up the socket.
// Every pane is adopted by now, so the organisation is the one Irie found.
func (c *Core) startChain() {
	c.mu.Lock()
	c.noteOrg(ipc.ChainWhat{Do: chainBegin})
	c.mu.Unlock()
	go c.openChain(false)
}

// chainStop is how long a stopping core waits for the versions still to
// write, as an install restarts it.
const chainStop = time.Second

// stopChain writes, for up to d, what the chain still has to write as the
// core stops, and says what it could not.
func (c *Core) stopChain(d time.Duration) {
	if c.chain.flush(d) {
		return
	}
	c.chain.mu.Lock()
	queued := len(c.chain.queue)
	c.chain.mu.Unlock()
	c.log.Warn("chain: the core stopped with versions still to write", "queued", queued, "and one being written", queued == 0, "problem", c.chain.problem())
}

// vaultAccount names the sidebar's chain among what the system's key store
// keeps for Irie's vaults.
const vaultAccount = "sidebar"

// openChain opens the chain's vault, moves into it the versions kept
// before it, reads HEAD and checks the version it names, then lets the
// writer go on. Whatever does not check is said, and kept as it is: a HEAD
// that names no version goes aside to HEAD.bad and the next version starts
// the chain again, its older versions still on disk; a newest version that
// does not check stays where it is, and the next version names it, so the
// log stops there and says why. A vault that does not open, as under a
// wrong key, locks the chain: lockedWords says why and what to do, nothing
// is read or written and nothing on disk changes, and what the actions
// meanwhile make waits (holdMax of it) for the chain to open, as a chain op
// asks it to again (retryChain). again is that asking: once the chain is
// open, a new one, its folder set aside by hand, begins as a chain does.
func (c *Core) openChain(again bool) {
	store := chain.Store{Dir: c.paths.Chain()}
	var locked, broken, head string
	var headOrg []byte
	if err := settleFolder(store.Dir); err != nil {
		broken = fmt.Sprintf("the chain kept before it had a folder of its own could not all move into %s: %v", store.Dir, err)
	}
	keys := keyring.System(c.paths.Keys())
	vault, why, err := openVault(store, keys, c.paths.Keys())
	if err != nil {
		locked = lockedWords(store.Dir, why, err)
	} else {
		store.Vault = vault
		broken = c.moveIn(store)
		if head, err = store.Head(); err != nil {
			bad := filepath.Join(store.Dir, "HEAD")
			os.Rename(bad, bad+".bad")
			broken = joinProblems(broken, fmt.Sprintf("HEAD did not name a version (%v): it was moved to HEAD.bad, and the chain starts again from the next version", err))
			head = ""
		} else if head != "" {
			v, err := readVersion(store, head)
			if err != nil {
				broken = joinProblems(broken, err.Error())
			} else if headOrg, err = chain.Canonical(v.Org); err != nil {
				broken = joinProblems(broken, fmt.Sprintf("version %s: %v", chain.Short(head), err))
			}
		}
	}
	if again && vault != nil && head == "" {
		c.mu.Lock()
		c.noteOrg(ipc.ChainWhat{Do: chainBegin})
		c.mu.Unlock()
	}
	oc := &c.chain
	oc.mu.Lock()
	oc.store, oc.head, oc.headOrg, oc.locked, oc.broken, oc.ready = store, head, headOrg, locked, broken, true
	oc.count, oc.kept = countVersions(store), ""
	if vault != nil {
		oc.kept = keyring.Kind(keys)
		// What waited while it was locked goes first, in its order.
		oc.queue = append(oc.held, oc.queue...)
		oc.held = nil
	}
	oc.lazy()
	if len(oc.queue) > 0 && oc.idleClosed {
		oc.idle, oc.idleClosed = make(chan struct{}), false
	}
	kick := oc.kick
	oc.mu.Unlock()
	for _, p := range []string{locked, broken} {
		if p != "" {
			c.log.Warn("chain", "problem", p)
		}
	}
	oc.writer.Do(func() { go c.chainWriter() })
	select {
	case kick <- struct{}{}:
	default:
	}
}

// settleFolder moves the chain kept at the top of the chains' folder, as
// the first build of it kept it, into dir, its folder of its own beside
// the other chains (chain/charters): objects, HEAD, HEAD.bad and the key
// file, each once. What dir has already stays, and so does what would
// clash with it, which an error says.
func settleFolder(dir string) error {
	top := filepath.Dir(dir)
	if _, err := os.Lstat(filepath.Join(top, "HEAD")); err != nil {
		if _, err := os.Lstat(filepath.Join(top, "objects")); err != nil {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var stayed []string
	for _, name := range []string{"objects", "HEAD", "HEAD.bad", "key"} {
		from, to := filepath.Join(top, name), filepath.Join(dir, name)
		if _, err := os.Lstat(from); err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			stayed = append(stayed, from)
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	os.Remove(filepath.Join(top, ".HEAD.lock"))
	if len(stayed) > 0 {
		return fmt.Errorf("%s stayed, as %s has its own", strings.Join(stayed, ", "), dir)
	}
	return nil
}

// chainRetry is how soon a chain op asks a locked chain to open again.
const chainRetry = 5 * time.Second

// retryChain opens a locked chain again, as a chain op asks it to, at most
// every chainRetry: a key store out of reach may be back, a key put back,
// or the chain set aside by hand.
func (c *Core) retryChain() {
	oc := &c.chain
	oc.mu.Lock()
	try := oc.ready && oc.locked != "" && time.Since(oc.tried) >= chainRetry
	if try {
		oc.tried = time.Now()
	}
	oc.mu.Unlock()
	if try {
		c.openChain(true)
	}
}

// Why a chain's vault does not open (lockedWords).
const (
	lockOther = iota
	lockAway  // the key store is off or out of reach now
	lockWrong // the key does not open the chain
	lockGone  // the key store plainly lost the key
)

// openVault opens the chain's vault in store's folder: the data key there,
// sealed under the key the system keeps (internal/keyring), made on the
// first open. The system's key is read, and made only while the chain has
// sealed nothing: a key lost must read as lost, never be made again over
// what the old one sealed. A store that keeps no key may have a chain
// whose key went to a file in keysDir, as before there was a store: that
// key opens it, and then goes into the store, so no key sits in a file
// while the system can keep it. Should no version open under the key, the
// newest or any other, the key is not the chain's. why says which, when it
// fails.
func openVault(store chain.Store, keys keyring.Source, keysDir string) (v *chain.Vault, why int, err error) {
	var kek []byte
	if f, ok := keys.(keyring.Finder); ok {
		kek, err = f.Find(vaultAccount)
	} else {
		kek, err = keys.Key(vaultAccount)
	}
	var fromFile bool
	if errors.Is(err, keyring.ErrNoKey) && keyring.Kind(keys) != keyring.KindFile {
		if k, ferr := (keyring.Files{Dir: keysDir}).Find(vaultAccount); ferr == nil {
			kek, err, fromFile = k, nil, true
		}
	}
	switch {
	case errors.Is(err, keyring.ErrNoKey):
		if n := sealedVersions(store.Dir); n > 0 {
			if keyring.Kind(keys) == keyring.KindFile && os.Getenv(brand.Env("KEYCHAIN")) == "off" {
				return nil, lockAway, fmt.Errorf("%w: %d versions were sealed under a key the store keeps", keyring.ErrAway, n)
			}
			return nil, lockGone, fmt.Errorf("%s keeps no key for it, and %d versions were sealed under one", whereKeys(keys), n)
		}
		if kek, err = keys.Key(vaultAccount); err != nil {
			return nil, lockAway, err
		}
	case errors.Is(err, keyring.ErrAway):
		return nil, lockAway, err
	case err != nil:
		return nil, lockOther, err
	}
	if v, err = chain.OpenVault(store.Dir, kek); err != nil {
		if errors.Is(err, chain.ErrWrongKey) {
			if _, serr := os.Stat(filepath.Join(store.Dir, "key")); errors.Is(serr, os.ErrNotExist) {
				return nil, lockGone, err
			}
			return nil, lockWrong, err
		}
		return nil, lockOther, err
	}
	store.Vault = v
	if head, err := store.Head(); err == nil && head != "" {
		if _, err := store.Get(head); err != nil && !opensAny(store) {
			return nil, lockWrong, fmt.Errorf("%w: none of the chain's versions opens under it", chain.ErrWrongKey)
		}
	}
	// The key kept in a file goes into the store, now that there is one;
	// should the store not take it, it stays in its file, and opens the
	// chain the next time too.
	if p, ok := keys.(keyring.Putter); ok && fromFile {
		if err := p.Put(vaultAccount, kek); err == nil {
			os.Remove(filepath.Join(keysDir, vaultAccount+".key"))
		}
	}
	return v, 0, nil
}

// sealedVersions counts the sealed versions in a chain's folder.
func sealedVersions(dir string) int {
	files, _ := filepath.Glob(filepath.Join(dir, "objects", "*.sealed"))
	return len(files)
}

// opensAny reports whether any of the store's sealed versions, the first
// 32, opens under its vault's key, or there are none to try.
func opensAny(store chain.Store) bool {
	files, _ := filepath.Glob(filepath.Join(store.Dir, "objects", "*.sealed"))
	if len(files) == 0 {
		return true
	}
	for i, f := range files {
		if i == 32 {
			break
		}
		h := strings.TrimSuffix(filepath.Base(f), ".sealed")
		if _, err := store.Get(chain.Prefix + h); err == nil {
			return true
		}
	}
	return false
}

// whereKeys says where keys keeps its keys, for a person.
func whereKeys(keys keyring.Source) string {
	switch keyring.Kind(keys) {
	case keyring.KindKeychain:
		return "the login Keychain"
	case keyring.KindSecrets:
		return "the Secret Service"
	}
	return "the key files"
}

// moveIn seals into the vault the versions the chain kept before it had
// one (chain.Store.Seal), and says what did not move, or "".
func (c *Core) moveIn(store chain.Store) string {
	moved, err := store.Seal()
	if moved > 0 {
		c.log.Info("chain: moved into the vault", "versions", moved)
	}
	if err != nil {
		return fmt.Sprintf("not every version kept before the vault moved into it, and those stay as they were: %v", err)
	}
	return ""
}

// joinProblems is a and b, either of which may be "", as one.
func joinProblems(a, b string) string {
	return strings.Trim(a+"; "+b, "; ")
}

// lockedWords says why the chain in dir is locked and what to do, for
// err, why its vault did not open, and why, its cause. Whatever the cause,
// it stays locked, one chain under one key, and never begins another by
// itself: a wrong key fails loudly, and the key may come back. A store off
// or out of reach is waited for; a wrong key or one gone is the person's
// to settle, so the words say how to set the chain aside by hand.
func lockedWords(dir string, why int, err error) string {
	aside := fmt.Sprintf("to begin a new chain instead, move %s aside (its versions stay there) and run `%s ctl chain`", dir, brand.Slug)
	switch why {
	case lockAway:
		what := "the key store is off or out of reach now"
		if os.Getenv(brand.Env("KEYCHAIN")) == "off" {
			what = brand.Env("KEYCHAIN") + "=off turns the Keychain off"
		}
		return fmt.Sprintf("%s (%v). It opens again once the store is back: `%s ctl chain` asks", what, err, brand.Slug)
	case lockWrong:
		return fmt.Sprintf("this key does not open these versions (%v). Nothing on disk was changed; %s", err, aside)
	case lockGone:
		return fmt.Sprintf("the key store lost the key (%v), so the history reads again only if a backup of the Keychain brings the key back. Nothing on disk was changed; %s", err, aside)
	}
	return err.Error()
}

// countVersions counts the versions in a chain's store: its sealed
// records, or with no vault its plain ones.
func countVersions(store chain.Store) int {
	ext := ".json"
	if store.Vault != nil {
		ext = ".sealed"
	}
	files, _ := os.ReadDir(filepath.Join(store.Dir, "objects"))
	n := 0
	for _, f := range files {
		if h, ok := strings.CutSuffix(f.Name(), ext); ok && chain.Valid(chain.Prefix+h) {
			n++
		}
	}
	return n
}

// chainWriter writes the snapshots as versions, one at a time, in the
// order they were taken. It runs for the core's life.
func (c *Core) chainWriter() {
	oc := &c.chain
	for range oc.kick {
		for {
			oc.mu.Lock()
			if len(oc.queue) == 0 {
				if !oc.idleClosed {
					close(oc.idle)
					oc.idleClosed = true
				}
				oc.mu.Unlock()
				break
			}
			n := oc.queue[0]
			oc.queue = slices.Delete(oc.queue, 0, 1)
			store, parent, parentOrg, locked := oc.store, oc.head, oc.headOrg, oc.locked
			oc.mu.Unlock()
			// A locked chain writes nothing: the problem says why. What comes
			// meanwhile waits for it to open.
			if locked != "" {
				oc.mu.Lock()
				oc.held = append(oc.held, n)
				dropped := len(oc.held) > holdMax
				if dropped {
					oc.held = slices.Delete(oc.held, 0, len(oc.held)-holdMax)
				}
				oc.mu.Unlock()
				if dropped {
					c.log.Warn("chain: locked, so its oldest waiting version went", "held", holdMax)
				}
				continue
			}
			name, org, err := writeVersion(store, n, parent, parentOrg)
			oc.mu.Lock()
			if name != "" {
				oc.head, oc.headOrg = name, org
				oc.count++
			}
			if err != nil {
				oc.failed = fmt.Sprintf("a version could not be written (%s): %v", time.Now().Format("15:04:05"), err)
			} else if name != "" {
				oc.failed = ""
			}
			oc.mu.Unlock()
			if err != nil {
				c.log.Warn("chain", "err", err)
			}
		}
	}
}

// writeVersion keeps n as the version after parent, whose organisation's
// canonical bytes are parentOrg, and makes it HEAD. It writes nothing, and
// returns no name, when the organisation is as parent has it, or when n
// begins a chain that has begun. A version kept whose HEAD could not be
// written is still returned, with the error, so the next one names it.
func writeVersion(store chain.Store, n orgNote, parent string, parentOrg []byte) (string, []byte, error) {
	if n.what.Do == chainBegin && parent != "" {
		return "", nil, nil
	}
	org, err := chain.Canonical(n.org)
	if err != nil {
		return "", nil, err
	}
	if parentOrg != nil && bytes.Equal(org, parentOrg) {
		return "", nil, nil
	}
	b, err := chain.Canonical(ipc.ChainVersion{Format: ipc.ChainFormat, Parent: parent, At: n.at.UnixMilli(), What: n.what, Org: n.org})
	if err != nil {
		return "", nil, err
	}
	name, err := store.Put(b)
	if err != nil {
		return "", nil, err
	}
	return name, org, store.SetHead(name)
}

// readVersion reads the version named and decodes it, refusing one that
// does not match its name or is not a version of the sidebar.
func readVersion(store chain.Store, name string) (ipc.ChainVersion, error) {
	b, err := store.Get(name)
	if err != nil {
		return ipc.ChainVersion{}, err
	}
	return decodeVersion(name, b)
}

// decodeVersion decodes a version's bytes, which its name was checked
// against.
func decodeVersion(name string, b []byte) (ipc.ChainVersion, error) {
	var v ipc.ChainVersion
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return v, fmt.Errorf("version %s does not read as a version of the sidebar: %v", chain.Short(name), err)
	}
	if v.Format != ipc.ChainFormat {
		return v, fmt.Errorf("version %s is of the form %q, which this version of %s does not read", chain.Short(name), v.Format, ipc.ChainFormat)
	}
	return v, nil
}

// chainNow is the chain's store and newest version, once every snapshot
// taken so far is written or d has passed.
func (c *Core) chainNow(d time.Duration) (chain.Store, string, error) {
	c.retryChain()
	c.chain.flush(d)
	oc := &c.chain
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if !oc.ready {
		return oc.store, "", ipc.Errorf(ipc.CodeBusy, "the sidebar's chain is not open yet; try again in a moment")
	}
	if oc.locked != "" {
		return oc.store, "", ipc.Errorf(ipc.CodeBusy, "the sidebar's chain is locked, so nothing is read or written: %s", oc.locked)
	}
	return oc.store, oc.head, nil
}

// chainStatus is the chain as it stands, for every chain op.
func (c *Core) chainStatus() ipc.ChainResult {
	oc := &c.chain
	oc.mu.Lock()
	defer oc.mu.Unlock()
	return ipc.ChainResult{Dir: c.paths.Chain(), Head: oc.head, Count: oc.count, Vault: oc.kept, Problem: oc.problemLocked()}
}

// chainOp reads the chain, or brings a version of it back (ipc.ChainArgs).
func (c *Core) chainOp(a ipc.ChainArgs) (ipc.ChainResult, error) {
	switch a.Do {
	case ipc.ChainStatus, ipc.ChainHead:
		c.retryChain()
		c.chain.flush(chainWait)
		return c.chainStatus(), nil
	case ipc.ChainLog:
		return c.chainLog(a.Limit)
	case ipc.ChainShow:
		return c.chainShow(a.Version)
	case ipc.ChainRestore:
		return c.chainRestore(a.Version)
	}
	return ipc.ChainResult{}, ipc.Errorf(ipc.CodeBadArgs, "unknown chain do %q", a.Do)
}

// chainLog is the newest versions, newest first, limit of them. A version
// that does not check ends the walk there, and the result says why.
func (c *Core) chainLog(limit int) (ipc.ChainResult, error) {
	store, head, err := c.chainNow(chainWait)
	if err != nil {
		return c.chainStatus(), err
	}
	if limit <= 0 {
		limit = ipc.ChainLogDefault
	}
	res := c.chainStatus()
	entries, walkErr := store.Log(head, limit)
	var vs []ipc.ChainVersion
	for _, e := range entries {
		v, err := decodeVersion(e.Name, e.Bytes)
		if err != nil {
			walkErr = err
			break
		}
		vs = append(vs, v)
		res.Versions = append(res.Versions, ipc.ChainEntry{Name: e.Name})
	}
	names := c.sessionNames(vs...)
	for i, v := range vs {
		res.Versions[i] = entryOf(res.Versions[i].Name, v, names)
	}
	if walkErr != nil {
		res.Problem = strings.TrimPrefix(res.Problem+"; "+walkErr.Error(), "; ")
	}
	return res, nil
}

// chainShow is one version, with its canonical bytes.
func (c *Core) chainShow(ref string) (ipc.ChainResult, error) {
	store, head, err := c.chainNow(chainWait)
	if err != nil {
		return c.chainStatus(), err
	}
	name, err := findVersion(store, head, ref)
	if err != nil {
		return c.chainStatus(), err
	}
	b, err := store.Get(name)
	if err != nil {
		return c.chainStatus(), ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	v, err := decodeVersion(name, b)
	if err != nil {
		return c.chainStatus(), ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	names := c.sessionNames(v)
	e := entryOf(name, v, names)
	e.Doc, e.Names = &v, names
	res := c.chainStatus()
	res.Version, res.Raw = &e, string(b)
	return res, nil
}

// findVersion is the version ref names: its name, at least six hex digits
// of its start, or "head" for the newest.
func findVersion(store chain.Store, head, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.EqualFold(ref, "head") {
		if head == "" {
			return "", ipc.Errorf(ipc.CodeNotFound, "the sidebar's chain has no version yet")
		}
		return head, nil
	}
	name, err := store.Find(ref)
	if errors.Is(err, chain.ErrNone) {
		return "", ipc.Errorf(ipc.CodeNotFound, "%v", err)
	}
	if err != nil {
		return "", ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	return name, nil
}

// sessionNames are the names the sessions of versions vs have now, open or
// closed, by id, for words a person reads: a version keeps only the names
// you gave.
func (c *Core) sessionNames(vs ...ipc.ChainVersion) map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := map[string]string{}
	add := func(id string) {
		if _, ok := names[id]; !ok && id != "" {
			if n := c.nameOf(id); n != id {
				names[id] = n
			}
		}
	}
	for _, v := range vs {
		for _, t := range v.Org.Cards {
			add(t.ID)
		}
		for _, id := range v.What.Sessions {
			add(id)
		}
	}
	return names
}

// entryOf is version v, named name, as a log gives it.
func entryOf(name string, v ipc.ChainVersion, names map[string]string) ipc.ChainEntry {
	org, _ := chain.Canonical(v.Org)
	e := ipc.ChainEntry{Name: name, Parent: v.Parent, State: chain.Name(org), At: time.UnixMilli(v.At).UTC(),
		What: chainWords(v, names), Cards: len(v.Org.Cards), Groups: len(v.Org.Groups), Spaces: len(v.Org.Spaces)}
	for _, t := range v.Org.Cards {
		if t.Pinned {
			e.Pinned++
		}
		if t.Parked {
			e.Parked++
		}
	}
	return e
}

// chainWords says what made version v, as the header names a step:
// `park "api-fix"`, each session by the name v keeps for it, else by the
// name it has now, else its id.
func chainWords(v ipc.ChainVersion, names map[string]string) string {
	w := v.What
	var who []string
	for _, id := range w.Sessions {
		who = append(who, cardName(v.Org, id, names))
	}
	group := func() string {
		for _, id := range w.Groups {
			if i := slices.IndexFunc(v.Org.Groups, func(g ipc.Group) bool { return g.ID == id }); i >= 0 {
				return fmt.Sprintf("%q", v.Org.Groups[i].Name)
			}
		}
		return "a group"
	}
	space := func() string {
		for _, id := range w.Spaces {
			if i := slices.IndexFunc(v.Org.Spaces, func(s ipc.Space) bool { return s.ID == id }); i >= 0 {
				return fmt.Sprintf("%q", v.Org.Spaces[i].Name)
			}
		}
		return "a space"
	}
	switch w.Do {
	case chainBegin:
		return "the chain begins"
	case chainBefore:
		return "as it stood before a restore"
	case actRestore:
		return "restore " + chain.Short(w.Version)
	case dirUndo, dirRedo:
		inner := v
		inner.What = ipc.ChainWhat{Do: w.Of, Sessions: w.Sessions, Groups: w.Groups, Spaces: w.Spaces, Version: w.Version}
		return w.Do + " " + chainWords(inner, names)
	case chainRenameGroup:
		return "rename a group " + group()
	case chainNewSpace:
		return "new space " + space()
	case chainRenameSpace:
		return "rename a space " + space()
	case chainMoveSpace:
		return "move space " + space()
	case chainRemoveSpace:
		return "remove a space"
	case actSpace:
		return namesLabel("move", who) + spaceTail
	case actNest:
		return "move group " + group()
	}
	verb := w.Do
	if k, ok := actionKinds[w.Do]; ok {
		verb = k.verb
	}
	return namesLabel(verb, who)
}

// cardName is the name version org keeps for session id, else the one it
// has now, else its id.
func cardName(org ipc.ChainOrg, id string, names map[string]string) string {
	if i := slices.IndexFunc(org.Cards, func(t ipc.ChainCard) bool { return t.ID == id }); i >= 0 && org.Cards[i].Name != "" {
		return org.Cards[i].Name
	}
	return cmp.Or(names[id], id)
}

// withStep is w, with what one step of kind on session took: its session,
// and the group, space or version it names.
func withStep(w ipc.ChainWhat, kind, session string, data map[string]any) ipc.ChainWhat {
	if session != "" && !slices.Contains(w.Sessions, session) {
		w.Sessions = append(w.Sessions, session)
	}
	str := func(k string) string { s, _ := data[k].(string); return s }
	add := func(list []string, id string) []string {
		if id == "" || slices.Contains(list, id) {
			return list
		}
		return append(list, id)
	}
	switch kind {
	case actGroup:
		w.Groups = add(w.Groups, str("to"))
	case actUngroup:
		w.Groups = add(w.Groups, str("from"))
	case actNest:
		w.Groups = add(w.Groups, str("group"))
	case actSpace:
		w.Spaces = add(w.Spaces, str("to"))
	case actRestore:
		w.Version = str("to")
	}
	return w
}

// Action kind of a restore (undo.go): undo brings back the organisation as
// it stood before, which the chain keeps as the version the step names as
// from, and redo the version it brought back, to. It is no one session's,
// so it names none.
const actRestore = "restore"

func init() {
	registerAction(actRestore, actionKind{verb: "restore",
		label: func(a action) string { return "restore " + chain.Short(a.str("to")) },
		undo:  effect{check: checkRestore("from"), do: doRestore("from")},
		redo:  effect{check: checkRestore("to"), do: doRestore("to")}})
}

// checkRestore is what makes bringing back the version a's data keeps as
// k uncertain: that the chain can't read it, or that the sidebar is
// organised as it says already.
func checkRestore(k string) func(c *Core, a action) []problem {
	return func(c *Core, a action) []problem {
		v, err := c.readChain(a.str(k))
		if err != nil {
			return []problem{{code: "unreadable", question: fmt.Sprintf("The sidebar's chain can't give back version %s: %v.", chain.Short(a.str(k)), err), recommend: ipc.ChoiceSkip}}
		}
		if t, _, _ := c.restoreTarget(v.Org); sameOrg(c.orgNow(), t) {
			return []problem{alreadyDone("The sidebar is organised as that version has it already.")}
		}
		return nil
	}
}

// doRestore brings back the version a's data keeps as k.
func doRestore(k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		v, err := c.readChain(a.str(k))
		if err != nil {
			return a, err
		}
		c.mu.Lock()
		t, keptGroups, _ := c.restoreTarget(v.Org)
		c.applyOrg(t, keptGroups)
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		c.publishHeader()
		c.publishHistory()
		return a, nil
	}
}

// readChain reads the version named from the chain, checked.
func (c *Core) readChain(name string) (ipc.ChainVersion, error) {
	oc := &c.chain
	oc.mu.Lock()
	store, ready, locked := oc.store, oc.ready, oc.locked
	oc.mu.Unlock()
	switch {
	case !ready:
		return ipc.ChainVersion{}, errors.New("the chain is not open yet")
	case locked != "":
		return ipc.ChainVersion{}, errors.New("the chain is locked: " + locked)
	}
	return readVersion(store, name)
}

// sameOrg reports whether two organisations are one.
func sameOrg(a, b ipc.ChainOrg) bool {
	x, err1 := chain.Canonical(a)
	y, err2 := chain.Canonical(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// chainRestore brings back the organisation of the version ref names, as a
// new action that one undo takes back: first the organisation as it stands
// is kept as a version, should the newest not be it, which the undo goes
// back to; then the version's is applied, and the step recorded adds the
// version of the restore.
func (c *Core) chainRestore(ref string) (ipc.ChainResult, error) {
	store, head, err := c.chainNow(chainWait)
	if err != nil {
		return c.chainStatus(), err
	}
	name, err := findVersion(store, head, ref)
	if err != nil {
		return c.chainStatus(), err
	}
	v, err := readVersion(store, name)
	if err != nil {
		return c.chainStatus(), ipc.Errorf(ipc.CodeBadArgs, "%v, so it was not restored", err)
	}
	c.mu.Lock()
	cur := c.orgNow()
	target, keptGroups, r := c.restoreTarget(v.Org)
	if sameOrg(cur, target) {
		c.mu.Unlock()
		res := c.chainStatus()
		r.Cards = 0
		res.Restored = &r
		return res, nil
	}
	// The way back: no snapshot can come in while c.mu is held, so once
	// the queue is written the newest version is the organisation as it
	// stands, or the write failed.
	c.noteOrg(ipc.ChainWhat{Do: chainBefore})
	written := c.chain.flush(chainWait)
	c.chain.mu.Lock()
	from, fromOrg := c.chain.head, c.chain.headOrg
	c.chain.mu.Unlock()
	if want, _ := chain.Canonical(cur); !written || !bytes.Equal(fromOrg, want) {
		c.mu.Unlock()
		why := cmp.Or(c.chain.problem(), "the chain is still writing")
		return c.chainStatus(), ipc.Errorf(ipc.CodeBusy, "nothing was restored: the chain could not keep the sidebar as it is now, which an undo would go back to: %s", why)
	}
	c.applyOrg(target, keptGroups)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
	c.publishHistory()
	c.recordAction(actRestore, "", map[string]any{"from": from, "to": name})
	r.Label = "restore " + chain.Short(name)
	c.log.Info("chain: restored", "version", chain.Short(name), "cards", r.Cards, "closed", r.Closed, "kept", r.Kept)
	c.chain.flush(chainWait)
	res := c.chainStatus()
	res.Restored = &r
	return res, nil
}

// restoreTarget is the organisation that bringing back o makes of the one
// that stands. Its spaces and groups are o's, in o's order, then those
// that hold a session o does not name; its cards are o's that are open, in
// o's order, each with its pin or park, its space, its group and the name
// you gave it, then the open sessions o does not name, as they are. Such a
// session leaves a group whose cards of o go to another space: each space
// has its own groups. A card of o that is closed now stays closed. kept
// are the groups it keeps that o has not, and r counts what it brings
// back. The caller holds c.mu.
func (c *Core) restoreTarget(o ipc.ChainOrg) (t ipc.ChainOrg, kept map[string]bool, r ipc.ChainRestored) {
	cards := map[string]ipc.ChainCard{}
	for _, card := range o.Cards {
		if _, dup := cards[card.ID]; dup || !sessionID.MatchString(card.ID) {
			continue
		}
		cards[card.ID] = card
		if c.sessions[card.ID] == nil {
			r.Closed++
		}
	}
	now := c.orgNow()
	// A card says "" for the first space; o's may be another.
	firstNow := firstSpace.ID
	if len(now.Spaces) > 0 {
		firstNow = now.Spaces[0].ID
	}
	keepSpace, keepGroup := map[string]bool{}, map[string]bool{}
	var rest []ipc.ChainCard
	for _, card := range now.Cards {
		if _, ok := cards[card.ID]; ok {
			continue
		}
		card.Space = cmp.Or(card.Space, firstNow)
		rest = append(rest, card)
		keepSpace[card.Space] = true
		for _, g := range c.pathOf(card.Group) {
			keepGroup[g] = true
		}
	}
	r.Kept = len(rest)

	takenIn := func(list []string) func(string) bool {
		return func(n string) bool {
			return slices.ContainsFunc(list, func(m string) bool { return strings.EqualFold(m, n) })
		}
	}
	var spaceNames []string
	addSpace := func(s ipc.Space) {
		if s.ID == "" || slices.ContainsFunc(t.Spaces, func(x ipc.Space) bool { return x.ID == s.ID }) {
			return
		}
		s.Name = numberedName(cmp.Or(cleanName(s.Name, maxSpaceName), "Space"), takenIn(spaceNames))
		spaceNames = append(spaceNames, s.Name)
		t.Spaces = append(t.Spaces, s)
	}
	for _, s := range o.Spaces {
		addSpace(s)
	}
	if len(t.Spaces) == 0 {
		addSpace(firstSpace)
	}
	for _, s := range now.Spaces {
		if keepSpace[s.ID] {
			addSpace(s)
		}
	}
	hasSpace := func(id string) bool {
		return slices.ContainsFunc(t.Spaces[1:], func(s ipc.Space) bool { return s.ID == id })
	}

	kept = map[string]bool{}
	hasGroup := func(id string) bool {
		return slices.ContainsFunc(t.Groups, func(g ipc.Group) bool { return g.ID == id })
	}
	for _, g := range o.Groups {
		if g.ID != "" && !hasGroup(g.ID) {
			t.Groups = append(t.Groups, ipc.Group{ID: g.ID, Name: cmp.Or(cleanGroupName(g.Name), "Group"), Parent: g.Parent})
		}
	}
	for _, g := range now.Groups {
		if keepGroup[g.ID] && !hasGroup(g.ID) {
			t.Groups = append(t.Groups, g)
			kept[g.ID] = true
		}
	}
	// A group whose group is gone, or which would be inside itself, goes
	// to the top of the list, as fixGroups puts one.
	for i := range t.Groups {
		if p := t.Groups[i].Parent; p != "" && (!hasGroup(p) || groupWithin(t.Groups, p, t.Groups[i].ID)) {
			t.Groups[i].Parent = ""
		}
	}

	for _, card := range o.Cards {
		x := c.sessions[card.ID]
		if x == nil || cards[card.ID] != card {
			continue
		}
		delete(cards, card.ID) // each once
		if card.Name == "" && x.named {
			card.Name = x.name
		}
		card.Pinned = card.Pinned && !card.Parked
		if card.Space != "" && !hasSpace(card.Space) {
			card.Space = ""
		}
		if !hasGroup(card.Group) {
			card.Group = ""
		}
		t.Cards = append(t.Cards, card)
		r.Cards++
	}
	restored := len(t.Cards)
	for _, card := range rest {
		if card.Space == t.Spaces[0].ID || !hasSpace(card.Space) {
			card.Space = ""
		}
		if g := card.Group; g != "" {
			for _, mine := range t.Cards[:restored] {
				if groupWithin(t.Groups, mine.Group, g) {
					if mine.Space != card.Space {
						card.Group = ""
					}
					break
				}
			}
		}
		t.Cards = append(t.Cards, card)
	}
	return t, kept, r
}

// applyOrg makes the organisation t, which restoreTarget made: its spaces
// and groups, made again with their ids should they be gone; then each of
// its cards' pin or park, space, group and name, and their order, in the
// places the open sessions take, the closed ones keeping theirs. A group
// of kept is numbered should another group beside it have its name. The
// caller holds c.mu.
func (c *Core) applyOrg(t ipc.ChainOrg, kept map[string]bool) {
	names := map[string]string{}
	for _, g := range c.groups {
		names[g.ID] = g.Name
	}
	c.spaces = slices.Clone(t.Spaces)
	if len(c.spaces) == 0 {
		c.spaces = []ipc.Space{firstSpace}
	}
	c.groups = slices.Clone(t.Groups)
	c.fixGroups()
	for _, card := range t.Cards {
		x := c.sessions[card.ID]
		if x == nil {
			continue
		}
		if card.Name != "" && (card.Name != x.name || !x.named) {
			if _, _, err := c.nameAs(x.id, cleanText(card.Name, maxName), true); err != nil {
				c.log.Warn("chain: restore", "session", x.id, "err", err)
			}
		}
		if x.track.pinned && !card.Pinned {
			c.setPinned(x.id, false)
		}
		if x.track.parked != card.Parked {
			c.setParked(x.id, card.Parked)
		}
		if card.Pinned && !x.track.pinned {
			c.setPinned(x.id, true)
		}
		c.putInSpace(x, card.Space)
		g := card.Group
		if c.groupIndex(g) < 0 {
			g = ""
		}
		c.setGroup(x, g, event.SourceUser)
	}
	for i, g := range c.groups {
		if kept[g.ID] {
			c.groups[i].Name = c.groupName(g.Name, g.ID, g.Parent, c.groupSpace(g.ID))
		}
	}
	full := c.fullOrder()
	var next []string
	for _, card := range t.Cards {
		if c.sessions[card.ID] != nil {
			next = append(next, card.ID)
		}
	}
	for _, id := range c.openOrder() {
		if !slices.Contains(next, id) {
			next = append(next, id)
		}
	}
	j := 0
	for i, id := range full {
		if c.sessions[id] != nil && j < len(next) {
			full[i] = next[j]
			j++
		}
	}
	c.setOrder(full)
	c.saveSpaces()
	c.saveGroups()
	c.tidyOrder()
	for _, g := range c.groups {
		if n, ok := names[g.ID]; ok && n != g.Name {
			c.renameInHistory(g.ID, g.Name)
		}
	}
}

// groupWithin reports whether group id is group anc or inside it, among
// groups, as within does for the groups the core has.
func groupWithin(groups []ipc.Group, id, anc string) bool {
	if anc == "" {
		return true
	}
	for range len(groups) + 1 {
		if id == "" {
			return false
		}
		if id == anc {
			return true
		}
		i := slices.IndexFunc(groups, func(g ipc.Group) bool { return g.ID == id })
		if i < 0 {
			return false
		}
		id = groups[i].Parent
	}
	return false
}

// cleanName is name on one line, its spaces as one, cut to max characters.
func cleanName(name string, max int) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(name); len(r) > max {
		name = strings.TrimSpace(string(r[:max]))
	}
	return name
}

// numberedName is name, or with a number after it, from 2, when taken says
// another has it, as spaces and groups are named.
func numberedName(name string, taken func(string) bool) string {
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !taken(n) {
			return n
		}
	}
}
