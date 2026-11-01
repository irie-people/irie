# Irie for iPhone, the native app

The native form of the Home Screen web app ([docs/PHONE.md](../docs/PHONE.md)): every session on each machine the phone is paired with, red when one needs you, its conversation, a message box, buttons for what it asks, and a task bar that starts work on any machine. It talks to each machine's gateway (`irie remote serve`) as the web app does, through the same API (PHONE.md §2.1).

What it does differently:

- **No certificate profile.** The app pins Irie's certificate authority for each machine itself. Every connection must present a chain that ends in that authority, and the machine's own certificate must be valid for the name or address dialed under it alone; any other certificate is refused.
- **Pairing by QR code or link.** `irie remote pair` shows a QR code for this app: `irie://pair?u=<gateway>&c=<code>&f=<SHA-256 of the authority>&n=<name>`. The phone's camera or the app's own scanner reads it, and the app pins the authority it names. Typing an address and code works too: the app then shows the first eight bytes of the authority's fingerprint, and pins it only once you say they match what `irie remote pair` printed on the machine. Either way the address must be one Irie's authority can vouch for, a `.local`, `home.arpa` or `localhost` name or a private, shared, link-local or loopback address, so a link to anywhere else is refused.
- **Tokens in the Keychain**, readable only on this phone after it is first unlocked; the machines' names, addresses and pinned fingerprints are in the app's preferences.
- No notifications yet: the gateways send Web Push, which reaches only the web app, and Apple's push needs a paid developer account. Use the Home Screen web app for them.

## Put it on your iPhone

You need a Mac with Xcode and an Apple ID; a free account works.

1. Open `ios/Irie.swiftpm` in Xcode (double-click it, or File › Open).
2. Select the **Irie** app in the project navigator, then **Signing & Capabilities**: turn on *Automatically manage signing* and pick your **Team** (your Apple ID's personal team will do; add the account under Xcode › Settings › Accounts first). The bundle identifier is `io.github.irie-people.irie`; if Xcode says it is taken, change it to one of your own, such as `io.github.irie-people.irie.alice`.
3. Connect the iPhone with a cable, unlock it and tap *Trust* on it. On iOS 16 and later turn on **Developer Mode** (Settings › Privacy & Security › Developer Mode) and let the phone restart.
4. Choose the iPhone as the run destination at the top of the window and press **Run** (⌘R).
5. The first time, the phone won't open an app from an unknown developer: on it, open Settings › General › VPN & Device Management, tap your Apple ID under *Developer App* and **Trust** it. Then open Irie, and allow it on the local network when it asks.

Picking a team makes Xcode write `teamIdentifier: "<your team ID>"` into `Package.swift`. That ID is your account's, so keep it out of commits: leave the change uncommitted, or `git restore ios/Irie.swiftpm/Package.swift` when you are done.

An app signed with a free team runs for seven days; run it from Xcode again to renew it. Xcode installs only on the iOS versions it knows, so use the Xcode made for your phone's iOS ([Apple's table](https://developer.apple.com/xcode/system-requirements/)): Xcode 15.0 goes up to iOS 17 (macOS 13.5 to 14), Xcode 26.0 to 26.3 to iOS 26 (macOS 15.6 or later), and Xcode 27 to iOS 27 (macOS 26.6 or later).

## Pair it

On the machine, run `irie remote pair`. Scan the QR code for the app built in Xcode with the iPhone's camera and tap the banner, or tap **Scan the QR code** in Irie. Or type the machine's address (`studio.local`, or `10.0.0.7`) and the six-digit code, and check the fingerprint Irie shows against the one the command printed before you tap **It matches**. Add more machines from the machine chip at the top of the list.

## Build it from the command line

```sh
cd ios/Irie.swiftpm
xcodebuild -scheme Irie -destination 'platform=iOS Simulator,name=iPhone 15' -derivedDataPath /tmp/irie-ios build
xcrun simctl install booted /tmp/irie-ios/Build/Products/Debug-iphonesimulator/Irie.app
xcrun simctl launch booted io.github.irie-people.irie
xcrun simctl openurl booted 'irie://pair?u=https%3A%2F%2Fstudio.local%3A7387&c=123456&f=<64 hex digits>&n=Studio'
```

The simulator has no camera: pair it with a link or by typing.

## Files

| Path | What |
|---|---|
| `Package.swift` | The app: bundle id, icon, orientations, the camera and local network purposes; `Info.plist` adds the `irie` URL scheme |
| `Sources/IrieApp.swift` | The app and its root: sheets, dialogs, links, coming back to the front |
| `Sources/Store.swift` | The paired machines, what each core said last, and the ops a person sends (`app.js`) |
| `Sources/Net.swift` | The gateway's API, the pinned trust, the NDJSON streams that reconnect, addresses and pairing links (`js/net.js`) |
| `Sources/Keep.swift` | The Keychain and the preferences |
| `Sources/Wire.swift` | The core's wire types, after `mac/Sources/Irie/Wire.swift` with what a phone shows |
| `Sources/Home.swift`, `TaskBar.swift` | The sessions and the task bar |
| `Sources/Conversation.swift`, `SessionScreen.swift`, `NeedsCard.swift` | A session's conversation, its foot, and the needs card (`js/conv.js`) |
| `Sources/Markdown.swift` | The agent's Markdown (`js/md.js`) |
| `Sources/Look.swift` | The Evergreen palette, states, the agents' colours, the sun (`js/look.js`, `States.swift`, `Theme.swift`) |
| `Sources/Pairing.swift`, `Scanner.swift`, `Machines.swift` | Pairing, the QR scanner, and the machines |
| `Sources/Parts.swift` | Buttons, menus and toasts |
| `Sources/Assets.xcassets` | The icon (`go run ./tools/mkicon -ios`) and the accent colour |
