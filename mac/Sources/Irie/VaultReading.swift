import AVKit
import AppKit
import PDFKit
import SwiftUI

// A Vault card's note in Reading view, as Obsidian's help has it ("Views
// and editing mode"): its Markdown drawn rather than written, its [[links]]
// a click away, its tasks ticked with a click, its callouts, embeds and
// properties drawn, and its %%comments%% and ^block ids out of sight.
// VaultMarkdown.swift reads the note; this draws it, in the card's colours.

/// What the Reading view's parts share: the card's colours and size, the
/// vault and the note, and what a click on a link or a task does.
struct VaultReadingContext {
    let theme: CodeTheme
    let size: CGFloat
    let folder: String
    var note: String
    let accent: Color
    let fixture: Bool
    /// 0 for the note on show, 1 for a note embedded in it, whose own
    /// embeds show as links, so a note that embeds itself ends there.
    var depth = 0
    /// Follows a link, as a ⌘-click in the editor does (VaultPane.follow).
    let open: (String) -> Void
    /// Ticks or unticks the task on a line of the note, from 0; nil in an
    /// embedded note, whose lines are another note's.
    var tick: ((Int) -> Void)?

    var ink: Color { Color(nsColor: theme.foreground) }
    var muted: Color { Color(nsColor: theme.gutterInk) }
    var linkInk: Color { Color(nsColor: theme.color(.link)) }

    @MainActor func words(_ markdown: String) -> Text { Text(VaultWords.styled(markdown, theme: theme)) }
}

/// The note on show in Reading view. It reads the document, a beat behind
/// the typing when the editor shows beside it, so what is typed shows
/// before it is saved, and a tick goes into the document as typing would.
@MainActor
struct VaultReading: View {
    let doc: CodeDocument
    let ctx: VaultReadingContext
    /// Where a link asked to go: a heading's or a block's line, from 1.
    let goTo: CodeGoTo?
    /// Done with a goTo; nil while the editor beside it takes them.
    let went: ((CodeGoTo) -> Void)?
    @State private var blocks: [VaultBlock]?

    /// The card's colours decide light or dark, as the code card's pretty
    /// view has it: the code colours may be dark in a light window.
    private var scheme: ColorScheme {
        let c = ctx.theme.background.usingColorSpace(.sRGB) ?? ctx.theme.background
        let light = 0.2126 * c.redComponent + 0.7152 * c.greenComponent + 0.0722 * c.blueComponent
        return light < 0.5 ? .dark : .light
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                Group {
                    if let blocks = shown {
                        if blocks.isEmpty {
                            Text("Nothing to read yet. ⌘E to write.")
                                .font(.system(size: ctx.size))
                                .foregroundStyle(ctx.muted)
                        } else {
                            VaultBlocks(blocks: blocks, ctx: ctx, top: true)
                        }
                    }
                }
                .frame(maxWidth: 720, alignment: .leading)
                .padding(.horizontal, 24)
                .padding(.vertical, 18)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .onChange(of: goTo) { _, g in go(g, proxy, animated: true) }
            // A note opened at a heading: there once it is laid out.
            .onAppear { DispatchQueue.main.async { go(goTo, proxy, animated: false) } }
        }
        .background(Color(nsColor: ctx.theme.background))
        .environment(\.colorScheme, scheme)
        .tint(ctx.linkInk)
        .environment(\.openURL, OpenURLAction { url in
            guard let link = VaultReading.link(url) else { return .systemAction }
            ctx.open(link)
            return .handled
        })
        .task(id: doc.revision) {
            if blocks != nil {
                try? await Task.sleep(for: .milliseconds(150))
                if Task.isCancelled { return }
            }
            // A copy: the storage goes on changing as the typing does.
            let text = (doc.storage.mutableString.copy() as? String) ?? ""
            let parsed = await Task.detached(priority: .userInitiated) { VaultMarkdown.parse(text) }.value
            if !Task.isCancelled { blocks = parsed }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(VaultPane.title(doc.path)), in Reading view")
    }

    /// The blocks on show: the last read, or on the first draw the note
    /// read at once, so it never shows blank, and `--snapshot`, which draws
    /// before a task runs, shows it too.
    private var shown: [VaultBlock]? {
        if let blocks { return blocks }
        guard doc.storage.length < 300_000 else { return nil }
        return VaultReading.read(doc)
    }

    /// The notes read at once, by path, with the revision and length they
    /// were read at.
    private static var firsts: [String: (revision: Int, length: Int, blocks: [VaultBlock])] = [:]

    private static func read(_ doc: CodeDocument) -> [VaultBlock] {
        if let hit = firsts[doc.path], hit.revision == doc.revision, hit.length == doc.storage.length { return hit.blocks }
        let blocks = VaultMarkdown.parse(doc.storage.string)
        if firsts.count > 50 { firsts.removeAll() }
        firsts[doc.path] = (doc.revision, doc.storage.length, blocks)
        return blocks
    }

    /// Scrolls to the block a link's heading or ^block is in.
    private func go(_ g: CodeGoTo?, _ proxy: ScrollViewProxy, animated: Bool) {
        guard let g, g.path == doc.path, let blocks = shown, let first = blocks.first else { return }
        let to = blocks.last { $0.line + 1 <= g.line } ?? first
        if animated {
            withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo(to.line, anchor: .top) }
        } else {
            proxy.scrollTo(to.line, anchor: .top)
        }
        went?(g)
    }

    /// A link the words hold, as VaultPane's follow takes links: a note's
    /// words, a path from the note, or a page's address; nil for one the
    /// Mac opens itself, as a mailto: one.
    nonisolated static func link(_ url: URL) -> String? {
        let scheme = url.scheme?.lowercased()
        if scheme == VaultMarkdown.noteScheme {
            let s = url.absoluteString.dropFirst(VaultMarkdown.noteScheme.count + 1)
            return s.removingPercentEncoding ?? String(s)
        }
        if scheme == nil || scheme == "http" || scheme == "https" { return url.absoluteString }
        return nil
    }
}

/// Blocks one under another, as far apart as Obsidian's; at the note's
/// top each is named by its line, to scroll to.
@MainActor
struct VaultBlocks: View {
    let blocks: [VaultBlock]
    let ctx: VaultReadingContext
    var top = false

    var body: some View {
        if top {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(Array(blocks.enumerated()), id: \.element.line) { i, b in
                    row(i, b).id(b.line)
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(blocks.enumerated()), id: \.element.line) { i, b in row(i, b) }
            }
        }
    }

    private func row(_ i: Int, _ b: VaultBlock) -> some View {
        VaultBlockView(block: b, ctx: ctx)
            .padding(.top, i == 0 ? 0 : VaultBlocks.gap(blocks[i - 1], b))
    }

    nonisolated static func gap(_ a: VaultBlock, _ b: VaultBlock) -> CGFloat {
        switch (a.kind, b.kind) {
        case (.item, .item): return 4
        case let (_, .heading(level, _)): return level <= 2 ? 20 : 14
        case (.heading, _): return 8
        default: return 11
        }
    }
}

/// One block.
@MainActor
struct VaultBlockView: View {
    let block: VaultBlock
    let ctx: VaultReadingContext

    /// The headings' sizes, from the words'.
    static let scale: [CGFloat] = [1.75, 1.45, 1.25, 1.1, 1.0, 1.0]

    var body: some View {
        switch block.kind {
        case let .properties(list):
            properties(list)
        case let .heading(level, text):
            ctx.words(text)
                .font(.system(size: ctx.size * VaultBlockView.scale[min(max(level, 1), 6) - 1], weight: level <= 2 ? .bold : .semibold))
                .foregroundStyle(level == 6 ? ctx.muted : ctx.ink)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        case let .paragraph(text):
            ctx.words(text)
                .font(.system(size: ctx.size))
                .foregroundStyle(ctx.ink)
                .lineSpacing(3)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        case let .item(it):
            item(it)
        case let .code(info, text):
            VaultCode(info: info, text: text, ctx: ctx)
        case let .quote(inner):
            HStack(alignment: .top, spacing: 12) {
                RoundedRectangle(cornerRadius: 1.5).fill(ctx.muted.opacity(0.55)).frame(width: 3)
                VaultBlocks(blocks: inner, ctx: ctx)
            }
            .fixedSize(horizontal: false, vertical: true)
        case let .callout(c):
            VaultCalloutView(callout: c, ctx: ctx)
        case let .table(rows):
            table(rows)
        case let .embed(e):
            VaultEmbedView(embed: e, ctx: ctx)
        case .rule:
            Rectangle().fill(ctx.muted.opacity(0.35)).frame(height: 1).padding(.vertical, 4)
        }
    }

    /// An item; a task's box ticks it, or unticks it, as in Obsidian's
    /// Reading view, and a done task's words are struck through.
    private func item(_ it: VaultItem) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            if it.task != nil {
                Button { ctx.tick?(block.line) } label: {
                    Image(systemName: it.done ? "checkmark.square.fill" : "square")
                        .font(.system(size: ctx.size))
                        .foregroundStyle(it.done ? ctx.accent : ctx.muted)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .allowsHitTesting(ctx.tick != nil)
                .help(it.done ? "Not done after all" : "Done")
                .accessibilityLabel(it.done ? "Done: untick it" : "To do: tick it")
            } else {
                Text(it.marker)
                    .font(.system(size: ctx.size))
                    .foregroundStyle(ctx.muted)
                    .monospacedDigit()
            }
            ctx.words(it.text)
                .strikethrough(it.done, color: ctx.muted)
                .font(.system(size: ctx.size))
                .foregroundStyle(it.done ? ctx.muted : ctx.ink)
                .lineSpacing(3)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.leading, CGFloat(it.depth) * 22)
    }

    /// The note's properties, as Obsidian shows them at its top; tags as
    /// tags.
    private func properties(_ list: [VaultProperty]) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text("Properties")
                .font(.system(size: ctx.size - 1, weight: .semibold))
                .foregroundStyle(ctx.muted)
            Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 16, verticalSpacing: 5) {
                ForEach(Array(list.enumerated()), id: \.offset) { _, p in
                    GridRow {
                        Text(p.key)
                            .font(.system(size: ctx.size - 1))
                            .foregroundStyle(ctx.muted)
                        ctx.words(VaultBlockView.values(p))
                            .font(.system(size: ctx.size - 1))
                            .foregroundStyle(ctx.ink)
                            .textSelection(.enabled)
                    }
                }
            }
            Rectangle().fill(ctx.muted.opacity(0.25)).frame(height: 1).padding(.top, 4)
        }
    }

    nonisolated static func values(_ p: VaultProperty) -> String {
        guard ["tags", "tag"].contains(p.key.lowercased()) else { return p.values.joined(separator: ", ") }
        return p.values.map { v in
            let name = v.hasPrefix("#") || v.hasPrefix("\\#") ? v : "#" + v
            return String(VaultMarkdown.tagStart) + VaultMarkdown.escaped(name.replacingOccurrences(of: "\\", with: "")) + String(VaultMarkdown.tagEnd)
        }.joined(separator: " ")
    }

    private func table(_ rows: [[String]]) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 6) {
                ForEach(Array(rows.enumerated()), id: \.offset) { i, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            ctx.words(cell)
                                .font(.system(size: ctx.size - 0.5, weight: i == 0 ? .semibold : .regular))
                                .foregroundStyle(ctx.ink)
                                .textSelection(.enabled)
                        }
                    }
                    if i == 0 {
                        Rectangle().fill(ctx.muted.opacity(0.35)).frame(height: 1).gridCellUnsizedAxes(.horizontal)
                    }
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
        }
        .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(ctx.ink.opacity(0.04)))
        .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(ctx.muted.opacity(0.25)))
    }
}

/// A fenced block: in the code card's colours for its language, or a
/// Mermaid diagram drawn.
@MainActor
struct VaultCode: View {
    let info: String
    let text: String
    let ctx: VaultReadingContext

    var body: some View {
        if info == "mermaid" {
            Diagram(source: text)
        } else {
            ScrollView(.horizontal, showsIndicators: false) {
                Text(VaultWords.code(text, info, theme: ctx.theme))
                    .font(.system(size: ctx.size - 1, design: .monospaced))
                    .foregroundStyle(ctx.ink)
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.top, info.isEmpty ? 10 : 22)
                    .padding(.bottom, 10)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(ctx.ink.opacity(0.05)))
            .overlay(alignment: .topTrailing) {
                if !info.isEmpty {
                    Text(info)
                        .font(.system(size: 10))
                        .foregroundStyle(ctx.muted)
                        .padding(6)
                }
            }
        }
    }
}

/// A callout, in its type's colour, with its icon and title; one that
/// folds opens and closes with a click on its title.
@MainActor
struct VaultCalloutView: View {
    let callout: VaultCallout
    let ctx: VaultReadingContext
    @State private var opened: Bool?

    var body: some View {
        let ink = VaultWords.callout(callout.family, theme: ctx.theme)
        let open = opened ?? (callout.fold != "-")
        VStack(alignment: .leading, spacing: 8) {
            if callout.fold != nil {
                Button {
                    withAnimation(.easeOut(duration: 0.15)) { opened = !open }
                } label: {
                    head(ink, open: open)
                }
                .buttonStyle(.plain)
                .help(open ? "Fold it" : "Unfold it")
            } else {
                head(ink, open: open)
            }
            if open, !callout.body.isEmpty {
                VaultBlocks(blocks: callout.body, ctx: ctx)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(ink.opacity(ctx.theme.dark ? 0.16 : 0.1)))
    }

    private func head(_ ink: Color, open: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Image(systemName: VaultWords.icon(callout.family))
            ctx.words(callout.title).fontWeight(.semibold)
            if callout.fold != nil {
                Image(systemName: "chevron.right")
                    .font(.system(size: max(8, ctx.size - 3), weight: .semibold))
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
            Spacer(minLength: 0)
        }
        .font(.system(size: ctx.size))
        .foregroundStyle(ink)
        .contentShape(Rectangle())
    }
}

/// A file shown in the note ("Embed files"): a note's words, or the part
/// a heading or block names, framed under its name; a picture; a PDF at its
/// page; sound or a film to play; any other file, a link to open it. A
/// picture on the web stays a link: the card fetches nothing from outside.
@MainActor
struct VaultEmbedView: View {
    let embed: VaultEmbed
    let ctx: VaultReadingContext
    @State private var found: Found?

    enum Found {
        case looking
        case missing(String)
        case note(String, [VaultBlock])
        case image(NSImage)
        case pdf(URL, page: Int, height: Int)
        case media(URL, video: Bool)
        case file(String)
        case page(URL)
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .task(id: embed) { found = await look() }
    }

    /// What shows: what was found, or the fixture's at once, as
    /// `--snapshot` draws before a task runs.
    private var shown: Found {
        if let found { return found }
        return ctx.fixture ? fixtureLook() : .looking
    }

    @ViewBuilder private var content: some View {
        switch shown {
        case .looking:
            Color.clear.frame(height: ctx.size + 6)
        case let .missing(name):
            Button { ctx.open(embed.link) } label: {
                Label("“\(name)” isn’t in the vault yet", systemImage: "questionmark.square.dashed")
            }
            .buttonStyle(.plain)
            .font(.system(size: ctx.size - 1))
            .foregroundStyle(ctx.muted)
            .help("Make it, as Obsidian does")
        case let .note(path, blocks):
            VStack(alignment: .leading, spacing: 8) {
                link(VaultPane.title(path), "doc.text", weight: .semibold)
                VaultBlocks(blocks: blocks, ctx: inner(path))
            }
            .padding(.leading, 14)
            .padding(.vertical, 6)
            .overlay(alignment: .leading) { Rectangle().fill(ctx.accent.opacity(0.6)).frame(width: 2) }
        case let .image(img):
            Image(nsImage: img)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: embed.width.map(CGFloat.init) ?? img.size.width, maxHeight: embed.height.map(CGFloat.init))
                .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
                .help(embed.shown ?? VaultLinks.target(embed.link).name)
                .accessibilityLabel(embed.shown ?? VaultLinks.target(embed.link).name)
        case let .pdf(url, page, height):
            VaultPDF(url: url, page: page)
                .frame(height: CGFloat(height))
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(ctx.muted.opacity(0.3)))
        case let .media(url, video):
            VaultPlayer(url: url, video: video)
                .frame(maxWidth: video ? CGFloat(embed.width ?? 560) : 420)
                .frame(height: video ? CGFloat(embed.height ?? 300) : 44)
        case let .file(path):
            link((path as NSString).lastPathComponent, "doc")
        case let .page(url):
            link(embed.shown ?? url.absoluteString, "photo", to: url.absoluteString)
                .help("A picture on the web, which the card doesn’t fetch: open it")
        }
    }

    private func link(_ words: String, _ symbol: String, weight: Font.Weight = .regular, to: String? = nil) -> some View {
        Button { ctx.open(to ?? embed.link) } label: {
            Label(words, systemImage: symbol)
                .font(.system(size: ctx.size - 1, weight: weight))
        }
        .buttonStyle(.plain)
        .foregroundStyle(ctx.linkInk)
    }

    private func inner(_ path: String) -> VaultReadingContext {
        var c = ctx
        c.depth += 1
        c.note = path
        c.tick = nil
        return c
    }

    /// The fixture's embed, found from its files at once.
    private func fixtureLook() -> Found {
        let target = VaultLinks.target(embed.link)
        let here = ctx.note.hasPrefix(ctx.folder + "/") ? String(ctx.note.dropFirst(ctx.folder.count + 1)) : nil
        guard let rel = VaultLinks.resolve(target.name, from: here, files: Fixture.vaultFiles) else { return .missing(target.name) }
        let path = (ctx.folder as NSString).appendingPathComponent(rel)
        switch VaultMarkdown.media(rel) {
        case .note:
            guard ctx.depth == 0, let text = Fixture.vaultEmbeds[rel], let part = VaultMarkdown.section(target, of: text) else { return .file(path) }
            return .note(path, VaultMarkdown.parse(part))
        case .image:
            return Fixture.vaultImage.map { Found.image($0) } ?? .file(path)
        default:
            return .file(path)
        }
    }

    /// The file the embed names, found as a [[link]]'s is, and what of it
    /// shows.
    private func look() async -> Found {
        if ctx.fixture { return fixtureLook() }
        let target = VaultLinks.target(embed.link)
        if embed.markdown, let u = URL(string: embed.link), let s = u.scheme?.lowercased() {
            return s == "http" || s == "https" ? .page(u) : .file(embed.link)
        }
        let files = await CodeFiles.shared.list(ctx.folder).filter(VaultLinks.isFile)
        let here = ctx.note.hasPrefix(ctx.folder + "/") ? String(ctx.note.dropFirst(ctx.folder.count + 1)) : nil
        var rel: String?
        // A Markdown image's path is from the note's folder first.
        if embed.markdown {
            let dir = ((here ?? "") as NSString).deletingLastPathComponent
            let joined = ((dir as NSString).appendingPathComponent(target.name) as NSString).standardizingPath
            if files.contains(joined) { rel = joined }
        }
        rel = rel ?? VaultLinks.resolve(target.name, from: here, files: files)
        guard let rel else { return .missing(target.name) }
        let path = (ctx.folder as NSString).appendingPathComponent(rel)
        let url = URL(fileURLWithPath: path)
        switch VaultMarkdown.media(rel) {
        case .note:
            guard ctx.depth == 0 else { return .file(path) }
            let text = await Task.detached(priority: .utility) { try? String(contentsOfFile: path, encoding: .utf8) }.value
            guard let text, let part = VaultMarkdown.section(target, of: text) else { return .missing(VaultMarkdown.display(embed.link)) }
            let blocks = await Task.detached(priority: .utility) { VaultMarkdown.parse(part) }.value
            return .note(path, blocks)
        case .image:
            let data = await Task.detached(priority: .utility) { () -> Data? in
                guard let size = (try? FileManager.default.attributesOfItem(atPath: path))?[.size] as? Int, size < 40 << 20 else { return nil }
                return FileManager.default.contents(atPath: path)
            }.value
            guard let data, let img = NSImage(data: data) else { return .file(path) }
            return .image(img)
        case .pdf:
            // #page=3&height=400
            var page = 1, height = 480
            for part in (target.heading ?? "").split(separator: "&") {
                let kv = part.split(separator: "=", maxSplits: 1)
                guard kv.count == 2, let n = Int(kv[1]) else { continue }
                if kv[0] == "page" { page = max(1, n) }
                if kv[0] == "height" { height = min(2000, max(120, n)) }
            }
            return .pdf(url, page: page, height: height)
        case .audio:
            return .media(url, video: false)
        case .video:
            return .media(url, video: true)
        case .other:
            return .file(path)
        }
    }
}

/// A PDF in the note, at its page.
private struct VaultPDF: NSViewRepresentable {
    let url: URL
    let page: Int

    func makeNSView(context: Context) -> PDFView {
        let v = PDFView()
        v.autoScales = true
        v.displayMode = .singlePageContinuous
        v.backgroundColor = .clear
        if let d = PDFDocument(url: url) {
            v.document = d
            if page > 1, d.pageCount > 0, let p = d.page(at: min(page, d.pageCount) - 1) { v.go(to: p) }
        }
        return v
    }

    func updateNSView(_ v: PDFView, context: Context) {}
}

/// Sound or a film in the note, to play where it is.
private struct VaultPlayer: NSViewRepresentable {
    let url: URL
    let video: Bool

    func makeNSView(context: Context) -> AVPlayerView {
        let v = AVPlayerView()
        v.controlsStyle = .inline
        v.showsFullScreenToggleButton = video
        v.player = AVPlayer(url: url)
        return v
    }

    func updateNSView(_ v: AVPlayerView, context: Context) {}

    static func dismantleNSView(_ v: AVPlayerView, coordinator: ()) {
        v.player?.pause()
        v.player = nil
    }
}

/// The words of a block as Text draws them: Foundation's Markdown, then
/// VaultMarkdown's marks coloured, a tag in the tag colour on a tint of
/// it and a highlight on its colour, the marks themselves taken out.
@MainActor
enum VaultWords {
    private static var cache: [String: AttributedString] = [:]
    private static var codes: [String: AttributedString] = [:]

    static func styled(_ markdown: String, theme: CodeTheme) -> AttributedString {
        let key = theme.id + "\u{1}" + markdown
        if let hit = cache[key] { return hit }
        var a = (try? AttributedString(markdown: markdown, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            ?? AttributedString(markdown)
        mark(&a, theme: theme)
        // A Markdown link to a note, as [words](Some%20Note.md), is a
        // [[link]]'s too.
        for run in a.runs {
            guard let u = run.link, u.scheme == nil else { continue }
            let words = u.absoluteString.removingPercentEncoding ?? u.absoluteString
            if let to = URL(string: VaultMarkdown.noteScheme + ":" + VaultMarkdown.encoded(words)) { a[run.range].link = to }
        }
        if cache.count > 4000 { cache.removeAll() }
        cache[key] = a
        return a
    }

    private static func mark(_ a: inout AttributedString, theme: CodeTheme) {
        let starts = Set(VaultMarkdown.highlightStart + [VaultMarkdown.tagStart])
        while let o = a.characters.firstIndex(where: { starts.contains($0) }) {
            let c = a.characters[o]
            let tag = c == VaultMarkdown.tagStart
            let after = a.characters.index(after: o)
            if let e = a.characters[after...].firstIndex(of: tag ? VaultMarkdown.tagEnd : VaultMarkdown.highlightEnd) {
                if after < e {
                    if tag {
                        let ink = Color(nsColor: theme.color(.tag))
                        a[after..<e].foregroundColor = ink
                        a[after..<e].backgroundColor = ink.opacity(theme.dark ? 0.2 : 0.13)
                    } else {
                        a[after..<e].backgroundColor = highlight(VaultMarkdown.highlightStart.firstIndex(of: c) ?? 0, theme: theme)
                    }
                }
                a.characters.remove(at: e)
            }
            // The start again, found afresh: taking the end out moved what
            // came after it.
            if let o = a.characters.firstIndex(of: c) { a.characters.remove(at: o) }
        }
        while let e = a.characters.firstIndex(where: { $0 == VaultMarkdown.highlightEnd || $0 == VaultMarkdown.tagEnd }) {
            a.characters.remove(at: e)
        }
    }

    /// A highlight's colour: the theme's amber, or the colour an emoji
    /// named, from the theme's own inks, lightly.
    nonisolated static func highlight(_ n: Int, theme: CodeTheme) -> Color {
        let tokens: [CodeToken] = [.number, .deleted, .constant, .number, .string, .function, .keyword]
        return Color(nsColor: theme.color(tokens[min(max(n, 0), tokens.count - 1)])).opacity(theme.dark ? 0.34 : 0.26)
    }

    /// A callout's colour, from the theme's own inks: blue for notes, teal
    /// for tips and summaries, green for success, amber for questions and
    /// warnings, the deleted ink for failures, dangers and bugs, violet
    /// for examples and the quiet ink for quotes.
    nonisolated static func callout(_ family: String, theme: CodeTheme) -> Color {
        let token: CodeToken
        switch family {
        case "abstract", "tip": token = .type
        case "success": token = .string
        case "question", "warning": token = .number
        case "failure", "danger", "bug": token = .deleted
        case "example": token = .keyword
        case "quote": return Color(nsColor: theme.gutterInk)
        default: token = .function
        }
        return Color(nsColor: theme.color(token))
    }

    nonisolated static func icon(_ family: String) -> String {
        switch family {
        case "abstract": return "list.bullet.clipboard"
        case "info": return "info.circle"
        case "todo": return "checkmark.circle"
        case "tip": return "flame"
        case "success": return "checkmark"
        case "question": return "questionmark.circle"
        case "warning": return "exclamationmark.triangle"
        case "failure": return "xmark"
        case "danger": return "bolt"
        case "bug": return "ladybug"
        case "example": return "list.bullet"
        case "quote": return "quote.opening"
        default: return "pencil"
        }
    }

    /// A fenced block's words in the code card's colours for the language
    /// its info string names.
    static func code(_ text: String, _ info: String, theme: CodeTheme) -> AttributedString {
        let key = theme.id + "\u{1}" + info + "\u{1}" + text
        if let hit = codes[key] { return hit }
        var a = AttributedString(text)
        let ns = text as NSString
        let lang = language(info)
        if lang != .plain, ns.length < 100_000 {
            let colorer = SyntaxColorer(language: lang)
            colorer.load(ns)
            var spans: [(NSRange, CodeToken)] = []
            colorer.tokens(in: NSRange(location: 0, length: ns.length)) { r, k in spans.append((r, k)) }
            for (r, k) in spans {
                if let range = Range(r, in: a) { a[range].foregroundColor = Color(nsColor: theme.color(k)) }
            }
        }
        if codes.count > 500 { codes.removeAll() }
        codes[key] = a
        return a
    }

    /// The language an info string names: by name, as Dockerfile, or as a
    /// file's extension, python as .py.
    nonisolated static func language(_ info: String) -> CodeLanguage {
        guard !info.isEmpty else { return .plain }
        let named = CodeLanguage.detect(path: info, firstLine: nil)
        if named != .plain { return named }
        let aliases = ["python": "py", "javascript": "js", "typescript": "ts", "bash": "sh", "shell": "sh", "zsh": "sh",
                       "console": "sh", "golang": "go", "rust": "rs", "ruby": "rb", "markdown": "md", "kotlin": "kt",
                       "objective-c": "m", "objc": "m", "c++": "cpp", "yml": "yaml"]
        return CodeLanguage.detect(path: "fence." + (aliases[info] ?? info), firstLine: nil)
    }
}
