#if !NOFOUNDING
import AppKit
import SwiftUI
import WebKit

// The Charters Graph window (DESIGN §4.17): the charters as a map to
// explore, the texts on a timeline by when and where they were made, their
// parts and the rights they name, and the links between them. The page is
// the core's (internal/founding, page.go), one file in a folder of its own;
// this window shows it in a web view that loads nothing from the network
// and reads that folder alone. Window › Charters Graph opens it, and so
// does the Charters tab's Explore button, at the text the tab shows.

// MARK: Wire

/// Asks the founding op for the page.
struct ChartersGraphArgs: Encodable {
    var graph = true
}

struct ChartersGraphResult: Decodable {
    var page: String?
}

// MARK: Menu

/// Window › Charters Graph, while the founding plugin is on. The window's
/// own item, which SwiftUI would add whatever the plugin, is removed
/// (IrieApp.swift).
struct ChartersGraphCommands: Commands {
    let menu: MenuState
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        let on = MainActor.assumeIsolated { menu.facts.plugins.contains { $0.name == "founding" && $0.on } }
        CommandGroup(before: .windowList) {
            if on {
                Button("Charters Graph") { openWindow(id: ChartersGraphWindow.id) }
                Divider()
            }
        }
    }
}

// MARK: Window

/// The Charters Graph window: the page, or why it isn't there.
@MainActor
struct ChartersGraphWindow: View {
    static let id = "charters-graph"

    @Environment(Store.self) private var store
    @State private var page: String?
    @State private var problem: String?

    var body: some View {
        content
            .frame(minWidth: 720, minHeight: 480)
            .background(Theme.sidebar)
            .tint(Theme.accent)
            .toolbar {
                if let page {
                    ToolbarItem(placement: .primaryAction) {
                        Menu {
                            Button("Open in Browser") { NSWorkspace.shared.open(URL(fileURLWithPath: page)) }
                            Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: page)]) }
                        } label: {
                            Label("More", systemImage: "ellipsis.circle")
                        }
                        .help("More for the graph")
                    }
                }
            }
            .task { await look() }
    }

    @ViewBuilder private var content: some View {
        if let page {
            ChartersGraphWeb(page: URL(fileURLWithPath: page), show: store.founding.graphShow)
        } else if let problem {
            VStack(spacing: 12) {
                Image(systemName: "point.3.connected.trianglepath.dotted")
                    .font(.system(size: 34, weight: .light))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                Text("The graph isn’t here")
                    .font(Theme.font(17, .semibold))
                    .foregroundStyle(Theme.ink)
                Text(problem)
                    .font(Theme.font(13))
                    .foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 440)
                    .textSelection(.enabled)
                Button("Try Again") { Task { await look() } }
                    .keyboardShortcut(.defaultAction)
                    .padding(.top, 6)
            }
            .padding(32)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func look() async {
        do {
            page = try await store.chartersGraphPage()
            problem = nil
        } catch let e as WireError {
            problem = e.msg
        } catch {
            problem = "The graph comes from \(Brand.name)’s core, which isn’t answering. Start it in a terminal with `\(Brand.slug) up`."
        }
    }
}

// MARK: Page

/// The graph's page: a web view that reads only the page's folder and loads
/// nothing from the network, so nothing it shows comes from elsewhere. A
/// pinch zooms the map, which the page takes for its own, never the view.
/// When the tab asks for another text, the page picks it as a click in the
/// page would; the page has no bridge back to the app.
private struct ChartersGraphWeb: NSViewRepresentable {
    let page: URL
    /// The text to show, and a count that changes with each ask, so asking
    /// for the same one again brings it back into view.
    let show: FoundingModel.GraphShow?

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.uiDelegate = context.coordinator
        web.allowsMagnification = true
        web.allowsBackForwardNavigationGestures = false
        context.coordinator.shown = show
        let start = Self.at(page, show?.id)
        let folder = page.deletingLastPathComponent()
        Self.offline { list in
            guard let list else {
                web.loadHTMLString(Self.unblocked, baseURL: nil)
                return
            }
            config.userContentController.add(list)
            web.loadFileURL(start, allowingReadAccessTo: folder)
        }
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        let c = context.coordinator
        guard let show, show != c.shown else { return }
        c.shown = show
        guard let id = try? String(data: JSONEncoder().encode(show.id), encoding: .utf8) else { return }
        web.evaluateJavaScript("window.chartersGraph && window.chartersGraph.pick(\(id), {fly: true})")
    }

    /// The page's address, with a text to pick in its fragment.
    private static func at(_ page: URL, _ id: String?) -> URL {
        guard let id, var c = URLComponents(url: page, resolvingAgainstBaseURL: false) else { return page }
        c.fragment = id
        return c.url ?? page
    }

    /// What shows instead of the page when the network can't be blocked.
    private static let unblocked = """
    <p style="font: 13px -apple-system, sans-serif; color: #888; text-align: center; margin-top: 30vh">\
    The graph isn’t shown: this window couldn’t keep the page off the network. Open it from the toolbar’s More menu instead.</p>
    """

    /// Compiles, once, a content rule list that blocks every load from the
    /// network; nil when it does not compile.
    private static func offline(done: @escaping (WKContentRuleList?) -> Void) {
        let rules = """
        [{"trigger":{"url-filter":"^https?:"},"action":{"type":"block"}},
         {"trigger":{"url-filter":"^wss?:"},"action":{"type":"block"}}]
        """
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: "\(Brand.slug)-charters-offline", encodedContentRuleList: rules) { list, _ in
            DispatchQueue.main.async { done(list) }
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        var shown: FoundingModel.GraphShow?

        /// The page moves within itself, by its address's fragment; it has
        /// no links out, and any other load goes nowhere.
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let target = action.request.url else { return decisionHandler(.cancel) }
            decisionHandler(target.isFileURL || target.scheme == "about" ? .allow : .cancel)
        }

        /// The page opens no windows.
        func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                     for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
            nil
        }
    }
}

// MARK: Store

extension Store {
    /// Where the Charters Graph page is, which the core writes as it's asked.
    func chartersGraphPage() async throws -> String {
        let r = try await client.call("founding", ChartersGraphArgs(), timeout: 20, as: ChartersGraphResult.self)
        guard let page = r.page, !page.isEmpty else {
            throw WireError(code: "internal", msg: "\(Brand.name)’s core gave no page for the graph. An older core may not have it: restart \(Brand.name).")
        }
        return page
    }
}
#endif
