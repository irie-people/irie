package yoink

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// progress is how far a download is, read from the lines yt-dlp printed:
// those that start with Yoink's tags, and its errors. A video and its sound
// come apart on many sites, so a download can be two in turn, each with its
// own bytes; the plan says which, and how big each is.
type progress struct {
	plan    []part           // the formats it downloads in turn; one or none for a single file
	passes  map[string]*pass // each format's bytes, by its id
	current string           // the format it downloads now
	speed   float64          // bytes a second, now
	eta     int              // seconds left of the format now
	post    string           // the step after the download that runs now, such as Merger; "" = none
	file    string           // the file, once in the folder
	err     string           // the last error yt-dlp, or uvx, said
	other   string           // the last line that was neither progress nor an error
}

// part is one format a download takes in turn.
type part struct {
	ID     string  `json:"format_id"`
	Size   float64 `json:"filesize"`
	Approx float64 `json:"filesize_approx"`
	VCodec string  `json:"vcodec"`
	ACodec string  `json:"acodec"`
}

// pass is one format's bytes so far.
type pass struct {
	done, total int64
}

// line takes one line yt-dlp printed, on its output or its errors.
func (p *progress) line(s string) {
	s = strings.TrimRight(s, "\r")
	tag, rest, _ := strings.Cut(s, " ")
	switch tag {
	case tagProgress:
		p.progressLine(rest)
	case tagPost:
		status, name, _ := strings.Cut(rest, " ")
		switch status {
		case "started", "processing":
			p.post = name
		case "finished":
			p.post = ""
		}
	case tagPlan:
		var plan []part
		if json.Unmarshal([]byte(rest), &plan) == nil && len(plan) > 1 {
			p.plan = plan
		}
	case tagFormat:
		var f part
		if json.Unmarshal([]byte(rest), &f) == nil && len(p.plan) == 0 {
			p.plan = []part{f}
		}
	case tagFile:
		var file string
		if json.Unmarshal([]byte(rest), &file) == nil {
			p.file = file
		}
	default:
		t := strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(t, "ERROR:"):
			p.err = t
		case strings.HasPrefix(t, "error:") || strings.Contains(t, ": error: "):
			// uvx's own, and a yt-dlp that doesn't know an option.
			p.err = t
		case t != "" && !strings.HasPrefix(t, "WARNING:"):
			p.other = t
		}
	}
}

// progressLine reads a download line: its status, the bytes so far, the
// size and the size's guess, the speed, the seconds left and the format.
func (p *progress) progressLine(rest string) {
	f := strings.SplitN(rest, " ", 7)
	if len(f) < 7 {
		return
	}
	id := f[6]
	if p.passes == nil {
		p.passes = map[string]*pass{}
	}
	x := p.passes[id]
	if x == nil {
		x = &pass{}
		p.passes[id] = x
	}
	p.current = id
	if n, ok := number(f[1]); ok {
		x.done = int64(n)
	}
	if n, ok := number(f[2]); ok && n > 0 {
		x.total = int64(n)
	} else if n, ok := number(f[3]); ok && n > 0 {
		x.total = int64(n)
	}
	if f[0] == "finished" && x.done > 0 {
		x.total = x.done
	}
	p.speed, p.eta = 0, 0
	if n, ok := number(f[4]); ok && n > 0 {
		p.speed = n
	}
	if n, ok := number(f[5]); ok && n > 0 {
		p.eta = int(n)
	}
}

// number reads one of yt-dlp's numbers: NA, and None, are none.
func number(s string) (float64, bool) {
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil && n >= 0
}

// started says whether yt-dlp has said anything of the download yet.
func (p *progress) started() bool { return len(p.passes) > 0 || len(p.plan) > 0 }

// show puts how far it is on d: its step in words and its bytes, over
// every format it takes in turn when their sizes are known, else the
// format's now.
func (p *progress) show(d *ipc.YoinkDownload) {
	d.Done, d.Total, d.Speed, d.ETA = 0, 0, 0, 0
	switch {
	case p.post != "":
		d.State, d.Step = ipc.YoinkProcessing, postStep(p.post, d.Get)
	case len(p.passes) > 0:
		d.State, d.Step = ipc.YoinkDownloading, p.downloadStep()
	default:
		d.State, d.Step = ipc.YoinkStarting, "Starting"
	}
	if x := p.passes[p.current]; x != nil {
		d.Done, d.Total = x.done, x.total
	}
	if len(p.plan) > 1 {
		var done, total int64
		known := true
		for _, f := range p.plan {
			size := int64(f.Size)
			if size == 0 {
				size = int64(f.Approx)
			}
			if x := p.passes[f.ID]; x != nil {
				done += x.done
				if x.total > 0 {
					size = x.total
				}
			}
			known = known && size > 0
			total += size
		}
		if known {
			d.Done, d.Total = done, total
		}
	}
	if d.State == ipc.YoinkProcessing && d.Total > 0 {
		d.Done = d.Total
	}
	if d.State == ipc.YoinkDownloading {
		d.Speed = p.speed
		if p.speed > 0 && d.Total > d.Done {
			d.ETA = int(float64(d.Total-d.Done)/p.speed) + 1
		} else {
			d.ETA = p.eta
		}
	}
}

// downloadStep says what comes down now: the video or its sound, when
// they come apart.
func (p *progress) downloadStep() string {
	if len(p.plan) > 1 {
		for _, f := range p.plan {
			if f.ID != p.current {
				continue
			}
			if f.VCodec != "" && f.VCodec != "none" {
				return "Downloading the video"
			}
			return "Downloading the sound"
		}
	}
	return "Downloading"
}

// postStep names a step after the download, by yt-dlp's name for it.
func postStep(name, get string) string {
	switch {
	case name == "Merger":
		return "Merging the video and its sound"
	case name == "ExtractAudio" || get == ipc.YoinkAudio && name == "FFmpegExtractAudio":
		return "Making the mp3"
	case name == "MoveFiles":
		return "Putting it in the folder"
	}
	return "Finishing"
}

// problem is why a download failed, in a sentence, from yt-dlp's last
// error; code is how it ended, -1 when no one saw it end.
func (p *progress) problem(code int) string {
	if p.err != "" {
		return errorText(p.err)
	}
	switch {
	case code > 0 && p.other != "":
		return errorText(p.other)
	case code > 0:
		return "yt-dlp stopped with exit code " + strconv.Itoa(code) + " before the file was whole."
	}
	return "It stopped before the file was whole: try it again."
}

// errorText makes an error yt-dlp printed a sentence for a person: without
// ERROR:, the site's tag and the video's id before it, the cause in
// brackets after it, and the request to report it. A site that wants a
// sign-in says so plainly: Yoink signs in to no site yet.
func errorText(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	for _, w := range []string{"--cookies", "sign in to confirm", "login required", "log in", "requires authentication", "private video", "members-only"} {
		if strings.Contains(lower, w) {
			return "The site wants you signed in to give this video, and Yoink doesn't sign in to sites yet."
		}
	}
	if _, opt, ok := strings.Cut(s, "no such option: "); ok {
		return "This yt-dlp is too old for Yoink: it doesn't know " + text(opt, 60) + ". Update it, or leave yoink.ytdlp empty to run the release Yoink pins."
	}
	uv := false
	switch {
	case strings.HasPrefix(s, "ERROR:"):
		s = strings.TrimSpace(strings.TrimPrefix(s, "ERROR:"))
	case strings.HasPrefix(s, "error:"):
		// uvx's own: it couldn't get or run the pinned yt-dlp.
		s, uv = strings.TrimSpace(strings.TrimPrefix(s, "error:")), true
	}
	// [generic] nothing: Unable to … → Unable to …
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "] "); i > 0 {
			s = s[i+2:]
			if j := strings.Index(s, ": "); j > 0 && !strings.ContainsAny(s[:j], " \t") {
				s = s[j+2:]
			}
		}
	}
	for _, cut := range []string{" (caused by ", "; please report this issue", ". Confirm you are on the latest version"} {
		if i := strings.Index(s, cut); i > 0 {
			s = s[:i]
		}
	}
	s = text(s, 300)
	if s == "" {
		return "yt-dlp stopped with an error it didn't name."
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' && !uv {
		r[0] -= 'a' - 'A'
	}
	if uv {
		r = append([]rune("Couldn't run yt-dlp through uvx: "), r...)
	}
	if last := r[len(r)-1]; !strings.ContainsRune(".!?…", last) {
		r = append(r, '.')
	}
	return string(r)
}
