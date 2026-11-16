//go:build !noyoink

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
)

// yoinkFake is a core whose yoink op downloads one video in three looks:
// downloading, merging, done; or fails one whose link says fail.
type yoinkFake struct {
	mu    sync.Mutex
	looks int
	asked []ipc.YoinkArgs
	fail  bool
}

func (f *yoinkFake) Call(op string, raw json.RawMessage) (any, error) {
	if op != ipc.OpYoink {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "no %s here", op)
	}
	var a ipc.YoinkArgs
	json.Unmarshal(raw, &a)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, a)
	d := ipc.YoinkDownload{ID: "0a1b2c3d", URL: "https://example.com/v", Title: "A clip", Get: "best", Label: "Best", State: ipc.YoinkStarting, Step: "Starting"}
	switch a.Do {
	case ipc.YoinkStart:
		f.fail = strings.Contains(a.URL, "fail")
		return ipc.YoinkResult{Download: &d, Folder: "/home/alice/Downloads", Text: "Yoinking “A clip” into ~/Downloads. The Yoink tab shows how far it is."}, nil
	case ipc.YoinkFormats:
		return ipc.YoinkResult{Info: &ipc.YoinkInfo{Title: "A clip", Site: "Youtube", Duration: 125, Choices: []ipc.YoinkChoice{
			{Get: "best", Label: "Best (1080p)", Size: 39100000}, {Get: "720p", Label: "720p", Size: 20350000}, {Get: "audio", Label: "Audio only (mp3)"},
		}}}, nil
	}
	f.looks++
	switch {
	case f.fail:
		d.State, d.Error = ipc.YoinkFailed, "Unable to download video data: HTTP Error 403: Forbidden."
	case f.looks == 1:
		d.State, d.Step, d.Done, d.Total = ipc.YoinkDownloading, "Downloading the video", 300, 1000
	case f.looks == 2:
		d.State, d.Step = ipc.YoinkProcessing, "Merging the video and its sound"
	default:
		d.State, d.Step, d.File = ipc.YoinkDone, "Done", "/home/alice/Downloads/A_clip [abc].mp4"
	}
	return ipc.YoinkResult{Downloads: []ipc.YoinkDownload{d}}, nil
}

func (f *yoinkFake) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	return nil, nil, errors.New("no")
}

// TestIrieYoink: irie yoink starts a download through the core, says each
// step on standard error and prints the file's path alone on standard
// output; a failure is an error with yt-dlp's reason; formats lists the
// choices with their flags; and with the plugin off it says how to turn it
// on.
func TestIrieYoink(t *testing.T) {
	bare(t)
	run, err := os.MkdirTemp("/tmp", "yk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	t.Setenv("XDG_RUNTIME_DIR", run)
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(p.Config, 0o700)
	os.MkdirAll(p.Runtime, 0o700)
	yoink := func(args ...string) (string, string, error) {
		var out, errs bytes.Buffer
		err := yoinkCmd(args, &out, &errs)
		return out.String(), errs.String(), err
	}
	if _, _, err := yoink("https://example.com/v"); err == nil || !strings.Contains(err.Error(), "plugins on yoink") {
		t.Errorf("off: %v", err)
	}
	if err := os.WriteFile(p.ConfigFile(), []byte("[plugins]\nyoink = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := ipc.Listen(p.Socket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fake := &yoinkFake{}
	go ipc.Serve(l, fake)

	out, errs, err := yoink("example.com/v", "--res", "720")
	if err != nil || out != "/home/alice/Downloads/A_clip [abc].mp4\n" {
		t.Errorf("irie yoink: %v, stdout %q", err, out)
	}
	for _, want := range []string{"Yoinking “A clip” into /home/alice/Downloads.\n", "Downloading the video: 30% of 1 KB", "Merging the video and its sound"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}
	if a := fake.asked[0]; a.Do != ipc.YoinkStart || a.URL != "https://example.com/v" || a.Get != "720" {
		t.Errorf("asked %+v", a)
	}
	if _, _, err := yoink("--audio", "https://example.com/fail"); err == nil || err.Error() != "Unable to download video data: HTTP Error 403: Forbidden." {
		t.Errorf("a failure: %v", err)
	}
	if a := fake.asked[len(fake.asked)-2]; a.Get != "audio" {
		t.Errorf("--audio asked %+v", a)
	}
	out, _, err = yoink("formats", "https://example.com/v")
	if err != nil || !strings.Contains(out, "A clip (Youtube, 2:05)") || !strings.Contains(out, "about 20.4 MB") || !strings.Contains(out, "--res 720") || !strings.Contains(out, "--audio") {
		t.Errorf("formats: %v\n%s", err, out)
	}
	out, _, err = yoink("list")
	if err != nil || !strings.HasPrefix(out, "0a1b2c3d  “A clip”") {
		t.Errorf("list: %v\n%s", err, out)
	}
	if _, _, err := yoink("https://example.com/a", "https://example.com/b"); err == nil || !strings.Contains(err.Error(), "one link at a time") {
		t.Errorf("two links: %v", err)
	}
	if _, _, err := yoink("--bogus", "https://example.com/a"); err == nil || !strings.Contains(err.Error(), "unknown flag --bogus") {
		t.Errorf("a bogus flag: %v", err)
	}
	if out, _, err := yoink("help"); err != nil || !strings.Contains(out, "yoink formats LINK") {
		t.Errorf("help: %v\n%s", err, out)
	}
}
