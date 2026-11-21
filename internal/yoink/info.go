package yoink

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/ipc"
)

// info is what Yoink reads of the JSON yt-dlp's --dump-single-json prints
// for a link. Every field is the site's word, so it is shown as text and
// never run.
type info struct {
	Type          string   `json:"_type"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Site          string   `json:"extractor_key"`
	Page          string   `json:"webpage_url"`
	Uploader      string   `json:"uploader"`
	Channel       string   `json:"channel"`
	Duration      float64  `json:"duration"`
	PlaylistCount int      `json:"playlist_count"`
	Entries       []any    `json:"entries"`
	Formats       []format `json:"formats"`
}

// format is one of a video's formats.
type format struct {
	ID     string  `json:"format_id"`
	Ext    string  `json:"ext"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	VCodec string  `json:"vcodec"`
	ACodec string  `json:"acodec"`
	Size   float64 `json:"filesize"`
	Approx float64 `json:"filesize_approx"`
	TBR    float64 `json:"tbr"`
	ABR    float64 `json:"abr"`
}

// hasVideo and hasSound say what a format holds: "none" is yt-dlp's word
// for nothing, and a format that says nothing of a codec may hold it.
func (f format) hasVideo() bool { return f.VCodec != "none" }
func (f format) hasSound() bool { return f.ACodec != "none" }

// res is a format's resolution as yt-dlp sorts by it: the smaller side,
// so a phone's upright 1080×1920 video is 1080p, as its wide one is.
func (f format) res() int {
	switch {
	case f.Width > 0 && f.Height > 0:
		return int(math.Min(f.Width, f.Height))
	default:
		return int(f.Height)
	}
}

// size is about how many bytes f comes to: what the site says, else its
// bitrate over the video's length; 0 = unknown.
func (f format) size(duration float64) int64 {
	switch {
	case f.Size > 0:
		return int64(f.Size)
	case f.Approx > 0:
		return int64(f.Approx)
	case f.TBR > 0 && duration > 0:
		return int64(f.TBR * 125 * duration) // kbit/s to bytes
	}
	return 0
}

// mp3Rate is about the bytes a second of the mp3s yt-dlp makes, at its
// default quality: LAME's -V 5, about 130 kbit/s.
const mp3Rate = 130 * 125

// parseInfo reads yt-dlp's JSON for the link url. A playlist is refused:
// Yoink takes one video at a time.
func parseInfo(url string, data []byte) (ipc.YoinkInfo, error) {
	var in info
	if err := json.Unmarshal(data, &in); err != nil {
		return ipc.YoinkInfo{}, errors.New("yt-dlp's answer doesn't read as JSON")
	}
	if in.Type == "playlist" || in.Type == "multi_video" {
		n := in.PlaylistCount
		if n == 0 {
			n = len(in.Entries)
		}
		what := "several videos"
		if n > 1 {
			what = strconv.Itoa(n) + " videos"
		}
		return ipc.YoinkInfo{}, fmt.Errorf("that link is a playlist of %s, and Yoink takes one video at a time: give the link of one of them", what)
	}
	out := ipc.YoinkInfo{
		URL: url, Page: text(in.Page, 2000), Title: text(in.Title, 300), Site: text(siteName(in.Site), 80),
		Uploader: text(cmp.Or(in.Uploader, in.Channel), 120), Choices: choices(in),
	}
	if in.Duration > 0 && !math.IsInf(in.Duration, 0) {
		out.Duration = in.Duration
	}
	if out.Title == "" {
		out.Title = cmp.Or(text(in.ID, 120), "Untitled video")
	}
	return out, nil
}

// choices are what a person can download of in: the best, each height,
// tallest first, then the sound alone as an mp3. A size is about what
// yt-dlp downloads for the choice: the best format at that height, H.264
// first as the download picks, with the best sound beside a video that has
// none.
func choices(in info) []ipc.YoinkChoice {
	var videos, sounds []format
	for _, f := range in.Formats {
		switch {
		case !f.hasVideo() && !f.hasSound():
			// A storyboard or a still: nothing to watch or hear.
		case f.hasVideo():
			videos = append(videos, f)
		default:
			sounds = append(sounds, f)
		}
	}
	sound, hasSound := bestSound(sounds)
	sizeAt := func(v format) int64 {
		n := v.size(in.Duration)
		if v.ACodec == "none" && hasSound {
			s := sound.size(in.Duration)
			if n == 0 || s == 0 {
				return 0
			}
			n += s
		}
		return n
	}
	byRes := map[int]format{}
	for _, v := range videos {
		r := v.res()
		if r <= 0 {
			continue
		}
		if old, ok := byRes[r]; !ok || better(v, old) {
			byRes[r] = v
		}
	}
	heights := make([]int, 0, len(byRes))
	for r := range byRes {
		heights = append(heights, r)
	}
	slices.Sort(heights)
	slices.Reverse(heights)
	var out []ipc.YoinkChoice
	best := ipc.YoinkChoice{Get: ipc.YoinkBest, Label: "Best"}
	switch {
	case len(heights) > 0:
		best.Label = "Best (" + shortRes(heights[0]) + ")"
		best.Size = sizeAt(byRes[heights[0]])
	case len(videos) > 0:
		top := videos[0]
		for _, v := range videos[1:] {
			if better(v, top) {
				top = v
			}
		}
		best.Size = sizeAt(top)
	}
	if len(videos) > 0 || len(sounds) == 0 {
		out = append(out, best)
	}
	// One height is the best already; past eight, the shortest go.
	if len(heights) > 1 {
		for _, r := range heights[:min(len(heights), 8)] {
			out = append(out, ipc.YoinkChoice{Get: strconv.Itoa(r) + "p", Label: resLabel(r), Size: sizeAt(byRes[r])})
		}
	}
	// The sound, when something here may have it.
	someSound := hasSound || slices.ContainsFunc(videos, format.hasSound)
	if someSound {
		c := ipc.YoinkChoice{Get: ipc.YoinkAudio, Label: "Audio only (mp3)"}
		switch {
		case in.Duration > 0:
			c.Size = int64(in.Duration * mp3Rate)
		case hasSound:
			c.Size = sound.size(0)
		}
		out = append(out, c)
	}
	return out
}

// better says whether a is the format yt-dlp's download would sooner take
// than b at the same height: H.264 first, as the download asks, then the
// higher bitrate, then the bigger file.
func better(a, b format) bool {
	if avcA, avcB := strings.HasPrefix(a.VCodec, "avc"), strings.HasPrefix(b.VCodec, "avc"); avcA != avcB {
		return avcA
	}
	if a.TBR != b.TBR {
		return a.TBR > b.TBR
	}
	return a.size(0) > b.size(0)
}

// bestSound is the sound the download would take beside a video: AAC
// first, as the download asks, then the higher bitrate.
func bestSound(sounds []format) (format, bool) {
	if len(sounds) == 0 {
		return format{}, false
	}
	best := sounds[0]
	for _, s := range sounds[1:] {
		aacS, aacB := strings.HasPrefix(s.ACodec, "mp4a"), strings.HasPrefix(best.ACodec, "mp4a")
		switch {
		case aacS != aacB:
			if aacS {
				best = s
			}
		case cmp.Or(s.ABR, s.TBR) > cmp.Or(best.ABR, best.TBR):
			best = s
		}
	}
	return best, true
}

// siteName is the site as people write it, from yt-dlp's key for its
// extractor: WikimediaCommons is Wikimedia Commons, Youtube YouTube. A page
// yt-dlp has no extractor of its own for, Generic, names none.
func siteName(key string) string {
	switch strings.ToLower(key) {
	case "generic", "":
		return ""
	case "youtube", "youtubetab", "youtubeclip":
		return "YouTube"
	case "tiktok":
		return "TikTok"
	case "twitter":
		return "X"
	case "soundcloud":
		return "SoundCloud"
	}
	// A word starts at a capital after a small letter, or at the last
	// capital of a run that a small letter follows: NBCNews, NBC News.
	rs := []rune(key)
	var b strings.Builder
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(rs[i-1]) || unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1])) {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// shortRes names a resolution in a word: 1080p, 4K.
func shortRes(r int) string {
	switch r {
	case 2160:
		return "4K"
	case 4320:
		return "8K"
	}
	return strconv.Itoa(r) + "p"
}

// resLabel names a resolution as people do: 1080p, 2160p (4K).
func resLabel(r int) string {
	s := strconv.Itoa(r) + "p"
	switch r {
	case 2160:
		s += " (4K)"
	case 4320:
		s += " (8K)"
	}
	return s
}

// text is a site's words as Yoink shows them: on one line, without
// control characters or the marks that reverse text, and at most n
// characters.
func text(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}
