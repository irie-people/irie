import AppKit
import SwiftUI
import WebKit

// A browser card of the rolodex (Rolodex.swift): an address bar over a web
// view. A card's page stays loaded as the wheel turns and as you move
// between sessions, for the ten cards used last. Every card shares one data
// store that keeps nothing once the app quits, so a sign-in holds across
// cards for the run and leaves no trace after it. The page has no bridge to
// Irie; a link to another app (mail, a phone number) opens that app.

/// What an address bar's text asks for: an address as typed, a host or a
/// local port made into one, or else a search.
enum Address {
    static func url(from text: String) -> URL? {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return nil }
        if let u = URL(string: t), let s = u.scheme?.lowercased(), ["http", "https", "about"].contains(s), u.host != nil || s == "about" {
            return u
        }
        // A port alone, as 3000 or :3000, is your own app.
        if t.range(of: #"^:?\d{2,5}(/.*)?$"#, options: .regularExpression) != nil {
            return URL(string: "http://localhost:" + (t.hasPrefix(":") ? String(t.dropFirst()) : t))
        }
        if !t.contains(" "), t.range(of: #"^[A-Za-z0-9.\-]+(:\d{1,5})?(/.*)?$"#, options: .regularExpression) != nil,
           t.contains(".") || t.hasPrefix("localhost") {
            let host = t.split(separator: "/", maxSplits: 1).first.map(String.init) ?? t
            let local = host.hasPrefix("localhost") || host.hasPrefix("127.") || host.hasPrefix("0.0.0.0") || host.hasPrefix("192.168.")
                || host.hasPrefix("10.") || host.split(separator: ":").first?.hasSuffix(".local") == true
            return URL(string: (local ? "http://" : "https://") + t)
        }
        var c = URLComponents(string: "https://duckduckgo.com/")
        c?.queryItems = [URLQueryItem(name: "q", value: t)]
        return c?.url
    }

    /// An address as the bar shows it while you aren't typing.
    static func shown(_ url: URL?) -> String {
        guard let url else { return "" }
        var s = url.absoluteString
        for p in ["https://", "http://"] where s.hasPrefix(p) {
            s.removeFirst(p.count)
            break
        }
        return s.hasSuffix("/") && s.filter({ $0 == "/" }).count == 1 ? String(s.dropLast()) : s
    }
}

/// The browser cards' web views, by card, the most used ten.
@MainActor
final class WebPool {
    static let shared = WebPool()
    private var pages: [String: WebPage] = [:]
    private var used: [String] = []
    private let keep = 10

    func page(for id: String, url: String?) -> WebPage {
        if used.last != id {
            used.removeAll { $0 == id }
            used.append(id)
        }
        if let p = pages[id] { return p }
        let p = WebPage(start: url)
        pages[id] = p
        while used.count > keep {
            pages[used.removeFirst()] = nil
        }
        return p
    }

    func drop(_ id: String) {
        pages[id] = nil
        used.removeAll { $0 == id }
    }
}

/// One card's web view and how it stands.
@MainActor
final class WebPage: ObservableObject {
    let web: WKWebView
    @Published private(set) var url: URL?
    @Published private(set) var title: String?
    @Published private(set) var canGoBack = false
    @Published private(set) var canGoForward = false
    @Published private(set) var loading = false
    @Published private(set) var progress = 0.0
    @Published var failed: String?
    /// Told of each page's address and title, which its card keeps.
    var changed: ((URL?, String?) -> Void)?
    private var watches: [NSKeyValueObservation] = []
    private let guide = WebGuide()

    /// One store for every card this run, which keeps nothing after it.
    private static let data = WKWebsiteDataStore.nonPersistent()

    init(start: String?) {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = Self.data
        web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = guide
        web.uiDelegate = guide
        web.allowsBackForwardNavigationGestures = true
        web.allowsMagnification = true
        guide.page = self
        watches = [
            web.observe(\.url, options: [.new]) { [weak self] _, c in
                let v = c.newValue ?? nil
                onMain { self?.url = v; self?.report() }
            },
            web.observe(\.title, options: [.new]) { [weak self] _, c in
                let v = c.newValue ?? nil
                onMain { self?.title = v; self?.report() }
            },
            web.observe(\.canGoBack, options: [.new]) { [weak self] _, c in
                let v = c.newValue ?? false
                onMain { self?.canGoBack = v }
            },
            web.observe(\.canGoForward, options: [.new]) { [weak self] _, c in
                let v = c.newValue ?? false
                onMain { self?.canGoForward = v }
            },
            web.observe(\.isLoading, options: [.new]) { [weak self] _, c in
                let v = c.newValue ?? false
                onMain { self?.loading = v }
            },
            web.observe(\.estimatedProgress, options: [.new]) { [weak self] _, c in
                let v = c.newValue ?? 0
                onMain { self?.progress = v }
            },
        ]
        if let s = start, let u = URL(string: s) { web.load(URLRequest(url: u)) }
    }

    func go(_ url: URL) {
        failed = nil
        web.load(URLRequest(url: url))
    }

    private func report() {
        changed?(url, title)
    }
}

/// Runs work on the main actor: at once from the main thread, where WebKit
/// sends its news, else soon.
private func onMain(_ work: @escaping @MainActor () -> Void) {
    if Thread.isMainThread {
        MainActor.assumeIsolated(work)
    } else {
        Task { @MainActor in work() }
    }
}

/// A card's navigation: pages of the web load in the card, and a new window
/// too; a link to another app opens that app.
private final class WebGuide: NSObject, WKNavigationDelegate, WKUIDelegate {
    weak var page: WebPage?

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let target = action.request.url, let scheme = target.scheme?.lowercased() else { return decisionHandler(.cancel) }
        if ["http", "https", "about", "data", "blob"].contains(scheme) {
            decisionHandler(.allow)
            return
        }
        if action.navigationType == .linkActivated { NSWorkspace.shared.open(target) }
        decisionHandler(.cancel)
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let target = action.request.url {
            onMain { webView.load(URLRequest(url: target)) }
        }
        return nil
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        failed(error)
    }

    func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
        onMain { [weak self] in self?.page?.failed = nil }
    }

    private func failed(_ error: Error) {
        let e = error as NSError
        // A page left before it loaded is no failure.
        guard !(e.domain == NSURLErrorDomain && e.code == NSURLErrorCancelled) else { return }
        let text = e.localizedDescription
        onMain { [weak self] in self?.page?.failed = text }
    }
}

/// A browser card.
@MainActor
struct BrowserPane: View {
    @Environment(Store.self) private var store
    let session: Session
    let pane: DeckPane
    let shows: Bool
    let token: Int

    var body: some View {
        if store.isFixture {
            BrowserSketch(pane: pane)
        } else {
            BrowserLive(session: session, pane: pane, page: WebPool.shared.page(for: pane.id, url: pane.url), shows: shows, token: token)
        }
    }
}

/// The address bar over the page, or over where to go first.
@MainActor
private struct BrowserLive: View {
    @Environment(Store.self) private var store
    let session: Session
    let pane: DeckPane
    @ObservedObject var page: WebPage
    let shows: Bool
    let token: Int
    @State private var text = ""
    @FocusState private var editing: Bool

    var body: some View {
        VStack(spacing: 0) {
            bar
            ZStack {
                WebHost(page: page, token: token, takesKeys: page.url != nil && !editing)
                    .opacity(page.url == nil ? 0 : 1)
                if page.url == nil { start }
                if let f = page.failed { failure(f) }
            }
            .clipShape(UnevenRoundedRectangle(bottomLeadingRadius: 9, bottomTrailingRadius: 9, style: .continuous))
        }
        .onAppear {
            page.changed = { [weak store] url, title in
                store?.rolodex.update(pane.id, in: session.id, url: url?.absoluteString, title: title)
            }
            text = Address.shown(page.url)
            if store.rolodex.keys == pane.id, page.url == nil { editing = true }
        }
        .onChange(of: page.url) { _, u in if !editing { text = Address.shown(u) } }
        // Sent the keyboard: a card with no page yet types its address.
        .onChange(of: token) { if page.url == nil { editing = true } }
    }

    private var bar: some View {
        HStack(spacing: 2) {
            icon("chevron.left", help: "Back", off: !page.canGoBack) { page.web.goBack() }
            icon("chevron.right", help: "Forward", off: !page.canGoForward) { page.web.goForward() }
            icon(page.loading ? "xmark" : "arrow.clockwise", help: page.loading ? "Stop" : "Reload", off: page.url == nil) {
                if page.loading { page.web.stopLoading() } else { page.web.reload() }
            }
            TextField("Address, a port such as 3000, or words to search", text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: 12))
                .focused($editing)
                .onSubmit { go(text) }
                .onExitCommand {
                    text = Address.shown(page.url)
                    editing = false
                }
                .padding(.horizontal, 9)
                .frame(height: 26)
                .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Theme.field))
                .overlay(RoundedRectangle(cornerRadius: 7, style: .continuous)
                    .strokeBorder(editing ? Theme.agent(slot: pane.slot) : Theme.edge, lineWidth: editing ? 1.4 : 1))
                .overlay(alignment: .bottomLeading) {
                    if page.loading {
                        GeometryReader { g in
                            Capsule().fill(Theme.agent(slot: pane.slot))
                                .frame(width: max(8, g.size.width * page.progress), height: 2)
                                .offset(y: g.size.height - 2)
                        }
                        .allowsHitTesting(false)
                    }
                }
                .padding(.horizontal, 4)
            icon("arrow.up.right.square", help: "Open this page in your browser", off: page.url == nil) {
                if let u = page.url { NSWorkspace.shared.open(u) }
            }
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 5)
    }

    private func icon(_ symbol: String, help: String, off: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(off ? Theme.muted.opacity(0.45) : Theme.muted)
                .frame(width: 24, height: 24)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(off)
        .help(help)
        .accessibilityLabel(help)
    }

    /// Where to go, before the card has a page: the pages typed lately, and
    /// the ports apps most often serve on.
    private var start: some View {
        let picks = (store.rolodex.recent + ["localhost:3000", "localhost:5173", "localhost:8080"]).reduce(into: [String]()) { out, s in
            if !out.contains(s), out.count < 6 { out.append(s) }
        }
        return VStack(alignment: .leading, spacing: 10) {
            Text("Where to?")
                .font(Theme.font(15, .bold))
                .foregroundStyle(Theme.ink)
            Text("Type an address above, a port such as 3000 for your own app, or words to search.")
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 4) {
                ForEach(picks, id: \.self) { s in
                    Button { go(s) } label: {
                        Label(s, systemImage: s.hasPrefix("localhost") ? "laptopcomputer" : "clock.arrow.circlepath")
                            .font(Theme.font(12, .medium))
                            .foregroundStyle(Theme.accentInk)
                            .padding(.vertical, 3)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(18)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Theme.card)
    }

    private func failure(_ text: String) -> some View {
        VStack(spacing: 8) {
            Text("The page didn’t load")
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text(text)
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
            Button("Try Again") { if let u = page.url { page.go(u) } else { page.web.reload() } }
                .controlSize(.small)
        }
        .padding(18)
        .frame(maxWidth: 320)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.panel).shadow(radius: 4, y: 1))
    }

    private func go(_ typed: String) {
        guard let u = Address.url(from: typed) else { return }
        page.go(u)
        text = Address.shown(u)
        if u.host != nil, !(u.host ?? "").hasSuffix("duckduckgo.com") { store.rolodex.noteRecent(Address.shown(u)) }
        editing = false
        DispatchQueue.main.async { page.web.window?.makeFirstResponder(page.web) }
    }
}

/// Holds a card's web view, which the pool keeps when the card goes, and
/// gives it the keyboard when the card is sent it.
private struct WebHost: NSViewRepresentable {
    let page: WebPage
    let token: Int
    let takesKeys: Bool

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSView {
        let box = NSView()
        place(page.web, in: box)
        context.coordinator.token = token
        return box
    }

    func updateNSView(_ box: NSView, context: Context) {
        if page.web.superview !== box { place(page.web, in: box) }
        if context.coordinator.token != token {
            context.coordinator.token = token
            if takesKeys { box.window?.makeFirstResponder(page.web) }
        }
    }

    static func dismantleNSView(_ box: NSView, coordinator _: Coordinator) {
        for v in box.subviews { v.removeFromSuperview() }
    }

    private func place(_ web: WKWebView, in box: NSView) {
        web.removeFromSuperview()
        web.frame = box.bounds
        web.autoresizingMask = [.width, .height]
        box.addSubview(web)
    }

    final class Coordinator {
        var token = 0
    }
}

/// What the snapshots show for a browser card.
private struct BrowserSketch: View {
    let pane: DeckPane

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "chevron.left")
                Image(systemName: "chevron.right").opacity(0.4)
                Image(systemName: "arrow.clockwise")
                Text(Address.shown(pane.url.flatMap(URL.init(string:))))
                    .font(.system(size: 12))
                    .foregroundStyle(Theme.ink)
                    .padding(.horizontal, 9)
                    .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Theme.field))
                    .overlay(RoundedRectangle(cornerRadius: 7, style: .continuous).strokeBorder(Theme.edge))
                Image(systemName: "arrow.up.right.square")
            }
            .font(.system(size: 11, weight: .semibold))
            .foregroundStyle(Theme.muted)
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            VStack(spacing: 10) {
                Text("Acme").font(.system(size: 20, weight: .bold)).foregroundStyle(Color(white: 0.15))
                Text("Sign in to your workspace").font(.system(size: 12)).foregroundStyle(Color(white: 0.4))
                RoundedRectangle(cornerRadius: 6).strokeBorder(Color(white: 0.82)).frame(width: 220, height: 28)
                RoundedRectangle(cornerRadius: 6).strokeBorder(Color(white: 0.82)).frame(width: 220, height: 28)
                RoundedRectangle(cornerRadius: 6).fill(Color(red: 0.23, green: 0.44, blue: 0.85)).frame(width: 220, height: 28)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.white)
            .clipShape(UnevenRoundedRectangle(bottomLeadingRadius: 9, bottomTrailingRadius: 9, style: .continuous))
        }
    }
}
