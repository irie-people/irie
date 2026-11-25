#if !NOFOUNDING
import AppKit
import SwiftUI
import WebKit

// The Common Schools window (docs/proposals/common-schools.md): how public
// schools began in the United States, as pies that open into smaller
// ones, with the laws behind them. The page is the core's
// (internal/schools, page.go), one file in a folder of its own; this
// window shows it in a web view that loads nothing from the network and
// reads that folder alone, as the Charters Graph's does. Window › Common
// Schools opens it, and so does the Plugins menu's Common Schools part.

// MARK: Wire

/// Asks the founding op for the Common Schools page.
struct CommonSchoolsArgs: Encodable {
    var schools = true
}

// MARK: Menu

/// Window › Common Schools, while the plugin's part is on. The window's own
/// item, which SwiftUI would add whatever the plugin, is removed
/// (IrieApp.swift).
struct CommonSchoolsCommands: Commands {
    let menu: MenuState
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        let on = MainActor.assumeIsolated {
            menu.facts.plugins.contains { $0.name == "founding" && $0.on && $0.parts.contains { $0.name == "schools" && $0.isOn } }
        }
        CommandGroup(before: .windowList) {
            if on {
                Button("Common Schools") { openWindow(id: CommonSchoolsWindow.id) }
                Divider()
            }
        }
    }
}

// MARK: Window

/// The Common Schools window: the page, or why it isn't there.
@MainActor
struct CommonSchoolsWindow: View {
    static let id = "common-schools"

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
                        .help("More for the page")
                    }
                }
            }
            .task { await look() }
    }

    @ViewBuilder private var content: some View {
        if let page {
            CommonSchoolsWeb(page: URL(fileURLWithPath: page))
        } else if let problem {
            VStack(spacing: 12) {
                Image(systemName: "chart.pie")
                    .font(.system(size: 34, weight: .light))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                Text("The page isn’t here")
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
            page = try await store.commonSchoolsPage()
            problem = nil
        } catch let e as WireError {
            problem = e.msg
        } catch {
            problem = "The page comes from \(Brand.name)’s core, which isn’t answering. Start it in a terminal with `\(Brand.slug) up`."
        }
    }
}

// MARK: Page

/// The page: a web view that reads only the page's folder and loads nothing
/// from the network, so nothing it shows comes from elsewhere.
private struct CommonSchoolsWeb: NSViewRepresentable {
    let page: URL

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.preferences.javaScriptCanOpenWindowsAutomatically = false
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.uiDelegate = context.coordinator
        web.allowsBackForwardNavigationGestures = false
        let folder = page.deletingLastPathComponent()
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

    func updateNSView(_ web: WKWebView, context: Context) {}

    /// What shows instead of the page when the network can't be blocked.
    private static let unblocked = """
    <p style="font: 13px -apple-system, sans-serif; color: #888; text-align: center; margin-top: 30vh">\
    The page isn’t shown: this window couldn’t keep it off the network. Open it from the toolbar’s More menu instead.</p>
    """

    /// Compiles, once, a content rule list that blocks every load from the
    /// network; nil when it does not compile.
    private static func offline(done: @escaping (WKContentRuleList?) -> Void) {
        let rules = """
        [{"trigger":{"url-filter":"^https?:"},"action":{"type":"block"}},
         {"trigger":{"url-filter":"^wss?:"},"action":{"type":"block"}}]
        """
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: "\(Brand.slug)-schools-offline", encodedContentRuleList: rules) { list, _ in
            DispatchQueue.main.async { done(list) }
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate {
        /// The page moves within itself, by its address's fragment; a link
        /// to a source goes nowhere here (the page's More menu opens it in
        /// a browser), and any other load goes nowhere.
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
    /// Where the Common Schools page is, which the core writes as it's asked.
    func commonSchoolsPage() async throws -> String {
        let r = try await client.call("founding", CommonSchoolsArgs(), timeout: 20, as: ChartersGraphResult.self)
        guard let page = r.page, !page.isEmpty else {
            throw WireError(code: "internal", msg: "\(Brand.name)’s core gave no page for Common Schools. An older core may not have it: restart \(Brand.name).")
        }
        return page
    }
}
#endif
