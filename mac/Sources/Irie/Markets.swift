import AppKit
import SwiftUI
import WebKit

// The markets plugin's window (DESIGN §4.17): the market map you keep in a
// folder of your own, markets.folder, shown as its page in a web view that
// loads nothing from the network, and shown again each time the page is
// rebuilt, as a session that :markets TASK started may do.

// MARK: Wire

/// The markets op's arguments: none, to ask where the map is. A task goes
/// through the task bar's words, :markets TASK.
struct MarketsArgs: Encodable {}

/// Where the market map is, and how it stands.
struct MarketsResult: Decodable, Equatable {
    /// ready, needs (text says what it lacks) or plugin_off.
    var state: String
    var folder: String?
    var page: String?
    /// When the page last changed, while it is there.
    var changed: Date?
    var text: String
}

extension Notification.Name {
    /// Opens the Markets window, as :markets in a task bar asks.
    static let irieMarkets = Notification.Name("irieMarkets")
}

// MARK: Menu

/// Window › Markets (⇧⌘M), while the markets plugin is on. The window's own
/// item, which SwiftUI would add whatever the plugin, is removed
/// (IrieApp.swift).
struct MarketsCommands: Commands {
    let menu: MenuState
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        let on = MainActor.assumeIsolated { menu.facts.plugins.contains { $0.name == "markets" && $0.on } }
        CommandGroup(before: .windowList) {
            if on {
                Button("Markets") { openWindow(id: MarketsWindow.id) }
                    .keyboardShortcut("m", modifiers: [.command, .shift])
                Divider()
            }
        }
    }
}

// MARK: Window

/// The Markets window: the map, or what it lacks with what fixes it. It asks
/// the core how the map stands as it opens and every few seconds while it is
/// open. In the same folder a change shows once two looks in a row agree on
/// it, so a page being written, or taken away and written again, never shows
/// half done; a look that fails, as while the core restarts, changes
/// nothing.
@MainActor
struct MarketsWindow: View {
    static let id = "markets"

    @Environment(Store.self) private var store
    @State private var map: MarketsResult?
    @State private var loaded = false
    /// What decides what shows: the map's state and when its page changed,
    /// as on show and as the last look saw them.
    private struct Seen: Equatable {
        var state: String
        var changed: Date?
    }

    @State private var shown: Seen?
    @State private var seen: Seen?
    @State private var reloads = 0

    var body: some View {
        content
            .frame(minWidth: 720, minHeight: 480)
            .background(Theme.sidebar)
            .tint(Theme.accent)
            .toolbar {
                if let page = map?.page, map?.state == "ready" {
                    ToolbarItemGroup(placement: .primaryAction) {
                        Button { reloads += 1 } label: { Label("Reload", systemImage: "arrow.clockwise") }
                            .help("Load the page again")
                        Menu {
                            Button("Open in Browser") { NSWorkspace.shared.open(URL(fileURLWithPath: page)) }
                            Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: page)]) }
                            Divider()
                            Button("Choose Another Folder…") { Task { if await store.chooseMarketsFolder() { await look() } } }
                        } label: {
                            Label("More", systemImage: "ellipsis.circle")
                        }
                        .help("More for the map")
                    }
                }
            }
            .task { await watch() }
    }

    @ViewBuilder private var content: some View {
        if let map, map.state == "ready", let page = map.page, let folder = map.folder {
            MarketsWeb(page: URL(fileURLWithPath: page), folder: URL(fileURLWithPath: folder, isDirectory: true), reload: reloads)
        } else if let map {
            MarketsLacking(map: map) { await look() }
        } else if loaded {
            Text("The map comes from \(Brand.name)’s core, which isn’t answering, or has no markets plugin. Start it in a terminal with `\(Brand.slug) up`.")
                .font(Theme.font(13))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    /// Looks every three seconds while the window is open.
    private func watch() async {
        while !Task.isCancelled {
            await look()
            try? await Task.sleep(nanoseconds: 3_000_000_000)
        }
    }

    private func look() async {
        let answer = await store.markets()
        loaded = true
        guard let m = answer else { return }
        let now = Seen(state: m.state, changed: m.changed)
        // The first look, another folder, or the plugin turned on or off
        // shows at once.
        guard let old = map, m.folder == old.folder, (m.state == "plugin_off") == (old.state == "plugin_off") else {
            map = m
            shown = now
            seen = now
            return
        }
        if now == shown {
            if m != old { map = m }
        } else if now == seen {
            map = m
            shown = now
            // A page rebuilt where one showed loads again; one that appears
            // where none was loads as the window draws it.
            if m.state == "ready", old.state == "ready" { reloads += 1 }
        }
        seen = now
    }
}

/// What the map lacks, in the window's own words, with what fixes it.
@MainActor
private struct MarketsLacking: View {
    let map: MarketsResult
    let changed: () async -> Void
    @Environment(Store.self) private var store

    private enum Lack { case plugin, folder, gone, page }

    private var lack: Lack {
        if map.state == "plugin_off" { return .plugin }
        guard let folder = map.folder else { return .folder }
        var dir: ObjCBool = false
        return FileManager.default.fileExists(atPath: folder, isDirectory: &dir) && dir.boolValue ? .page : .gone
    }

    private var folderName: String {
        guard let folder = map.folder else { return "" }
        let home = NSHomeDirectory()
        return folder.hasPrefix(home + "/") ? "~" + folder.dropFirst(home.count) : folder
    }

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "chart.bar.doc.horizontal")
                .font(.system(size: 34, weight: .light))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            Text(title)
                .font(Theme.font(17, .semibold))
                .foregroundStyle(Theme.ink)
            Text(sentence)
                .font(Theme.font(13))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 440)
                .textSelection(.enabled)
            HStack(spacing: 10) {
                switch lack {
                case .plugin:
                    Button("Turn On Markets") {
                        store.setPlugin("markets", on: true)
                        Task {
                            try? await Task.sleep(nanoseconds: 500_000_000)
                            await changed()
                        }
                    }
                    .keyboardShortcut(.defaultAction)
                case .folder, .gone:
                    Button("Choose Folder…") { choose() }
                        .keyboardShortcut(.defaultAction)
                case .page:
                    Button("Choose Another Folder…") { choose() }
                    if let folder = map.folder {
                        Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: folder, isDirectory: true)]) }
                    }
                }
            }
            .padding(.top, 6)
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var title: String {
        switch lack {
        case .plugin: return "Markets is off"
        case .folder: return "Where is your market map?"
        case .gone: return "Your map’s folder has moved"
        case .page: return "No map in this folder yet"
        }
    }

    private var sentence: String {
        switch lack {
        case .plugin:
            return "Turn the plugin on to see your market map here: the page you keep in a folder of your own, shown again each time it is rebuilt."
        case .folder:
            return "Choose the folder that holds your map’s \(Self.page). It shows here, and again each time it is rebuilt. Nothing in it leaves this Mac."
        case .gone:
            return "\(folderName) isn’t there any more. Choose the folder that holds your map’s \(Self.page)."
        case .page:
            return "\(folderName) has no \(Self.page). Type :markets and what you want in a task bar, such as “:markets make a map of the markets I research”, and a session makes one there."
        }
    }

    private static let page = "index.html"

    private func choose() {
        Task { if await store.chooseMarketsFolder() { await changed() } }
    }
}

// MARK: Page

/// The map's page: a web view that reads only the map's folder and loads
/// nothing from the network, so the research in it stays on this Mac. A link
/// out opens in the browser only when the person clicks it, never at the
/// page's own word: a script can open no window, and one that follows a link
/// itself, at load or on a timer, goes nowhere. Without its block on the
/// network the page doesn't load at all. The page has no bridge to the app.
private struct MarketsWeb: NSViewRepresentable {
    let page: URL
    let folder: URL
    let reload: Int

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        // On the Mac, unlike iOS, a script may open windows unless told not to.
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        let web = MarketsWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.uiDelegate = context.coordinator
        web.allowsMagnification = true
        web.allowsBackForwardNavigationGestures = false
        context.coordinator.page = page
        context.coordinator.reloads = reload
        Self.offline { list in
            guard let list else {
                web.loadHTMLString(Self.unblocked, baseURL: nil)
                return
            }
            config.userContentController.add(list)
            web.loadFileURL(page, allowingReadAccessTo: folder)
        }
        return web
    }

    /// What shows instead of the page when the network can't be blocked.
    private static let unblocked = """
    <p style="font: 13px -apple-system, sans-serif; color: #888; text-align: center; margin-top: 30vh">\
    The map isn’t shown: this window couldn’t keep the page off the network. Open it from the toolbar’s More menu instead.</p>
    """

    func updateNSView(_ web: WKWebView, context: Context) {
        let c = context.coordinator
        if c.page != page {
            c.page = page
            c.reloads = reload
            web.loadFileURL(page, allowingReadAccessTo: folder)
        } else if c.reloads != reload {
            c.reloads = reload
            web.reloadFromOrigin()
        }
    }

    /// Compiles, once, a content rule list that blocks every load from the
    /// network; nil when it does not compile.
    private static func offline(done: @escaping (WKContentRuleList?) -> Void) {
        let rules = """
        [{"trigger":{"url-filter":"^https?:"},"action":{"type":"block"}},
         {"trigger":{"url-filter":"^wss?:"},"action":{"type":"block"}}]
        """
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: "\(Brand.slug)-markets-offline", encodedContentRuleList: rules) { list, _ in
            DispatchQueue.main.async { done(list) }
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        var page: URL?
        var reloads = 0
        /// Opens a link out in the browser.
        var open: (URL) -> Void = { NSWorkspace.shared.open($0) }

        /// The page moves within itself (its #explore links); a link out
        /// that the person clicked opens in the browser, and the page stays.
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let target = action.request.url else { return decisionHandler(.cancel) }
            if target.isFileURL || target.scheme == "about" {
                decisionHandler(.allow)
                return
            }
            if action.navigationType == .linkActivated, Self.outside(target), Self.pressed(webView) { open(target) }
            decisionHandler(.cancel)
        }

        /// A link meant for a new window, as target="_blank" makes one.
        func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                     for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
            if let target = action.request.url, Self.outside(target), Self.pressed(webView) { open(target) }
            return nil
        }

        private static func outside(_ url: URL) -> Bool {
            ["http", "https", "mailto"].contains(url.scheme?.lowercased() ?? "")
        }

        /// Whether the person just pressed in the page, as no script can.
        private static func pressed(_ web: WKWebView) -> Bool {
            (web as? MarketsWebView)?.justPressed == true
        }
    }
}

/// The map's web view, which knows when the person last pressed a mouse
/// button or a key in it: only the system's own events say so.
private final class MarketsWebView: WKWebView {
    private var pressed = Date.distantPast

    var justPressed: Bool { Date().timeIntervalSince(pressed) < 2 }

    override func mouseDown(with event: NSEvent) {
        pressed = Date()
        super.mouseDown(with: event)
    }

    override func otherMouseDown(with event: NSEvent) {
        pressed = Date()
        super.otherMouseDown(with: event)
    }

    override func keyDown(with event: NSEvent) {
        pressed = Date()
        super.keyDown(with: event)
    }
}

// MARK: Store

extension Store {
    /// How the market map stands, from the core; nil when the core isn't
    /// answering or has no markets plugin.
    func markets() async -> MarketsResult? {
        try? await client.call("markets", MarketsArgs(), timeout: 5, as: MarketsResult.self)
    }

    /// Asks for the folder that holds the map, and writes it to
    /// markets.folder, under ~ when it is in the home folder. Says whether
    /// it changed.
    func chooseMarketsFolder() async -> Bool {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Choose"
        panel.message = "Choose the folder that holds your market map’s index.html."
        guard panel.runModal() == .OK, let url = panel.url else { return false }
        var path = url.path
        let home = NSHomeDirectory()
        if path == home {
            path = "~"
        } else if path.hasPrefix(home + "/") {
            path = "~" + path.dropFirst(home.count)
        }
        do {
            _ = try await client.call("config", ConfigArgs(set: [ConfigSet(key: "markets.folder", value: .string(path))]), timeout: 10, as: ConfigResult.self)
            loadPluginsSoon()
            await settings.load()
            return true
        } catch {
            say("Couldn’t use that folder: \(error.localizedDescription)", error: true)
            return false
        }
    }
}
