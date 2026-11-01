# Irie for iPhone

Date: 2026-10-31; the native app 2026-11-01. Status: first build. Read with [DESIGN.md](DESIGN.md) and [CORE.md](CORE.md).

Irie for iPhone runs Irie from your phone: every session on each of your machines, red when one needs you, its conversation, a message box, buttons for what it asks, and a task bar that starts work on any machine. It is a web app that you add to the Home Screen, served by Irie itself on each machine over your own network, and a native app of the same shape for whoever builds it in Xcode (§4.1). The core still runs every agent on its machine, so nothing stops when the phone sleeps or leaves.

## 1. Why a web app

A native iPhone app needs a current Xcode to install, and without a paid developer account Apple lets a sideloaded build run seven days. A web app on the Home Screen has its own icon and full screen, runs on any recent iOS, takes updates the moment a machine has them, and needs no account. So the web app is the one every phone can have; the native app (§4.1) talks to the same gateways for those who sign it themselves.

## 2. How it works

```mermaid
flowchart LR
  phone(["iPhone: the Irie web app"])
  subgraph mac["each machine"]
    gw["irie remote serve: HTTPS gateway"]
    core["irie core"]
  end
  phone -- "HTTPS on the local network: /api/call, /api/subscribe" --> gw
  gw -- "unix socket, NDJSON" --> core
```

- **The gateway** is `irie remote serve`, a client of the core's socket like the Mac app (CORE.md §2). It keeps no rules: it relays an op and its reply as the core wrote them, and a subscription's frames as they come. It serves the app's files from the binary (`internal/remote/web`), so every install brings the phone the new app. The core runs it in its own window, `irie-agents:irie-remote`, under the supervisor that restarts the core, while `remote.on` is set, and starts it again after an install or a change of its settings (`internal/core/remote.go`). Restarting it touches no agent (DESIGN §2.1, principle 6).
- **Only a phone's ops** pass (`remote.Ops`): `hello`, `list`, `input`, `complete`, `conversation`, `send`, `park`, `unpark`, `close`, `reopen`, `undo`, `redo`, `rename`, `priority`, `freeze`, `thaw`, `rate`, `result`, `route`, `dog`, `reorder`, `screen` and `keys`. What acts on the machine's own screen (`show`, `flip`), on Irie's setup (`config`, `theme`, `tool`, `local`, `preflight`, `move`, `trod`) or only agents send (`hook`, `status`) is refused with `unknown_op`, and `hello` names only the features a phone may use. A subscription takes only `session`, `agent` and `conversation`.
- **Two ops for a client without a terminal** (DESIGN §2.4): `screen` reads what a session's terminal shows, as plain text, and while it waits on you the numbered choices of the prompt at its bottom, as Claude Code draws a permission, a question or a plan (`1. Yes`, `2. Yes, and don't ask again…`). `keys` presses keys there: a digit or letter, or Enter, Escape, the arrows, Tab and the like, never Ctrl+C, and types text without Enter. So a phone answers a prompt with the key a person would press. Both go through the core, the only writer to tmux, and `keys` takes its turn with `send`.

### 2.1 The API

The HTTPS port (`remote.port`, 7387) serves the app and its API; the port below it serves only the setup page, over plain HTTP.

| Call | What |
|---|---|
| `GET /api/info` | The machine's id, name, version, API and OS. No token |
| `POST /api/pair` `{code, device}` | A paired phone's token, and the machine. A wrong code is `bad_code`, an expired or used one `expired`, the fifth wrong one `too_many` |
| `POST /api/call` `{op, args}` | The core's reply: `{ok, result}` or `{ok:false, error}`, with the core's own words. A machine whose core restarts says `core_down` |
| `POST /api/subscribe` args | A stream of NDJSON: `{ok}` first, then the core's frames (DESIGN §2.4), and `{"t":"ping"}` every 15 s |
| `DELETE /api/device` | This phone forgets itself on the machine |
| `POST /api/push` `{subscription, vapid, done}` | Notifications to this phone: its `PushSubscription`, the key it subscribed with, and whether a finished turn is news too. A first notification goes out at once, so the phone knows they work; a push service that can't be reached is `push_failed` |
| `DELETE /api/push` | No more notifications to this phone |

Every call but `info` and `pair` takes `Authorization: Bearer <token>`; an unknown token is 401 `unpaired`. The app on one machine talks to every other machine's gateway, so the API lets a page from any local name or address ask (CORS), and no page on the internet; a token, never a cookie, says who calls. Every POST must say `Content-Type: application/json`, which no page can send to another origin without asking first, so no page elsewhere can spend a pairing code's tries. The setup port serves only the setup page, its script, its icons and the profile.

## 3. Trust

- **Turning it on.** `irie remote on --name Studio` sets `remote.on` and the name the phone shows, through the core's `config` op, which starts the gateway within a second; `irie remote off` stops it.
- **Pairing.** `irie remote pair` opens a six-digit code for ten minutes (`--for` longer) and prints where the phone sets up, with a QR code, then the native app's pairing link with a QR code of its own (§4.1); `--open` shows them in the browser. The code pairs one phone, once. Five wrong codes end it, so a guess has five chances in a million. A paired phone holds a 256-bit token; the machine keeps only its SHA-256, in `remote/devices.json`. `irie remote devices` lists the phones and when each was last used, and `irie remote forget` unpairs one, or all.
- **HTTPS with Irie's own authority.** Each machine signs its certificate with Irie's certificate authority, which the phone installs once as a profile from the setup page and trusts in Settings > General > About > Certificate Trust Settings. The authority's certificate carries name constraints (RFC 5280 §4.2.1.10): it can vouch only for `.local`, `home.arpa` and `localhost` names and for private, shared, link-local and loopback addresses, for no e-mail address or URI (each permitted only under `.invalid`, which none can be), and only for TLS servers (its extended key usage), so its key could not stand in for any site on the internet. The server's certificate has the machine's own Bonjour name and local addresses, never a name a client asks for, lasts 397 days, and is made again for an address the network gives the machine later. Apple's verifier accepts the chain for the machine's name and address and refuses it for any other (checked with `security verify-cert` on macOS 14). `irie remote ca export` and `import` share one authority between your machines, so one profile on the phone serves them all.
- **Checking the profile before trusting it.** The setup page and its profile come over plain HTTP, since the phone can't check HTTPS before it trusts the authority. So a stranger on the same Wi-Fi at that moment could answer for the machine and hand out an authority of their own. `irie remote pair`, its `--open` page and the `pair` op show the first eight bytes of the authority's SHA-256 fingerprint, and the setup page asks the person to compare them with the certificate under the profile's More Details before tapping Install. The comparison counts only against a copy that came another way: the machine's own screen, or a message from it. Matching eight bytes by chance takes about 2^64 tries.
- **Local network only.** The gateway listens on the machine's addresses (`remote.listen` picks one) and answers only requests from its local networks: private, shared, link-local and loopback addresses, and any address on the same link as one of the machine's own, as a phone's global IPv6 address at home is. Everything else gets 403, so a machine with a global IPv6 address, or on a café's Wi-Fi, exposes nothing beyond the network it is on. A VPN such as Mullvad keeps its local-network sharing for it. Reaching it from away from home needs a network that joins the phone to the machines, which this build leaves to you (§6).
- **What a phone may do.** A paired phone is trusted as you are at the machine: it starts tasks, and `input` runs whatever its words say, as for any client (CORE.md §3), `:move` and `:setup` included, which do no more than a task could. What it can't reach is the machine's own screen and Irie's settings. A forgotten phone's open streams end within 15 s.
- **What stays on the machine.** The authority's key (`ca-key.pem`) and each phone's notification key (in `devices.json`) are files of the user's, mode 0600. Agents run as the same user, so an agent turned against you could read them: it could then sign certificates for your local names, or push notifications to your phone. Running agents as another user would close that; this build doesn't.

## 4. The app

- **Sessions**: a card per session in the core's order, each machine a section, parked last; a state change never moves a card. Red when it needs you.
- **A session**: its conversation, as the Mac app draws it; a message box (`send`); while it waits on you, a red card with what it asks and its choices as buttons (`screen`, `keys`), or a key pad and its terminal's text.
- **Actions**: park, freeze, promote, rate, rename, stop the turn, close, with undo.
- **A task bar** that starts a task on the machine you pick, with its repo and profile, and takes every word of the task bar's language (`input`, CORE.md §3).
- **Machines**: each paired machine, whether it answers, and Add a machine (its address and a code from that machine).

It keeps the paired machines in the browser's storage on the phone, and a service worker keeps the app's files, so it opens even while the machine that served it sleeps.

### 4.1 The native app

`ios/Irie.swiftpm` is the same app in SwiftUI, an Xcode app package ([ios/README.md](../ios/README.md) says how to build, sign and install it). It shows what the web app shows and sends the same calls (§2.1); it differs where a native app can do better:

- **No profile.** It pins Irie's authority for each machine itself. A connection must present a chain that ends in that authority, and the machine's certificate must be valid for the name or address dialed under that authority alone; it trusts no other certificate on the phone, and needs none installed.
- **Pairing by link.** `irie remote pair`, its `--open` page and the `pair` op also give `irie://pair?u=<gateway>&c=<code>&f=<SHA-256 of the authority>&n=<name>` and a QR code of it. Read from the machine's own screen, it carries the whole fingerprint by a channel the person trusts, so the app pins it at once; the phone's camera opens it in the app, or the app's own scanner reads it. With an address and code typed instead, the app shows the first eight bytes of the fingerprint the machine answered with, and sends the code only after the person says they match what `irie remote pair` printed. Either way the address must be one Irie's authority can vouch for (§3), so a link to anywhere else is refused.
- **The token in the Keychain**, readable only on that phone after it is first unlocked; the machines' names, addresses and pinned fingerprints are in its preferences.
- **No notifications yet.** The gateways send Web Push, which reaches only the web app. A native app's notifications come through Apple's own push service, which needs a paid developer account and its key on every machine. Keep the web app on the phone for them.

Installing it takes an Xcode that knows the phone's iOS and an Apple ID to sign it. With a free account the signature lasts seven days, then Xcode installs it again; a paid one lasts a year and opens TestFlight.

## 5. Notifications

Each gateway follows its core's sessions (a `subscribe` of its own) and tells every phone that asked when one needs you, failed or stalled, and, for a phone that asked for them too, when a turn ends. A first look after the gateway or the core starts is not news, so a restart never sends a flood; a question that changes while the session still needs you is. One kind of news about one session goes out at most every 20 s, with the session's name, what it asks or did (cut at 200 characters), and the machine; a tap opens the session.

They go through the phone's own push service (Apple's, for an iPhone on iOS 16.4 or later with the app on its Home Screen), as Web Push has it: RFC 8030 for the push, RFC 8291 for encrypting each message to the phone, RFC 8292 (VAPID) for signing the request. A subscription is bound to one key, and every machine pushes to it, so the app makes that key itself, a P-256 pair, and gives each machine the subscription and the key. A machine keeps them with the phone in `devices.json`, and posts only to the browsers' push services (Apple's, Google's, Mozilla's and Microsoft's hosts, on port 443), so a subscription can't point it at anything else; a push service's refusal reaches the phone as its status alone. A subscription the push service no longer has (404, 410) is dropped. Each VAPID token lasts 12 hours and is reused, as Apple asks. Pushes leave the machine for the push service over the internet, a VPN's tunnel included; nothing comes in.

## 6. Next

- Reaching the machines from away from home: a mesh network such as Tailscale joins them, but on iOS only one VPN runs at a time, so it means choosing it over Mullvad on the phone, or Mullvad through it.
- Helpers' own conversations, and teammates.
- Notifications in the native app, through Apple's push service, once there is a paid developer account.

## 7. Files

| Path | What |
|---|---|
| `internal/remote` | The gateway: `remote.go` (server, relay), `certs.go` (authority, certificates, profile), `devices.go` (pairing, tokens), `push.go` (notifications), `qr.go` |
| `internal/remote/web` | The app: `index.html`, `app.js`, `app.css`, `js/`, `manifest.webmanifest`, `sw.js`, `setup.html`, and icons that `go run ./tools/mkicon -phone internal/remote/web` draws |
| `ios/Irie.swiftpm` | The native app (§4.1): `Package.swift`, `Sources/` and its icon, which `go run ./tools/mkicon -ios ios/Irie.swiftpm/Sources/Assets.xcassets/AppIcon.appiconset` draws; `ios/README.md` |
| `internal/core/remote.go` | The core keeps the gateway's window |
| `internal/core/screen.go` | `screen` and `keys` |
| `cmd/irie/remote.go` | `irie remote` |
| `$XDG_STATE_HOME/irie/remote/` | `ca.pem`, `ca-key.pem` (0600), `devices.json`, `pairing.json`, `machine.json`; `remote.log` beside it |
