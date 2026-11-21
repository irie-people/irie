import AppKit
import SwiftUI

// The Jammin plugin's side of the app (docs/proposals/jammin.md, DESIGN
// §4.17): the core reads and controls each source of music, Spotify
// through an app of the person's own and the Music app on this Mac, and
// the app only draws, in the right sidebar's Jammin tab, a pane of the
// app's own view (JamminViews.swift), and the sheet that links Spotify
// (JamminSignIn.swift). It asks the core only while the tab shows.

// MARK: Wire

/// What the jammin op is asked (internal/ipc/jammin.go): Do says what, of
/// Source.
struct JamminArgs: Encodable {
    var `do`: String?
    var source: String?
    var uri: String?
    var context: String?
    var query: String?
    var position: Int?
    var volume: Int?
    var on: Bool?
    var `repeat`: String?
    var device: String?
    var clientID: String?
    var folder: String?
    var all: Bool?

    enum CodingKeys: String, CodingKey {
        case `do`, source, uri, context, query, position, volume, on, `repeat`, device, folder, all
        case clientID = "client_id"
    }
}

/// The jammin op's answer: every source, and what the call asked for.
struct JamminResult: Decodable {
    var sources: [JamSource]?
    var source: String?
    var lists: [JamItem]?
    var found: [JamItem]?
    var devices: [JamDevice]?
    var link: JamLink?
    var text: String?
}

/// One source of music and how it stands.
struct JamSource: Decodable, Equatable, Identifiable {
    var name: String
    var title: String
    /// ready, needs, linking, error or off.
    var state: String
    var problem: String?
    var account: String?
    /// What it can do now: play, skip, seek, volume, shuffle, repeat,
    /// lists, search, devices.
    var can: [String]?
    var now: JamNow?
    var link: JamLink?
    /// The Client ID of the person's Spotify app, once the core keeps one:
    /// a sign-in again needs no new one. No secret: PKCE has none.
    var clientID: String?
    /// How it reaches its music: account, through the service's API, or
    /// app, through the service's app on this Mac; nil for the files.
    var via: String?
    /// The files' folder, as config.toml names it, ~ and all.
    var folder: String?

    enum CodingKeys: String, CodingKey {
        case name, title, state, problem, account, can, now, link, via, folder
        case clientID = "client_id"
    }

    var id: String { name }
    var ready: Bool { state == "ready" }
    var playing: Bool { now?.playing == true }
    var isSpotify: Bool { name == JamSource.spotify }
    var isMusic: Bool { name == JamSource.music }
    var isFiles: Bool { name == JamSource.files }
    /// Spotify through its app on this Mac, with no account linked.
    var throughApp: Bool { via == "app" }

    static let spotify = "spotify"
    static let music = "music"
    static let files = "files"

    func does(_ what: String) -> Bool { can?.contains(what) == true }

    /// Its mark in the strip.
    var symbol: String {
        switch name {
        case JamSource.spotify: return "dot.radiowaves.left.and.right"
        case JamSource.music: return "music.note"
        case JamSource.files: return "folder"
        default: return "music.quarternote.3"
        }
    }
}

/// What plays on a source.
struct JamNow: Decodable, Equatable {
    var playing: Bool
    var title: String
    var artists: [String]?
    var album: String?
    /// track, episode or ad.
    var kind: String?
    var uri: String?
    var link: String?
    /// An https address, or a file on this Mac.
    var art: String?
    /// In milliseconds; Position was read at At, in Unix milliseconds.
    var duration: Int?
    var position: Int?
    var at: Int64?
    var volume: Int?
    var shuffle: Bool?
    /// off, track or all.
    var `repeat`: String?
    var device: JamDevice?
    var context: JamItem?

    var by: String { (artists ?? []).joined(separator: ", ") }

    /// How far it is at date, in milliseconds: from where it was read, on
    /// by the time since while it plays, never past its end.
    func position(at date: Date) -> Int {
        var p = position ?? 0
        if playing, let at, at > 0 {
            p += max(0, Int(date.timeIntervalSince1970 * 1000) - Int(at))
        }
        if let d = duration, d > 0 { p = min(p, d) }
        return max(0, p)
    }

    /// Milliseconds as a player's clock shows them: 3:07, 1:02:03.
    static func clock(_ ms: Int) -> String {
        let s = max(0, ms) / 1000
        return s >= 3600 ? String(format: "%d:%02d:%02d", s / 3600, s % 3600 / 60, s % 60) : String(format: "%d:%02d", s / 60, s % 60)
    }
}

/// A track, album, playlist or episode at a source.
struct JamItem: Decodable, Equatable, Identifiable {
    var kind: String
    var title: String
    var by: String?
    var uri: String
    var link: String?
    var art: String?
    var tracks: Int?
    var duration: Int?
    var album: String?
    /// The list a found track plays within, such as its album, so the
    /// music goes on after it.
    var context: String?

    var id: String { uri }

    /// Who made it, and how many tracks it holds or how long it runs, in a
    /// line.
    var about: String {
        var parts: [String] = []
        if let by, !by.isEmpty { parts.append(by) }
        if kind == "track", let album, !album.isEmpty { parts.append(album) }
        if let tracks, tracks > 0 { parts.append(tracks == 1 ? "1 track" : "\(tracks) tracks") }
        if kind != "playlist", let duration, duration > 0 { parts.append(JamNow.clock(duration)) }
        return parts.joined(separator: " · ")
    }
}

/// A place a source can play: a computer, a phone, a speaker.
struct JamDevice: Decodable, Equatable, Identifiable {
    var id: String
    var name: String
    /// As the source names it: Computer, Smartphone, Speaker.
    var kind: String?
    var active: Bool?
    var here: Bool?
    var volume: Int?
    var restricted: Bool?

    var symbol: String {
        if here == true { return "laptopcomputer" }
        switch (kind ?? "").lowercased() {
        case "computer": return "desktopcomputer"
        case "smartphone": return "iphone"
        case "tablet": return "ipad"
        case "speaker", "avr", "stb", "audiodongle": return "hifispeaker"
        case "tv", "castvideo": return "tv"
        case "castaudio": return "hifispeaker"
        case "automobile": return "car"
        case "gameconsole": return "gamecontroller"
        default: return "speaker.wave.2"
        }
    }

    /// Its name, or this Mac's, as a person says it.
    var shownName: String { here == true ? "This Mac (\(name))" : name }
}

/// A sign-in under way: the page to open, and until when the core listens.
struct JamLink: Decodable, Equatable {
    var source: String
    var url: String
    var redirect: String
    var until: Int64
}

/// What a search of one source found, and for which words.
struct JamFound: Equatable {
    var words: String
    var items: [JamItem]
}

// MARK: The model

/// The Jammin tab's state: each source as the core last said, the one on
/// show, its playlists and what a search of it found. While the tab shows
/// it asks the core every 2 seconds and draws a track's progress between
/// asks itself; it never asks while the tab is hidden.
@MainActor
@Observable
final class JamminModel {
    let client: CoreClient

    init(client: CoreClient) {
        self.client = client
    }

    private(set) var sources: [JamSource] = []
    /// The source on show, picked in the strip: one at a time, so no list
    /// ever holds two sources' music.
    private(set) var shown: String?
    private(set) var loaded = false
    /// Why the last control, list or search failed, said in place.
    var problem: String?
    /// Why the last look failed, as when the core restarts.
    private(set) var offline: String?
    /// The control under way, so its button waits.
    private(set) var busy: String?
    /// Each source's playlists, as `lists` read them.
    private(set) var lists: [String: [JamItem]] = [:]
    private(set) var listing: String?
    /// The search box, and what each source's last search found.
    var query = ""
    private(set) var found: [String: JamFound] = [:]
    private(set) var searching = false
    /// Where Spotify can play, as `devices` read them.
    private(set) var devices: [JamDevice] = []
    private(set) var lookingForDevices = false
    /// The sheet that links Spotify shows (JamminSignIn.swift).
    var linking = false
    /// Artwork, by its address, once loaded.
    private(set) var art: [String: NSImage] = [:]
    /// The Music app runs on this Mac.
    private(set) var musicOpen = false
    /// What a control just asked for, drawn until a look read after it:
    /// Spotify applies a command a moment after it answers.
    private var holds: [String: Hold] = [:]

    @ObservationIgnored private var artLoading: Set<String> = []
    @ObservationIgnored private var artOrder: [String] = []
    @ObservationIgnored private(set) var isFixture = false

    private struct Hold {
        var playing: Bool?
        /// Where it is at since, in milliseconds.
        var position: Int?
        var volume: Int?
        var shuffle: Bool?
        var `repeat`: String?
        var since = Date()
    }

    /// The source on show.
    var source: JamSource? { sources.first { $0.name == shown } ?? sources.first }

    /// What plays on s, with what a control just asked for over it.
    func now(_ s: JamSource) -> JamNow? {
        guard var n = s.now else { return nil }
        guard let h = holds[s.name] else { return n }
        if let p = h.playing { n.playing = p }
        if let p = h.position {
            n.position = p
            n.at = Int64(h.since.timeIntervalSince1970 * 1000)
        }
        if let v = h.volume { n.volume = v }
        if let on = h.shuffle { n.shuffle = on }
        if let r = h.repeat { n.repeat = r }
        return n
    }

    private func call(_ args: JamminArgs, timeout: TimeInterval = 15) async throws -> JamminResult {
        do {
            return try await client.call("jammin", args, timeout: timeout, as: JamminResult.self)
        } catch let e as WireError {
            throw e
        } catch {
            throw WireError(code: "offline", msg: "\(Brand.name)’s core isn’t answering now. What plays keeps playing.")
        }
    }

    private func take(_ r: JamminResult) {
        if let s = r.sources, s != sources { sources = s }
        // A hold ends once a look read after its control came, or after a
        // few seconds whatever came.
        let nowMs = Date().timeIntervalSince1970 * 1000
        for (name, h) in holds {
            let since = h.since.timeIntervalSince1970 * 1000
            let read = sources.first { $0.name == name }?.now?.at.map { Double($0) } ?? nowMs
            if read >= since + 600 || nowMs - since > 5000 { holds[name] = nil }
        }
        // The first look opens on the source the core says; one that went
        // away gives way to the first.
        if shown == nil || !sources.contains(where: { $0.name == shown }) {
            shown = r.source.flatMap { name in sources.first { $0.name == name }?.name } ?? sources.first?.name
        }
        if !isFixture {
            let open = !NSRunningApplication.runningApplications(withBundleIdentifier: "com.apple.Music").isEmpty
            if open != musicOpen { musicOpen = open }
        }
        loaded = true
    }

    /// Reads every source again: a look.
    func refresh() async {
        guard !isFixture else { return }
        do {
            take(try await call(JamminArgs()))
            offline = nil
        } catch {
            offline = error.localizedDescription
        }
    }

    /// Shows s in the tab.
    func show(_ name: String) {
        guard name != shown else { return }
        shown = name
        problem = nil
        query = found[name]?.words ?? ""
    }

    // MARK: Controls

    /// Asks the core for a control on the source on show, and draws the
    /// answer's sources at once.
    private func act(_ args: JamminArgs, busy key: String, hold: Hold? = nil) {
        guard !isFixture, let s = source else { return }
        var a = args
        a.source = s.name
        if let hold { holds[s.name] = hold }
        busy = key
        problem = nil
        Task {
            defer { if busy == key { busy = nil } }
            do {
                take(try await call(a))
            } catch {
                holds[s.name] = nil
                problem = error.localizedDescription
            }
        }
    }

    func toggle() {
        guard let s = source else { return }
        let n = now(s)
        let playing = n?.playing ?? false
        act(JamminArgs(do: playing ? "pause" : "play"), busy: "toggle",
            hold: Hold(playing: !playing, position: n?.position(at: Date())))
    }

    func next() { act(JamminArgs(do: "next"), busy: "next") }

    func previous() { act(JamminArgs(do: "previous"), busy: "previous") }

    /// Moves the playing to ms from the start.
    func seek(_ ms: Int) {
        act(JamminArgs(do: "seek", position: max(0, ms)), busy: "seek", hold: Hold(position: max(0, ms)))
    }

    func setVolume(_ v: Int) {
        let v = min(100, max(0, v))
        act(JamminArgs(do: "volume", volume: v), busy: "volume", hold: Hold(volume: v))
    }

    func setShuffle(_ on: Bool) {
        act(JamminArgs(do: "shuffle", on: on), busy: "shuffle", hold: Hold(shuffle: on))
    }

    /// Repeat's next setting, in the order Spotify's own button goes: off,
    /// all, track.
    func cycleRepeat() {
        guard let s = source else { return }
        let next: String
        switch now(s)?.repeat ?? "off" {
        case "off": next = "all"
        case "all": next = "track"
        default: next = "off"
        }
        act(JamminArgs(do: "repeat", repeat: next), busy: "repeat", hold: Hold(repeat: next))
    }

    /// Plays an item of the source on show: a playlist, an album or a
    /// track.
    func play(_ item: JamItem) {
        let context = (item.context ?? "").isEmpty ? nil : item.context
        act(JamminArgs(do: "play", uri: item.uri, context: context), busy: "play:" + item.uri)
    }

    /// Moves the playing to device.
    func move(_ device: JamDevice) {
        act(JamminArgs(do: "move", device: device.id), busy: "move")
    }

    // MARK: Lists, search and devices

    /// Reads the source's playlists, once, or again when asked.
    func loadLists(_ name: String, again: Bool = false) async {
        guard !isFixture, again || lists[name] == nil, listing != name else { return }
        listing = name
        defer { if listing == name { listing = nil } }
        do {
            let r = try await call(JamminArgs(do: "lists", source: name), timeout: 30)
            take(r)
            lists[name] = r.lists ?? []
        } catch {
            if shown == name { problem = error.localizedDescription }
        }
    }

    /// Searches the source on show alone for the words in the box.
    func search() {
        let words = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !words.isEmpty, !isFixture, let s = source else { return }
        searching = true
        problem = nil
        Task {
            defer { searching = false }
            do {
                let r = try await call(JamminArgs(do: "search", source: s.name, query: words), timeout: 30)
                take(r)
                found[s.name] = JamFound(words: words, items: r.found ?? [])
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Puts the search away: the playlists show again.
    func clearSearch() {
        guard let s = source else { return }
        found[s.name] = nil
        query = ""
    }

    /// Reads where the source on show can play.
    func loadDevices() async {
        guard !isFixture, let s = source else { return }
        lookingForDevices = true
        defer { lookingForDevices = false }
        do {
            let r = try await call(JamminArgs(do: "devices", source: s.name), timeout: 20)
            take(r)
            devices = r.devices ?? []
        } catch {
            problem = error.localizedDescription
        }
    }

    // MARK: Linking

    /// Starts signing in to Spotify with the Client ID of the person's
    /// app, or with the one the core kept when it is nil: the page to show
    /// comes back.
    func link(clientID: String?) async throws -> JamLink {
        let r = try await call(JamminArgs(do: "link", source: JamSource.spotify, clientID: clientID), timeout: 20)
        take(r)
        guard let link = r.link ?? sources.first(where: { $0.isSpotify })?.link else {
            throw WireError(code: "link", msg: r.text ?? "Spotify’s sign-in didn’t start.")
        }
        return link
    }

    /// Stops Spotify's sign-in under way, as when its sheet closes before
    /// it is done.
    func cancelLink() {
        guard !isFixture else { return }
        Task {
            if let r = try? await call(JamminArgs(do: "cancel", source: JamSource.spotify)) { take(r) }
        }
    }

    /// Spotify is linked: the tab shows it.
    func linked() {
        shown = JamSource.spotify
        lists[JamSource.spotify] = nil
        problem = nil
    }

    // MARK: Elsewhere

    /// Opens the item, or what plays, in the source's own app or site, as
    /// its attribution asks.
    func open(_ link: String?) {
        guard let link, let url = URL(string: link), ["https", "spotify", "music", "itms"].contains(url.scheme?.lowercased() ?? "") else { return }
        NSWorkspace.shared.open(url)
    }

    /// Starts the Music app hidden, behind what the person works in, so the
    /// tab can reach it.
    func openMusic() {
        guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.apple.Music") else {
            problem = "The Music app isn’t on this Mac."
            return
        }
        let config = NSWorkspace.OpenConfiguration()
        config.activates = false
        config.hides = true
        config.addsToRecentItems = false
        NSWorkspace.shared.openApplication(at: url, configuration: config) { _, error in
            let why = error?.localizedDescription
            Task { @MainActor [weak self] in
                guard let self else { return }
                if let why {
                    self.problem = "Music didn’t open: \(why)"
                    return
                }
                self.musicOpen = true
                await self.refresh()
            }
        }
    }

    /// Asks for the folder of music files in an open panel, and gives it to
    /// the core with the home folder written as ~, as config.toml keeps it.
    func chooseFolder() {
        guard !isFixture else { return }
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Choose"
        panel.message = "Choose the folder that holds your music files."
        if let f = sources.first(where: { $0.isFiles })?.folder, !f.isEmpty {
            panel.directoryURL = URL(fileURLWithPath: (f as NSString).expandingTildeInPath, isDirectory: true)
        }
        guard panel.runModal() == .OK, let url = panel.url else { return }
        let folder = Self.tilde(url.path)
        busy = "folder"
        problem = nil
        Task {
            defer { if busy == "folder" { busy = nil } }
            do {
                let r = try await call(JamminArgs(do: "folder", source: JamSource.files, folder: folder))
                // Another folder's folders and finds are gone.
                lists[JamSource.files] = nil
                found[JamSource.files] = nil
                if shown == JamSource.files { query = "" }
                take(r)
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// A path with the home folder written as ~.
    nonisolated static func tilde(_ path: String) -> String {
        let home = NSHomeDirectory()
        if path == home { return "~" }
        if path.hasPrefix(home + "/") { return "~" + path.dropFirst(home.count) }
        return path
    }

    // MARK: Artwork

    /// Loads the artwork at address, an https address or a file on this
    /// Mac, once, keeping the newest few dozen in memory.
    func loadArt(_ address: String) async {
        guard !address.isEmpty, art[address] == nil, !artLoading.contains(address), !isFixture else { return }
        artLoading.insert(address)
        defer { artLoading.remove(address) }
        var data: Data?
        if address.hasPrefix("/") {
            data = await Task.detached { try? Data(contentsOf: URL(fileURLWithPath: address)) }.value
        } else if let url = URL(string: address), url.scheme?.lowercased() == "https",
                  let got = try? await URLSession.shared.data(from: url),
                  (got.1 as? HTTPURLResponse)?.statusCode ?? 200 < 300 {
            data = got.0
        }
        guard let image = data.flatMap(NSImage.init(data:)) else { return }
        art[address] = image
        artOrder.append(address)
        if artOrder.count > 60 {
            let gone = artOrder.removeFirst()
            art[gone] = nil
        }
    }

    /// Spotify's app is on this Mac: its attribution's link says Play on
    /// Spotify, and else Get Spotify free.
    static let spotifyInstalled = NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.spotify.client") != nil
}

// MARK: Snapshots

extension JamminModel {
    /// The snapshots' sources (Snapshot.swift), which no core reads: Spotify
    /// playing, Apple Music paused, or Spotify waiting to be linked. Every
    /// name is made up, and the artwork is a gradient drawn here.
    func loadFixture(_ scene: String) {
        isFixture = true
        loaded = true
        problem = nil
        offline = nil
        query = ""
        found = [:]
        holds = [:]
        musicOpen = scene != "spotify-link"
        let at = Int64(Date().timeIntervalSince1970 * 1000)
        art = [
            "fixture:harbour": Self.gradient(0.56, 0.08),
            "fixture:morning": Self.gradient(0.12, 0.95),
            "fixture:rain": Self.gradient(0.62, 0.75),
            "fixture:road": Self.gradient(0.33, 0.15),
            "fixture:garden": Self.gradient(0.42, 0.22),
            "fixture:quiet": Self.gradient(0.80, 0.55),
        ]
        let spotifyNow = JamNow(playing: true, title: "Harbour Lights", artists: ["Night Owls"], album: "Low Tide", kind: "track",
                                uri: "spotify:track:0000000000000000000001", link: "https://open.spotify.com/track/0000000000000000000001",
                                art: "fixture:harbour", duration: 214_000, position: 83_000, at: at, volume: 62, shuffle: true, repeat: "off",
                                device: JamDevice(id: "d1", name: "Living Room", kind: "Speaker", active: true, volume: 62))
        let spotifyCan = ["play", "skip", "seek", "volume", "shuffle", "repeat", "lists", "search", "devices"]
        let musicNow = JamNow(playing: false, title: "Rain on the Old Road", artists: ["The Slow Ferries"], album: "Tidepool Diary", kind: "track",
                              uri: "4F2A9C0D1E3B5A77", art: "fixture:rain", duration: 251_000, position: 96_000, at: at, volume: 40,
                              shuffle: false, repeat: "all")
        let musicCan = ["play", "skip", "seek", "volume", "shuffle", "repeat", "lists", "search"]
        switch scene {
        case "music":
            sources = [
                JamSource(name: "spotify", title: "Spotify", state: "ready", account: "alice", can: spotifyCan,
                          now: JamNow(playing: true, title: "Harbour Lights", artists: ["Night Owls"], album: "Low Tide", art: "fixture:harbour",
                                      duration: 214_000, position: 12_000, at: at,
                                      device: JamDevice(id: "d2", name: "Phone", kind: "Smartphone", active: true))),
                JamSource(name: "music", title: "Apple Music", state: "ready", can: musicCan, now: musicNow),
            ]
            shown = JamSource.music
            lists = [JamSource.music: [
                JamItem(kind: "playlist", title: "Sunday Kitchen", uri: "7A1B2C3D4E5F6071", art: "fixture:garden", tracks: 31),
                JamItem(kind: "playlist", title: "Long Drives", uri: "7A1B2C3D4E5F6072", art: "fixture:road", tracks: 58),
                JamItem(kind: "playlist", title: "Quiet Hours", uri: "7A1B2C3D4E5F6073", tracks: 19),
            ]]
        case "spotify-link":
            sources = [
                JamSource(name: "spotify", title: "Spotify", state: "needs",
                          problem: "No Spotify app yet: make one at https://developer.spotify.com/dashboard with the redirect URI http://127.0.0.1/jammin/spotify, then Link Spotify takes its Client ID. Spotify lets apps in development mode work only for Premium accounts."),
                JamSource(name: "music", title: "Apple Music", state: "ready", can: musicCan),
            ]
            shown = JamSource.spotify
            lists = [:]
        case "files":
            sources = [
                JamSource(name: "spotify", title: "Spotify", state: "ready", account: "alice", can: spotifyCan, via: "account"),
                JamSource(name: "music", title: "Apple Music", state: "ready", can: musicCan),
                JamSource(name: "files", title: "Files", state: "ready", can: ["play", "skip", "shuffle", "repeat", "lists", "search"],
                          now: JamNow(playing: true, title: "Kettle on the Stove", artists: ["The Copper Moths"], album: "Small Rooms", kind: "track",
                                      uri: "/Users/alice/Music/Demos/Kettle on the Stove.m4a", duration: 187_000, position: 41_000, at: at,
                                      shuffle: false, repeat: "all"),
                          folder: "~/Music"),
            ]
            shown = JamSource.files
            lists = [JamSource.files: [
                JamItem(kind: "folder", title: "Demos", uri: "/Users/alice/Music/Demos", tracks: 23),
                JamItem(kind: "folder", title: "Field Recordings", uri: "/Users/alice/Music/Field Recordings", art: "fixture:garden", tracks: 12),
                JamItem(kind: "folder", title: "Live Sets", uri: "/Users/alice/Music/Live Sets", tracks: 8),
            ]]
        case "spotify-app":
            sources = [
                JamSource(name: "spotify", title: "Spotify", state: "ready", can: ["play", "skip", "seek", "volume", "shuffle", "repeat"],
                          now: JamNow(playing: true, title: "Harbour Lights", artists: ["Night Owls"], album: "Low Tide", kind: "track",
                                      uri: "spotify:track:0000000000000000000001", link: "https://open.spotify.com/track/0000000000000000000001",
                                      art: "fixture:harbour", duration: 214_000, position: 83_000, at: at, volume: 48, shuffle: false, repeat: "all"),
                          via: "app"),
                JamSource(name: "music", title: "Apple Music", state: "ready", can: musicCan),
                JamSource(name: "files", title: "Files", state: "needs", problem: "No folder of music yet: Choose Folder… picks one."),
            ]
            shown = JamSource.spotify
            lists = [:]
        default:
            sources = [
                JamSource(name: "spotify", title: "Spotify", state: "ready", account: "alice", can: spotifyCan, now: spotifyNow),
                JamSource(name: "music", title: "Apple Music", state: "ready", can: musicCan, now: musicNow),
            ]
            shown = JamSource.spotify
            lists = [JamSource.spotify: [
                JamItem(kind: "playlist", title: "Morning Focus", by: "alice", uri: "spotify:playlist:0000000000000000000002",
                        link: "https://open.spotify.com/playlist/0000000000000000000002", art: "fixture:morning", tracks: 42),
                JamItem(kind: "playlist", title: "Quiet Evenings by the Water, for Reading and Writing", by: "alice", uri: "spotify:playlist:0000000000000000000003",
                        link: "https://open.spotify.com/playlist/0000000000000000000003", art: "fixture:quiet", tracks: 117),
                JamItem(kind: "playlist", title: "Garden Radio", by: "alice", uri: "spotify:playlist:0000000000000000000004",
                        link: "https://open.spotify.com/playlist/0000000000000000000004", art: "fixture:garden", tracks: 50),
            ]]
        }
    }

    /// Artwork for the snapshots: a soft diagonal gradient between two hues,
    /// never anyone's art.
    private static func gradient(_ from: CGFloat, _ to: CGFloat) -> NSImage {
        NSImage(size: NSSize(width: 300, height: 300), flipped: false) { box in
            let a = NSColor(hue: from, saturation: 0.45, brightness: 0.78, alpha: 1)
            let b = NSColor(hue: to, saturation: 0.40, brightness: 0.52, alpha: 1)
            NSGradient(starting: a, ending: b)?.draw(in: box, angle: -45)
            return true
        }
    }
}

extension Store {
    /// The Jammin tab's pane, while its part is on (PluginPanes.swift).
    var jamminPane: PluginPaneInfo? { panes.first { $0.kind == "jammin" } }

    /// Jammin's Spotify part, while it and its plugin are on: Bitwarden's
    /// unlock then keeps the logins for Spotify's sign-in too.
    var jamminSpotifyPart: PluginInfo.Part? {
        guard let p = plugins.first(where: { $0.name == "jammin" }), p.on, p.problem == nil else { return nil }
        return p.parts.first { $0.name == JamSource.spotify && $0.isOn }
    }

    /// Shows the Jammin tab, as :jammin asks.
    func showJammin() {
        guard let p = jamminPane else { return }
        if !(side.open && side.tab == .plugin && side.pane == p.id) { openPane(p.id) }
        Task { await jammin.refresh() }
    }
}
