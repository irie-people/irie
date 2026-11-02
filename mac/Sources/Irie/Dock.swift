import AppKit
import SwiftUI
import WebKit

// The right sidebar's Dock tab (docs/proposals/right-dock.md §5.2): controls
// the user keeps an eye on, as cmux's Dock has them. A terminal control's
// command runs in a tmux session of its own on the core's dock server, in
// the shown session's folder, and shows here as a client of it, so it never
// resizes an agent. A web control shows a page in a web view that keeps no
// data and loads only from its page's own origin. The controls come from
// the repo's .irie/dock.json (or cmux's .cmux/dock.json), which runs nothing
// until the user trusts it, and from the user's own dock.json.

struct DockArgs: Encodable {
    var session: String
    var start: String?
    var stop: String?
    var restart: String?
    var ensure: Bool?
    var trust: String?
}

struct DockResult: Decodable, Equatable {
    var controls: [DockControl]
    var tmux: String?
    var socket: String?
    var files: [DockFileInfo]
    var untrusted: DockUntrusted?
    var mine: String
}

struct DockControl: Decodable, Equatable, Identifiable {
    var key: String
    var id: String
    var title: String
    var type: String
    var url: String?
    var chrome: Bool
    var height: Double?
    var source: String
    var scope: String
    var state: String
    var exit: Int?
    var target: String?
    var cwd: String?
    var command: String?

    var web: Bool { type == "browser" }
}

struct DockFileInfo: Decodable, Equatable {
    var path: String
    var source: String
    var trusted: Bool
    var problem: String?
}

struct DockUntrusted: Decodable, Equatable {
    var path: String
    var hash: String
    var commands: [String]
}

extension Store {
    func dock(_ args: DockArgs) async throws -> DockResult {
        try await client.call("dock", args, timeout: 20, as: DockResult.self)
    }
}

/// What the snapshots show in the Dock, with no core.
enum DockFixture {
    static let result = DockResult(controls: [
        DockControl(key: "repo:git", id: "git", title: "Git", type: "terminal", chrome: true, height: 170, source: "repo",
                    scope: "session", state: "running", target: "dock-r-git-fixture", command: "lazygit"),
        DockControl(key: "repo:tests", id: "tests", title: "Tests", type: "terminal", chrome: true, source: "repo",
                    scope: "session", state: "running", target: "dock-r-tests-fixture", command: "go test ./... -json | tparse -follow"),
        DockControl(key: "mine:app", id: "app", title: "App", type: "browser", url: "http://localhost:8080/", chrome: true,
                    source: "mine", scope: "session", state: "ready"),
    ], files: [DockFileInfo(path: "/home/alice/src/website/.irie/dock.json", source: "repo", trusted: true)],
    mine: "/home/alice/.config/irie/dock.json")

    static let lines: [String: [String]] = [
        "repo:git": ["fix-login-test  ↑2 on live", " M internal/auth/login_test.go", " M internal/auth/session.go",
                     "a1b2c3d Wait for the session write", "9f8e7d6 Add a helper that reads it back"],
        "repo:tests": ["watching ./internal/auth …", "ok   app/internal/auth  1.204s", "ok   app/internal/web   0.381s"],
    ]
}

/// The Dock tab.
struct DockTab: View {
    @EnvironmentObject private var store: Store
    @State private var result: DockResult?
    @State private var shownFor: String?
    @State private var problem: String?
    @State private var working = false
    /// Repo files the user said Not Now to, by hash, this run.
    @State private var notNow: Set<String> = []

    private var selected: Session? {
        store.outsideSelection == nil ? store.sessions.first { $0.id == store.selection } : nil
    }

    var body: some View {
        Group {
            if let s = selected {
                if store.isFixture {
                    dock(DockFixture.result, session: s)
                } else if !store.canDock {
                    SideNote(title: "The Dock needs a newer Irie", text: "This Irie's core has no Dock yet. It comes with the next install.")
                } else if let r = result, shownFor == s.id {
                    dock(r, session: s)
                } else if let problem {
                    SideNote(title: "The Dock didn't load", text: problem)
                } else {
                    ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            } else {
                SideNote(title: "Pick a session", text: "Its Dock shows here: the commands and pages you keep an eye on, run in its folder.")
            }
        }
        .task(id: store.selection) { await follow() }
    }

    // MARK: Loading

    /// Loads the shown session's Dock, starts its trusted terminal controls
    /// that have no copy (but those the user stopped), and keeps its states
    /// fresh while the tab shows.
    private func follow() async {
        problem = nil
        guard let id = selected?.id else { return }
        guard store.canDock, !store.isFixture else { return }
        await load(id, startMissing: true)
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 4_000_000_000)
            if Task.isCancelled { return }
            await load(id, startMissing: false)
        }
    }

    private func load(_ id: String, startMissing: Bool) async {
        do {
            var r = try await store.dock(DockArgs(session: id))
            if startMissing {
                let stopped = store.dockStopped[id] ?? []
                let missing = r.controls.filter { !$0.web && $0.state == "stopped" && !stopped.contains($0.key) }
                for c in missing {
                    _ = try? await store.dock(DockArgs(session: id, start: c.key))
                }
                if !missing.isEmpty { r = try await store.dock(DockArgs(session: id)) }
            }
            guard store.selection == id else { return }
            if r != result { result = r }
            shownFor = id
            problem = nil
        } catch {
            guard store.selection == id else { return }
            problem = error.localizedDescription
        }
    }

    /// Runs one change, then shows the Dock as it is after it.
    private func act(_ args: DockArgs) {
        let id = args.session
        working = true
        Task {
            defer { working = false }
            do {
                let r = try await store.dock(args)
                if store.selection == id {
                    result = r
                    shownFor = id
                }
            } catch {
                store.say("The Dock: \(error.localizedDescription)", error: true)
            }
        }
    }

    private func start(_ c: DockControl, _ s: Session) {
        store.dockStopped[s.id, default: []].remove(c.key)
        act(DockArgs(session: s.id, start: c.key))
    }

    private func stop(_ c: DockControl, _ s: Session) {
        store.dockStopped[s.id, default: []].insert(c.key)
        act(DockArgs(session: s.id, stop: c.key))
    }

    private func restart(_ c: DockControl, _ s: Session) {
        store.dockStopped[s.id, default: []].remove(c.key)
        act(DockArgs(session: s.id, restart: c.key))
    }

    // MARK: Drawing

    @ViewBuilder private func dock(_ r: DockResult, session s: Session) -> some View {
        let trust = r.untrusted.flatMap { notNow.contains($0.hash) ? nil : $0 }
        let shown = r.controls.filter { $0.state != "untrusted" }
        VStack(spacing: 8) {
            if let u = trust { trustCard(u, session: s) }
            ForEach(r.files.filter { $0.problem != nil }, id: \.path) { f in
                Label("\((f.path as NSString).lastPathComponent) doesn't read: \(f.problem ?? "")", systemImage: "exclamationmark.triangle")
                    .font(Theme.font(11))
                    .foregroundStyle(Theme.accentInk)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .help(f.path)
            }
            if shown.isEmpty {
                if trust == nil { empty(r, session: s) }
                Spacer(minLength: 0)
            } else {
                ForEach(shown) { c in
                    DockControlView(control: c, result: r, session: s,
                                    start: { start(c, s) }, stop: { stop(c, s) }, restart: { restart(c, s) })
                        .frame(height: c.height.flatMap { $0 > 0 ? CGFloat($0) : nil })
                        .frame(maxHeight: (c.height ?? 0) > 0 ? nil : .infinity)
                }
            }
        }
        .padding(10)
        .disabled(working)
    }

    private func trustCard(_ u: DockUntrusted, session s: Session) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("This repo's Dock wants to run these on your Mac", systemImage: "checkmark.shield")
                .font(Theme.font(12.5, .bold))
                .foregroundStyle(Theme.ink)
            VStack(alignment: .leading, spacing: 3) {
                ForEach(u.commands, id: \.self) { c in
                    Text(c)
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(Theme.ink)
                        .textSelection(.enabled)
                        .lineLimit(3)
                }
            }
            .padding(8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.field))
            Text("From \((u.path as NSString).abbreviatingWithTildeInPath). Irie asks again whenever the file changes, so an agent's edit never runs before you look.")
                .font(Theme.font(11))
                .foregroundStyle(Theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                Button("Trust and Run") { act(DockArgs(session: s.id, ensure: true, trust: u.hash)) }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.accent)
                Button("Not Now") { notNow.insert(u.hash) }
                Spacer(minLength: 0)
            }
            .controlSize(.small)
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.card))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.edge))
        .accessibilityElement(children: .contain)
    }

    private func empty(_ r: DockResult, session s: Session) -> some View {
        VStack(spacing: 10) {
            Text("No Dock yet")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text("The commands and pages you keep an eye on, such as a git view, a log, a test watcher or the page your app serves, run here in the session's folder.")
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Button("Set Up My Dock…") { setUp(s) }
                .buttonStyle(.borderedProminent)
                .tint(Theme.accent)
                .help("Starts an agent that reads this repo and writes its Dock file, .irie/dock.json, asking you what to watch when unsure")
            Button("Open My Dock File") { openMine(r.mine) }
                .help("Your own controls, for every session: \((r.mine as NSString).abbreviatingWithTildeInPath)")
        }
        .controlSize(.small)
        .frame(maxWidth: 280)
        .padding(.top, 24)
        .frame(maxWidth: .infinity)
    }

    /// Starts an agent that writes the repo's Dock file.
    private func setUp(_ s: Session) {
        let task = "Set up Irie's Dock for this repo. Irie's Dock is the right sidebar's control area, and its file has the same shape as cmux's Dock: read https://cmux.com/docs/dock for the fields. Look at this repo's scripts, dev servers, logs, tests and tools, then write .irie/dock.json with up to four controls that are worth watching, each a terminal command or a local web page. If it is not clear what to watch, ask me first. Never put secrets in the file. Commit it when it is done."
        Task {
            _ = await store.input(task, repo: s.repo ?? s.cwd, worktree: true, agent: nil, model: nil, tutor: nil)
        }
    }

    /// Opens the user's own Dock file, writing a first control in it when
    /// there is none yet.
    private func openMine(_ path: String) {
        let url = URL(fileURLWithPath: path)
        if !FileManager.default.fileExists(atPath: path) {
            let first = """
            {
              "controls": [
                {"id": "status", "title": "Git status", "command": "while true; do clear; git status --short --branch; sleep 5; done"}
              ]
            }

            """
            try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try? first.write(to: url, atomically: true, encoding: .utf8)
        }
        NSWorkspace.shared.open(url)
    }
}

/// One control: a header with its title and buttons, over its terminal or
/// its page.
private struct DockControlView: View {
    @EnvironmentObject private var store: Store
    let control: DockControl
    let result: DockResult
    let session: Session
    let start: () -> Void
    let stop: () -> Void
    let restart: () -> Void
    @State private var reload = 0

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: 10, style: .continuous) }

    var body: some View {
        VStack(spacing: 0) {
            header
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .clipShape(shape)
        .overlay(shape.strokeBorder(Theme.edge))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(control.title), \(said)")
    }

    private var said: String {
        switch control.state {
        case "running": return "running"
        case "exited": return control.exit.map { $0 == 0 ? "ended" : "ended with code \($0)" } ?? "ended"
        case "stopped": return "stopped"
        default: return control.web ? "a page" : control.state
        }
    }

    private var header: some View {
        HStack(spacing: 6) {
            Text(control.title)
                .font(Theme.font(12, .bold))
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
            Text(control.web ? (control.chrome ? host : "") : (control.state == "running" ? (control.command ?? "") : said))
                .font(control.web ? .system(size: 10.5, design: .monospaced) : Theme.font(11))
                .foregroundStyle(control.state == "exited" && (control.exit ?? 0) != 0 ? Theme.accentInk : Theme.muted)
                .lineLimit(1)
                .truncationMode(.tail)
                .help(control.web ? (control.url ?? "") : (control.command ?? ""))
            Spacer(minLength: 4)
            if control.web {
                icon("arrow.clockwise", help: "Reload the page") { reload += 1 }
                icon("arrow.up.right.square", help: "Open the page in your browser") {
                    if let u = control.url.flatMap(URL.init(string:)) { NSWorkspace.shared.open(u) }
                }
            } else {
                icon("arrow.clockwise", help: "Restart \(control.title)") { restart() }
                if control.state == "running" {
                    icon("stop.fill", help: "Stop \(control.title); it stays stopped until you start it") { stop() }
                } else {
                    icon("play.fill", help: "Start \(control.title)") { start() }
                }
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(Theme.card)
    }

    private var host: String {
        guard let u = control.url.flatMap(URL.init(string:)) else { return control.url ?? "" }
        return (u.host ?? "") + (u.port.map { ":\($0)" } ?? "") + (u.path == "/" ? "" : u.path)
    }

    @ViewBuilder private var content: some View {
        if control.web {
            if store.isFixture {
                FixturePage()
            } else if let u = control.url.flatMap(URL.init(string:)) {
                DockWeb(url: u, reload: reload)
            }
        } else if store.isFixture {
            FixtureScreen(lines: DockFixture.lines[control.key] ?? [])
                .padding(6)
                .background(Color(nsColor: Theme.terminalBackground))
        } else if let target = control.target, let tmux = result.tmux, let socket = result.socket {
            SplitTerminalView(tmux: tmux, socket: socket, target: "=" + target, fontSize: max(9, store.fontSize - 1))
                .padding(6)
                .background(Color(nsColor: Theme.terminalBackground))
        } else {
            VStack(spacing: 8) {
                Text("Stopped").font(Theme.font(12, .semibold)).foregroundStyle(Theme.muted)
                Button("Start") { start() }.controlSize(.small)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(nsColor: Theme.terminalBackground))
        }
    }

    private func icon(_ symbol: String, help: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(Theme.muted)
                .frame(width: 20, height: 20)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
    }
}

/// A page the snapshots show for a web control.
private struct FixturePage: View {
    var body: some View {
        VStack(spacing: 8) {
            Text("Sign in").font(.system(size: 13, weight: .semibold)).foregroundStyle(Color(white: 0.15))
            RoundedRectangle(cornerRadius: 5).strokeBorder(Color(white: 0.8)).frame(height: 22)
            RoundedRectangle(cornerRadius: 5).fill(Color(red: 0.23, green: 0.44, blue: 0.85)).frame(height: 22)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.white)
    }
}

/// A web control's page: a web view with a data store that keeps nothing,
/// which loads only from its page's own origin. A link elsewhere, and any
/// new window, opens in the browser; the page has no bridge to Irie.
private struct DockWeb: NSViewRepresentable {
    let url: URL
    let reload: Int

    func makeCoordinator() -> Coordinator { Coordinator(url: url) }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.uiDelegate = context.coordinator
        context.coordinator.reloads = reload
        Self.originOnly(url) { list in
            if let list { config.userContentController.add(list) }
            web.load(URLRequest(url: url))
        }
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        if context.coordinator.url != url {
            context.coordinator.url = url
            web.load(URLRequest(url: url))
        } else if context.coordinator.reloads != reload {
            context.coordinator.reloads = reload
            web.reload()
        }
    }

    /// Compiles a content rule list that blocks every load but those from
    /// url's own origin, once per origin; nil when it does not compile.
    private static func originOnly(_ url: URL, done: @escaping (WKContentRuleList?) -> Void) {
        guard let scheme = url.scheme, let host = url.host else { return done(nil) }
        let origin = scheme + "://" + host + (url.port.map { ":\($0)" } ?? "")
        let escaped = NSRegularExpression.escapedPattern(for: origin)
        let rules = """
        [{"trigger":{"url-filter":".*"},"action":{"type":"block"}},
         {"trigger":{"url-filter":"^\(escaped.replacingOccurrences(of: "\\", with: "\\\\"))/"},"action":{"type":"ignore-previous-rules"}}]
        """
        // FNV-1a names the compiled list, stably across launches.
        var h: UInt64 = 0xcbf29ce484222325
        for b in origin.utf8 { h = (h ^ UInt64(b)) &* 0x100000001b3 }
        let id = "irie-dock-" + String(h, radix: 16)
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: id, encodedContentRuleList: rules) { list, _ in
            DispatchQueue.main.async { done(list) }
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        var url: URL
        var reloads = 0

        init(url: URL) { self.url = url }

        private func sameOrigin(_ u: URL?) -> Bool {
            guard let u else { return false }
            return u.scheme == url.scheme && u.host == url.host && u.port == url.port
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            let target = action.request.url
            if sameOrigin(target) || target?.scheme == "about" {
                decisionHandler(.allow)
                return
            }
            if action.navigationType == .linkActivated, let target { NSWorkspace.shared.open(target) }
            decisionHandler(.cancel)
        }

        func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                     for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
            if let target = action.request.url { NSWorkspace.shared.open(target) }
            return nil
        }
    }
}
