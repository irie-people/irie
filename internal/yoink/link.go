// Package yoink downloads a video, or its sound, from a link: the yoink
// plugin's engine (DESIGN §4.17). yt-dlp does the downloading, a release
// pinned in config.toml that uvx runs, or a yt-dlp program of the
// person's; ffmpeg merges a video with its sound and makes mp3s. Each runs
// as a program and its arguments, never through a shell, and a link
// reaches yt-dlp only as one argument, after --.
//
// A download runs in a process group of its own and writes what it says to
// files in its own folder, under the core's state, so it carries on while
// the core restarts, as it does at each install, and the core after picks
// it up from those files.
package yoink

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/ipc"
)

// MaxURL is the longest link Yoink takes, in bytes.
const MaxURL = 4096

// CheckURL says whether raw is a link Yoink takes, and gives it back as
// yt-dlp gets it: one link, http or https, with a site, and no name or
// password before the site, which can hide where a link goes. A link
// pasted in angle brackets, as mail quotes them, loses them.
func CheckURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	switch {
	case s == "":
		return "", errors.New("give a link to a video, such as https://commons.wikimedia.org/wiki/File:Example.webm")
	case len(s) > MaxURL:
		return "", fmt.Errorf("that link is %d characters long, and Yoink takes links of up to %d", len(s), MaxURL)
	case strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return "", errors.New("a link has no spaces or line breaks in it: give one link")
	}
	u, err := url.Parse(s)
	switch {
	case err != nil:
		return "", errors.New("that isn't a link Yoink can read: give one that starts with https://")
	case u.Scheme != "http" && u.Scheme != "https":
		return "", fmt.Errorf("Yoink takes http and https links only, and this one is %s", schemeOf(u.Scheme))
	case u.Hostname() == "":
		return "", errors.New("that link names no site: give one such as https://example.com/video")
	case u.User != nil:
		return "", errors.New("a link with a name or password before its site, as https://name@site/, can hide where it goes, so Yoink doesn't take one")
	}
	// The link as given, which yt-dlp reads itself: a link signed by its
	// site breaks when its escapes change.
	return s, nil
}

// schemeOf names a link's scheme in a sentence.
func schemeOf(scheme string) string {
	if scheme == "" {
		return "missing its https://"
	}
	return "a " + scheme + ": link"
}

// ParseGet reads what to download: best (also empty or video), audio (also
// mp3 or sound), or a height such as 720p (also 720, 4k or 8k).
func ParseGet(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "", ipc.YoinkBest, "video":
		return ipc.YoinkBest, nil
	case ipc.YoinkAudio, "mp3", "sound":
		return ipc.YoinkAudio, nil
	case "4k":
		return "2160p", nil
	case "8k":
		return "4320p", nil
	}
	n, err := strconv.Atoi(strings.TrimSuffix(v, "p"))
	if err != nil || n < 100 || n > 9999 {
		return "", fmt.Errorf("%q isn't a choice Yoink has: say best, audio for an mp3, or a height such as 720p", s)
	}
	return strconv.Itoa(n) + "p", nil
}

// Height is the most a choice's video may stand: 720 for 720p, and 0 for
// best and audio.
func Height(get string) int {
	n, err := strconv.Atoi(strings.TrimSuffix(get, "p"))
	if err != nil || !strings.HasSuffix(get, "p") {
		return 0
	}
	return n
}

// Label is a choice in words, as a download's row shows it.
func Label(get string) string {
	switch get {
	case ipc.YoinkBest:
		return "Best"
	case ipc.YoinkAudio:
		return "mp3"
	}
	return get
}
