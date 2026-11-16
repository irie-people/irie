package yoink

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

// Package is what uvx runs: yt-dlp with its default extras, as its own
// installation instructions give it, which carry what YouTube's player
// needs (yt-dlp-ejs) and the certificates, brotli and websockets it uses.
// uvx takes it at the release yoink.version names, as uv's guide to tools
// gives extras with a version: uvx --from 'yt-dlp[default]==VERSION'
// yt-dlp.
const Package = "yt-dlp[default]"

// Engine is how a download runs: yt-dlp's program with its first
// arguments, ffmpeg's place and the environment they run in.
type Engine struct {
	// Argv starts yt-dlp: uvx and the pinned release, as uvx --from
	// yt-dlp[default]==2026.10.26 yt-dlp, or a yt-dlp program alone.
	Argv []string
	// FFmpeg is the folder that holds ffmpeg and ffprobe, which yt-dlp is
	// told; "" = yt-dlp looks on its PATH.
	FFmpeg string
	// Env is the environment they run in; nil = this process's.
	Env []string
}

// Setup is what the yoink plugin finds on this computer for the config's
// engine, each path "" when it isn't there. Finding it reads only the file
// system: it runs nothing.
type Setup struct {
	Folder  string // where downloads land: yoink.folder, ~ expanded
	Version string // the yt-dlp release uvx runs
	Named   string // the yt-dlp program yoink.ytdlp names, ~ expanded; "" = uvx runs yt-dlp
	UVX     string // uvx, which runs the pinned yt-dlp
	YtDlp   string // the program Named names, when it is one
	FFmpeg  string // ffmpeg, which merges a video with its sound and makes mp3s
	FFprobe string // ffprobe, which comes with ffmpeg: yt-dlp reads a file's sound with it to make an mp3
	Deno    string // Deno, which yt-dlp runs YouTube's player in
}

// Places are where Yoink looks for a program after the PATH: the folders
// uv's installer, Homebrew and Deno's installer put theirs in. A test
// points them elsewhere.
var Places = func(home string) []string {
	return []string{
		filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin",
		filepath.Join(home, ".cargo", "bin"), filepath.Join(home, ".deno", "bin"),
	}
}

// Look finds what cfg's engine needs, on the PATH path and in Places.
func Look(cfg config.Yoink, path, home string) Setup {
	s := Setup{Folder: Folder(cfg, home), Version: cfg.Version}
	if cfg.YtDlp != "" {
		s.Named = repo.Expand(cfg.YtDlp, home)
		if isProgram(s.Named) {
			s.YtDlp = s.Named
		}
	} else {
		s.UVX = find("uvx", path, home)
	}
	s.FFmpeg = find("ffmpeg", path, home)
	s.FFprobe = find("ffprobe", path, home)
	s.Deno = find("deno", path, home)
	return s
}

// Folder is where cfg's downloads land: yoink.folder, ~ expanded, else
// ~/Downloads.
func Folder(cfg config.Yoink, home string) string {
	if cfg.Folder == "" {
		return filepath.Join(home, "Downloads")
	}
	return filepath.Clean(repo.Expand(cfg.Folder, home))
}

// Lacks says what the setup lacks, and how to get each, in sentences; ""
// when it has what a download needs. Deno is no need: yt-dlp downloads
// from every other site without it.
func (s Setup) Lacks() string {
	var out []string
	switch {
	case s.Named != "" && s.YtDlp == "":
		out = append(out, "yoink.ytdlp names "+s.Named+", which isn't a program here: install yt-dlp there, or set yoink.ytdlp to \"\" to run yt-dlp "+s.Version+" through uvx.")
	case s.Named == "" && s.UVX == "":
		out = append(out, "It needs uvx, which runs yt-dlp "+s.Version+": `brew install uv`, or uv's installer at https://docs.astral.sh/uv/.")
	}
	switch {
	case s.FFmpeg == "":
		out = append(out, "It needs ffmpeg, to merge a video with its sound and to make mp3s: `brew install ffmpeg`.")
	case s.FFprobe == "":
		out = append(out, "It needs ffprobe, which comes with ffmpeg and makes mp3s with it: `brew install ffmpeg` brings both.")
	}
	return strings.Join(out, " ")
}

// Engine is the engine the setup runs, with env; ok is false while it
// lacks something. yt-dlp is told ffmpeg's folder when ffprobe is there
// too, as it looks for both there; else it finds them on its PATH.
func (s Setup) Engine(env []string) (Engine, bool) {
	e := Engine{Env: env}
	if s.FFmpeg != "" && s.FFprobe != "" && filepath.Dir(s.FFmpeg) == filepath.Dir(s.FFprobe) {
		e.FFmpeg = filepath.Dir(s.FFmpeg)
	}
	switch {
	case s.Named != "":
		if s.YtDlp == "" {
			return e, false
		}
		e.Argv = []string{s.YtDlp}
	case s.UVX != "":
		e.Argv = []string{s.UVX, "--from", Package + "==" + s.Version, "yt-dlp"}
	default:
		return e, false
	}
	return e, s.FFmpeg != "" && s.FFprobe != ""
}

// find looks for a program by name on the PATH path, then in Places.
func find(name, path, home string) string {
	dirs := filepath.SplitList(path)
	for _, dir := range append(dirs, Places(home)...) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, name); isProgram(p) {
			return p
		}
	}
	return ""
}

// isProgram says whether p is a file that can run.
func isProgram(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// The first word of each line Yoink asks yt-dlp to print, so it can tell
// them from yt-dlp's own.
const (
	tagProgress = "yoink-progress" // a download's progress
	tagPost     = "yoink-step"     // a step after it: Merger, ExtractAudio, MoveFiles
	tagPlan     = "yoink-plan"     // the formats it downloads in turn, for a video and its sound apart
	tagFormat   = "yoink-format"   // the format it picked
	tagFile     = "yoink-file"     // the file, once in the folder
)

// lookArgs are yt-dlp's arguments to say what url offers, as JSON: one
// video, and of a playlist only its own facts, never each of its videos'.
// A config file of yt-dlp's own, which could add a command to run, is
// ignored.
func (e Engine) lookArgs(url string) []string {
	return append(slices.Clone(e.Argv),
		"--ignore-config", "--dump-single-json", "--no-playlist", "--flat-playlist",
		"--socket-timeout", "30", "--", url)
}

// template is the file's name in the folder: yt-dlp's own, its title cut
// to 150 bytes, with --restrict-filenames, which keeps each field's words
// to ASCII letters and digits and takes / out of it, so a title can't reach
// outside the folder.
const template = "%(title).150B [%(id)s].%(ext)s"

// downloadArgs are yt-dlp's arguments to download url into folder, as get
// says, writing its parts to temp until they are whole. Its progress, the
// formats it picks and the file it makes come as lines that start with
// Yoink's tags.
func (e Engine) downloadArgs(url, get, folder, temp string) []string {
	args := append(slices.Clone(e.Argv),
		"--ignore-config", "--no-playlist", "--no-simulate",
		"--newline", "--progress", "--progress-delta", "0.5",
		"--progress-template", "download:"+tagProgress+" %(progress.status)s %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s %(info.format_id)s",
		"--progress-template", "postprocess:"+tagPost+" %(progress.status)s %(progress.postprocessor)s",
		"--print", "video:"+tagPlan+" %(requested_formats.:.{format_id,filesize,filesize_approx,vcodec,acodec})j",
		"--print", "video:"+tagFormat+" %(.{format_id,filesize,filesize_approx,vcodec,acodec,ext,height})j",
		"--print", "after_move:"+tagFile+" %(filepath)j",
		"--restrict-filenames", "-o", template,
		"-P", "home:"+folder, "-P", "temp:"+temp,
		"--socket-timeout", "30",
	)
	switch h := Height(get); {
	case get == ipc.YoinkAudio:
		// As yt-dlp's own mp3 preset: an mp3 as it is, else the best sound.
		args = append(args, "-f", "ba[acodec^=mp3]/ba/b", "-x", "--audio-format", "mp3")
	default:
		// The tallest video, no taller than h when it is a height, with
		// H.264 and AAC where the site has them at that height, as a Mac
		// plays them; merged into an mp4 where they fit one, else an mkv.
		sort := "res,+codec:avc:m4a"
		if h > 0 {
			sort = "res:" + strconv.Itoa(h) + ",+codec:avc:m4a"
		}
		args = append(args, "-f", "bv*+ba/b", "-S", sort, "--merge-output-format", "mp4/mkv")
	}
	if e.FFmpeg != "" {
		args = append(args, "--ffmpeg-location", e.FFmpeg)
	}
	return append(args, "--", url)
}

// env is the environment yt-dlp runs with: e's, with Python writing each
// line as it comes, so progress shows as it is made.
func (e Engine) env() []string {
	env := e.Env
	if env == nil {
		env = os.Environ()
	}
	return append(slices.Clone(env), "PYTHONUNBUFFERED=1")
}
