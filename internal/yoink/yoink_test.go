package yoink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// standin is testdata/yt-dlp, a stand-in for yt-dlp that answers by the
// link's words, with no network.
func standin(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "yt-dlp"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// engine runs the stand-in, which writes its arguments to the file it
// returns.
func engine(t *testing.T) (Engine, string) {
	t.Helper()
	args := filepath.Join(t.TempDir(), "args")
	return Engine{Argv: []string{standin(t)}, Env: append(os.Environ(), "YOINK_ARGS="+args)}, args
}

// until waits for the download id to come to a state other than running,
// and gives it.
func until(t *testing.T, s *Service, id string) ipc.YoinkDownload {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range s.List() {
			if d.ID == id && (d.State == ipc.YoinkDone || d.State == ipc.YoinkFailed || d.State == ipc.YoinkCanceled) {
				return d
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("download %s never finished: %+v", id, s.List())
	return ipc.YoinkDownload{}
}

// downloading waits for the download id to say how far it is.
func downloading(t *testing.T, s *Service, id string) ipc.YoinkDownload {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range s.List() {
			if d.ID == id && d.State == ipc.YoinkDownloading {
				return d
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("download %s never started: %+v", id, s.List())
	return ipc.YoinkDownload{}
}

// TestCheckURL: a link is http or https, with a site, on one line, with no
// name and password before the site; it comes back as given.
func TestCheckURL(t *testing.T) {
	for _, ok := range []string{
		"https://www.example.com/watch?v=abc123",
		"http://example.com/a.mp4",
		"HTTPS://EXAMPLE.COM/x",
		"  https://example.com/x  ",
		"<https://example.com/x>",
		"https://commons.wikimedia.org/wiki/File:Example.webm",
	} {
		got, err := CheckURL(ok)
		want := strings.Trim(strings.TrimSpace(ok), "<>")
		if err != nil || got != want {
			t.Errorf("CheckURL(%q) = %q, %v; want %q", ok, got, err, want)
		}
	}
	for in, why := range map[string]string{
		"":                                  "give a link",
		"example.com/x":                     "missing its https://",
		"ftp://example.com/x.mp4":           "a ftp: link",
		"file:///etc/passwd":                "a file: link",
		"javascript:alert(1)":               "a javascript: link",
		"https:///nohost":                   "names no site",
		"https://user:pass@example.com/x":   "name or password",
		"https://youtube.com@example.com/x": "name or password",
		"https://example.com/a b":           "no spaces",
		"https://example.com/a\nb":          "no spaces",
		"https://example.com/a\x00b":        "no spaces",
		"-https://example.com/x":            "isn't a link Yoink can read",
		"https://example.com/" + strings.Repeat("a", MaxURL): "characters long",
	} {
		if _, err := CheckURL(in); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("CheckURL(%q) = %v; want %q", in, err, why)
		}
	}
}

// TestParseGet: best, the sound as an mp3, or a height, in the words people
// use for each.
func TestParseGet(t *testing.T) {
	for in, want := range map[string]string{
		"": "best", "best": "best", "Video": "best", "audio": "audio", "MP3": "audio", "sound": "audio",
		"720": "720p", "720p": "720p", "1080P": "1080p", "4k": "2160p", "8K": "4320p", "144p": "144p",
	} {
		if got, err := ParseGet(in); err != nil || got != want {
			t.Errorf("ParseGet(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"720x", "99p", "10000p", "flac", "-1"} {
		if _, err := ParseGet(bad); err == nil || !strings.Contains(err.Error(), "a height such as 720p") {
			t.Errorf("ParseGet(%q) = %v", bad, err)
		}
	}
	if Height("720p") != 720 || Height("best") != 0 || Height("audio") != 0 || Label("audio") != "mp3" || Label("best") != "Best" || Label("720p") != "720p" {
		t.Error("Height or Label")
	}
}

// TestFormatsReadsWhatALinkOffers: a look gives the video's title, site,
// uploader and length, and its choices: the best, each height, H.264 with
// the best AAC sound for its size, and the sound alone as an mp3; a
// storyboard is no choice.
func TestFormatsReadsWhatALinkOffers(t *testing.T) {
	e, args := engine(t)
	s := &Service{Dir: t.TempDir()}
	info, err := s.Formats(e, "https://www.example.com/watch?v=abc123")
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "A clip, with sound" || info.Site != "YouTube" || info.Uploader != "Alice" || info.Duration != 100 ||
		info.URL != "https://www.example.com/watch?v=abc123" || info.Page != "https://www.example.com/watch?v=abc123" {
		t.Errorf("info = %+v", info)
	}
	want := []ipc.YoinkChoice{
		{Get: "best", Label: "Best (1080p)", Size: 37500000 + 1600000},
		{Get: "1080p", Label: "1080p", Size: 37500000 + 1600000},
		{Get: "720p", Label: "720p", Size: 18750000 + 1600000},
		{Get: "360p", Label: "360p", Size: 6250000},
		{Get: "audio", Label: "Audio only (mp3)", Size: 100 * mp3Rate},
	}
	if !slices.Equal(info.Choices, want) {
		t.Errorf("choices = %+v\nwant %+v", info.Choices, want)
	}
	got := argsOf(t, args)
	for _, w := range []string{"--ignore-config", "--dump-single-json", "--no-playlist", "--flat-playlist"} {
		if !slices.Contains(got, w) {
			t.Errorf("a look's arguments lack %s: %q", w, got)
		}
	}
	if n := len(got); n < 2 || got[n-2] != "--" || got[n-1] != "https://www.example.com/watch?v=abc123" {
		t.Errorf("the link isn't the last argument, after --: %q", got)
	}
}

// TestFormatsRefusesAPlaylist: Yoink takes one video at a time, and says
// how many a playlist holds.
func TestFormatsRefusesAPlaylist(t *testing.T) {
	e, _ := engine(t)
	s := &Service{Dir: t.TempDir()}
	_, err := s.Formats(e, "https://www.example.com/playlist?list=PL1")
	if err == nil || !strings.Contains(err.Error(), "a playlist of 12 videos") || !strings.Contains(err.Error(), "one video at a time") {
		t.Errorf("err = %v", err)
	}
	if _, err := s.Start(e, t.TempDir(), "https://www.example.com/playlist?list=PL1", ""); err == nil || !strings.Contains(err.Error(), "playlist") {
		t.Errorf("Start = %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("a refused link left a download: %+v", s.List())
	}
}

// TestFormatsSaysWhyALinkFails: yt-dlp's error in a sentence, without its
// tags and the cause in brackets; a site that wants a sign-in says Yoink
// signs in to no site yet.
func TestFormatsSaysWhyALinkFails(t *testing.T) {
	e, _ := engine(t)
	s := &Service{Dir: t.TempDir()}
	if _, err := s.Formats(e, "https://www.example.com/missing"); err == nil || err.Error() != "Unable to download webpage: HTTP Error 404: Not Found" {
		t.Errorf("missing: %v", err)
	}
	if _, err := s.Formats(e, "https://www.example.com/login/abc"); err == nil || !strings.Contains(err.Error(), "wants you signed in") {
		t.Errorf("login: %v", err)
	}
	if _, err := s.Formats(Engine{Argv: []string{filepath.Join(t.TempDir(), "none")}}, "https://example.com/x"); err == nil || !strings.Contains(err.Error(), "couldn't run yt-dlp") {
		t.Errorf("no yt-dlp: %v", err)
	}
	if _, err := s.Formats(e, "ftp://example.com/x"); err == nil || !strings.Contains(err.Error(), "http and https") {
		t.Errorf("ftp: %v", err)
	}
}

// TestTheChoicesOfAnUprightVideo: a phone's upright video is named by its
// smaller side, as yt-dlp sorts it, and a link with no heights, such as a
// file's, still has the best and the sound.
func TestTheChoicesOfAnUprightVideo(t *testing.T) {
	if four := choices(info{Formats: []format{{VCodec: "vp9", ACodec: "opus", Width: 3840, Height: 2160}, {VCodec: "avc1", ACodec: "mp4a", Width: 1920, Height: 1080}}}); four[0].Label != "Best (4K)" || four[1].Label != "2160p (4K)" {
		t.Errorf("4K = %+v", four)
	}
	up := choices(info{Duration: 10, Formats: []format{
		{ID: "a", VCodec: "avc1", ACodec: "mp4a", Width: 1080, Height: 1920, Size: 5000},
		{ID: "b", VCodec: "avc1", ACodec: "mp4a", Width: 720, Height: 1280, Size: 3000},
	}})
	if len(up) != 4 || up[0].Label != "Best (1080p)" || up[1].Get != "1080p" || up[2].Get != "720p" || up[3].Get != "audio" || up[0].Size != 5000 {
		t.Errorf("upright = %+v", up)
	}
	file := choices(info{Formats: []format{{ID: "mp4", Ext: "mp4"}}})
	if len(file) != 2 || file[0].Get != "best" || file[0].Label != "Best" || file[1].Get != "audio" {
		t.Errorf("a file's = %+v", file)
	}
	sound := choices(info{Duration: 10, Formats: []format{{ID: "m", VCodec: "none", ACodec: "mp3", Size: 160000}}})
	if len(sound) != 1 || sound[0].Get != "audio" || sound[0].Size != 10*mp3Rate {
		t.Errorf("sound alone = %+v", sound)
	}
}

// TestProgressReadsYtDlpsLines: the lines yt-dlp 2026.10.26 printed for a
// video and its sound downloaded apart, then merged, as Yoink asks for
// them; the bytes count over both, and the step says which comes down.
func TestProgressReadsYtDlpsLines(t *testing.T) {
	var p progress
	d := ipc.YoinkDownload{Get: "best"}
	feed := func(lines ...string) ipc.YoinkDownload {
		for _, l := range lines {
			p.line(l)
		}
		p.show(&d)
		return d
	}
	if d := feed(); d.State != ipc.YoinkStarting || d.Step != "Starting" {
		t.Errorf("before anything: %+v", d)
	}
	d = feed(
		`yoink-plan [{"format_id": "240v", "filesize": 17280, "vcodec": "avc1.64000d", "acodec": "none"}, {"format_id": "aud", "filesize": 27634, "vcodec": "none", "acodec": "mp4a.40.2"}]`,
		`yoink-format {"format_id": "240v+aud", "filesize_approx": 44914, "vcodec": "avc1.64000d", "acodec": "mp4a.40.2", "ext": "mp4", "height": 240}`,
		`yoink-progress downloading 1024 17280 NA NA NA 240v`,
	)
	if d.State != ipc.YoinkDownloading || d.Step != "Downloading the video" || d.Done != 1024 || d.Total != 17280+27634 || d.Speed != 0 {
		t.Errorf("the video: %+v", d)
	}
	d = feed(`yoink-progress finished 17280 17280 NA 2088751.0625666445 NA 240v`, `yoink-progress downloading 1024 27634 NA 1000 NA aud`)
	if d.Step != "Downloading the sound" || d.Done != 17280+1024 || d.Total != 17280+27634 || d.Speed != 1000 || d.ETA != (27634-1024)/1000+1 {
		t.Errorf("the sound: %+v", d)
	}
	d = feed(`yoink-progress finished 27634 27634 NA 17070014.246833578 NA aud`, `yoink-step started Merger`)
	if d.State != ipc.YoinkProcessing || d.Step != "Merging the video and its sound" || d.Done != d.Total || d.Speed != 0 {
		t.Errorf("merging: %+v", d)
	}
	feed(`yoink-step finished Merger`, `yoink-step started MoveFiles`, `yoink-step finished MoveFiles`,
		`yoink-file "/home/alice/Downloads/Test_-_a_clip [abc123].mp4"`)
	if p.file != "/home/alice/Downloads/Test_-_a_clip [abc123].mp4" {
		t.Errorf("file = %q", p.file)
	}

	// One file, its size a guess, as a stream's is.
	var q progress
	for _, l := range []string{`yoink-plan []`, `yoink-format {"format_id": "mp4", "ext": "mp4"}`, `yoink-progress downloading 500 NA 2000.0 250 NA mp4`} {
		q.line(l)
	}
	q.show(&d)
	if d.Step != "Downloading" || d.Done != 500 || d.Total != 2000 || d.ETA != 7 {
		t.Errorf("one file: %+v", d)
	}
	// An mp3 in the making.
	q.line(`yoink-step started ExtractAudio`)
	d.Get = "audio"
	q.show(&d)
	if d.Step != "Making the mp3" {
		t.Errorf("mp3: %+v", d)
	}
}

// TestSiteName: yt-dlp's keys for its extractors, as people write them.
func TestSiteName(t *testing.T) {
	for key, want := range map[string]string{"WikimediaCommons": "Wikimedia Commons", "Youtube": "YouTube", "Vimeo": "Vimeo", "Generic": "", "TwitterBroadcast": "Twitter Broadcast", "BBCCoUk": "BBC Co Uk", "NBCNews": "NBC News", "CNN": "CNN", "TikTok": "TikTok"} {
		if got := siteName(key); got != want {
			t.Errorf("siteName(%q) = %q; want %q", key, got, want)
		}
	}
}

// TestErrorText: yt-dlp's and uvx's errors, said for a person.
func TestErrorText(t *testing.T) {
	for in, want := range map[string]string{
		"ERROR: [generic] nothing: Unable to download webpage: HTTP Error 404: File not found (caused by <HTTPError 404: File not found>)": "Unable to download webpage: HTTP Error 404: File not found.",
		"ERROR: [youtube] abc: Sign in to confirm you're not a bot. Use --cookies-from-browser":                                            "The site wants you signed in to give this video, and Yoink doesn't sign in to sites yet.",
		"ERROR: Unsupported URL: https://example.com/; please report this issue on  https://github.com/yt-dlp/yt-dlp/issues":               "Unsupported URL: https://example.com/.",
		"error: Failed to fetch: `https://pypi.org/simple/yt-dlp/`":                                                                        "Couldn't run yt-dlp through uvx: Failed to fetch: `https://pypi.org/simple/yt-dlp/`.",
		"yt-dlp: error: no such option: --progress-delta":                                                                                  "This yt-dlp is too old for Yoink: it doesn't know --progress-delta. Update it, or leave yoink.ytdlp empty to run the release Yoink pins.",
		"ERROR: [x] y: odd ‮evil\x1b[31m text":                                                                                             "Odd evil[31m text.",
	} {
		if got := errorText(in); got != want {
			t.Errorf("errorText(%q) =\n %q\nwant\n %q", in, got, want)
		}
	}
}

// TestADownloadLandsInTheFolder: a download goes from starting through
// its progress to done, with its file in the folder under yt-dlp's
// restricted name; the parts and logs it wrote go, and its record stays.
func TestADownloadLandsInTheFolder(t *testing.T) {
	e, args := engine(t)
	e.FFmpeg = "/opt/ffmpeg/bin"
	folder := t.TempDir()
	s := &Service{Dir: t.TempDir()}
	d, err := s.Start(e, folder, "https://www.example.com/watch?v=ok", "720p")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "A clip, with sound" || d.Site != "YouTube" || d.Get != "720p" || d.Label != "720p" || d.ID == "" {
		t.Errorf("started = %+v", d)
	}
	d = until(t, s, d.ID)
	file := filepath.Join(folder, "Clip [abc123].mp4")
	if d.State != ipc.YoinkDone || d.File != file || d.Done != 6 || d.Total != 6 || d.Step != "Done" || d.Ended.IsZero() {
		t.Errorf("done = %+v", d)
	}
	if _, err := os.Stat(file); err != nil {
		t.Error(err)
	}
	dir := filepath.Join(s.Dir, d.ID)
	for _, gone := range []string{partsDir, outFile, errFile} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s stayed: %v", gone, err)
		}
	}
	var r record
	if data, err := os.ReadFile(filepath.Join(dir, recordFile)); err != nil || json.Unmarshal(data, &r) != nil || r.State != ipc.YoinkDone || r.File != file {
		t.Errorf("record = %+v, %v", r, err)
	}
	got := argsOf(t, args)
	for _, w := range []string{"--ignore-config", "--no-playlist", "--restrict-filenames", "--newline", "--progress", template,
		"home:" + folder, "temp:" + filepath.Join(dir, partsDir), "res:720,+codec:avc:m4a", "/opt/ffmpeg/bin"} {
		if !slices.Contains(got, w) {
			t.Errorf("the download's arguments lack %q: %q", w, got)
		}
	}
}

// TestTheSoundAlone: audio asks yt-dlp for an mp3 of the best sound.
func TestTheSoundAlone(t *testing.T) {
	e, args := engine(t)
	folder := t.TempDir()
	s := &Service{Dir: t.TempDir()}
	d, err := s.Start(e, folder, "https://www.example.com/watch?v=ok", "mp3")
	if err != nil {
		t.Fatal(err)
	}
	if d = until(t, s, d.ID); d.State != ipc.YoinkDone || d.File != filepath.Join(folder, "Clip [abc123].mp3") || d.Label != "mp3" {
		t.Errorf("done = %+v", d)
	}
	got := strings.Join(argsOf(t, args), " ")
	if !strings.Contains(got, "-f ba[acodec^=mp3]/ba/b -x --audio-format mp3") {
		t.Errorf("arguments = %s", got)
	}
}

// TestAFailedDownloadSaysWhy: yt-dlp's error, in a sentence, and the parts
// it wrote go.
func TestAFailedDownloadSaysWhy(t *testing.T) {
	e, _ := engine(t)
	folder := t.TempDir()
	s := &Service{Dir: t.TempDir()}
	d, err := s.Start(e, folder, "https://www.example.com/fail", "")
	if err != nil {
		t.Fatal(err)
	}
	d = until(t, s, d.ID)
	if d.State != ipc.YoinkFailed || d.Error != "Unable to download video data: HTTP Error 403: Forbidden." || d.File != "" {
		t.Errorf("failed = %+v", d)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, d.ID, partsDir)); !os.IsNotExist(err) {
		t.Errorf("its parts stayed: %v", err)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Errorf("the folder has %v", entries)
	}
}

// TestCancelStopsADownload: a cancel ends its whole process group and
// removes what it wrote; the folder gets nothing, and a second cancel says
// it is over.
func TestCancelStopsADownload(t *testing.T) {
	e, _ := engine(t)
	folder := t.TempDir()
	s := &Service{Dir: t.TempDir()}
	d, err := s.Start(e, folder, "https://www.example.com/slow", "")
	if err != nil {
		t.Fatal(err)
	}
	if d = downloading(t, s, d.ID); d.Done != 300 || d.Total != 1000 || d.Step != "Downloading the video" {
		t.Errorf("downloading = %+v", d)
	}
	s.mu.Lock()
	pid, start := s.jobs[d.ID].PID, s.jobs[d.ID].PStart
	s.mu.Unlock()
	if !alive(pid, start) {
		t.Fatal("the download isn't running")
	}
	c, err := s.Cancel(d.ID)
	if err != nil || c.State != ipc.YoinkCanceled {
		t.Fatalf("Cancel = %+v, %v", c, err)
	}
	if alive(pid, start) {
		t.Error("the download still runs")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, d.ID, partsDir)); !os.IsNotExist(err) {
		t.Errorf("its parts stayed: %v", err)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Errorf("the folder has %v", entries)
	}
	if _, err := s.Cancel(d.ID); err == nil || !strings.Contains(err.Error(), "canceled already") {
		t.Errorf("a second cancel = %v", err)
	}
	if _, err := s.Cancel("00000000"); err == nil || !strings.Contains(err.Error(), "no download") {
		t.Errorf("no such download = %v", err)
	}
	// Clear forgets it, and only finished ones.
	if n, err := s.Clear(""); n != 1 || err != nil || len(s.List()) != 0 {
		t.Errorf("Clear = %d, %v: %+v", n, err, s.List())
	}
}

// TestTheLinkIsOneArgument: a link reaches yt-dlp whole, as its last
// argument after --, and nothing in it runs.
func TestTheLinkIsOneArgument(t *testing.T) {
	e, args := engine(t)
	dir := t.TempDir()
	link := "https://www.example.com/ok?a=$(touch%20" + dir + "/one)&b=`touch${IFS}" + dir + "/two`;touch$IFS" + dir + "/three|x&&y"
	s := &Service{Dir: t.TempDir()}
	d, err := s.Start(e, t.TempDir(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	until(t, s, d.ID)
	got := argsOf(t, args)
	if n := len(got); got[n-2] != "--" || got[n-1] != link {
		t.Errorf("the link came as %q", got[len(got)-2:])
	}
	for _, name := range []string{"one", "two", "three"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("the link ran a command: %s is there", name)
		}
	}
}

// TestDownloadsAreCapped: three run at once, and a fourth is refused until
// one ends.
func TestDownloadsAreCapped(t *testing.T) {
	e, _ := engine(t)
	s := &Service{Dir: t.TempDir()}
	var ids []string
	for range MaxRunning {
		d, err := s.Start(e, t.TempDir(), "https://www.example.com/slow", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			s.Cancel(id)
		}
	})
	if _, err := s.Start(e, t.TempDir(), "https://www.example.com/watch?v=ok", ""); err == nil || !strings.Contains(err.Error(), "3 downloads run already") {
		t.Errorf("a fourth = %v", err)
	}
	if s.Running() != MaxRunning {
		t.Errorf("Running = %d", s.Running())
	}
	if _, err := s.Cancel(ids[0]); err != nil {
		t.Fatal(err)
	}
	d, err := s.Start(e, t.TempDir(), "https://www.example.com/watch?v=ok", "")
	if err != nil {
		t.Fatalf("after a cancel = %v", err)
	}
	until(t, s, d.ID)
}

// TestADownloadOutlivesItsCore: a download carries on after the core that
// started it is gone, and the next core lists it as it goes and finishes
// it from its folder; a cancel from the next core stops one the core
// before started; and one that ended while no core watched is finished from
// what it wrote.
func TestADownloadOutlivesItsCore(t *testing.T) {
	e, _ := engine(t)
	dir := t.TempDir()
	folder := t.TempDir()
	// The core before starts them and is gone: nothing of it waits for
	// them, so each stays a zombie of the test once it ends, as a core's
	// would be until launchd took it.
	before := &Service{Dir: dir}
	gone := func(url string) string {
		before.mu.Lock()
		defer before.mu.Unlock()
		j, cmd, err := before.launch(e, record{URL: url, Title: "A clip", Get: "best", Folder: folder})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		return j.ID
	}
	lag, slow := gone("https://www.example.com/lag"), gone("https://www.example.com/slow")
	after := &Service{Dir: dir}
	list := after.List()
	if len(list) != 2 {
		t.Fatalf("the core after lists %+v", list)
	}
	for _, d := range list {
		if d.State == ipc.YoinkDone || d.State == ipc.YoinkFailed || d.State == ipc.YoinkCanceled {
			t.Errorf("%s finished before it could: %+v", d.URL, d)
		}
	}
	if d := until(t, after, lag); d.State != ipc.YoinkDone || d.File != filepath.Join(folder, "Clip [abc123].mp4") {
		t.Errorf("lag = %+v", d)
	}
	downloading(t, after, slow)
	if c, err := after.Cancel(slow); err != nil || c.State != ipc.YoinkCanceled {
		t.Errorf("a cancel from the core after = %+v, %v", c, err)
	}
	// One that ended while no core watched.
	done := gone("https://www.example.com/watch?v=ok")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(folder, "Clip [abc123].mp4")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ok never made its file")
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := map[string]string{}
	last := &Service{Dir: dir}
	for _, d := range last.List() {
		if d.ID == done && d.State != ipc.YoinkDone {
			d = until(t, last, done)
		}
		got[d.ID] = d.State
	}
	if got[lag] != ipc.YoinkDone || got[slow] != ipc.YoinkCanceled || got[done] != ipc.YoinkDone {
		t.Errorf("the last core lists %v", got)
	}
}

// TestAFolderWithoutItsRecordGoes: what a core left before it could say
// what a folder holds is taken away; other folders are left alone.
func TestAFolderWithoutItsRecordGoes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"0123abcd", "notmine"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{Dir: dir}
	if len(s.List()) != 0 {
		t.Fatal(s.List())
	}
	if _, err := os.Stat(filepath.Join(dir, "0123abcd")); !os.IsNotExist(err) {
		t.Errorf("an empty download's folder stayed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notmine")); err != nil {
		t.Errorf("another folder went: %v", err)
	}
}

// TestLookFindsTheEngine: uvx runs the pinned yt-dlp with its extras, or
// yoink.ytdlp's program alone; ffmpeg comes from the PATH or Homebrew's
// places; what lacks says how to get it.
func TestLookFindsTheEngine(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	brew := filepath.Join(t.TempDir(), "brew")
	old := Places
	Places = func(string) []string { return []string{brew} }
	t.Cleanup(func() { Places = old })
	cfg := config.Default().Yoink

	s := Look(cfg, bin, home)
	if s.Folder != filepath.Join(home, "Downloads") || s.Version != config.YtDlpVersion {
		t.Errorf("setup = %+v", s)
	}
	lacks := s.Lacks()
	if !strings.Contains(lacks, "needs uvx, which runs yt-dlp "+config.YtDlpVersion) || !strings.Contains(lacks, "brew install uv") || !strings.Contains(lacks, "brew install ffmpeg") {
		t.Errorf("Lacks = %q", lacks)
	}
	if _, ok := s.Engine(nil); ok {
		t.Error("an engine with nothing")
	}

	tool(t, bin, "uvx")
	tool(t, brew, "ffmpeg")
	s = Look(cfg, bin, home)
	if _, ok := s.Engine(nil); ok || !strings.Contains(s.Lacks(), "needs ffprobe, which comes with ffmpeg") {
		t.Errorf("ffmpeg without ffprobe: %q", s.Lacks())
	}
	tool(t, bin, "ffprobe")
	s = Look(cfg, bin, home)
	e, ok := s.Engine([]string{"PATH=/usr/bin"})
	if !ok || s.Lacks() != "" || e.FFmpeg != "" {
		t.Errorf("ffmpeg and ffprobe apart: %+v, %v; lacks %q", e, ok, s.Lacks())
	}
	os.Remove(filepath.Join(bin, "ffprobe"))
	tool(t, brew, "ffprobe")
	s = Look(cfg, bin, home)
	e, ok = s.Engine([]string{"PATH=/usr/bin"})
	if !ok || s.Lacks() != "" || !slices.Equal(e.Argv, []string{filepath.Join(bin, "uvx"), "--from", "yt-dlp[default]==" + config.YtDlpVersion, "yt-dlp"}) || e.FFmpeg != brew {
		t.Errorf("engine = %+v, %v; lacks %q", e, ok, s.Lacks())
	}

	cfg.YtDlp, cfg.Folder = "~/tools/yt-dlp", "~/Videos"
	s = Look(cfg, bin, home)
	if !strings.Contains(s.Lacks(), "yoink.ytdlp names "+filepath.Join(home, "tools", "yt-dlp")) || s.Folder != filepath.Join(home, "Videos") {
		t.Errorf("a missing yt-dlp: %+v, %q", s, s.Lacks())
	}
	tool(t, filepath.Join(home, "tools"), "yt-dlp")
	s = Look(cfg, bin, home)
	if e, ok := s.Engine(nil); !ok || !slices.Equal(e.Argv, []string{filepath.Join(home, "tools", "yt-dlp")}) {
		t.Errorf("yoink.ytdlp's engine = %+v, %v", e, ok)
	}
}

// tool makes a program named name in dir.
func tool(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// argsOf are the stand-in's arguments, as it wrote them.
func argsOf(t *testing.T, file string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}
