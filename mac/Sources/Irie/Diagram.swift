import SwiftUI
import WebKit

/// A Mermaid diagram from an agent's reply, drawn in the palette's colours
/// (docs/proposals/learning.md §4.8). In a tutor mode with learn.visual on,
/// agents draw one where a picture helps.
///
/// Mermaid's own library (MIT), bundled with the app at a pinned version
/// (mac/build.sh), draws it in a web view that loads nothing but that script:
/// no network, no files, and Mermaid's strict security level, so a diagram
/// can neither fetch nor run anything. Without the library, or when the
/// diagram does not parse, it shows as its text, as a code block would.
struct Diagram: View {
    let source: String

    @Environment(\.colorScheme) private var scheme
    @State private var height: CGFloat = 0
    @State private var failed = false
    @State private var showSource = false

    /// Mermaid's script, from the app's resources; nil in a build without it.
    static let script: Data? = Bundle.main.url(forResource: "mermaid.min", withExtension: "js").flatMap { try? Data(contentsOf: $0) }

    var body: some View {
        Group {
            if Self.script == nil || failed || showSource {
                Text(source)
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, Theme.inset)
                    .padding(.vertical, 10)
            } else {
                DiagramWeb(html: page, height: $height, failed: $failed)
                    .frame(height: max(height, 48))
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Diagram")
                    .accessibilityValue(source.split(separator: "\n").first.map(String.init) ?? "")
                    .accessibilityHint("Show Source shows its text")
            }
        }
        .background(RoundedRectangle(cornerRadius: Theme.radius - 2, style: .continuous).fill(Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius - 2, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: Theme.radius - 2, style: .continuous))
        .overlay(alignment: .topTrailing) {
            if Self.script != nil && !failed {
                Button(showSource ? "Show Diagram" : "Show Source") { showSource.toggle() }
                    .buttonStyle(.borderless)
                    .font(Theme.font(11, .semibold))
                    .foregroundStyle(Theme.muted)
                    .padding(6)
            }
        }
        // A new palette or appearance draws it afresh, in the new colours.
        .id("\(Theme.palette.id)-\(scheme == .dark)")
    }

    /// The page that draws it: the source goes in as a JSON string, never as
    /// markup, and the page's policy loads scripts only from Irie's own scheme.
    private var page: String {
        let s = scheme == .dark ? Theme.palette.dark : Theme.palette.light
        func css(_ hex: UInt32) -> String { String(format: "#%06X", hex) }
        var vars: [String: String] = [
            "background": css(s.panel), "mainBkg": css(s.card), "primaryColor": css(s.card),
            "primaryTextColor": css(s.ink), "primaryBorderColor": css(s.accent), "lineColor": css(s.muted),
            "secondaryColor": css(s.field), "tertiaryColor": css(s.hover), "textColor": css(s.ink),
            "noteBkgColor": css(s.field), "noteTextColor": css(s.ink), "noteBorderColor": css(s.edge),
            "commitLabelColor": css(s.ink), "commitLabelBackground": css(s.card),
            "tagLabelColor": css(s.ink), "tagLabelBackground": css(s.field), "tagLabelBorder": css(s.edge),
            "fontFamily": "-apple-system, system-ui, sans-serif", "fontSize": "13px",
        ]
        // A git graph's branches in the palette's marks, each with a label
        // that reads on it: light marks with dark words in dark mode, deep
        // marks with white words in light mode.
        let dark = scheme == .dark
        let branches = dark ? [s.accent, s.paused, s.frozen, s.done, s.needs, s.sun, s.accentInk, s.muted]
                            : [s.accent, s.paused, s.frozen, s.done, s.needs, s.accentInk, s.muted, s.ink]
        for (i, c) in branches.enumerated() {
            vars["git\(i)"] = css(c)
            vars["gitBranchLabel\(i)"] = dark ? css(s.onAccent) : "#FFFFFF"
        }
        let json = { (v: Any) -> String in
            let d = (try? JSONSerialization.data(withJSONObject: v, options: [.fragmentsAllowed, .sortedKeys])) ?? Data("null".utf8)
            // "</" could end the script element; JSON allows its slash escaped.
            return String(decoding: d, as: UTF8.self).replacingOccurrences(of: "</", with: "<\\/")
        }
        return """
        <!doctype html><html><head><meta charset="utf-8">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src irie-diagram: 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:">
        <style>html,body{margin:0;padding:0;background:\(css(s.panel));}#d{padding:12px 14px;}#d svg{display:block;margin:0 auto;max-width:100%;height:auto;}</style>
        <script src="irie-diagram://local/mermaid.min.js"></script></head><body><div id="d"></div><script>
        const post = (m) => window.webkit.messageHandlers.diagram.postMessage(m);
        try {
          mermaid.initialize({startOnLoad: false, securityLevel: 'strict', theme: 'base', themeVariables: \(json(vars))});
          mermaid.render('irie-diagram', \(json(source))).then((r) => {
            const d = document.getElementById('d');
            d.innerHTML = r.svg;
            post({height: Math.ceil(d.getBoundingClientRect().height)});
          }).catch((e) => post({error: String((e && e.message) || e)}));
        } catch (e) { post({error: String((e && e.message) || e)}); }
        </script></body></html>
        """
    }
}

/// The web view that draws a diagram. It loads only its page and Mermaid's
/// script from Irie's own scheme, keeps nothing between runs, cancels any
/// other navigation, and passes the scroll wheel on, so the conversation
/// still scrolls with the pointer over a diagram.
private struct DiagramWeb: NSViewRepresentable {
    let html: String
    @Binding var height: CGFloat
    @Binding var failed: Bool

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        config.setURLSchemeHandler(DiagramScheme(), forURLScheme: DiagramScheme.name)
        config.userContentController.add(WeakHandler(context.coordinator), name: "diagram")
        let web = PassScrollWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.setAccessibilityElement(false)
        web.loadHTMLString(html, baseURL: URL(string: DiagramScheme.name + "://local/"))
        context.coordinator.loaded = html
        return web
    }

    func updateNSView(_ web: WKWebView, context: Context) {
        context.coordinator.parent = self
        if context.coordinator.loaded != html {
            context.coordinator.loaded = html
            web.loadHTMLString(html, baseURL: URL(string: DiagramScheme.name + "://local/"))
        }
    }

    static func dismantleNSView(_ web: WKWebView, coordinator: Coordinator) {
        web.configuration.userContentController.removeScriptMessageHandler(forName: "diagram")
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
        var parent: DiagramWeb
        var loaded = ""

        init(_ parent: DiagramWeb) { self.parent = parent }

        func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
            guard let m = message.body as? [String: Any] else { return }
            if let h = m["height"] as? Double, h > 0 {
                parent.height = CGFloat(h)
            } else if m["error"] != nil {
                parent.failed = true
            }
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            // Only the page itself loads: a link in a diagram goes nowhere.
            let scheme = action.request.url?.scheme
            decisionHandler(scheme == DiagramScheme.name || scheme == "about" ? .allow : .cancel)
        }
    }
}

/// Serves Mermaid's script, and nothing else, to a diagram's page.
private final class DiagramScheme: NSObject, WKURLSchemeHandler {
    static let name = "irie-diagram"

    func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
        guard let url = task.request.url, url.lastPathComponent == "mermaid.min.js", let data = Diagram.script else {
            task.didFailWithError(URLError(.fileDoesNotExist))
            return
        }
        task.didReceive(URLResponse(url: url, mimeType: "text/javascript", expectedContentLength: data.count, textEncodingName: "utf-8"))
        task.didReceive(data)
        task.didFinish()
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
}

/// Holds the coordinator weakly: the content controller keeps its handlers,
/// which would otherwise keep the coordinator and its view forever.
private final class WeakHandler: NSObject, WKScriptMessageHandler {
    weak var target: WKScriptMessageHandler?

    init(_ target: WKScriptMessageHandler) { self.target = target }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        target?.userContentController(controller, didReceive: message)
    }
}

/// A web view that hands the scroll wheel to the view around it.
private final class PassScrollWebView: WKWebView {
    override func scrollWheel(with event: NSEvent) {
        nextResponder?.scrollWheel(with: event)
    }
}
