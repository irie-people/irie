package raphael

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// duRunner answers du with the sizes it is given, in KB by each folder's
// name, for any list of folders, and the rest as fakeRunner does.
type duRunner struct {
	fakeRunner
	kb map[string]int64
}

func (d *duRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if name != "/usr/bin/du" {
		return d.fakeRunner.Run(ctx, name, args...)
	}
	var out strings.Builder
	for _, p := range args {
		if strings.HasPrefix(p, "-") {
			continue
		}
		if kb, ok := d.kb[filepath.Base(p)]; ok {
			out.WriteString(strconv.FormatInt(kb, 10) + "\t" + p + "\n")
		}
	}
	return []byte(out.String()), nil, nil
}

// ownMac is a home with the person's own things: a Trash, Downloads with a
// download untouched for months and a new one, Pictures, a device's
// backup and local models, with Full Disk Access when full is set. The
// time is as far on as now says.
func ownMac(t *testing.T, full bool) (home string, now time.Time, r *duRunner) {
	t.Helper()
	home = t.TempDir()
	now = time.Now().Add(100 * 24 * time.Hour)
	blob(t, filepath.Join(home, ".Trash", "old.dmg"), time.Hour)
	blob(t, filepath.Join(home, "Downloads", "installer.iso"), 24*time.Hour)
	blob(t, filepath.Join(home, "Downloads", "fresh.zip"), -95*24*time.Hour) // changed after the look's cutoff
	blob(t, filepath.Join(home, "Pictures", "Photos Library.photoslibrary", "database"), time.Hour)
	backup := filepath.Join(backupsDir(home), "00008000-000A1B2C3D4E5F60")
	os.MkdirAll(backup, 0o700)
	os.WriteFile(filepath.Join(backup, "Info.plist"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>Device Name</key><string>Alice's iPhone</string>
<key>Last Backup Date</key><date>2025-05-28T10:00:00Z</date>
</dict></plist>`), 0o600)
	blob(t, filepath.Join(backup, "ab", "abcdef"), time.Hour)
	blob(t, filepath.Join(home, ".ollama", "models", "blobs", "sha256-1"), time.Hour)
	if full {
		blob(t, filepath.Join(home, "Library", "Application Support", "com.apple.TCC", "TCC.db"), time.Hour)
	}
	r = &duRunner{kb: map[string]int64{
		".Trash": 3 << 20, "Downloads": 16 << 20, "Pictures": 200 << 20, "00008000-000A1B2C3D4E5F60": 210 << 20, "models": 30 << 20,
	}}
	return home, now, r
}

func TestOwnFindingsNameTheBiggestAndAskBeforeAnyFix(t *testing.T) {
	home, now, r := ownMac(t, true)
	fs := OwnFindings(context.Background(), r, OwnInput{Home: home, Now: now})
	var ids []string
	byID := map[string]Finding{}
	for _, f := range fs {
		ids = append(ids, f.ID)
		byID[f.ID] = f
	}
	want := []string{"own:backup:00008000-000A1B2C3D4E5F60", "own:pictures", "own:models", "own:downloads", "own:trash"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("findings %v, want the biggest first %v", ids, want)
	}
	b := byID[want[0]]
	if b.Fix == nil || b.Fix.kind != fixDeleteBackup || !strings.Contains(b.Text, "Alice's iPhone") || !strings.Contains(b.Text, "28 May 2025") || !strings.Contains(b.Fix.About, "for good") {
		t.Errorf("the backup: %+v %+v", b, b.Fix)
	}
	if byID["own:pictures"].Fix != nil || byID["own:models"].Fix != nil {
		t.Error("pictures and models get a fix: they are Photos' and Ollama's to clear")
	}
	if tr := byID["own:trash"]; tr.Fix == nil || tr.Fix.kind != fixEmptyTrash || tr.Fix.Label != "Empty the Trash" {
		t.Errorf("the Trash: %+v", tr)
	}
	d := byID["own:downloads"]
	if d.Fix == nil || d.Fix.kind != fixOldDownloads || strings.Join(d.Fix.names, ",") != "installer.iso" || len(d.Evidence) != 1 || !strings.Contains(d.Evidence[0], "installer.iso") {
		t.Errorf("Downloads: %+v %+v", d, d.Fix)
	}
	for _, f := range fs {
		if f.Area != AreaDisk || f.Level != Note {
			t.Errorf("%s: %s %s", f.ID, f.Area, f.Level)
		}
	}
	if got := OwnName(b); got != "a device's backup" {
		t.Errorf("name %q", got)
	}
}

func TestOwnFindingsNeverMakeMacOSAskForAccess(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS asks for its protected folders")
	}
	home, now, r := ownMac(t, false)
	for _, f := range OwnFindings(context.Background(), r, OwnInput{Home: home, Now: now}) {
		if f.ID == "own:downloads" || f.ID == "own:pictures" || strings.HasPrefix(f.ID, "own:backup:") {
			t.Errorf("weighed %s without Full Disk Access", f.ID)
		}
	}
}

func TestTheFixesOfTheOwnersFilesCheckAgainFirst(t *testing.T) {
	home, now, r := ownMac(t, true)
	fs := OwnFindings(context.Background(), r, OwnInput{Home: home, Now: now})
	fix := func(id string) Fix {
		for _, f := range fs {
			if f.ID == id && f.Fix != nil {
				return *f.Fix
			}
		}
		t.Fatalf("no fix for %s", id)
		return Fix{}
	}
	in := DoInput{Home: home}
	ctx := context.Background()
	// A download changed since the look is kept; the rest goes.
	dl := fix("own:downloads")
	dl.names = append(dl.names, "../Documents", "fresh.zip")
	os.MkdirAll(filepath.Join(home, "Documents"), 0o700)
	said, err := Do(ctx, r, dl, in)
	if err != nil || exists(filepath.Join(home, "Downloads", "installer.iso")) || !exists(filepath.Join(home, "Downloads", "fresh.zip")) || !exists(filepath.Join(home, "Documents")) {
		t.Errorf("downloads: %q %v", said, err)
	}
	if !strings.Contains(said, "Deleted 1 download") || !strings.Contains(said, "kept it") {
		t.Errorf("said %q", said)
	}
	// The backup goes only from where backups are, and only a backup.
	bk := fix("own:backup:00008000-000A1B2C3D4E5F60")
	notBackup := bk
	notBackup.path = filepath.Join(home, "Pictures")
	if _, err := Do(ctx, r, notBackup, in); err == nil || !exists(filepath.Join(home, "Pictures")) {
		t.Errorf("deleted a folder that is no backup: %v", err)
	}
	if said, err := Do(ctx, r, bk, in); err != nil || exists(bk.path) || !strings.Contains(said, "freed") {
		t.Errorf("the backup: %q %v", said, err)
	}
	// The Trash is emptied, and nothing but the Trash.
	tr := fix("own:trash")
	other := tr
	other.path = filepath.Join(home, "Downloads")
	if _, err := Do(ctx, r, other, in); err == nil || !exists(filepath.Join(home, "Downloads", "fresh.zip")) {
		t.Errorf("emptied another folder: %v", err)
	}
	if said, err := Do(ctx, r, tr, in); err != nil || exists(filepath.Join(home, ".Trash", "old.dmg")) || !exists(filepath.Join(home, ".Trash")) || !strings.HasPrefix(said, "Emptied the Trash") {
		t.Errorf("the Trash: %q %v", said, err)
	}
	// An unknown kind of fix does nothing.
	if _, err := Do(ctx, r, Fix{kind: "nothing"}, in); err == nil || !strings.Contains(err.Error(), "no such fix") {
		t.Errorf("unknown: %v", err)
	}
}
