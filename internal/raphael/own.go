package raphael

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// The person's own files (DESIGN §4.11): the Trash, Downloads, Pictures, a
// device's backup and local models. Raffi never clears them by himself.
// While the disk is short of the room he keeps for swap, he weighs them
// and says which are biggest, each with a fix where one tap is safe, which
// asks first, as every fix does: emptying the Trash, deleting a backup,
// deleting the downloads nothing has changed in 90 days. Pictures and
// models get advice alone: Photos keeps its own library, and Ollama its
// models' names.

// The kinds of fix for the person's own files.
const (
	fixEmptyTrash   = "emptytrash"   // the Trash is emptied
	fixDeleteBackup = "deletebackup" // a device's backup is deleted
	fixOldDownloads = "olddownloads" // the downloads untouched for oldDownloads are deleted
)

// oldDownloads is how long nothing in a download has changed before he
// offers to delete it.
const oldDownloads = 90 * 24 * time.Hour

// OwnInput is what the look at the person's own files needs.
type OwnInput struct {
	Home string
	Now  time.Time
	// Models is where Ollama keeps its models; "" for its default.
	Models string
}

// own is one of the person's own folders, as he weighs it.
type own struct {
	id, path string
	size     int64
}

// OwnFindings weighs the person's own biggest things, the biggest first.
// Downloads, Pictures and the backups are weighed only with Full Disk
// Access, so that weighing them never has macOS ask the person for it.
func OwnFindings(ctx context.Context, r Runner, in OwnInput) []Finding {
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	full := fullDiskAccess(in.Home)
	var places []own
	add := func(id, path string) {
		if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
			places = append(places, own{id: id, path: path})
		}
	}
	add("own:trash", filepath.Join(in.Home, ".Trash"))
	models := in.Models
	if models == "" {
		models = os.Getenv("OLLAMA_MODELS")
	}
	if models == "" {
		models = filepath.Join(in.Home, ".ollama", "models")
	}
	add("own:models", models)
	if full {
		add("own:downloads", filepath.Join(in.Home, "Downloads"))
		add("own:pictures", filepath.Join(in.Home, "Pictures"))
		backups := backupsDir(in.Home)
		names, _ := readNames(backups)
		slices.Sort(names)
		for _, name := range names {
			add("own:backup:"+name, filepath.Join(backups, name))
		}
	}
	paths := make([]string, len(places))
	for i, p := range places {
		paths[i] = p.path
	}
	sizes := duSizes(ctx, r, paths)
	var fs []Finding
	for _, p := range places {
		p.size = sizes[p.path]
		if p.size < cacheFloor {
			continue
		}
		var f Finding
		switch {
		case p.id == "own:trash":
			f = trashFinding(p, in)
		case p.id == "own:models":
			f = Finding{Text: fmt.Sprintf("Local models take %s (%s).", Size(p.size), tildePath(p.path, in.Home)),
				Advice: "Raffi never deletes them himself. `ollama list` names them and `ollama rm NAME` deletes one; or move them to another drive and point OLLAMA_MODELS there."}
		case p.id == "own:downloads":
			f = downloadsFinding(p, in)
		case p.id == "own:pictures":
			f = Finding{Text: fmt.Sprintf("Pictures takes %s.", Size(p.size)),
				Advice: "Raffi never clears your pictures. If Photos keeps them in iCloud, Photos › Settings › iCloud › Optimize Mac Storage keeps smaller copies here and the full ones there."}
		default:
			f = backupFinding(ctx, r, p, in)
		}
		f.ID, f.Area, f.Level, f.Size = p.id, AreaDisk, Note, p.size
		fs = append(fs, f)
	}
	slices.SortStableFunc(fs, func(a, b Finding) int {
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		}
		return 0
	})
	return fs
}

// fullDiskAccess reports whether Irie may read the folders macOS keeps
// for an app the person allowed: only then can it read the TCC database,
// and only then does reading Downloads never ask them. Elsewhere there is
// nothing to allow.
func fullDiskAccess(home string) bool {
	if runtime.GOOS != "darwin" {
		return true
	}
	f, err := os.Open(filepath.Join(home, "Library", "Application Support", "com.apple.TCC", "TCC.db"))
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// backupsDir is where Finder keeps the backups of an iPhone or iPad.
func backupsDir(home string) string {
	return filepath.Join(home, "Library", "Application Support", "MobileSync", "Backup")
}

// trashFinding is the Trash, which a fix empties.
func trashFinding(p own, in OwnInput) Finding {
	return Finding{
		Text:   fmt.Sprintf("The Trash holds %s.", Size(p.size)),
		Advice: "Emptying it frees that room; what is in it can't be put back after.",
		Fix: &Fix{
			Label: "Empty the Trash",
			Run:   []string{command("rm", "-rf", tildePath(p.path, in.Home)) + "/*"},
			About: fmt.Sprintf("Deletes what is in the Trash for good, which frees about %s.", Size(p.size)),
			kind:  fixEmptyTrash, path: p.path,
		},
	}
}

// backupFinding is a device's backup, which a fix deletes. Its Info.plist
// says whose and from when, where it reads.
func backupFinding(ctx context.Context, r Runner, p own, in OwnInput) Finding {
	name, when := "a device", ""
	if m, err := readPlistFile(ctx, r, filepath.Join(p.path, "Info.plist")); err == nil {
		if n := plistString(m, "Device Name"); n != "" {
			name = n
		}
		if t, err := time.Parse(time.RFC3339, plistString(m, "Last Backup Date")); err == nil {
			when = t.Local().Format("2 Jan 2006")
		}
	}
	if when == "" {
		if fi, err := os.Stat(p.path); err == nil {
			when = fi.ModTime().Local().Format("2 Jan 2006")
		}
	}
	return Finding{
		Text:   fmt.Sprintf("The backup of %s from %s takes %s (%s).", name, when, Size(p.size), tildePath(filepath.Dir(p.path), in.Home)),
		Advice: "Finder makes a new one each time you back the device up to this Mac, and the device may back itself up to iCloud too.",
		Fix: &Fix{
			Label: "Delete this backup",
			Run:   []string{command("rm", "-rf", tildePath(p.path, in.Home))},
			About: fmt.Sprintf("Deletes the backup of %s from %s for good, which frees about %s. Back the device up again, to this Mac or to iCloud, to have one.", name, when, Size(p.size)),
			kind:  fixDeleteBackup, path: p.path,
		},
	}
}

// download is one of the top entries of Downloads.
type download struct {
	name string
	size int64
	last time.Time // when anything in it last changed
}

// downloadsFinding is Downloads, with a fix that deletes what nothing has
// changed in oldDownloads, when there is any.
func downloadsFinding(p own, in OwnInput) Finding {
	f := Finding{Text: fmt.Sprintf("Downloads takes %s.", Size(p.size))}
	cutoff := in.Now.Add(-oldDownloads)
	var old []download
	var total int64
	names, _ := readNames(p.path)
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		d := download{name: name, last: lastChange(filepath.Join(p.path, name)), size: treeSize(filepath.Join(p.path, name))}
		if !d.last.IsZero() && d.last.Before(cutoff) {
			old = append(old, d)
			total += d.size
		}
	}
	if len(old) == 0 {
		f.Advice = "Raffi never deletes your downloads by himself, and something in each has changed in the last 90 days."
		return f
	}
	slices.SortFunc(old, func(a, b download) int {
		switch {
		case a.size > b.size:
			return -1
		case a.size < b.size:
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	f.Text += fmt.Sprintf(" %s of it is in %s nothing has changed in 90 days.", Size(total), plural(len(old), "1 download", "%d downloads"))
	f.Advice = "Raffi never deletes your downloads by himself. The biggest of the old ones:"
	run := []string{}
	keep := make([]string, len(old))
	for i, d := range old {
		keep[i] = d.name
		if i < 3 {
			f.Evidence = append(f.Evidence, fmt.Sprintf("%s, %s, last changed %s", d.name, Size(d.size), d.last.Local().Format("2 Jan 2006")))
		}
		if i < 8 {
			run = append(run, command("rm", "-rf", tildePath(filepath.Join(p.path, d.name), in.Home)))
		}
	}
	if len(old) > 8 {
		run = append(run, fmt.Sprintf("… and %d more like them", len(old)-8))
	}
	f.Fix = &Fix{
		Label: "Delete downloads untouched for 90 days",
		Run:   run,
		About: fmt.Sprintf("Deletes %s nothing has changed in 90 days, for good, which frees about %s. One that changed since is kept.", plural(len(old), "the 1 download", "the %d downloads"), Size(total)),
		kind:  fixOldDownloads, path: p.path, start: cutoff.Unix(), names: keep,
	}
	return f
}

// lastChange is when path, or anything in it, last changed or came to be
// where it is: the latest of each one's modification and status change.
func lastChange(path string) time.Time {
	var last time.Time
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			if t := changed(fi); t.After(last) {
				last = t
			}
		}
		return nil
	})
	return last
}

// changed is the later of fi's modification and status change: a file
// moved into a folder keeps its modification time, and changes status.
func changed(fi fs.FileInfo) time.Time {
	t := fi.ModTime()
	if c, ok := statusChange(fi); ok && c.After(t) {
		t = c
	}
	return t
}

// doOwn runs a fix of the person's own files, after it checks again that
// it is still what they said OK to.
func doOwn(ctx context.Context, f Fix, in DoInput) (string, error) {
	switch f.kind {
	case fixEmptyTrash:
		return doEmptyTrash(f, in)
	case fixDeleteBackup:
		return doDeleteBackup(f, in)
	case fixOldDownloads:
		return doOldDownloads(f, in)
	}
	return "", errors.New("Raffi has no such fix")
}

// doEmptyTrash deletes what is in the Trash.
func doEmptyTrash(f Fix, in DoInput) (string, error) {
	trash := filepath.Join(in.Home, ".Trash")
	if f.path != trash || !realDir(trash, in.Home) {
		return "", fmt.Errorf("%s is not your Trash, so Raffi leaves it be", f.path)
	}
	names, err := readNames(trash)
	if err != nil {
		return "", err
	}
	var freed int64
	var errs []string
	for _, name := range names {
		p := filepath.Join(trash, name)
		size := treeSize(p)
		if err := os.RemoveAll(p); err != nil {
			errs = append(errs, name)
			size -= treeSize(p)
		}
		freed += size
	}
	if len(errs) > 0 {
		return fmt.Sprintf("Emptied the Trash but for %s, which couldn't be deleted (Finder's Empty Trash can): freed %s.", plural(len(errs), "1 item", "%d items"), Size(freed)), nil
	}
	return fmt.Sprintf("Emptied the Trash: freed %s.", Size(freed)), nil
}

// doDeleteBackup deletes a device's backup, once it is sure it is one.
func doDeleteBackup(f Fix, in DoInput) (string, error) {
	if filepath.Dir(f.path) != backupsDir(in.Home) || !realDir(f.path, in.Home) {
		return "", fmt.Errorf("%s is not a device's backup, so Raffi leaves it be", tildePath(f.path, in.Home))
	}
	if !exists(filepath.Join(f.path, "Info.plist")) && !exists(filepath.Join(f.path, "Manifest.db")) && !exists(filepath.Join(f.path, "Manifest.plist")) {
		return "", fmt.Errorf("%s doesn't look like a backup, so Raffi leaves it be", tildePath(f.path, in.Home))
	}
	size := treeSize(f.path)
	if err := os.RemoveAll(f.path); err != nil {
		return "", fmt.Errorf("couldn't delete all of it (%v): freed %s", err, Size(size-treeSize(f.path)))
	}
	return fmt.Sprintf("Deleted the backup in %s: freed %s.", tildePath(f.path, in.Home), Size(size)), nil
}

// doOldDownloads deletes the downloads the fix named that nothing has
// changed in since its look's cutoff.
func doOldDownloads(f Fix, in DoInput) (string, error) {
	dir := filepath.Join(in.Home, "Downloads")
	if f.path != dir || !realDir(dir, in.Home) || f.start == 0 {
		return "", fmt.Errorf("%s is not your Downloads, so Raffi leaves it be", f.path)
	}
	cutoff := time.Unix(f.start, 0)
	var freed int64
	var n, kept int
	for _, name := range f.names {
		p := filepath.Join(dir, name)
		if name == "" || strings.ContainsRune(name, filepath.Separator) || name == "." || name == ".." {
			continue
		}
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if last := lastChange(p); last.IsZero() || !last.Before(cutoff) {
			kept++
			continue
		}
		size := treeSize(p)
		if err := os.RemoveAll(p); err != nil {
			size -= treeSize(p)
		} else {
			n++
		}
		freed += size
	}
	said := fmt.Sprintf("Deleted %s untouched since %s: freed %s.", plural(n, "1 download", "%d downloads"), cutoff.Local().Format("2 Jan"), Size(freed))
	if kept > 0 {
		said += fmt.Sprintf(" %s had changed since, so he kept %s.", plural(kept, "1", "%d"), plural(kept, "it", "them"))
	}
	return said, nil
}

// OwnName is a short name for one of the person's own things, as a line
// that names several says it: "the Trash", "a device's backup".
func OwnName(f Finding) string {
	switch {
	case f.ID == "own:trash":
		return "the Trash"
	case f.ID == "own:downloads":
		return "Downloads"
	case f.ID == "own:pictures":
		return "Pictures"
	case f.ID == "own:models":
		return "local models"
	case strings.HasPrefix(f.ID, "own:backup:"):
		return "a device's backup"
	}
	return f.ID
}
