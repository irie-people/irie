import AppKit
import Darwin
import SwiftUI

/// A preview of a checkout's app, which `tools/hot.sh` runs beside the
/// installed one and replaces with a new build a few seconds after each
/// save under mac/ (docs/MAC.md, "Hot loading"). It is the same app, a
/// client of the running core as the installed one is, with these
/// differences:
///
/// - Every tmux client it starts attaches with `ignore-size`, so it never
///   sizes a window: a preview beside the app never resizes the slot or an
///   agent (DESIGN §2.1, principle 6).
/// - It leaves the palette the agents' colours follow to the installed app.
/// - The phrase bar says it is a preview, of which checkout, and how the
///   next build is going (PreviewBadge).
/// - A new build keeps its window out of sight until it has drawn, then
///   gives it the frame of the window of the build before and ends that
///   build, so a rebuild looks like the window changing in place
///   (PreviewState).
/// - It quits when the hot.sh that runs it ends.
///
/// The installed app gets none of hot.sh's arguments, and none of this.
enum Preview {
    /// The checkout's name, from `--preview NAME`; nil in the installed app.
    static let name = argument("--preview")
    static var isOn: Bool { name != nil }
    /// hot.sh's folder, whose `status` says how the next build goes.
    static let folder = argument("--hot")
    /// The build this one takes over from.
    static let replaces = argument("--replace").flatMap { pid_t($0) }
    /// The hot.sh that runs the preview.
    static let owner = argument("--hot-pid").flatMap { pid_t($0) }

    /// What every tmux client the app starts adds to `attach-session`.
    static var clientFlags: [String] { isOn ? ["-f", "ignore-size"] : [] }

    /// Called first thing. hot.sh shows a preview's output, print() too,
    /// and gets it a line at a time, as a terminal would.
    static func boot() {
        guard isOn else { return }
        setvbuf(stdout, nil, _IOLBF, 0)
    }

    private static func argument(_ flag: String) -> String? {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: flag), i + 1 < args.count else { return nil }
        return args[i + 1]
    }
}

/// A preview's next build, as hot.sh reports it, the hand-over from the
/// build before, and the end with hot.sh.
@MainActor
final class PreviewState: ObservableObject {
    static let shared = PreviewState()

    enum Build: Equatable {
        case unknown
        case building
        case built(String) // the time it finished
        case failed(String) // its first error
    }

    @Published private(set) var build: Build = .unknown
    private var window: NSWindow?
    private var poll: Timer?
    private var ownerWatch: DispatchSourceProcess?

    /// Takes the window once it is there: from then on the preview follows
    /// hot.sh, and a build that replaces another takes over its window.
    func adopt(_ window: NSWindow, store: Store) {
        guard Preview.isOn, self.window == nil else { return }
        self.window = window
        watchOwner()
        readStatus()
        poll = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.readStatus() }
        }
        guard let old = Preview.replaces, kill(old, 0) == 0 else { return }
        window.alphaValue = 0
        Task { await takeOver(from: old, window: window, store: store) }
    }

    /// Quits when hot.sh ends, however it ends.
    private func watchOwner() {
        guard let pid = Preview.owner else { return }
        let watch = DispatchSource.makeProcessSource(identifier: pid, eventMask: .exit, queue: .main)
        watch.setEventHandler { MainActor.assumeIsolated { NSApp.terminate(nil) } }
        watch.resume()
        ownerWatch = watch
        if kill(pid, 0) != 0, errno == ESRCH { NSApp.terminate(nil) }
    }

    /// Shows the window once it has drawn the sessions, in the frame of the
    /// window of the build before, then ends that build. A core that does
    /// not answer leaves the window to show what it has. The window keeps
    /// the place in front or behind that it opened in, behind the app in
    /// front: ordering it against another app's window would put it in
    /// front of every window.
    private func takeOver(from old: pid_t, window: NSWindow, store: Store) async {
        var waited = 0
        while store.sessions.isEmpty, waited < 40 {
            try? await Task.sleep(nanoseconds: 100_000_000)
            waited += 1
        }
        // The terminal's first frame.
        try? await Task.sleep(nanoseconds: 500_000_000)
        let front = NSRunningApplication(processIdentifier: old)?.isActive ?? false
        if let frame = Self.largestWindow(of: old) {
            window.setFrame(frame, display: true)
        }
        window.alphaValue = 1
        if front {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
        }
        kill(old, SIGTERM)
    }

    /// The frame, in AppKit's coordinates, of a process's largest window on
    /// screen. Bounds and owners need no screen-recording permission; only
    /// titles would.
    private static func largestWindow(of pid: pid_t) -> NSRect? {
        guard let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]],
              let main = NSScreen.screens.first else { return nil }
        var best: NSRect?
        for w in list {
            guard (w[kCGWindowOwnerPID as String] as? Int) == Int(pid),
                  (w[kCGWindowLayer as String] as? Int) == 0,
                  let bounds = w[kCGWindowBounds as String] as? NSDictionary,
                  let r = CGRect(dictionaryRepresentation: bounds as CFDictionary) else { continue }
            // Quartz counts from the top of the main screen, AppKit from its
            // bottom.
            let frame = NSRect(x: r.minX, y: main.frame.maxY - r.maxY, width: r.width, height: r.height)
            if best.map({ $0.width * $0.height < frame.width * frame.height }) ?? true {
                best = frame
            }
        }
        return best
    }

    /// hot.sh keeps one line in `status`: `building`, `ok TIME` or
    /// `failed MESSAGE`.
    private func readStatus() {
        guard let dir = Preview.folder,
              let text = try? String(contentsOfFile: (dir as NSString).appendingPathComponent("status"), encoding: .utf8)
        else { return }
        let line = text.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? ""
        let word = line.prefix { $0 != " " }
        let rest = line.dropFirst(word.count).trimmingCharacters(in: .whitespaces)
        let next: Build
        switch word {
        case "building": next = .building
        case "ok": next = .built(rest)
        case "failed": next = .failed(rest)
        default: return
        }
        if next != build { build = next }
    }
}

/// A preview's line in the phrase bar, where the app has its phrase: how
/// its next build goes, in red when it failed, with the whole error on
/// hover; and on the right, where the app has the core's version, the
/// checkout it shows, unless the line needs the room.
struct PreviewBadge: View {
    @ObservedObject private var state = PreviewState.shared

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 6) {
                status
                Spacer(minLength: 12)
                Text(Preview.name ?? "")
                    .font(Theme.font(10.5))
                    .foregroundStyle(Theme.muted.opacity(0.7))
            }
            HStack(spacing: 6) {
                status
                Spacer(minLength: 0)
            }
        }
        .lineLimit(1)
        .help(help)
    }

    private var status: some View {
        HStack(spacing: 6) {
            Circle().fill(ink).frame(width: 6, height: 6)
            Text(note)
                .font(Theme.font(12, .semibold))
                .foregroundStyle(ink)
                .truncationMode(.tail)
        }
    }

    private var ink: Color {
        if case .failed = state.build { return Theme.needs }
        return Theme.accent
    }

    private var note: String {
        switch state.build {
        case .unknown: return "Preview"
        case .building: return "Preview · building…"
        case let .built(at): return at.isEmpty ? "Preview" : "Preview · built " + at
        case let .failed(why): return "Build failed: " + why
        }
    }

    private var help: String {
        let what = "A preview of \(Preview.name ?? "a checkout"), which tools/hot.sh builds again a few seconds after each save. Its sessions are the real ones."
        if case let .failed(why) = state.build { return why + "\n\n" + what }
        return what
    }
}
