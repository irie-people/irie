import AppKit
import Darwin
import SwiftUI

// Pop out: the toolbar's window button, View > Pop Out Terminal (⌥⌘O) or a
// card's Open in Ghostty moves the terminal out of Irie's window into a
// Ghostty window of its own, and the same button or key, the note left in
// its place, or closing that window pops it back in. The Ghostty window is a
// tmux client of the slot, as the app's own terminal is, so it shows the
// session picked in the sidebar and follows the pick.
//
// Only one of the two is attached at a time. tmux gives the slot the size of
// its latest client (window-size latest), and the core gives every agent the
// slot's size, so with both attached each key typed in one after the other
// would resize every agent. While the terminal is out the app's own terminal
// is gone, and with no split it shows the session alone. The Ghostty window
// opens at the slot's size in cells, in the terminal's colours and text
// size, so popping out resizes no agent while the window fits on the screen.
//
// Ghostty runs the client with -e, which makes it quit with its last window.
// Popping back in detaches only that window's client, so the window closes
// and Ghostty quits; its end, however it comes, tells the app the terminal
// is back. A pop out outlives the app: the next launch, as tools/live.sh
// makes, finds its Ghostty still running and stays popped out.

/// The Ghostty the terminal is popped out into: its process and when it
/// started, which together name it even once the number is used again.
struct PoppedOut: Equatable {
    var pid: pid_t
    var launched: Date
}

enum PopOut {
    static let ghostty = "com.mitchellh.ghostty"
    private static let pidKey = "popOutPID"
    private static let launchedKey = "popOutLaunched"

    /// The pop out a previous launch left, while its Ghostty still runs.
    static func saved() -> PoppedOut? {
        let d = UserDefaults.standard
        let pid = pid_t(truncatingIfNeeded: d.integer(forKey: pidKey))
        let at = d.double(forKey: launchedKey)
        guard pid > 0, at > 0 else { return nil }
        let p = PoppedOut(pid: pid, launched: Date(timeIntervalSince1970: at))
        guard app(p) != nil else {
            save(nil)
            return nil
        }
        return p
    }

    static func save(_ p: PoppedOut?) {
        let d = UserDefaults.standard
        if let p {
            d.set(Int(p.pid), forKey: pidKey)
            d.set(p.launched.timeIntervalSince1970, forKey: launchedKey)
        } else {
            d.removeObject(forKey: pidKey)
            d.removeObject(forKey: launchedKey)
        }
    }

    /// Its Ghostty, while that runs: a Ghostty with its process number that
    /// started when it did.
    static func app(_ p: PoppedOut) -> NSRunningApplication? {
        guard let app = NSRunningApplication(processIdentifier: p.pid), !app.isTerminated,
              app.bundleIdentifier == ghostty else { return nil }
        if let at = app.launchDate, abs(at.timeIntervalSince(p.launched)) >= 2 { return nil }
        return app
    }

    /// What Ghostty starts with: the window's title and its size in cells,
    /// the terminal's colours and text size, and nothing that asks before
    /// the window closes, since closing it ends only a tmux client, not an
    /// agent. As in the app's own terminal, nothing inside may read the
    /// clipboard. The window state is never saved, so the user's own Ghostty
    /// never brings this window back. After -e comes the client, which a
    /// preview starts sizing nothing (Preview.swift).
    nonisolated static func arguments(_ a: AttachInfo, cells: (cols: Int, rows: Int)?, fontSize: CGFloat, colours: TerminalColors) -> [String] {
        var args = [
            "--title=\(Brand.name) terminal",
            "--font-size=" + String(format: "%g", Double(fontSize)),
            "--background=" + String(format: "%06X", colours.ground),
            "--foreground=" + String(format: "%06X", colours.ink),
            "--confirm-close-surface=false",
            "--wait-after-command=false",
            "--window-save-state=never",
            "--clipboard-read=deny",
        ]
        if let cells { args += ["--window-width=\(cells.cols)", "--window-height=\(cells.rows)"] }
        return args + ["-e", a.tmux, "-S", a.socket, "attach-session"] + Preview.clientFlags + ["-t", a.target]
    }

    /// The slot's size in cells, as tmux has it now.
    nonisolated static func cells(_ a: AttachInfo) -> (cols: Int, rows: Int)? {
        guard let out = tmux(a, ["display-message", "-p", "-t", a.target, "#{window_width} #{window_height}"]) else { return nil }
        let v = out.split(whereSeparator: \.isWhitespace).compactMap { Int($0) }
        guard v.count == 2, v[0] >= 10, v[1] >= 4 else { return nil }
        return (v[0], v[1])
    }

    /// The tty of the tmux client that runs in the Ghostty whose process is
    /// pid: the client whose process descends from it.
    nonisolated static func client(of pid: pid_t, _ a: AttachInfo) -> String? {
        guard let out = tmux(a, ["list-clients", "-F", "#{client_pid} #{client_tty}"]) else { return nil }
        for line in out.split(separator: "\n") {
            let f = line.split(separator: " ", maxSplits: 1)
            if f.count == 2, let c = pid_t(f[0]), descends(c, from: pid) { return String(f[1]) }
        }
        return nil
    }

    /// Detaches one client, which ends it.
    nonisolated static func detach(_ tty: String, _ a: AttachInfo) {
        _ = tmux(a, ["detach-client", "-t", tty])
    }

    /// Whether process pid descends from ancestor: Ghostty starts the
    /// client through login.
    nonisolated static func descends(_ pid: pid_t, from ancestor: pid_t) -> Bool {
        var p = pid
        for _ in 0..<12 {
            if p == ancestor { return true }
            guard p > 1 else { return false }
            var info = proc_bsdinfo()
            let size = Int32(MemoryLayout<proc_bsdinfo>.size)
            guard proc_pidinfo(p, PROC_PIDTBSDINFO, 0, &info, size) == size else { return false }
            p = pid_t(truncatingIfNeeded: info.pbi_ppid)
        }
        return false
    }

    /// Runs a tmux command on Irie's server and returns what it printed, or
    /// nil when it failed.
    nonisolated private static func tmux(_ a: AttachInfo, _ args: [String]) -> String? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: a.tmux)
        p.arguments = ["-S", a.socket] + args
        var env = ProcessInfo.processInfo.environment
        env["TMUX"] = nil
        env["TMUX_PANE"] = nil
        p.environment = env
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        do { try p.run() } catch { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return p.terminationStatus == 0 ? String(decoding: data, as: UTF8.self) : nil
    }
}

extension Store {
    /// The toolbar's window button and View > Pop Out Terminal (⌥⌘O).
    func togglePopOut() {
        if poppedOut != nil { popBackIn() } else { popOut() }
    }

    /// Pops the terminal out into a Ghostty window of its own, or, with it
    /// out already, brings that window to the front.
    func popOut() {
        if let p = poppedOut {
            PopOut.app(p)?.activate(options: [])
            return
        }
        guard !popping, let a = attach else { return }
        guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: PopOut.ghostty) else {
            say("Popping out needs Ghostty (ghostty.org), which isn’t installed. In any terminal, \(a.tmux) -S \(a.socket) attach-session -t \(a.target) shows the same terminal.", error: true)
            return
        }
        popping = true
        let size = fontSize, colours = Theme.terminalColors
        Task {
            // A Ghostty of its own, which -e makes quit with its window.
            let opened = await Task.detached { () -> Result<PoppedOut?, Error> in
                let config = NSWorkspace.OpenConfiguration()
                config.createsNewApplicationInstance = true
                config.addsToRecentItems = false
                config.arguments = PopOut.arguments(a, cells: PopOut.cells(a), fontSize: size, colours: colours)
                do {
                    let app = try await NSWorkspace.shared.openApplication(at: url, configuration: config)
                    return .success(app.isTerminated ? nil : PoppedOut(pid: app.processIdentifier, launched: app.launchDate ?? Date()))
                } catch {
                    return .failure(error)
                }
            }.value
            popping = false
            switch opened {
            case let .success(p?):
                poppedOut = p
                if !isFixture { PopOut.save(p) }
                keepSplit()
                // One that quit before the app heard of it.
                if PopOut.app(p) == nil { endPopOut() }
            case .success(nil):
                say("Ghostty closed as it opened, so the terminal stays here.", error: true)
            case let .failure(error):
                say("Couldn’t open Ghostty: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Pops the terminal back in: the app's own terminal attaches again, and
    /// the Ghostty window's client detaches, so the window closes and Ghostty
    /// quits. A Ghostty still starting, whose client hasn't attached yet, is
    /// told to quit instead; one that has run a while may hold windows the
    /// user opened in it since.
    func popBackIn() {
        guard let p = poppedOut else { return }
        guard let a = attach else {
            say("The terminal comes back once \(Brand.name) is connected. Closing its Ghostty window brings it back too.", error: true)
            return
        }
        endPopOut()
        let starting = Date().timeIntervalSince(p.launched) < 15
        Task.detached {
            if let tty = PopOut.client(of: p.pid, a) {
                PopOut.detach(tty, a)
            } else if starting {
                await MainActor.run { _ = PopOut.app(p)?.terminate() }
            }
        }
    }

    /// The terminal is back in the window, whichever way it came back.
    func endPopOut() {
        guard poppedOut != nil else { return }
        poppedOut = nil
        if !isFixture { PopOut.save(nil) }
        keepSplit()
    }

    /// Pops the terminal back in when its Ghostty quits, as it does once its
    /// window closes, or quit while the app was away.
    func watchPopOut() {
        NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didTerminateApplicationNotification,
                                                          object: nil, queue: .main) { [weak self] note in
            guard let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication else { return }
            let pid = app.processIdentifier
            MainActor.assumeIsolated {
                guard let self, self.poppedOut?.pid == pid else { return }
                self.endPopOut()
            }
        }
        if let p = poppedOut, PopOut.app(p) == nil { endPopOut() }
    }
}

/// What the terminal's frame holds while the terminal is popped out: where
/// it went, and the ways back to it. The frame's ground is always dark.
@MainActor
struct PoppedOutNote: View {
    @Environment(Store.self) private var store

    var body: some View {
        let ink = Color(nsColor: Theme.terminalForeground)
        VStack(spacing: 12) {
            Image(systemName: "macwindow.on.rectangle")
                .font(.system(size: 30, weight: .light))
                .foregroundStyle(ink.opacity(0.75))
                .accessibilityHidden(true)
            Text("Popped out")
                .font(.system(size: 17, weight: .semibold, design: .rounded))
                .foregroundStyle(ink)
            Text("The terminal is in its own Ghostty window, where it follows the session you pick.")
                .font(.system(size: 13))
                .foregroundStyle(ink.opacity(0.75))
                .multilineTextAlignment(.center)
                .frame(maxWidth: 360)
            HStack(spacing: 10) {
                Button("Pop Back In") { store.popBackIn() }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.deepGreen)
                    .help("Bring the terminal back into this window (⌥⌘O)")
                Button("Show Window") { store.popOut() }
                    .buttonStyle(.bordered)
                    .help("Bring the Ghostty window to the front")
            }
            .controlSize(.large)
            .padding(.top, 4)
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .environment(\.colorScheme, .dark)
    }
}
