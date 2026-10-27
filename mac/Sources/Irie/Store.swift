import AppKit
import Foundation
import SwiftUI

/// The app's state: the core's rows, kept live by one subscription, and the
/// requests the window makes.
@MainActor
final class Store: ObservableObject {
    enum Link: Equatable {
        case connecting
        case online(version: String)
        case offline(String)
    }

    @Published private(set) var link: Link = .connecting
    @Published private(set) var sessions: [Session] = []
    @Published private(set) var attach: AttachInfo?
    @Published var selection: String?
    @Published var expanded: Set<String> = []
    @Published var notice: Notice?
    @Published var spawning = false
    /// Bumped when the terminal should take keyboard focus.
    @Published var focusTerminal = 0
    /// Bumped to move the keyboard to the session list or the find field.
    @Published var focusSidebar = 0
    @Published var focusFind = 0
    /// The sidebar's filter, which the keys that walk the sidebar follow.
    @Published var filter = ""
    @Published private(set) var fontSize: CGFloat = Store.savedFontSize()
    /// The terminal view, to tell whether it has the keyboard.
    weak var terminalView: NSView?
    /// Whether the terminal has the keyboard, which brightens its frame.
    @Published var terminalHasKeys = false
    /// The Claude Code sessions outside Irie, the one selected, the one a
    /// move asks about, and the one moving (OutsideSection.swift).
    @Published var outside: [OutsideSession] = []
    @Published var outsideSelection: String?
    @Published var confirmingMove: OutsideSession?
    @Published var moving: String?
    var outsideProblem = ""

    static let defaultFontSize: CGFloat = 13

    struct Notice: Equatable, Identifiable {
        let id = UUID()
        var text: String
        var isError: Bool
    }

    let client: CoreClient
    private var started = false

    init(socketPath: String = CorePaths.socket()) {
        client = CoreClient(socketPath: socketPath)
    }

    var socketPath: String { client.socketPath }

    // MARK: Connection

    func start() {
        guard !started else { return }
        started = true
        Task { await run() }
    }

    /// Connects, subscribes and stays subscribed; on any failure it waits and
    /// tries again, so starting Irie later just works.
    private func run() async {
        while true {
            do {
                let hello = try await client.call("hello", HelloArgs(), timeout: 2, as: HelloResult.self)
                guard hello.features.contains("attach-info") else {
                    link = .offline("This Irie is too old for the app. Run the new build's `irie up` to update it.")
                    try await Task.sleep(nanoseconds: 3_000_000_000)
                    continue
                }
                attach = try await client.call("attach-info", Optional<Empty>.none, timeout: 2, as: AttachInfo.self)
                link = .online(version: hello.version)
                // The store lives as long as the app.
                try await client.subscribe { frame in
                    Task { @MainActor in self.apply(frame) }
                }
                link = .offline("The core went away.")
            } catch {
                link = .offline(Self.describe(error))
            }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
        }
    }

    private static func describe(_ error: Error) -> String {
        if case let CoreError.system(call, code) = error, call == "connect", code == ENOENT || code == ECONNREFUSED {
            return "Irie isn’t running."
        }
        return error.localizedDescription
    }

    private func apply(_ f: Frame) {
        switch f.t {
        case "snapshot":
            sessions = (f.sessions ?? []).sorted(by: Self.before)
        case "upsert":
            guard let s = f.session else { return }
            if let i = sessions.firstIndex(where: { $0.id == s.id }) {
                sessions[i] = s
            } else {
                sessions.append(s)
                sessions.sort(by: Self.before)
            }
        case "remove":
            sessions.removeAll { $0.id == f.id }
        case "outside":
            applyOutside(f)
            return
        default:
            return
        }
        if selection == nil, let shown = sessions.first(where: { $0.shown == true }) {
            selection = shown.id
        }
        if let sel = selection, !sessions.contains(where: { $0.id == sel }) {
            selection = nil
        }
    }

    private static func before(_ a: Session, _ b: Session) -> Bool {
        a.created != b.created ? a.created < b.created : a.id < b.id
    }

    // MARK: Keyboard

    /// The rows in the order the sidebar draws them: the order they started,
    /// which a state change never alters (DESIGN §4.1), without the rows the
    /// filter hides.
    var drawnOrder: [Session] {
        let f = filter.trimmingCharacters(in: .whitespaces).lowercased()
        return f.isEmpty ? sessions : sessions.filter { $0.name.lowercased().contains(f) }
    }

    /// Shows the next (+1) or previous (-1) row of the drawn order, wrapping
    /// at the ends unless told not to. The keyboard stays where it is.
    func step(_ delta: Int, wrap: Bool = true) {
        let order = drawnOrder
        guard !order.isEmpty else { return }
        var next = delta > 0 ? 0 : order.count - 1
        if let i = order.firstIndex(where: { $0.id == selection }) {
            next = wrap ? ((i + delta) % order.count + order.count) % order.count : min(max(i + delta, 0), order.count - 1)
        }
        if order[next].id != selection { show(order[next].id) }
    }

    /// Shows the nth row (from 1) of the drawn order.
    func showRow(_ n: Int) {
        let order = drawnOrder
        guard n >= 1, n <= order.count else { return }
        show(order[n - 1].id)
    }

    /// Moves the keyboard between the session list and the terminal.
    func toggleFocus() {
        if let t = terminalView, t.window?.firstResponder === t {
            focusSidebar += 1
        } else {
            focusTerminal += 1
        }
    }

    func setFontSize(_ size: CGFloat) {
        fontSize = min(max(size, 9), 28)
        UserDefaults.standard.set(Double(fontSize), forKey: "terminalFontSize")
    }

    private static func savedFontSize() -> CGFloat {
        let v = UserDefaults.standard.double(forKey: "terminalFontSize")
        return v >= 9 && v <= 28 ? CGFloat(v) : defaultFontSize
    }

    // MARK: Requests

    /// Swaps the session into the slot, which the terminal shows.
    func show(_ id: String, focus: Bool = false) {
        selection = id
        outsideSelection = nil
        Task {
            do {
                _ = try await client.call("show", ShowArgs(session: id, focus: focus), as: Empty.self)
                if focus { focusTerminal += 1 }
            } catch {
                say("Couldn’t show it: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Starts a task typed in the task bar. Returns whether it started.
    func spawn(_ input: String) async -> Bool {
        let args: SpawnArgs
        do {
            args = try TaskParser.parse(input, home: NSHomeDirectory())
        } catch {
            say(error.localizedDescription, error: true)
            return false
        }
        spawning = true
        defer { spawning = false }
        do {
            // A task in a repo may make a worktree and check out submodules first.
            let r = try await client.call("spawn", args, timeout: 600, as: SpawnResult.self)
            say("Started \(r.session.name).", error: false)
            // Straight to the new agent, ready to type to.
            show(r.session.id, focus: true)
            return true
        } catch {
            say(error.localizedDescription, error: true)
            return false
        }
    }

    /// Shows a notice above the task bar and has VoiceOver read it. A
    /// confirmation goes after 3 s; an error stays until the next notice, or
    /// until the user edits or submits (WCAG 2.2.1).
    func say(_ text: String, error: Bool) {
        notice = Notice(text: text, isError: error)
        AccessibilityNotification.Announcement(text).post()
        guard !error else { return }
        let id = notice?.id
        Task {
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            if notice?.id == id { notice = nil }
        }
    }

    func dismissNotice() { notice = nil }

    // MARK: Desktop actions

    func copy(_ text: String, what: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        say("Copied the \(what).", error: false)
    }

    func reveal(_ path: String) {
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }

    /// Opens the slot in a Ghostty window: the terminal for builds without
    /// SwiftTerm, and a second view for builds with it.
    func openInGhostty() {
        guard let a = attach else { return }
        guard NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.mitchellh.ghostty") != nil else {
            say("Ghostty isn’t installed. In any terminal, run: \(a.tmux) -S \(a.socket) attach-session -t \(a.target)", error: true)
            return
        }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        p.arguments = ["-na", "Ghostty", "--args", "--title=Irie", "-e", a.tmux, "-S", a.socket, "attach-session", "-t", a.target]
        do {
            try p.run()
        } catch {
            say("Couldn’t open Ghostty: \(error.localizedDescription)", error: true)
        }
    }
}
