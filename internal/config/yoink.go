package config

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/brand"
)

// The yoink plugin's settings (DESIGN §4.17) live here, so the config needs
// none of the plugin's packages: a build without it (go build -tags
// noyoink) still reads a config.toml that sets them.

// YtDlpVersion is the yt-dlp release the yoink plugin runs through uvx
// unless yoink.version says another. A release, never latest: Irie runs
// only a yt-dlp pinned to one.
const YtDlpVersion = "2026.10.26"

// Yoink is the yoink plugin: a video, or its sound, downloaded from a link.
type Yoink struct {
	// Folder is where downloads land; "" = ~/Downloads.
	Folder string `toml:"folder"`
	// Version is the yt-dlp release uvx runs, such as 2026.10.26.
	Version string `toml:"version"`
	// YtDlp is a yt-dlp program to run instead, by its path; "" = uvx
	// with Version.
	YtDlp string `toml:"ytdlp"`
}

// defaultYoink is the plugin's settings until config.toml says otherwise.
func defaultYoink() Yoink { return Yoink{Folder: "~/Downloads", Version: YtDlpVersion} }

// ytDlpRelease is a yt-dlp release's version, as PyPI and its releases
// name them: a date, and a build number for the nightly ones.
var ytDlpRelease = regexp.MustCompile(`^20[0-9]{2}\.[0-9]{1,2}\.[0-9]{1,2}(\.[0-9]{1,6})?$`)

func checkYoink(y Yoink, bad func(string, ...any)) {
	if y.Folder != "" && !pathLike(y.Folder) {
		bad("yoink.folder = %q: must be an absolute or ~ path to the folder downloads land in, or empty for ~/Downloads", y.Folder)
	}
	if !ytDlpRelease.MatchString(y.Version) {
		bad("yoink.version = %q: must be a yt-dlp release, such as %s: %s runs only a yt-dlp pinned to one", y.Version, YtDlpVersion, brand.Name)
	}
	if y.YtDlp != "" && (!pathLike(y.YtDlp) || strings.ContainsFunc(y.YtDlp, unicode.IsControl)) {
		bad("yoink.ytdlp = %q: must be an absolute or ~ path to a yt-dlp program, or empty for uvx with yoink.version", y.YtDlp)
	}
}
