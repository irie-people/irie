package ipc

import "time"

// OpYoink is the yoink plugin's downloads (DESIGN §4.17): what a link
// offers, a download of its video or its sound started, the downloads with
// how far each is, and one stopped. yt-dlp does the downloading, and the
// files land in yoink.folder, ~/Downloads unless it says otherwise. A link
// is the person's own word: the core refuses every call from inside an
// agent, a look as much as a start. It answers a YoinkResult.
const OpYoink = "yoink"

// What YoinkArgs.Do asks for.
const (
	YoinkList    = ""        // the downloads, newest first
	YoinkFormats = "formats" // what a link offers: its title, and the sizes to pick from
	YoinkStart   = "start"   // start a download of URL, as Get says
	YoinkCancel  = "cancel"  // stop download ID, and remove what it wrote so far
	YoinkClear   = "clear"   // forget download ID, or every finished one; the files stay
)

// What YoinkArgs.Get takes: best, audio, or a height such as 720p.
const (
	YoinkBest  = "best"  // the best video there is, with its sound
	YoinkAudio = "audio" // the sound alone, as an mp3
)

// A download's states.
const (
	YoinkStarting    = "starting"    // yt-dlp has started, and nothing has come yet
	YoinkDownloading = "downloading" // Done of Total bytes are here
	YoinkProcessing  = "processing"  // ffmpeg merges the video with its sound, or makes the mp3
	YoinkDone        = "done"        // File holds it
	YoinkFailed      = "failed"      // Error says why
	YoinkCanceled    = "canceled"    // stopped at the person's word; what it wrote is gone
)

// YoinkArgs ask the yoink op for something; Do says what.
type YoinkArgs struct {
	Do string `json:"do,omitempty"`
	// URL is the link to look at or download: http or https only.
	URL string `json:"url,omitempty"`
	// Get is what to download: best (the default), audio, or a height
	// such as 720p, the best video at most that tall.
	Get string `json:"get,omitempty"`
	// ID is the download to cancel or clear; clear without one forgets
	// every download that has finished.
	ID string `json:"id,omitempty"`
}

// YoinkResult is the yoink op's answer: always the downloads, and what the
// call asked for.
type YoinkResult struct {
	Downloads []YoinkDownload `json:"downloads"`
	// Folder is where downloads land: yoink.folder, ~ expanded.
	Folder string `json:"folder"`
	// Info is what a link offers, for formats.
	Info *YoinkInfo `json:"info,omitempty"`
	// Download is the download a start began or a cancel stopped.
	Download *YoinkDownload `json:"download,omitempty"`
	// Text is the answer in words, as :yoink says it.
	Text string `json:"text"`
}

// YoinkInfo is what a link offers: its video, and what of it can be
// downloaded.
type YoinkInfo struct {
	URL      string        `json:"url"`                // the link, as given
	Page     string        `json:"page,omitempty"`     // the video's page, as yt-dlp names it
	Title    string        `json:"title"`              // the video's title
	Site     string        `json:"site,omitempty"`     // where it is, as yt-dlp knows the site: Youtube, Vimeo, Generic
	Uploader string        `json:"uploader,omitempty"` // who put it there, when the site says
	Duration float64       `json:"duration,omitempty"` // how long it runs, in seconds; 0 = unknown
	Choices  []YoinkChoice `json:"choices"`            // best first, then each height, tallest first, then the sound
}

// YoinkChoice is one thing to download from a link.
type YoinkChoice struct {
	Get   string `json:"get"`   // what YoinkArgs.Get takes for it: best, audio, 720p
	Label string `json:"label"` // its name: Best (1080p), 720p, Audio only (mp3)
	// Size is about how many bytes it comes to, from what the site says
	// and how long the video runs; 0 = unknown.
	Size int64 `json:"size,omitempty"`
}

// YoinkDownload is one download and how far it is.
type YoinkDownload struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	Site  string `json:"site,omitempty"`
	Get   string `json:"get"`   // best, audio or a height such as 720p
	Label string `json:"label"` // what Get is in words: Best, 720p, mp3
	State string `json:"state"` // starting, downloading, processing, done, failed or canceled
	// Step is what it does now, in words: Downloading the video,
	// Merging the video and its sound, Making the mp3.
	Step string `json:"step,omitempty"`
	// Done of Total bytes are here; Total 0 = not known yet.
	Done  int64   `json:"done,omitempty"`
	Total int64   `json:"total,omitempty"`
	Speed float64 `json:"speed,omitempty"` // bytes a second, now; 0 = unknown
	ETA   int     `json:"eta,omitempty"`   // seconds left, about; 0 = unknown
	// File is the downloaded file, once done: in Folder, its name
	// yt-dlp's restricted one.
	File  string `json:"file,omitempty"`
	Error string `json:"error,omitempty"` // why it failed
	// Started and Ended say when; Ended is zero until it finishes.
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
}
