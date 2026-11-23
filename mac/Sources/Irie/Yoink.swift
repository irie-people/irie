#if !NOYOINK
import AppKit
import SwiftUI

// The Yoink plugin's side of the app (DESIGN §4.17): the core downloads a
// video, or its sound, from a link with yt-dlp, and the app only draws, in
// the right sidebar's Yoink tab, a pane of the app's own view
// (YoinkViews.swift). A download runs on in the core while the tab is shut
// or the app quits.

// MARK: Wire

struct YoinkArgs: Encodable {
    var `do`: String?
    var url: String?
    var get: String?
    var id: String?
}

struct YoinkResult: Decodable {
    var downloads: [YoinkDownload]?
    var folder: String?
    var info: YoinkInfo?
    var download: YoinkDownload?
    var text: String?
}

/// What a link offers: its video, and what of it can be downloaded.
struct YoinkInfo: Decodable, Equatable {
    var url: String
    var page: String?
    var title: String
    var site: String?
    var uploader: String?
    var duration: Double?
    var choices: [YoinkChoice]

    /// Its site, who put it there and how long it runs, in a line.
    var about: String {
        var parts: [String] = []
        if let s = site, !s.isEmpty { parts.append(s) }
        if let u = uploader, !u.isEmpty { parts.append(u) }
        if let d = duration, d > 0 { parts.append(YoinkDownload.clock(Int(d))) }
        return parts.joined(separator: " · ")
    }
}

/// One thing to download from a link: the best, a height or the sound.
struct YoinkChoice: Decodable, Equatable, Identifiable {
    var get: String
    var label: String
    /// About how many bytes it comes to; nil when the site doesn't say.
    var size: Int64?

    var id: String { self.get }

    var symbol: String { self.get == "audio" ? "music.note" : "film" }
}

/// One download and how far it is, as the core last said.
struct YoinkDownload: Decodable, Equatable, Identifiable {
    var id: String
    var url: String
    var title: String
    var site: String?
    var get: String
    var label: String
    /// starting, downloading, processing, done, failed or canceled.
    var state: String
    var step: String?
    var done: Int64?
    var total: Int64?
    var speed: Double?
    var eta: Int?
    var file: String?
    var error: String?
    var started: Date
    var ended: Date?

    var running: Bool { state == "starting" || state == "downloading" || state == "processing" }

    /// How far it is, from 0 to 1; nil while that isn't known, as while
    /// ffmpeg merges or makes the mp3.
    var fraction: Double? {
        if state == "done" { return 1 }
        guard state == "downloading", let total, total > 0 else { return nil }
        return min(1, Double(done ?? 0) / Double(total))
    }

    /// Its state's mark, so colour never says it alone.
    var look: Look {
        switch state {
        case "done": return .done
        case "failed": return .failed
        case "canceled": return .paused
        case "starting": return .starting
        default: return .working
        }
    }

    /// How it stands, in a line: its step while it runs, its file once
    /// done, why it failed.
    var standing: String {
        switch state {
        case "done":
            let name = file.map { ($0 as NSString).lastPathComponent } ?? "Done"
            if let total, total > 0 { return "\(name) · \(Self.bytes(total))" }
            return name
        case "failed": return error ?? "It failed."
        case "canceled": return "Canceled: what it downloaded is gone."
        case "starting": return "Starting…"
        default: return step ?? "Downloading"
        }
    }

    /// How far a download is, in figures, while it comes down: 45% · 112.4
    /// of 251.9 MB · 4.2 MB/s · 34 s left.
    var figures: String? {
        guard state == "downloading" else { return nil }
        var parts: [String] = []
        if let total, total > 0, let f = fraction {
            parts.append("\(Int(f * 100))%")
            parts.append(Self.part(done ?? 0, of: total))
        } else if let done, done > 0 {
            parts.append(Self.bytes(done) + " so far")
        }
        if let speed, speed > 0 { parts.append(Self.bytes(Int64(speed)) + "/s") }
        if let eta, eta > 0 { parts.append(Self.left(eta)) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// Bytes of a whole, its unit said once when both share it: 112.4 of
    /// 251.9 MB.
    static func part(_ done: Int64, of total: Int64) -> String {
        let d = bytes(done), t = bytes(total)
        if let space = d.lastIndex(of: " "), let tSpace = t.lastIndex(of: " "), d[space...] == t[tSpace...] {
            return "\(d[..<space]) of \(t)"
        }
        return "\(d) of \(t)"
    }

    /// What it is, as its row names it: its title, and its size or the mp3
    /// unless it is the best.
    var name: String { self.get == "best" ? title : "\(title) (\(label))" }

    /// Bytes in the Finder's own units.
    static func bytes(_ n: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: n, countStyle: .file)
    }

    static func left(_ s: Int) -> String {
        if s < 60 { return "\(s) s left" }
        if s < 3600 { return "\((s + 30) / 60) min left" }
        return "\(s / 3600) h \(s % 3600 / 60) min left"
    }

    /// A length as a clock shows it: 1:40, 1:02:03.
    static func clock(_ s: Int) -> String {
        s >= 3600 ? String(format: "%d:%02d:%02d", s / 3600, s % 3600 / 60, s % 60) : String(format: "%d:%02d", s / 60, s % 60)
    }
}

// MARK: The model

/// The Yoink tab's state: the link being looked at and what it offers, and
/// the downloads as the core last said. While the tab shows it asks the
/// core each second while a download runs, and every few seconds else.
@MainActor
@Observable
final class YoinkModel {
    let client: CoreClient

    init(client: CoreClient) {
        self.client = client
    }

    /// The link in the tab's box.
    var link = ""
    private(set) var looking = false
    /// What the link looked at offers, until a download of it starts.
    private(set) var info: YoinkInfo?
    private(set) var downloads: [YoinkDownload] = []
    /// Where downloads land, as the core says.
    private(set) var folder = ""
    /// Why the last call failed, under the box.
    var problem: String?
    private(set) var loaded = false
    /// A download being started, so its row's button waits.
    private(set) var starting: String?

    /// The snapshots' downloads, which no core runs.
    @ObservationIgnored private var isFixture = false
    @ObservationIgnored private var lookGeneration = 0

    var running: Bool { downloads.contains(where: \.running) }
    var finished: Bool { downloads.contains { !$0.running } }

    private func call(_ args: YoinkArgs, timeout: TimeInterval = 10) async throws -> YoinkResult {
        do {
            return try await client.call("yoink", args, timeout: timeout, as: YoinkResult.self)
        } catch let e as WireError {
            throw e
        } catch {
            throw WireError(code: "offline", msg: "\(Brand.name)’s core isn’t answering now. A download that runs keeps running.")
        }
    }

    private func take(_ r: YoinkResult) {
        if let d = r.downloads, d != downloads { downloads = d }
        if let f = r.folder, f != folder { folder = f }
        loaded = true
    }

    /// Reads the downloads again.
    func refresh() async {
        guard !isFixture else { return }
        guard let r = try? await call(YoinkArgs()) else { return }
        take(r)
    }

    /// Asks the core what the link in the box offers: its title and the
    /// sizes to pick from. The first look also fetches the pinned yt-dlp,
    /// so it may take a while.
    func look() {
        let url = link.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !url.isEmpty, !isFixture else { return }
        lookGeneration += 1
        let mine = lookGeneration
        looking = true
        problem = nil
        info = nil
        Task {
            defer { if mine == lookGeneration { looking = false } }
            do {
                let r = try await call(YoinkArgs(do: "formats", url: url), timeout: 200)
                guard mine == lookGeneration else { return }
                take(r)
                info = r.info
            } catch {
                guard mine == lookGeneration else { return }
                problem = error.localizedDescription
            }
        }
    }

    /// Takes the link on the pasteboard into the box and looks at it.
    func paste() {
        guard let s = NSPasteboard.general.string(forType: .string)?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty else {
            problem = "The pasteboard has no link on it: copy a video’s link first."
            return
        }
        link = s
        look()
    }

    /// Forgets the link looked at.
    func dismiss() {
        lookGeneration += 1
        looking = false
        info = nil
        problem = nil
    }

    /// Starts a download of the link looked at, as choice says.
    func start(_ choice: YoinkChoice) {
        guard let info, !isFixture else { return }
        start(url: info.url, get: choice.get)
    }

    /// Starts url again, as get says: Try Again on a download that failed.
    func start(url: String, get: String) {
        starting = get
        problem = nil
        Task {
            defer { starting = nil }
            do {
                let r = try await call(YoinkArgs(do: "start", url: url, get: get), timeout: 200)
                take(r)
                info = nil
                link = ""
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    func cancel(_ d: YoinkDownload) {
        Task {
            do {
                take(try await call(YoinkArgs(do: "cancel", id: d.id), timeout: 15))
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Forgets one finished download, or every one; their files stay.
    func clear(_ d: YoinkDownload? = nil) {
        Task {
            do {
                take(try await call(YoinkArgs(do: "clear", id: d?.id)))
            } catch {
                problem = error.localizedDescription
            }
        }
    }

    /// Shows the downloaded file in the Finder, as the person asked.
    func reveal(_ d: YoinkDownload) {
        guard let file = d.file, FileManager.default.fileExists(atPath: file) else {
            problem = "\(d.file.map { ($0 as NSString).lastPathComponent } ?? d.title) isn’t in the folder any more."
            return
        }
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: file)])
    }

    /// Opens the downloaded file in its own app.
    func open(_ d: YoinkDownload) {
        guard let file = d.file, FileManager.default.fileExists(atPath: file) else { return reveal(d) }
        NSWorkspace.shared.open(URL(fileURLWithPath: file))
    }

    /// Opens the folder downloads land in.
    func showFolder() {
        guard !folder.isEmpty else { return }
        NSWorkspace.shared.open(URL(fileURLWithPath: folder, isDirectory: true))
    }

    /// Copies a download's link.
    func copyLink(_ d: YoinkDownload) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(d.url, forType: .string)
    }
}

extension YoinkModel {
    /// The snapshots' downloads (Snapshot.swift): one coming down, one
    /// merging, one done and one that failed, with a link looked at when
    /// picking.
    func loadFixture(picking: Bool = false) {
        isFixture = true
        loaded = true
        folder = "/Users/alice/Downloads"
        problem = nil
        let now = Date()
        downloads = [
            YoinkDownload(id: "0a1b2c3d", url: "https://commons.wikimedia.org/wiki/File:Big_Buck_Bunny_4K.webm", title: "Big Buck Bunny 4K", site: "Wikimedia Commons",
                          get: "1080p", label: "1080p", state: "downloading", step: "Downloading the video", done: 112_400_000, total: 251_900_000,
                          speed: 4_200_000, eta: 34, started: now.addingTimeInterval(-30)),
            YoinkDownload(id: "1b2c3d4e", url: "https://vimeo.com/76979871", title: "The New Vimeo Player", site: "Vimeo",
                          get: "best", label: "Best", state: "processing", step: "Merging the video and its sound", done: 48_200_000, total: 48_200_000,
                          started: now.addingTimeInterval(-90)),
            YoinkDownload(id: "2c3d4e5f", url: "https://www.example.com/watch?v=abc123", title: "A talk on gardens", site: "YouTube",
                          get: "audio", label: "mp3", state: "done", step: "Done", done: 6_100_000, total: 6_100_000,
                          file: "/Users/alice/Downloads/A_talk_on_gardens [abc123].mp3", started: now.addingTimeInterval(-600), ended: now.addingTimeInterval(-540)),
            YoinkDownload(id: "3d4e5f60", url: "https://www.instagram.com/reel/xyz/", title: "A reel", site: "Instagram",
                          get: "best", label: "Best", state: "failed",
                          error: "The site wants you signed in to give this video, and Yoink doesn’t sign in to sites yet.",
                          started: now.addingTimeInterval(-900), ended: now.addingTimeInterval(-898)),
        ]
        if picking {
            link = "https://commons.wikimedia.org/wiki/File:Sintel_movie_4K.webm"
            info = YoinkInfo(url: link, title: "Sintel (4K)", site: "Wikimedia Commons", uploader: "Blender Foundation", duration: 888, choices: [
                YoinkChoice(get: "best", label: "Best (4K)", size: 1_431_000_000),
                YoinkChoice(get: "2160p", label: "2160p (4K)", size: 1_431_000_000),
                YoinkChoice(get: "1080p", label: "1080p", size: 402_000_000),
                YoinkChoice(get: "720p", label: "720p", size: 201_500_000),
                YoinkChoice(get: "480p", label: "480p", size: 96_000_000),
                YoinkChoice(get: "audio", label: "Audio only (mp3)", size: 14_430_000),
            ])
        } else {
            link = ""
            info = nil
        }
    }
}

extension Store {
    /// The Yoink tab's pane, while its part is on (PluginPanes.swift).
    var yoinkPane: PluginPaneInfo? { panes.first { $0.kind == "yoink" } }

    /// The Yoink plugin's part, as the core says it stands.
    var yoinkPart: PluginInfo.Part? {
        plugins.first { $0.name == "yoink" }?.parts.first { $0.pane?.kind == "yoink" }
    }

    /// Shows the Yoink tab, as :yoink asks.
    func showYoink() {
        guard let p = yoinkPane else { return }
        if !(side.open && side.tab == .plugin && side.pane == p.id) { openPane(p.id) }
        Task { await yoink.refresh() }
    }
}
#endif
