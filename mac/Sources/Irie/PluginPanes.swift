import AppKit
import CryptoKit
import SwiftUI
import WebKit

// Plugin panes (docs/proposals/plugin-panes.md): a plugin's part can bring a
// tab to the right sidebar, which shows while the part is on. The core says
// which panes there are; this draws them. A web pane, which a manifest of
// yours in the plugins folder brings, shows a page from its one origin, on
// this Mac unless the manifest says otherwise, in a web view that keeps
// its plugin's logins and no one else's. Any other kind names a view of the
// app's own.

// MARK: Wire

/// A tab a plugin's part brings to the right sidebar.
struct PluginPaneInfo: Decodable, Equatable, Hashable, Identifiable {
    /// The plugin's name, a dot and the part's.
    var id: String
    var title: String
    /// An SF Symbols name.
    var icon: String?
    /// web, or the name of a view of the app's own.
    var kind: String
    /// A web pane's page, and the one origin it may load from.
    var address: String?
    var origin: String?
    /// Its origin is off this Mac, as its manifest allows.
    var remote: Bool?
    /// What starts the app behind it, for when nothing answers.
    var start: String?
    /// The website data it keeps: its plugin's.
    var store: String?

    var isWeb: Bool { kind == "web" }

    /// Its icon, or a globe for a web page.
    var symbol: String {
        if let icon, NSImage(systemSymbolName: icon, accessibilityDescription: nil) != nil { return icon }
        return isWeb ? "globe" : "puzzlepiece.extension"
    }
}

/// A bundle: plugins, or parts of them, that go on and off together under
/// one switch.
struct BundleInfo: Decodable, Equatable, Identifiable {
    var name: String
    var title: String
    var summary: String
    var manifest: String
    /// on, off or some.
    var state: String
    var plugins: [Member]
    /// Why its manifest doesn't read: it has no switch until it does.
    var problem: String?

    var id: String { name }

    /// A plugin or part the bundle names.
    struct Member: Decodable, Equatable, Identifiable {
        /// As the bundle names it: a plugin, or PLUGIN.PART.
        var name: String
        var plugin: String
        var part: String?
        var title: String
        var on: Bool
        /// A part's state, or missing when it isn't here.
        var state: String
        var detail: String

        var id: String { name }
    }

    /// The ones that count: those here and in this build.
    private var counted: [Member] { plugins.filter { $0.state != "missing" && $0.state != "held" } }

    /// Its state in words: on, off, or how many are on.
    var stateWords: String {
        switch state {
        case "on": return "on"
        case "some": return "\(counted.filter(\.on).count) of \(counted.count) on"
        default: return "off"
        }
    }

    /// Its plugins by name, for a sentence: Invoices, Ledger and Notes.
    var list: String {
        let names = plugins.map(\.title)
        guard names.count > 1 else { return names.first ?? "nothing yet" }
        return names.dropLast().joined(separator: ", ") + " and " + names.last!
    }
}

// MARK: Store

extension Store {
    /// The panes that show: those of every part that is on, in the order of
    /// the plugins, Irie's own first.
    var panes: [PluginPaneInfo] {
        plugins.filter { $0.on && $0.problem == nil }.flatMap { p in p.parts.compactMap { $0.isOn ? $0.pane : nil } }
    }

    /// The pane on show in the right sidebar, while one is.
    var shownPane: PluginPaneInfo? {
        guard side.tab == .plugin, let id = side.pane else { return nil }
        return panes.first { $0.id == id }
    }

    /// A pane's own item: shows it, or closes the sidebar when it already
    /// shows.
    func togglePane(_ id: String) {
        // Zen hides it: it gives way, and the pane shows (Zen.swift).
        if zenGivesWay() { return openPane(id) }
        if side.open && side.tab == .plugin && side.pane == id { closeSide() } else { openPane(id) }
    }

    /// Turns each plugin a bundle names on or off at once, and says why
    /// when the core refuses.
    func setBundle(_ bundle: String, on: Bool) {
        Task {
            do {
                let r = try await client.call("plugins", PluginsArgs(bundle: bundle, on: on), timeout: 10, as: PluginsResult.self)
                takePlugins(r)
                await settings.load()
            } catch {
                let title = bundles.first { $0.name == bundle }?.title ?? bundle
                say("Couldn’t turn the \(title) bundle \(on ? "on" : "off"): \(error.localizedDescription)", error: true)
            }
        }
    }
}

// MARK: The tab

/// The pane on show in the right sidebar, or why it isn't there.
@MainActor
struct PluginPaneTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        if let pane = store.shownPane {
            PluginPaneContent(pane: pane)
                .id(pane.id)
        } else if !store.canPlugins && !store.isFixture {
            SideNote(title: "No panes here", text: "This \(Brand.name)'s core has no plugin panes yet. They come with the next install.")
        } else if store.plugins.isEmpty {
            ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            SideNote(title: "That pane is gone", text: "Its plugin is off, or its manifest changed. Settings › Plugins turns it on again; the tabs above show what is on.")
        }
    }
}

/// A pane by its kind: a web page, or a view of the app's own.
@MainActor
struct PluginPaneContent: View {
    let pane: PluginPaneInfo

    var body: some View {
        switch pane.kind {
        case "web":
            PluginWebPane(pane: pane)
        // A pane of a part of Irie's own draws its own view here, by its
        // kind, as the Writing plugin's Variorum tab will.
        case "yoink":
            YoinkTab()
        case "founding":
            FoundingTab()
        default:
            SideNote(title: pane.title, text: "This app can't draw a \(pane.kind) pane yet: it comes with a newer build of \(Brand.name).")
        }
    }
}

// MARK: Web panes

/// A web pane: a header with its page's place and buttons, over the page,
/// or what to do while nothing answers there.
@MainActor
private struct PluginWebPane: View {
    @Environment(Store.self) private var store
    let pane: PluginPaneInfo

    var body: some View {
        if store.isFixture {
            PluginPaneFrame(pane: pane, place: PluginPaneFixture.place(pane), canGoBack: false, back: {}, reload: {}, browser: {}) {
                if PluginPaneFixture.down.contains(pane.id) {
                    PluginPaneDown(pane: pane, retry: {})
                } else {
                    VStack(spacing: 0) {
                        if PluginPaneFixture.signIn.contains(pane.id) {
                            BitwardenBarContent(unlocking: false, unlock: {})
                        }
                        PluginPaneFixture.Page(pane: pane)
                    }
                }
            }
        } else if let holder = PluginPaneWebs.shared.holder(for: pane, say: { store.say($0, error: $1) }) {
            PluginLiveWebPane(pane: pane, holder: holder)
        } else {
            SideNote(title: pane.title, text: "Its address doesn't read.")
        }
    }
}

/// A web pane with its web view.
@MainActor
private struct PluginLiveWebPane: View {
    let pane: PluginPaneInfo
    @ObservedObject var holder: PluginPaneHolder

    var body: some View {
        PluginPaneFrame(pane: pane, place: holder.place, canGoBack: holder.canGoBack,
                  back: { holder.web.goBack() }, reload: { holder.reload() }, browser: { holder.openInBrowser() }, holder: holder) {
            VStack(spacing: 0) {
                // While Bitwarden is locked, over a sign-in page it could fill.
                if holder.state == .shown { BitwardenBar(holder: holder) }
                ZStack {
                    PluginPaneWebHost(web: holder.web)
                        .opacity(holder.state == .shown ? 1 : 0)
                    switch holder.state {
                    case .loading:
                        ProgressView().controlSize(.small)
                    case .shown:
                        EmptyView()
                    case .down:
                        PluginPaneDown(pane: pane) { holder.retry() }
                    case let .failed(why):
                        SideNote(title: "\(pane.title) didn’t load", text: why)
                    }
                }
            }
        }
        // While nothing answers, it tries again every few seconds, and
        // only while it shows.
        .task(id: holder.state == .down) {
            while !Task.isCancelled, holder.state == .down {
                try? await Task.sleep(nanoseconds: 3_000_000_000)
                if Task.isCancelled { return }
                holder.retry(quietly: true)
            }
        }
    }
}

/// A web pane's header, over what it shows: its title and place, and back,
/// reload and open in the browser, with Bitwarden's key while its plugin is
/// on (Bitwarden.swift).
@MainActor
private struct PluginPaneFrame<Content: View>: View {
    @Environment(Store.self) private var store
    let pane: PluginPaneInfo
    let place: String
    let canGoBack: Bool
    let back: () -> Void
    let reload: () -> Void
    let browser: () -> Void
    var holder: PluginPaneHolder?
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                // Symbols as text, so those of two layers take the colour
                // too, in the app and its snapshots alike.
                Text(Image(systemName: pane.symbol))
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                Text(place)
                    .font(.system(size: 10.5, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .help(place)
                Spacer(minLength: 4)
                if store.bitwardenPart != nil {
                    BitwardenKey(pane: pane, holder: holder)
                }
                icon("chevron.left", help: "Back", action: back)
                    .disabled(!canGoBack)
                icon("arrow.clockwise", help: "Reload the page", action: reload)
                icon("arrow.up.right.square", help: "Open the page in your browser, which has its own logins", action: browser)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(Theme.card)
            Rectangle().fill(Theme.edge).frame(height: 1)
            content()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(pane.title), \(place)")
    }

    private func icon(_ symbol: String, help: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(Image(systemName: symbol))
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

/// What a web pane shows while nothing answers at its address: that its
/// app isn't running, and what starts it.
@MainActor
private struct PluginPaneDown: View {
    @Environment(Store.self) private var store
    let pane: PluginPaneInfo
    let retry: () -> Void

    private var place: String { pane.origin.flatMap { URL(string: $0) }.map(PluginPaneHolder.hostPort) ?? (pane.address ?? "") }

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "powerplug")
                .font(.system(size: 26, weight: .light))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            Text("\(pane.title) isn’t running")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text(pane.start == nil
                 ? "Nothing answers at \(place). Start the app that serves it, and it shows here."
                 : "Nothing answers at \(place). To start it:")
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            if let start = pane.start {
                Text(start)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .lineLimit(6)
                    .padding(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.field))
                Text("It shows here as soon as it answers.")
                    .font(Theme.font(11))
                    .foregroundStyle(Theme.muted)
            }
            HStack {
                if let start = pane.start {
                    Button("Copy") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(start, forType: .string)
                        store.say("Copied what starts \(pane.title).", error: false)
                    }
                    .help("Copies it, to paste into a terminal")
                }
                Button("Try Again", action: retry)
            }
            .controlSize(.small)
        }
        .frame(maxWidth: 300)
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .contain)
    }
}

/// Hosts a pane's web view, which outlives this view: the pane keeps its
/// page as other tabs show and it comes back. The web view sits in a
/// container of this view's own, so a host that goes takes only its
/// container.
private struct PluginPaneWebHost: NSViewRepresentable {
    let web: WKWebView

    func makeNSView(context: Context) -> NSView {
        let box = NSView()
        adopt(box)
        return box
    }

    func updateNSView(_ box: NSView, context: Context) {
        if web.superview !== box { adopt(box) }
    }

    private func adopt(_ box: NSView) {
        web.removeFromSuperview()
        web.frame = box.bounds
        web.autoresizingMask = [.width, .height]
        box.addSubview(web)
    }
}

// MARK: The web views

/// Every web pane's web view, by pane: each stays, with its page, while its
/// pane is on, and goes when its plugin goes off.
@MainActor
final class PluginPaneWebs {
    static let shared = PluginPaneWebs()

    private var holders: [String: PluginPaneHolder] = [:]

    /// The pane's holder, made the first time it shows; nil for a pane with
    /// no address. A pane whose address or origin changed gets a new one.
    func holder(for pane: PluginPaneInfo, say: @escaping (String, Bool) -> Void) -> PluginPaneHolder? {
        if let h = holders[pane.id], h.pane == pane { return h }
        guard let h = PluginPaneHolder(pane: pane, say: say) else { return nil }
        holders[pane.id]?.close()
        holders[pane.id] = h
        return h
    }

    /// Each pane looks at its page again, as Bitwarden unlocks.
    func lookAgain() {
        for h in holders.values {
            Task { await BitwardenFill.look(h) }
        }
    }

    /// Keeps only the panes that are on.
    func keep(_ ids: Set<String>) {
        for (id, h) in holders where !ids.contains(id) {
            h.close()
            holders[id] = nil
        }
    }
}

/// One web pane's web view, and how its page stands.
@MainActor
final class PluginPaneHolder: NSObject, ObservableObject, BitwardenPage {
    enum State: Equatable {
        case loading, shown, down
        case failed(String)
    }

    let pane: PluginPaneInfo
    let web: PluginPaneWebView
    @Published private(set) var state = State.loading
    @Published private(set) var canGoBack = false
    /// The page's place, its host, port and path, for the header.
    @Published private(set) var place: String
    /// The page shows a sign-in form Bitwarden could fill, and Bitwarden
    /// filled this page's already (Bitwarden.swift).
    @Published var signIn = false
    var filled = false
    private let address: URL
    private let origin: URL
    private let say: (String, Bool) -> Void
    private var watches: [NSKeyValueObservation] = []
    private var ruled = false
    private var closed = false
    /// The names downloads are saved under, by download, for the notice
    /// when each is done.
    private var downloads: [ObjectIdentifier: String] = [:]
    /// The page's origin as text, without a slash at its end.
    let originText: String
    /// When the page last asked something in a dialog, and whether the
    /// pane now answers its dialogs itself, until the page loads again.
    private var dialogs: [Date] = []
    private var hushed = false
    /// When the page's web process last stopped.
    private var lastCrash: Date?

    init?(pane: PluginPaneInfo, say: @escaping (String, Bool) -> Void) {
        guard let a = pane.address.flatMap(URL.init(string:)), let o = pane.origin.flatMap(URL.init(string:)), o.host != nil else { return nil }
        self.pane = pane
        address = a
        origin = o
        self.say = say
        place = Self.place(a)
        originText = o.absoluteString.hasSuffix("/") ? String(o.absoluteString.dropLast()) : o.absoluteString
        let config = WKWebViewConfiguration()
        // WebRTC is outside what a content rule list governs, so a page
        // could reach any host with it: each frame starts without it.
        config.userContentController.addUserScript(WKUserScript(source: Self.noWebRTC, injectionTime: .atDocumentStart, forMainFrameOnly: false))
        // Its plugin's own data, kept across launches: its logins, and
        // no one else's.
        config.websiteDataStore = WKWebsiteDataStore(forIdentifier: PluginPaneStores.id(pane.store ?? pane.id))
        // On the Mac, unlike iOS, a script may open windows unless told not to.
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        web = PluginPaneWebView(frame: .zero, configuration: config)
        super.init()
        web.navigationDelegate = self
        web.uiDelegate = self
        web.allowsMagnification = true
        watches = [
            web.observe(\.canGoBack) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.canGoBack = web.canGoBack }
            },
            web.observe(\.url) { [weak self] web, _ in
                MainActor.assumeIsolated { if let u = web.url, let self, self.sameOrigin(u) { self.place = Self.place(u) } }
            },
        ]
        Self.originOnly(o) { [weak self] list in
            guard let self, !self.closed else { return }
            guard let list else {
                self.state = .failed("The pane couldn’t keep the page to its own address, \(Self.hostPort(o)), so it shows nothing. Open it in your browser instead.")
                return
            }
            config.userContentController.add(list)
            self.ruled = true
            self.web.load(URLRequest(url: a))
        }
    }

    /// Loads the page's own address again, as when its app starts.
    func retry(quietly: Bool = false) {
        guard ruled else { return }
        if !quietly { state = .loading }
        web.load(URLRequest(url: address))
    }

    /// Reloads the page on show, or the pane's address when none showed.
    func reload() {
        guard ruled else { return }
        if state == .shown, web.url != nil { web.reload() } else { retry() }
    }

    /// Opens the page on show in the browser, which has logins of its own.
    func openInBrowser() {
        NSWorkspace.shared.open(web.url.flatMap { sameOrigin($0) ? $0 : nil } ?? address)
    }

    func close() {
        closed = true
        watches = []
        web.stopLoading()
        web.navigationDelegate = nil
        web.uiDelegate = nil
        web.removeFromSuperview()
    }

    /// Takes WebRTC's constructors out of a frame before its own scripts
    /// run, for good.
    private static let noWebRTC = """
    (() => { for (const k of ["RTCPeerConnection", "webkitRTCPeerConnection", "RTCDataChannel", "RTCIceCandidate", "RTCSessionDescription", "RTCRtpSender", "RTCRtpReceiver", "RTCRtpTransceiver"]) {
      try { Object.defineProperty(window, k, { value: undefined, writable: false, configurable: false }); } catch (e) {} } })();
    """

    // MARK: Origin

    /// The page on show, while it is on the pane's own origin.
    var page: URL? { web.url.flatMap { sameOrigin($0) ? $0 : nil } }

    /// Its web view, for Bitwarden's fill.
    var webView: WKWebView { web }

    func sameOrigin(_ u: URL) -> Bool {
        u.scheme?.lowercased() == origin.scheme?.lowercased() && u.host?.lowercased() == origin.host?.lowercased() && Self.port(u) == Self.port(origin)
    }

    private static func port(_ u: URL) -> Int? {
        u.port ?? (u.scheme?.lowercased() == "https" ? 443 : u.scheme?.lowercased() == "http" ? 80 : nil)
    }

    /// A page's host, port and path, as the header shows it.
    nonisolated static func place(_ u: URL) -> String {
        hostPort(u) + (u.path.isEmpty || u.path == "/" ? "" : u.path)
    }

    nonisolated static func hostPort(_ u: URL) -> String {
        guard let host = u.host else { return u.absoluteString }
        let h = host.contains(":") ? "[\(host)]" : host
        return h + (u.port.map { ":\($0)" } ?? "")
    }

    /// Compiles a content rule list that blocks every load but those from
    /// origin: its pages, its WebSockets and its blobs, and data the page
    /// holds itself. Once per origin; nil when it does not compile.
    private static func originOnly(_ origin: URL, done: @escaping (WKContentRuleList?) -> Void) {
        let o = origin.absoluteString.hasSuffix("/") ? String(origin.absoluteString.dropLast()) : origin.absoluteString
        let ws = (origin.scheme == "https" ? "wss" : "ws") + o.drop(while: { $0 != ":" })
        func allow(_ prefix: String) -> String {
            let pattern = "^" + NSRegularExpression.escapedPattern(for: prefix)
            return #"{"trigger":{"url-filter":"\#(pattern.replacingOccurrences(of: "\\", with: "\\\\"))"},"action":{"type":"ignore-previous-rules"}}"#
        }
        let rules = "[" + ([#"{"trigger":{"url-filter":".*"},"action":{"type":"block"}}"#] +
            [allow(o + "/"), allow(ws + "/"), allow("blob:" + o + "/"), allow("data:")]).joined(separator: ",") + "]"
        // FNV-1a names the compiled list, stably across launches.
        var h: UInt64 = 0xcbf29ce484222325
        for b in o.utf8 { h = (h ^ UInt64(b)) &* 0x100000001b3 }
        let id = "\(Brand.slug)-pane-" + String(h, radix: 16)
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: id, encodedContentRuleList: rules) { list, _ in
            DispatchQueue.main.async { done(list) }
        }
    }

    // MARK: Downloads

    private func save(_ download: WKDownload) {
        download.delegate = self
    }
}

extension PluginPaneHolder: WKNavigationDelegate {
    /// The page moves only within its origin. A link elsewhere opens in the
    /// browser when the person clicked it, never at the page's own word.
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let target = action.request.url else { return decisionHandler(.cancel) }
        let scheme = target.scheme?.lowercased() ?? ""
        // A blob the page made itself, as an in-page export does.
        let ownBlob = scheme == "blob" && target.absoluteString.hasPrefix("blob:" + originText + "/")
        if action.shouldPerformDownload, sameOrigin(target) || ownBlob || scheme == "data" {
            return decisionHandler(.download)
        }
        if sameOrigin(target) || ownBlob || scheme == "about" || scheme == "data" {
            return decisionHandler(.allow)
        }
        // Only a link the person clicked, never a page's script, even one
        // that runs as they type.
        if ["http", "https", "mailto"].contains(scheme), action.navigationType == .linkActivated, web.justPressed,
           action.targetFrame?.isMainFrame != false {
            NSWorkspace.shared.open(target)
        }
        decisionHandler(.cancel)
    }

    /// What the pane can't show, such as an export, it saves to Downloads.
    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse,
                 decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void) {
        let disposition = (response.response as? HTTPURLResponse)?.value(forHTTPHeaderField: "Content-Disposition") ?? ""
        let attachment = disposition.lowercased().hasPrefix("attachment")
        decisionHandler(response.canShowMIMEType && !attachment ? .allow : .download)
    }

    func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) {
        save(download)
    }

    func webView(_ webView: WKWebView, navigationResponse: WKNavigationResponse, didBecome download: WKDownload) {
        save(download)
    }

    func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        state = .shown
        dialogs = []
        hushed = false
        signIn = false
        filled = false
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        state = .shown
        // A sign-in page fills itself while Bitwarden is unlocked.
        Task { await BitwardenFill.look(self) }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    /// The page's web process stopped: it loads again once, and a second
    /// stop within a minute waits for Reload.
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        let now = Date()
        if let last = lastCrash, now.timeIntervalSince(last) < 60 {
            state = .failed("The page stopped twice in a minute, so the pane waits: Reload tries again.")
            return
        }
        lastCrash = now
        retry()
    }

    private func failed(_ error: Error) {
        let e = error as NSError
        switch (e.domain, e.code) {
        case (NSURLErrorDomain, NSURLErrorCancelled), (WKErrorDomain, 102):
            // A load this pane stopped, or one that became a download.
            return
        case (NSURLErrorDomain, NSURLErrorCannotConnectToHost), (NSURLErrorDomain, NSURLErrorNetworkConnectionLost),
             (NSURLErrorDomain, NSURLErrorTimedOut), (NSURLErrorDomain, NSURLErrorCannotFindHost),
             (NSURLErrorDomain, NSURLErrorNotConnectedToInternet), (NSURLErrorDomain, NSURLErrorDNSLookupFailed):
            state = .down
        default:
            // A page that showed stays; one that never did says why.
            if state != .shown { state = .failed(e.localizedDescription) }
        }
    }
}

extension PluginPaneHolder: WKUIDelegate {
    /// A link meant for a new window, as target="_blank" makes one: one of
    /// the pane's own opens in the pane, and one elsewhere in the browser at
    /// the person's click.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        guard let target = action.request.url else { return nil }
        if sameOrigin(target) {
            webView.load(action.request)
        } else if ["http", "https", "mailto"].contains(target.scheme?.lowercased() ?? ""), web.justPressed {
            NSWorkspace.shared.open(target)
        }
        return nil
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        ask(message, buttons: ["OK"]) { _ in completionHandler() }
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        ask(message, buttons: ["OK", "Cancel"]) { completionHandler($0 == .alertFirstButtonReturn) }
    }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String, defaultText: String?,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (String?) -> Void) {
        let field = NSTextField(string: defaultText ?? "")
        field.frame = NSRect(x: 0, y: 0, width: 260, height: 24)
        ask(prompt, buttons: ["OK", "Cancel"], accessory: field) { completionHandler($0 == .alertFirstButtonReturn ? field.stringValue : nil) }
    }

    /// A file field: the person picks files in a sheet.
    func webView(_ webView: WKWebView, runOpenPanelWith parameters: WKOpenPanelParameters,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping ([URL]?) -> Void) {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = parameters.allowsMultipleSelection
        panel.canChooseDirectories = parameters.allowsDirectories
        panel.canChooseFiles = true
        guard let window = web.window else { return completionHandler(nil) }
        panel.beginSheetModal(for: window) { r in
            MainActor.assumeIsolated { completionHandler(r == .OK ? panel.urls : nil) }
        }
    }

    /// No camera or microphone: the app has no use for them in a pane, and
    /// says so to macOS nowhere.
    func webView(_ webView: WKWebView, requestMediaCapturePermissionFor origin: WKSecurityOrigin, initiatedByFrame frame: WKFrameInfo,
                 type: WKMediaCaptureType, decisionHandler: @escaping (WKPermissionDecision) -> Void) {
        decisionHandler(.deny)
    }

    /// A page's alert, confirm or prompt, as a sheet on the window that
    /// names the pane, so it is plain who asks. A page that keeps asking,
    /// more than three times in ten seconds, gets no more sheets until it
    /// loads again: the pane answers them itself, as Cancel.
    private func ask(_ message: String, buttons: [String], accessory: NSView? = nil, done: @escaping (NSApplication.ModalResponse) -> Void) {
        let now = Date()
        dialogs = dialogs.filter { now.timeIntervalSince($0) < 10 } + [now]
        if hushed || dialogs.count > 3 {
            if !hushed {
                hushed = true
                say("\(pane.title) kept opening dialogs, so its pane answers them itself until the page loads again.", true)
            }
            return done(.alertSecondButtonReturn)
        }
        guard let window = web.window else { return done(.alertSecondButtonReturn) }
        let alert = NSAlert()
        alert.messageText = pane.title
        alert.informativeText = message
        for b in buttons { alert.addButton(withTitle: b) }
        alert.accessoryView = accessory
        alert.beginSheetModal(for: window) { r in
            MainActor.assumeIsolated { done(r) }
        }
    }
}

extension PluginPaneHolder: WKDownloadDelegate {
    /// A download goes to Downloads, under a name not taken there.
    func download(_ download: WKDownload, decideDestinationUsing response: URLResponse, suggestedFilename: String,
                  completionHandler: @escaping (URL?) -> Void) {
        let folder = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent("Downloads")
        let name = (suggestedFilename as NSString).lastPathComponent
        let safe = name.isEmpty || name.hasPrefix(".") ? "download" : name
        var url = folder.appendingPathComponent(safe)
        let stem = (safe as NSString).deletingPathExtension, ext = (safe as NSString).pathExtension
        var n = 2
        while FileManager.default.fileExists(atPath: url.path) {
            url = folder.appendingPathComponent(ext.isEmpty ? "\(stem) \(n)" : "\(stem) \(n).\(ext)")
            n += 1
        }
        downloads[ObjectIdentifier(download)] = url.lastPathComponent
        completionHandler(url)
    }

    func downloadDidFinish(_ download: WKDownload) {
        let name = downloads.removeValue(forKey: ObjectIdentifier(download)) ?? "the file"
        say("\(pane.title): saved \(name) in Downloads.", false)
    }

    func download(_ download: WKDownload, didFailWithError error: Error, resumeData: Data?) {
        downloads[ObjectIdentifier(download)] = nil
        say("\(pane.title): the download stopped: \(error.localizedDescription)", true)
    }
}

/// A pane's web view, which knows when the person last pressed a mouse
/// button or a key in it: only the system's own events say so, so a page's
/// script can't open a link out by itself (as the Markets window does).
final class PluginPaneWebView: WKWebView {
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

/// The website data stores of plugins' panes, one for each plugin, by a
/// name made from the plugin's: the same plugin gets the same store at every
/// launch, so its logins last, and no two plugins share one.
enum PluginPaneStores {
    static func id(_ store: String) -> UUID {
        var bytes = Array(Insecure.SHA1.hash(data: Data("\(Brand.slug)-pane-store:\(store)".utf8)).prefix(16))
        // A name-based UUID, as RFC 9562's version 5 makes.
        bytes[6] = (bytes[6] & 0x0f) | 0x50
        bytes[8] = (bytes[8] & 0x3f) | 0x80
        return UUID(uuid: (bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
                           bytes[8], bytes[9], bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15]))
    }
}

// MARK: Snapshots

/// What the snapshots show in a pane, with no web view and no network.
enum PluginPaneFixture {
    /// The panes drawn as their app not running.
    static var down: Set<String> = []
    /// The panes drawn with Bitwarden's banner over a sign-in page.
    static var signIn: Set<String> = []

    static func place(_ pane: PluginPaneInfo) -> String {
        pane.address.flatMap(URL.init(string:)).map(PluginPaneHolder.place) ?? ""
    }

    /// A page the snapshots show for a web pane: a small app's list.
    struct Page: View {
        let pane: PluginPaneInfo

        var body: some View {
            VStack(alignment: .leading, spacing: 10) {
                Text(pane.title).font(.system(size: 15, weight: .semibold)).foregroundStyle(Color(white: 0.12))
                ForEach(Array(["Market stall, Saturday", "Food truck, Friday", "Café order"].enumerated()), id: \.offset) { i, row in
                    HStack {
                        Text(row).font(.system(size: 12)).foregroundStyle(Color(white: 0.2))
                        Spacer()
                        Text(["$148.00", "$92.50", "$36.25"][i]).font(.system(size: 12).monospacedDigit()).foregroundStyle(Color(white: 0.35))
                    }
                    .padding(8)
                    .background(RoundedRectangle(cornerRadius: 6).fill(Color(white: 0.96)))
                }
                RoundedRectangle(cornerRadius: 6).fill(Color(red: 0.18, green: 0.48, blue: 0.36)).frame(height: 26)
                    .overlay(Text("New invoice").font(.system(size: 12, weight: .semibold)).foregroundStyle(.white))
                Spacer()
            }
            .padding(14)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.white)
        }
    }
}
