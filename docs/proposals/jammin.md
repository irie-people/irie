# Proposal: Jammin, a jam box for your music beside your sessions

Date: 2026-11-21. Status: step 1 of §10 built: the plugin, with Spotify through an app of yours and Apple Music through the Music app, the core's `jammin` op, `irie jammin`, `:jammin` and the Mac app's Jammin tab. The owner asked for "an irie music plugin", made "so users can hook up apple music google spotify local whatever", and for their own Spotify login to be kept in Bitwarden for "a spotify integration plugin that goes into the jam box, thats what i want to call it jammin". There is no issue tracker yet, so this file stands in for the issue that [AGENTS.md](../../AGENTS.md) asks for. An agent (Claude Code) wrote this file and the change it describes. Every source is listed in [PROVENANCE.md](../../PROVENANCE.md).

## Goal

Your music beside your sessions, in one small tab: what plays and where, play and pause, skip, seek, volume and shuffle, your playlists and a search, whichever service it comes from. One plugin does it for every service, each service a source.

**In short.**

1. **One plugin, a source per service.** Spotify and Apple Music first. Files on this computer, YouTube Music and the rest come later, each as one more source (§7), with no change to the tab, the words or the rules.
2. **The core reads and controls; the clients only draw.** Tokens live in the login Keychain. What a source says about your music stays in memory, but for the artwork of the track playing in the Music app.
3. **One source at a time.** The tab shows one source's music at a time and never puts two sources' music in one list, queue or search, as Spotify's Developer Policy asks. Starting one source pauses another that plays on this computer, so two never play over each other.
4. **Agents can't reach it.** What a person listens to is theirs, and Spotify's Developer Policy forbids feeding Spotify content to an AI model. The core refuses the op to every process inside an agent's pane, a look as much as a play.

## 1. What it is

Irie's twelfth plugin, **Jammin** (DESIGN §4.17), is on by default: `[plugins] jammin`, and `irie plugins off jammin` turns it off. It does nothing on its own. It sends no word to an app and asks no service until a client asks, which the Mac app does only while its tab shows. `go build -tags nojammin` leaves it out. Its parts are kinds of source, as Mail's parts are kinds of account:

| Part | How Irie reaches it | Its check |
|---|---|---|
| Spotify | Your Spotify account, through a Spotify app of yours: its Client ID, from Spotify's developer dashboard, and the Web API, signed in with the Authorization Code flow with PKCE | ready once you have signed in and the core's last look didn't fail |
| Apple Music | The Music app on this Mac, through its scripting dictionary, with JavaScript for Automation | ready where the Music app is. macOS asks you once whether Irie may control it |

## 2. Sources

`internal/jammin` holds the plugin's service and one source per kind. Each source adds itself to the sources this build has, behind one interface:

```go
type Source interface {
	Look(ctx context.Context) (ipc.JamSource, error)              // how it stands, and what plays
	Do(ctx context.Context, a ipc.JamminArgs) error               // play, pause, next, previous, seek, volume, shuffle, repeat, move
	Lists(ctx context.Context) ([]ipc.JamItem, error)             // your playlists
	Search(ctx context.Context, words string) ([]ipc.JamItem, error)
	Devices(ctx context.Context) ([]ipc.JamDevice, error)         // where it can play
}
```

A source says what it can do (`can`), so the tab offers only that source's own controls. What each call is:

| Call | Spotify (Web API, `https://api.spotify.com/v1`) | Apple Music (the Music app) |
|---|---|---|
| Look | `GET /me/player` (204 when nothing plays), and `GET /me` once, for whose account it is | player state, current track, player position, sound volume, shuffle enabled and song repeat, and the current track's artwork, written to a file |
| Play, pause | `PUT /me/player/play` (with `context_uri` or `uris` for an item), `PUT /me/player/pause` | play (a playlist or a track by its persistent ID), pause, playpause |
| Next, previous | `POST /me/player/next`, `POST /me/player/previous` | next track, back track |
| Seek, volume | `PUT /me/player/seek`, `PUT /me/player/volume` | player position, sound volume |
| Shuffle, repeat | `PUT /me/player/shuffle`, `PUT /me/player/repeat` (off, track, context) | shuffle enabled, song repeat (off, one, all) |
| Lists | `GET /me/playlists` | the user playlists, without folders |
| Search | `GET /search`, tracks, albums and playlists, 10 of each, the most development mode allows | search the library playlist |
| Devices, move | `GET /me/player/devices`, `PUT /me/player` (transfer playback) | none yet. AirPlay devices come later |

## 3. Spotify

### An app of yours

Spotify's Web API answers an app by its Client ID. Irie carries none, and each person makes their own, as Chat's bots are each person's own. Since 11 February 2026 for new apps, and 9 March for every other, an app in development mode works only while its owner has Spotify Premium, takes at most five users, and reaches a smaller set of endpoints. Since 23 July a developer may have 25 such apps, and they share one quota. One Client ID shipped with Irie would be one app for everyone, capped at five people, and Spotify's extended quota is for registered businesses with 250,000 monthly users. So the person makes an app at `https://developer.spotify.com/dashboard`, which takes a minute:

1. **Create app**, with any name and description, such as Jammin and "my jam box in Irie".
2. **Redirect URI:** `http://127.0.0.1/jammin/spotify`, exactly. Spotify takes a loopback address only as an IP literal, never `localhost`, and lets the port change at each sign-in, so Irie listens on a free port each time.
3. **Which API:** Web API. Agree to the terms and save.
4. Copy the **Client ID** from the app's settings. It isn't a secret: with PKCE there is none.

`irie jammin link spotify`, or Link Spotify… in the Jammin tab, takes the Client ID and keeps it in config.toml (`jammin.spotify.client_id`), then signs in.

### Signing in

The core makes a code verifier, 64 random letters and digits, its S256 challenge and a random state, and listens on `127.0.0.1` at a free port for 10 minutes. The sign-in page is `https://accounts.spotify.com/authorize` with the Client ID, `response_type=code`, the redirect URI with that port, the challenge and the state, and these scopes, all the plugin uses and no more:

- `user-read-playback-state`, `user-modify-playback-state` and `user-read-currently-playing`, for what plays and its controls;
- `playlist-read-private` and `playlist-read-collaborative`, for your playlists.

Spotify sends the browser back to the redirect URI with a code and the state. The core checks the state, trades the code and the verifier for tokens at `https://accounts.spotify.com/api/token`, keeps the refresh token in the login Keychain (service "Irie jammin", account `spotify`), and answers the page with a line that says it is done. It keeps the access token, which lasts an hour, in memory only, and refreshes it with the refresh token and the Client ID, keeping a new refresh token when Spotify sends one. A sign-in that is refused, canceled (the sheet's Cancel sends `cancel`), or doesn't come back within the 10 minutes ends the listening and says why. The Spotify source carries its Client ID, which is no secret, so the sheet signs in again without asking for it.

`irie jammin unlink spotify` signs out: it removes the token from the Keychain and forgets what the core read. The Client ID stays in config.toml, so the next sign-in needs no new app; `--all` removes it too. You can also take the app's access back at `https://www.spotify.com/account/apps`.

### Bitwarden fills the sign-in

The Mac app signs in on a sheet of its own, whose web view the Bitwarden plugin fills as it fills a web pane: a login in your vault whose address is `accounts.spotify.com` fills the Spotify sign-in, with the same rules as a pane's (that host only, empty fields only, never a form that makes a new password, never sent for you), and the key's Fill fills it at your word. The sheet first shows Spotify's dashboard, where you make your app with your login filled the same way, and Spotify's own sign-in carries over to the second page. While Jammin's Spotify part is on, an unlock of the vault keeps the logins for `accounts.spotify.com` with the panes' logins. They go when the panes' logins go. The sheet's website data is its own and goes when it closes.

From a terminal, `irie jammin link spotify` prints the sign-in address and offers to open it in your own browser, where your browser's own password manager may fill it.

### What it reads, and how often

The tab asks the core while it shows, every 2 seconds, and draws the track's progress between asks itself. The core asks Spotify at most once every 4 seconds while something plays and every 15 seconds while nothing does, whoever asks, and once more half a second after a control, since Spotify applies a command a moment later. It keeps what it read in memory. Spotify's refusals come back in words:

- **403 with Premium required:** "Spotify lets an app control playback only for Premium accounts". Since March 2026 a development mode app's owner needs Premium for any of it.
- **404, no active device:** "Nothing plays on any device of yours: open Spotify on one, or pick one under Devices".
- **429:** the wait `Retry-After` names, up to a minute, then the call fails and says how long. A `QUOTA_EXCEEDED` reason says the developer account's quota is used up, which no wait fixes.
- **401** refreshes the token once, then says to sign in again.

A device is **this computer** when it is a computer whose name is this Mac's name. The Spotify app names its device after the computer it runs on.

### Spotify's rules the tab keeps

Spotify's Developer Terms, its Developer Policy and its design guidelines say how an app may show Spotify's content. The tab keeps them:

- **Attribution:** Spotify's full logo, never smaller than 70 points wide, beside its content, black on a light ground and white on a dark one, and a link back to the item in Spotify ("Play on Spotify", or "Get Spotify free" where Spotify isn't installed).
- **Artwork as Spotify gives it:** uncropped, unblurred, with no overlay and no controls over it, its corners rounded 4 points in the list and 8 in the large view.
- **Names as Spotify gives them**, cut short only where room ends, with the whole name in the item's tooltip.
- **Not with another service's content:** no list, queue or search holds two sources' music (Policy, prohibited applications: "integrated with streams or content from another service"), and Jammin never plays one source over another (the same list: no mixing or overlapping Spotify content with other audio).
- **No voice control, no AI and no business use:** Jammin takes no spoken commands, gives no agent Spotify's content, and is for the person's own listening.
- **Nothing kept:** only the refresh token is kept, in the Keychain. What Spotify sends, the artwork's addresses included, stays in memory and goes with the core, or at sign-out.

## 4. Apple Music

The core speaks to the Music app with `osascript -l JavaScript`, through scripts of its own that take their words as the run handler's arguments, so a search or a playlist's name never becomes part of a script. Each script answers in JSON. The names the scripts use are the Music app's dictionary's (`com.apple.Music.sdef`) in JavaScript's form: player state, player position, current track, sound volume, shuffle enabled, song repeat, play, pause, playpause, next track, back track, search, user playlists and persistent ID.

- **Only while it runs:** the core sends Music nothing while it isn't running, which it learns from the processes, not from Music. While Music isn't running, the source shows nothing playing, and Play starts Music first with `open -g -j`, hidden and in the background, so no window of it comes forward, then plays.
- **Permission:** macOS asks once whether Irie may control Music, the first time it is asked to, which is when you show the Jammin tab while Music runs, or press Play. Without it, the source says where to allow it: System Settings › Privacy & Security › Automation.
- **Artwork:** the Music app gives a track's artwork as data, not an address. So when the track changes, the core writes it to `<state>/jammin/art`, named by the track's persistent ID, and keeps the newest 20. The phone shows no artwork for Apple Music.

## 5. One source at a time

The tab shows one source, picked from a strip of them, each with its own mark, as Mail's accounts are. A source playing shows a playing mark there whichever is on show. Starting a source pauses each other source that plays on this computer, so two never play over each other. Spotify playing on your phone in another room keeps playing. Which source the tab opens on is the one playing, else `jammin.source`, else the first ready.

## 6. Where it shows

- **The right sidebar's Jammin tab**, a plugin pane of the app's own view (kind `jammin`), while the plugin is on, or with `:jammin`. From the top:
  - **The sources**, the strip of §5.
  - **Now playing:** the artwork, the title, the artists and the album, its progress, which a drag moves, and the controls: shuffle, previous, play or pause, next and repeat, then the volume and the device it plays on, with the source's attribution.
  - **Your playlists**, each starting at a click, and **a search** of that source alone.
  - With nothing set up, the source says what it needs: Link Spotify…, or Open Music.
- **Words:** `:jammin` in any task bar says what plays on each source; the Mac app opens the Jammin tab instead. `:jammin play`, `pause`, `next` and `previous` (or `back`) act on the source playing, else the one on show.
- **`irie jammin`** from a terminal:

  ```
  irie jammin                        what plays on each source
  irie jammin play [URI]|pause|next|previous   (--source spotify|music)
  irie jammin seek SECONDS|volume N|shuffle on|off|repeat off|track|all
  irie jammin lists|search WORDS|devices|move DEVICE
  irie jammin link spotify [CLIENT_ID]|unlink spotify [--all]
  ```
- **Plugins:** Settings › Plugins, the Plugins menu, `irie plugins` and `:plugins` show each source's state and what it lacks.

## 7. Later

- **The Spotify app on this Mac**, through its own scripting dictionary, so an account without Premium gets play, pause and skip here, with no app at Spotify's dashboard.
- **Files on this computer:** a folder of yours (`jammin.folder`), its tracks read with Spotlight's metadata and played by the core, with no service at all.
- **YouTube Music** has no public API, YouTube's terms forbid automated access, and Google refuses sign-ins in an app's web view. It waits for a way in that keeps to them.
- **Others**, each a source if it has a public API that allows it: a media server of yours (Plex, Jellyfin, or one that speaks the Subsonic API), internet radio by a stream's address, SoundCloud, Tidal, Deezer.
- **AirPlay** devices for Apple Music, under Devices.
- **Playing in Irie itself**, with Spotify's Web Playback SDK, if WebKit's media keys allow it in an app's web view. It is to be tried in a harness first.
- **Agents**, perhaps play and pause behind a switch of their own. They would never be given Spotify's content.

## 8. Config

```toml
[plugins]
jammin = true             # the plugin; on by default

[jammin]
# source = "spotify"      # the source the tab opens on when none plays; "" = the first ready

[jammin.spotify]
client_id = "0123456789abcdef0123456789abcdef"   # your Spotify app's Client ID: 32 hex digits
```

## 9. Tests

- `internal/jammin`: Spotify against a fake Web API and a fake accounts service, `httptest` servers of the test's own that answer as Spotify's documentation says, never Spotify itself: the sign-in (verifier, challenge, state, the redirect with its port, a refused or late sign-in, a wrong state), the token kept and refreshed, a new refresh token kept, a 401 refreshing once; what plays, nothing playing, an episode, an ad; each control with its request; Premium required, no active device, a 429 waited out, one too long refused, the quota used up; playlists and a search, each in Spotify's 2026 shapes; this computer's device. Apple Music against a stand-in for `osascript`: the scripts run with their words as arguments, never in the script; what plays, nothing playing, Music not running and sent nothing, permission refused in words; artwork written and the oldest removed; each control. The service: one source at a time, another paused that plays here, and one elsewhere left playing; caching and its cadence; the token store, with the Keychain's in a temporary keychain of its own. Each name the Music scripts use is in the Music app's dictionary, read from the app's bundle where it is.
- `internal/core`: the op, `:jammin`, the plugin off, every call from an agent refused, a look as much as a play.
- `internal/plugins`, `internal/config`, `cmd/irie`: the parts' states, the settings checked, `irie jammin` against a stand-in core.

## 10. Order of the work

1. This proposal, the plugin with Spotify and Apple Music, the op, `irie jammin`, `:jammin`, the Mac app's Jammin tab and its sign-in sheet with Bitwarden, and the build tag.
2. The Spotify app on this Mac, and files on this computer.
3. AirPlay for Apple Music, a media server of yours, internet radio.
4. Whatever else §7 holds, one source at a time.
