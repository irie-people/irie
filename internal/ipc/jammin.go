package ipc

// OpJammin is the Jammin plugin's music (docs/proposals/jammin.md): what
// plays on each source, its controls, a source's playlists, a search of
// it, where it can play, and signing in to a source's account. Each source
// is a service: Spotify through an app of the person's own, and the Music
// app on this Mac. What a person listens to is theirs, and Spotify's
// Developer Policy forbids feeding its content to an AI model, so the core
// refuses every call from inside an agent, a look as much as a play. It
// answers a JamminResult.
const OpJammin = "jammin"

// What JamminArgs.Do asks for.
const (
	JamLook     = ""         // how each source stands, and what plays
	JamPlay     = "play"     // play, or resume; with URI, that item (Context, the list it plays within)
	JamPause    = "pause"    // pause
	JamToggle   = "toggle"   // play or pause, whichever it isn't doing
	JamNext     = "next"     // the next track
	JamPrevious = "previous" // the track before, or this one from its start, as the source's own button does
	JamSeek     = "seek"     // to Position
	JamVolume   = "volume"   // to Volume
	JamShuffle  = "shuffle"  // shuffle On or off
	JamRepeat   = "repeat"   // Repeat: off, track or all
	JamLists    = "lists"    // the source's playlists, into Lists
	JamSearch   = "search"   // the source's music that Query finds, into Found
	JamDevices  = "devices"  // where the source can play, into Devices
	JamMove     = "move"     // the playing to Device
	JamSignIn   = "link"     // sign in to the source's account, with ClientID: Link says where
	JamSignOut  = "unlink"   // sign out of the source's account; All forgets its Client ID too
	JamCancel   = "cancel"   // stop a sign-in under way, changing nothing else
	JamFolder   = "folder"   // the folder of music files to play, Folder: kept as jammin.folder
)

// The sources, by name.
const (
	JamSpotify = "spotify" // Spotify, through a Spotify app of the person's own, else the Spotify app on this Mac
	JamMusic   = "music"   // Apple Music: the Music app on this Mac
	JamFiles   = "files"   // music files in a folder on this computer, played by the core
)

// How a source reaches its music, in JamSource.Via.
const (
	JamViaAccount = "account" // the service's API, signed in to the person's account
	JamViaApp     = "app"     // the service's own app on this computer, through its scripting
)

// A source's states.
const (
	JamReady   = "ready"   // it can play, and Now says what plays
	JamNeeds   = "needs"   // it lacks something: Problem says what, and what fixes it
	JamLinking = "linking" // a sign-in is under way: Link says where
	JamError   = "error"   // the last look failed: Problem says why
	JamOff     = "off"     // its part is off
)

// What JamNow.Repeat takes.
const (
	JamRepeatOff   = "off"   // play on to the end and stop
	JamRepeatTrack = "track" // this track again and again
	JamRepeatAll   = "all"   // the list, from its start again
)

// What a source can do, in JamSource.Can.
const (
	JamCanPlay    = "play"    // play, pause and toggle
	JamCanSkip    = "skip"    // next and previous
	JamCanSeek    = "seek"    // seek
	JamCanVolume  = "volume"  // volume
	JamCanShuffle = "shuffle" // shuffle
	JamCanRepeat  = "repeat"  // repeat
	JamCanLists   = "lists"   // lists
	JamCanSearch  = "search"  // search
	JamCanDevices = "devices" // devices and move
)

// JamminArgs ask the jammin op for something; Do says what, of Source.
type JamminArgs struct {
	Do string `json:"do,omitempty"`
	// Source is the source to act on: spotify or music. "" = the one
	// playing, else jammin.source, else the first ready. A look reads
	// every source whatever it says.
	Source string `json:"source,omitempty"`
	// URI is the item to play: a Spotify URI such as spotify:playlist:ID,
	// or a playlist's or track's persistent ID in the Music app.
	URI string `json:"uri,omitempty"`
	// Context is the list a track of URI plays within, such as its
	// album, so the music goes on after it.
	Context string `json:"context,omitempty"`
	// Query is what to search for.
	Query string `json:"query,omitempty"`
	// Position is where to seek to, in milliseconds from the start.
	Position int `json:"position,omitempty"`
	// Volume is the volume to set, 0 to 100.
	Volume *int `json:"volume,omitempty"`
	// On is shuffle on or off.
	On *bool `json:"on,omitempty"`
	// Repeat is the repeat to set: off, track or all.
	Repeat string `json:"repeat,omitempty"`
	// Device is the device to move the playing to, by its ID.
	Device string `json:"device,omitempty"`
	// ClientID is the Client ID of the person's Spotify app, for link;
	// "" = jammin.spotify.client_id.
	ClientID string `json:"client_id,omitempty"`
	// Folder is the folder of music files, for folder: an absolute or ~
	// path.
	Folder string `json:"folder,omitempty"`
	// All, for unlink, forgets the Client ID too.
	All bool `json:"all,omitempty"`
}

// JamminResult is the jammin op's answer: always every source, and what
// the call asked for.
type JamminResult struct {
	Sources []JamSource `json:"sources"`
	// Source is the source the call was about: the one it acted on, or
	// for a look, the one a client shows first.
	Source string `json:"source,omitempty"`
	// Lists, Found and Devices are what lists, search and devices read.
	Lists   []JamItem   `json:"lists,omitempty"`
	Found   []JamItem   `json:"found,omitempty"`
	Devices []JamDevice `json:"devices,omitempty"`
	// Link is the sign-in under way, for link.
	Link *JamLink `json:"link,omitempty"`
	// Text is the answer in words, as :jammin says it.
	Text string `json:"text"`
}

// JamSource is one source and how it stands.
type JamSource struct {
	Name  string `json:"name"`  // spotify or music
	Title string `json:"title"` // Spotify, Apple Music
	State string `json:"state"` // ready, needs, linking, error or off
	// Problem is what it lacks, or why its last look failed, and what
	// fixes it.
	Problem string `json:"problem,omitempty"`
	// Account is whose account it reaches, as the service names them;
	// "" for an app on this computer.
	Account string `json:"account,omitempty"`
	// Can is what it can do now: play, skip, seek, volume, shuffle,
	// repeat, lists, search, devices.
	Can []string `json:"can,omitempty"`
	// Now is what plays, or played last; nil = nothing.
	Now *JamNow `json:"now,omitempty"`
	// Link is the sign-in under way, while State is linking.
	Link *JamLink `json:"link,omitempty"`
	// ClientID is the Client ID of the person's Spotify app, kept in
	// config.toml, so a client can sign in again without asking for it;
	// "" = none yet. It is no secret: PKCE has none.
	ClientID string `json:"client_id,omitempty"`
	// Via is how it reaches its music: account or app; "" for the files.
	Via string `json:"via,omitempty"`
	// Folder is the files' folder, as config.toml names it, ~ and all.
	Folder string `json:"folder,omitempty"`
}

// JamNow is what plays on a source.
type JamNow struct {
	Playing bool     `json:"playing"`
	Title   string   `json:"title"`
	Artists []string `json:"artists,omitempty"`
	Album   string   `json:"album,omitempty"`
	// Kind is what it is: track, episode, or ad.
	Kind string `json:"kind,omitempty"`
	// URI is the item's own name at its source, such as
	// spotify:track:ID, or the track's persistent ID in the Music app.
	URI string `json:"uri,omitempty"`
	// Link opens the item in the source's own app or site, as its
	// attribution asks; "" = none.
	Link string `json:"link,omitempty"`
	// Art is its artwork: an https address, or a file on this computer;
	// "" = none.
	Art string `json:"art,omitempty"`
	// Duration is how long it runs, and Position how far it is, both in
	// milliseconds; Position was read at At, in Unix milliseconds, so a
	// client draws its progress on from there while Playing.
	Duration int   `json:"duration,omitempty"`
	Position int   `json:"position,omitempty"`
	At       int64 `json:"at,omitempty"`
	// Volume is the volume, 0 to 100; nil = the source can't say.
	Volume  *int   `json:"volume,omitempty"`
	Shuffle bool   `json:"shuffle,omitempty"`
	Repeat  string `json:"repeat,omitempty"` // off, track or all
	// Device is where it plays; nil = this computer, or the source
	// can't say.
	Device *JamDevice `json:"device,omitempty"`
	// Context is the list it plays within, such as a playlist; nil =
	// none, or the source can't say.
	Context *JamItem `json:"context,omitempty"`
}

// JamItem is a track, album, playlist or episode at a source.
type JamItem struct {
	Kind  string `json:"kind"` // track, album, playlist, episode
	Title string `json:"title"`
	// By is who made it: a track's or album's artists, or a playlist's
	// owner.
	By string `json:"by,omitempty"`
	// URI is what JamminArgs.URI takes to play it.
	URI  string `json:"uri"`
	Link string `json:"link,omitempty"` // where it opens in the source's own app or site
	Art  string `json:"art,omitempty"`  // its artwork's address; "" = none
	// Tracks is how many tracks a list holds; 0 = unknown.
	Tracks int `json:"tracks,omitempty"`
	// Duration is how long a track runs, in milliseconds; 0 = unknown.
	Duration int `json:"duration,omitempty"`
	// Album is a track's album, for a search's tracks.
	Album string `json:"album,omitempty"`
	// Context is what JamminArgs.Context takes to play a track within the
	// list it comes from, such as its album, so the music goes on after
	// it; "" = none.
	Context string `json:"context,omitempty"`
}

// JamDevice is a place a source can play: a computer, a phone, a speaker.
type JamDevice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"` // as the source names it: Computer, Smartphone, Speaker
	Active bool   `json:"active,omitempty"`
	// Here says it is this computer.
	Here bool `json:"here,omitempty"`
	// Volume is its volume, 0 to 100; nil = it has none Irie can set.
	Volume *int `json:"volume,omitempty"`
	// Restricted says it takes no control from Irie.
	Restricted bool `json:"restricted,omitempty"`
}

// JamLink is a sign-in under way: the page to open, and until when the
// core listens for its answer.
type JamLink struct {
	Source string `json:"source"`
	// URL is the sign-in page to open: the Mac app shows it on a sheet,
	// and irie jammin offers to open it in the person's browser.
	URL string `json:"url"`
	// Redirect is the redirect URI the person's app must list, as they
	// type it in at the source's dashboard: without the port, which
	// changes at each sign-in.
	Redirect string `json:"redirect"`
	// Until is when the core stops listening, in Unix seconds.
	Until int64 `json:"until"`
}
