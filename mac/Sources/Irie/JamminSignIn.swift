#if !NOJAMMIN
import AppKit
import SwiftUI
import WebKit

// Linking Spotify (docs/proposals/jammin.md §3, "Bitwarden fills the
// sign-in"): a sheet with one web view, whose website data is its own,
// made fresh for the sheet and gone once it closes. Its first page is
// Spotify's dashboard, where the person makes the app Jammin signs in
// with; its second, Spotify's sign-in, which the core began and listens
// for. Spotify's own sign-in carries from the first page to the second.
// Bitwarden fills Spotify's sign-in on it as on a web pane (Bitwarden.swift),
// on accounts.spotify.com alone.

/// The sheet's web view and where it is.
@MainActor
final class JamminSignInPage: NSObject, ObservableObject, BitwardenPage {
    /// Where Bitwarden fills: Spotify's sign-in.
    static let origin = "https://accounts.spotify.com"
    static let dashboard = URL(string: "https://developer.spotify.com/dashboard")!
    /// The redirect URI the person's app lists, as they type it in at the
    /// dashboard: without the port, which changes at each sign-in.
    static let redirect = "http://127.0.0.1/jammin/spotify"

    let web: PluginPaneWebView
    /// The page shows a sign-in form Bitwarden could fill, and Bitwarden
    /// filled this page's already.
    @Published var signIn = false
    var filled = false
    /// The page's host, for the bar over it.
    @Published private(set) var place = ""
    @Published private(set) var loading = false
    /// Why the page didn't load.
    @Published private(set) var failed: String?
    private var watches: [NSKeyValueObservation] = []
    /// The sign-in's path last looked at, so each of its steps gets a look.
    private var path = ""
    /// The sheet closed: what was under way is the sheet's no longer.
    private(set) var closed = false
    private var lastCrash: Date?

    var originText: String { Self.origin }
    var webView: WKWebView { web }
    /// The page on show, while it is Spotify's sign-in.
    var page: URL? { web.url.flatMap { Self.fills($0) ? $0 : nil } }

    /// store is the sheet's own website data: a fresh one kept in memory
    /// only, which goes with the sheet.
    init(store: WKWebsiteDataStore) {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = store
        // No WebRTC, as in a web pane.
        config.userContentController.addUserScript(WKUserScript(source: PluginPaneHolder.noWebRTC, injectionTime: .atDocumentStart, forMainFrameOnly: false))
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        web = PluginPaneWebView(frame: .zero, configuration: config)
        super.init()
        web.navigationDelegate = self
        web.uiDelegate = self
        watches = [
            web.observe(\.url) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.moved(web.url) }
            },
            web.observe(\.isLoading) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.loading = web.isLoading }
            },
        ]
    }

    /// Where the sheet's page may go: Spotify's own pages, over https, and
    /// the core's answer on this Mac, at whatever port it listens on.
    nonisolated static func allowed(_ u: URL) -> Bool {
        let host = u.host?.lowercased() ?? ""
        switch u.scheme?.lowercased() ?? "" {
        case "https": return host == "spotify.com" || host.hasSuffix(".spotify.com")
        case "http": return host == "127.0.0.1"
        case "about": return true
        default: return false
        }
    }

    /// A page Bitwarden may fill: Spotify's sign-in, at its own origin.
    nonisolated static func fills(_ u: URL) -> Bool {
        u.scheme?.lowercased() == "https" && u.host?.lowercased() == "accounts.spotify.com" && (u.port ?? 443) == 443
    }

    func load(_ url: URL) {
        failed = nil
        signIn = false
        filled = false
        web.load(URLRequest(url: url))
    }

    func close() {
        closed = true
        watches = []
        web.stopLoading()
        web.navigationDelegate = nil
        web.uiDelegate = nil
        web.removeFromSuperview()
    }

    /// The page moved. Spotify's sign-in goes from its username to its
    /// password without loading again, so each step it moves to gets a
    /// look of its own, as a page that loads does.
    private func moved(_ u: URL?) {
        place = u?.host ?? ""
        let p = u.map { Self.fills($0) ? $0.path : "" } ?? ""
        guard p != path else { return }
        path = p
        guard !p.isEmpty else { return }
        signIn = false
        filled = false
        Task {
            try? await Task.sleep(nanoseconds: 400_000_000)
            await BitwardenFill.look(self)
        }
    }
}

extension JamminSignInPage: WKNavigationDelegate {
    /// The page itself goes only to Spotify and to the core's answer. A link
    /// elsewhere, as to another service's sign-in, opens in the browser when
    /// the person clicked it, never at a page's own word. A frame within the
    /// page, such as a captcha's, loads as the page asks.
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let target = action.request.url else { return decisionHandler(.cancel) }
        if let frame = action.targetFrame, !frame.isMainFrame { return decisionHandler(.allow) }
        if Self.allowed(target) { return decisionHandler(.allow) }
        if ["http", "https", "mailto"].contains(target.scheme?.lowercased() ?? ""), action.navigationType == .linkActivated, web.justPressed {
            NSWorkspace.shared.open(target)
        }
        decisionHandler(.cancel)
    }

    /// Nothing downloads here: what the page can't show, it doesn't load.
    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse,
                 decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void) {
        decisionHandler(response.canShowMIMEType ? .allow : .cancel)
    }

    func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        failed = nil
        signIn = false
        filled = false
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        // A sign-in page fills itself while Bitwarden is unlocked.
        Task { await BitwardenFill.look(self) }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        fail(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        fail(error)
    }

    /// The page's web process stopped: it loads again once, and a second
    /// stop within a minute says so.
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        let now = Date()
        if let last = lastCrash, now.timeIntervalSince(last) < 60 {
            failed = "Spotify’s page stopped twice in a minute. Back, or Try Again, loads it again."
            return
        }
        lastCrash = now
        webView.reload()
    }

    private func fail(_ error: Error) {
        let e = error as NSError
        // A load this sheet stopped, as a page it kept to Spotify.
        if (e.domain == NSURLErrorDomain && e.code == NSURLErrorCancelled) || (e.domain == WKErrorDomain && e.code == 102) { return }
        failed = "Spotify’s page didn’t load: \(e.localizedDescription)"
    }
}

extension JamminSignInPage: WKUIDelegate {
    /// A link meant for a new window opens in the browser, at the person's
    /// click: the sheet keeps to its two pages.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let target = action.request.url, ["http", "https", "mailto"].contains(target.scheme?.lowercased() ?? ""), web.justPressed {
            NSWorkspace.shared.open(target)
        }
        return nil
    }

    /// No camera or microphone.
    func webView(_ webView: WKWebView, requestMediaCapturePermissionFor origin: WKSecurityOrigin, initiatedByFrame frame: WKFrameInfo,
                 type: WKMediaCaptureType, decisionHandler: @escaping (WKPermissionDecision) -> Void) {
        decisionHandler(.deny)
    }
}

// MARK: The sheet

/// The sheet that links Spotify: first making the person's Spotify app at
/// its dashboard, then signing in to it. It closes by itself once the core
/// says Spotify is ready.
@MainActor
struct JamminSignInSheet: View {
    @Environment(Store.self) private var store
    @Environment(\.dismiss) private var dismiss
    @StateObject private var page = JamminSignInPage(store: .nonPersistent())
    /// 1, making the app at Spotify's dashboard; 2, signing in.
    @State private var step: Int
    @State private var clientID: String
    @State private var problem: String?
    @State private var working = false
    @State private var copied = false
    /// Each sign-in started, so its watch starts with it.
    @State private var attempt = 0
    /// Spotify was linked, so nothing is left to stop when it closes.
    @State private var linked = false
    /// The Client ID the core keeps, "" for none: with one, the sheet opens
    /// on the sign-in, and Back changes it.
    private let savedID: String
    /// The snapshots draw the sheet with no web view and no network.
    private let fixture: Bool

    /// Bitwarden's key names the sheet's page as a pane's (BitwardenViews.swift).
    private static let bitwardenPane = PluginPaneInfo(id: "jammin.signin", title: "Spotify", kind: "web", origin: JamminSignInPage.origin)

    init(savedID: String? = nil, fixture: Bool = false, step: Int = 1, clientID: String = "", problem: String? = nil) {
        let saved = (savedID ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        self.savedID = saved
        self.fixture = fixture
        _step = State(initialValue: saved.isEmpty ? step : 2)
        _clientID = State(initialValue: clientID.isEmpty ? saved : clientID)
        _problem = State(initialValue: problem)
    }

    private var vault: BitwardenVault { .shared }
    private var id: String { clientID.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() }
    /// A Client ID is 32 hex digits.
    private var idOK: Bool { id.count == 32 && id.allSatisfy(\.isHexDigit) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rectangle().fill(Theme.edge).frame(height: 1)
            web
            Rectangle().fill(Theme.edge).frame(height: 1)
            footer
        }
        .frame(width: 840, height: 700)
        .onAppear {
            guard !fixture else { return }
            if step == 2 { start() } else { page.load(JamminSignInPage.dashboard) }
        }
        // Closed on the sign-in before Spotify was linked: the core stops
        // listening for it.
        .onDisappear {
            page.close()
            if step == 2, !linked, !fixture { store.jammin.cancelLink() }
        }
        .task(id: attempt) { await watch() }
        // Unlocked while the sheet shows: its sign-in fills, as the panes'.
        .onChange(of: vault.state) { _, state in
            if state == .unlocked, !fixture { Task { await BitwardenFill.look(page) } }
        }
    }

    // MARK: Parts

    private var header: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 14) {
                Text("Link Spotify")
                    .font(Theme.font(17, .bold))
                    .foregroundStyle(Theme.ink)
                Spacer()
                stepMark(1, "Make your Spotify app")
                stepMark(2, "Sign in")
            }
            if step == 1 {
                VStack(alignment: .leading, spacing: 6) {
                    numbered(1) {
                        Text("**Create app**, with any name and description, such as Jammin and “my jam box”.")
                    }
                    numbered(2) {
                        HStack(spacing: 8) {
                            Text("**Redirect URI:**")
                            Text(JamminSignInPage.redirect)
                                .font(.system(size: 12, design: .monospaced))
                                .textSelection(.enabled)
                            Button(copied ? "Copied" : "Copy") { copy() }
                                .controlSize(.small)
                                .help("Copy the redirect URI, exactly as your app lists it")
                        }
                    }
                    numbered(3) {
                        Text("**Which API:** Web API. Agree to Spotify’s terms and save.")
                    }
                    numbered(4) {
                        Text("Copy the **Client ID** from your app’s settings into the box below. It isn’t a secret.")
                    }
                }
                .font(Theme.font(12.5))
                .foregroundStyle(Theme.ink)
            } else {
                Text("Sign in to Spotify and agree, and Jammin is linked. Your password goes only to Spotify, and Jammin keeps only what lets it in again, in your Keychain.")
                    .font(Theme.font(12.5))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(16)
    }

    private func stepMark(_ n: Int, _ title: String) -> some View {
        HStack(spacing: 5) {
            Text("\(n)")
                .font(Theme.font(10.5, .bold))
                .foregroundStyle(n == step ? Theme.onAccent : Theme.muted)
                .frame(width: 18, height: 18)
                .background(Circle().fill(n == step ? Theme.accentFill : Theme.field))
            Text(title)
                .font(Theme.font(11.5, n == step ? .semibold : .regular))
                .foregroundStyle(n == step ? Theme.ink : Theme.muted)
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(n == step ? .isSelected : [])
    }

    private func numbered<C: View>(_ n: Int, @ViewBuilder _ content: () -> C) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text("\(n).")
                .font(Theme.font(12.5, .semibold))
                .foregroundStyle(Theme.muted)
                .frame(width: 16, alignment: .trailing)
            content()
        }
    }

    /// The page, under its place and Bitwarden's key, with Bitwarden's
    /// banner over a sign-in while it is locked.
    private var web: some View {
        VStack(spacing: 0) {
            if vault.offered, page.signIn, vault.state != .unlocked, store.bitwardenPart?.state == "ready" {
                BitwardenBarContent(unlocking: vault.state == .unlocking) {
                    store.askBitwarden(on: page.web.window)
                }
            }
            HStack(spacing: 6) {
                Image(systemName: "lock.fill")
                    .font(.system(size: 9))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                Text(fixture ? (step == 1 ? "developer.spotify.com" : "accounts.spotify.com") : page.place)
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                if page.loading {
                    ProgressView().controlSize(.mini)
                }
                Spacer()
                if vault.offered {
                    BitwardenKey(pane: Self.bitwardenPane, holder: page)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .background(Theme.field)
            ZStack {
                if fixture {
                    JamminSignInFixturePage()
                } else {
                    PluginPaneWebHost(web: page.web)
                }
                if let f = page.failed {
                    VStack(spacing: 8) {
                        Text(f)
                            .font(Theme.font(12))
                            .foregroundStyle(Theme.muted)
                            .multilineTextAlignment(.center)
                            .fixedSize(horizontal: false, vertical: true)
                        Button("Load It Again") {
                            if step == 1 { page.load(JamminSignInPage.dashboard) } else { start() }
                        }
                            .buttonStyle(SoftButton())
                    }
                    .padding(24)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Theme.panel)
                }
            }
        }
    }

    private var footer: some View {
        HStack(spacing: 8) {
            if step == 1 {
                Text("Client ID")
                    .font(Theme.font(12, .semibold))
                    .foregroundStyle(Theme.ink)
                TextField("32 letters and digits", text: $clientID)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(size: 12, design: .monospaced))
                    .frame(width: 300)
                    .onSubmit { if idOK { start() } }
                    .accessibilityLabel("Your Spotify app’s Client ID")
                if let problem {
                    note(problem)
                } else if !id.isEmpty, !idOK {
                    note("A Client ID is 32 letters a to f and digits.")
                }
            } else if let problem {
                note(problem)
            } else {
                ProgressView().controlSize(.mini)
                Text("Waiting for Spotify…")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 8)
            if step == 2 {
                Button("Back") { back() }
            }
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
            if step == 1 {
                Button("Next") { start() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(!idOK || working)
            } else if problem != nil {
                Button("Try Again") { start() }
                    .keyboardShortcut(.defaultAction)
                    .disabled(working)
            }
        }
        .padding(12)
    }

    private func note(_ text: String) -> some View {
        HStack(alignment: .top, spacing: 5) {
            Image(systemName: "info.circle")
                .font(.system(size: 11))
                .accessibilityHidden(true)
            Text(text)
                .font(Theme.font(11.5))
                .lineLimit(3)
                .fixedSize(horizontal: false, vertical: true)
                .help(text)
        }
        .foregroundStyle(Theme.muted)
    }

    // MARK: Doing

    /// Asks the core to begin Spotify's sign-in with the Client ID in the
    /// box, and shows Spotify's page for it. The one the core keeps goes as
    /// none, so it signs in with it again and writes nothing.
    private func start() {
        guard !working, !fixture else { return }
        guard idOK else {
            problem = "That Client ID isn’t 32 letters a to f and digits: Back changes it."
            return
        }
        working = true
        problem = nil
        // Taken while the sheet shows: the answer may come after it closed.
        let p = page, jammin = store.jammin
        let given = id == savedID.lowercased() ? nil : id
        Task {
            defer { working = false }
            do {
                let link = try await jammin.link(clientID: given)
                if p.closed { return jammin.cancelLink() }
                guard let url = URL(string: link.url), JamminSignInPage.allowed(url) else {
                    problem = "Spotify’s sign-in came without a page of Spotify’s to show."
                    return
                }
                step = 2
                attempt += 1
                p.load(url)
            } catch {
                if !p.closed { problem = error.localizedDescription }
            }
        }
    }

    /// Back to the dashboard, to change the Client ID: the sign-in under
    /// way stops.
    private func back() {
        if step == 2 { store.jammin.cancelLink() }
        step = 1
        problem = nil
        attempt += 1
        page.load(JamminSignInPage.dashboard)
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(JamminSignInPage.redirect, forType: .string)
        copied = true
    }

    /// While Spotify's sign-in shows, looks each second at how Spotify
    /// stands: once ready the sheet closes, and a sign-in that ended
    /// without it says why.
    private func watch() async {
        guard step == 2, !fixture else { return }
        let jammin = store.jammin, app = store
        let started = Date()
        var seen = false
        while !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            guard !Task.isCancelled else { return }
            await jammin.refresh()
            guard !Task.isCancelled, let s = jammin.sources.first(where: { $0.isSpotify }) else { continue }
            if s.ready {
                linked = true
                jammin.linked()
                app.say("Spotify is linked: Jammin plays it from here.", error: false, seconds: 4)
                dismiss()
                return
            }
            if s.state == "linking" {
                seen = true
                continue
            }
            // Only once the sign-in was seen under way, or a few seconds on,
            // so a look from before it began never ends it.
            if seen || Date().timeIntervalSince(started) > 5 {
                problem = s.problem ?? "The sign-in ended without linking Spotify."
                return
            }
        }
    }
}

/// What the snapshots show for Spotify's page: a plain page of grey bars,
/// never Spotify's own.
private struct JamminSignInFixturePage: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            RoundedRectangle(cornerRadius: 4).fill(Color(white: 0.86)).frame(width: 220, height: 18)
            RoundedRectangle(cornerRadius: 4).fill(Color(white: 0.92)).frame(width: 420, height: 12)
            HStack(spacing: 12) {
                ForEach(0 ..< 3, id: \.self) { _ in
                    RoundedRectangle(cornerRadius: 8).fill(Color(white: 0.95)).frame(height: 110)
                }
            }
            RoundedRectangle(cornerRadius: 14).fill(Color(white: 0.2)).frame(width: 110, height: 28)
            Spacer()
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.white)
    }
}
#endif
