import AppKit
import SwiftUI

// A code card in the panes beside a session (docs/MAC.md, Rolodex.swift):
// files from the session's folder in a light editor of the app's own, in
// the code colours you pick, saved as you type. The session's agent pulls
// a file up on it with `irie ctl open FILE[:LINE]` (internal/core/card.go),
// as when you ask it to show you some code; you read it, change it by hand,
// and ask the agent about the lines you pick. Along its top, the files the
// card has open; along its foot, where the file is, the caret's place and
// whether it is saved.

@MainActor
struct CodePane: View {
    @Environment(Store.self) private var store
    @Environment(\.colorScheme) private var scheme
    @AppStorage(CodeThemes.defaultsKey) private var themeID = CodeThemes.standard
    let owner: PaneOwner
    let pane: DeckPane
    let shows: Bool
    let keys: Bool
    let token: Int
    @State private var caret = CodeCaret()
    @State private var finding = false
    @State private var going = false
    @State private var hovered: String?

    private var theme: CodeTheme { CodeThemes.theme(themeID, dark: scheme == .dark) }
    private var font: NSFont { NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular) }
    private var fontSize: CGFloat { max(9, store.fontSize - 1) }

    var body: some View {
        let t = theme
        VStack(spacing: 0) {
            header(t)
            Rectangle().fill(Color(nsColor: t.gutterInk).opacity(0.22)).frame(height: 1)
            if owner.peer != nil {
                note("Code from a session on another machine opens there; that comes later.", t)
            } else if let path = pane.file {
                let doc = document(path, t)
                ZStack(alignment: .top) {
                    content(doc, t)
                    if doc.conflict { conflict(doc, t) }
                    if doc.phase == .missing { missing(doc, t) }
                }
                footer(doc, t)
            } else {
                QuickOpen(folder: owner.folder, theme: t, focusToken: token, takesKeys: shows && keys,
                          open: open, close: nil)
            }
        }
        .background(Color(nsColor: t.background))
        .overlay(alignment: .top) {
            if finding, pane.file != nil {
                QuickOpen(folder: owner.folder, theme: t, focusToken: token, takesKeys: true, open: open) { finding = false }
                    .frame(maxWidth: 460, maxHeight: 360)
                    .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Color(nsColor: t.background))
                        .shadow(color: .black.opacity(0.3), radius: 12, y: 4))
                    .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color(nsColor: t.gutterInk).opacity(0.35)))
                    .padding(.top, 36)
                    .padding(.horizontal, 16)
            } else if going, pane.file != nil {
                GoToLine(theme: t) { line in
                    going = false
                    if let line { store.rolodex.go(to: line, on: pane.id) }
                }
                .padding(.top, 36)
            }
        }
        // While it shows, the file is checked against the disk each second,
        // and saved as it stops showing.
        .task(id: "\(pane.file ?? "")/\(shows)") { await follow() }
        .onChange(of: shows) { _, on in
            if !on, let p = pane.file { CodeDocs.shared.existing(p)?.saveNow() }
        }
        // As another session shows, and its cards with it.
        .onDisappear {
            if let p = pane.file { CodeDocs.shared.existing(p)?.saveNow() }
        }
    }

    /// The file's document; `--snapshot` makes its own, with no disk.
    private func document(_ path: String, _ t: CodeTheme) -> CodeDocument {
        CodeDocs.shared.existing(path) ?? CodeDocs.shared.doc(path, theme: t, font: font)
    }

    // MARK: Parts

    @ViewBuilder
    private func content(_ doc: CodeDocument, _ t: CodeTheme) -> some View {
        switch doc.phase {
        case .loading:
            ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
        case .ready, .missing:
            CodeEditorView(doc: doc, theme: t, fontSize: fontSize, mark: NSColor(Theme.agent(slot: pane.slot)),
                           focusToken: token, takesKeysOnStart: shows && keys, shows: shows,
                           goTo: store.rolodex.goTo[pane.id], caret: caret, actions: actions(doc))
        case .notText:
            note("\(doc.name) isn’t text this card can edit.", t, finder: doc.path)
        case .tooBig:
            note("\(doc.name) is over 8 MB, too big for a code card.", t, finder: doc.path)
        case let .failed(why):
            note("\(doc.name) didn’t open: \(why)", t, finder: doc.path)
        }
    }

    private func actions(_ doc: CodeDocument) -> CodeActions {
        var a = CodeActions()
        a.save = { Task { await doc.save() } }
        a.quickOpen = { finding = true }
        a.goToLine = { going = true }
        a.closeFile = { close(doc.path) }
        if store.paneSession?.id == owner.id {
            a.ask = { lines in store.askAgent(about: doc.path, lines: lines, folder: owner.folder) }
        }
        a.copyReference = { lines in
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(CodeReference.text(doc.path, lines: lines, folder: owner.folder), forType: .string)
        }
        return a
    }

    /// The files the card has open, the one on show filled in; a dot for one
    /// with typing unsaved, which a hover makes a ×.
    private func header(_ t: CodeTheme) -> some View {
        HStack(spacing: 4) {
            Button { finding.toggle() } label: {
                Image(systemName: "magnifyingglass")
                    .font(.system(size: 11, weight: .semibold))
                    .frame(width: 24, height: 22)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color(nsColor: t.gutterActiveInk))
            .help("Open a file from this session’s folder (⌘P)")
            .disabled(owner.peer != nil)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 3) {
                    ForEach(pane.files ?? [], id: \.self) { f in chip(f, t) }
                }
            }
            Spacer(minLength: 0)
            menu(t)
        }
        .padding(.horizontal, 6)
        .frame(height: 30)
    }

    private func chip(_ path: String, _ t: CodeTheme) -> some View {
        let on = pane.file == path
        let doc = CodeDocs.shared.existing(path)
        let unsaved = doc?.dirty == true || doc?.conflict == true
        let hover = hovered == path
        return HStack(spacing: 4) {
            Text((path as NSString).lastPathComponent)
                .font(.system(size: 11.5, weight: on ? .semibold : .regular))
                .lineLimit(1)
            Button { close(path) } label: {
                Image(systemName: unsaved && !hover ? "circle.fill" : "xmark")
                    .font(.system(size: unsaved && !hover ? 6 : 8, weight: .bold))
                    .frame(width: 14, height: 14)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .opacity(on || hover || unsaved ? 1 : 0)
            .help("Close \((path as NSString).lastPathComponent) (⌘W)")
        }
        .foregroundStyle(Color(nsColor: on ? t.foreground : t.gutterInk))
        .padding(.leading, 9)
        .padding(.trailing, 4)
        .frame(height: 22)
        .background(Capsule().fill(Color(nsColor: on ? t.currentLine : .clear)))
        .overlay(Capsule().strokeBorder(Color(nsColor: t.gutterInk).opacity(on ? 0.35 : 0)))
        .contentShape(Capsule())
        .onHover { inside in hovered = inside ? path : (hovered == path ? nil : hovered) }
        .onTapGesture { store.rolodex.show(file: path, on: pane.id, in: owner.id) }
        .help(CodeReference.shown(path, folder: owner.folder))
        .contextMenu {
            Button("Close") { close(path) }
            Button("Close Others") { for f in pane.files ?? [] where f != path { close(f) } }
            Divider()
            Button("Copy Path") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(path, forType: .string)
            }
            Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)]) }
        }
    }

    private func menu(_ t: CodeTheme) -> some View {
        Menu {
            Button("Open a File…") { finding = true }
            Button("Go to Line…") { going = true }.disabled(pane.file == nil)
            if let p = pane.file {
                Divider()
                Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: p)]) }
                Button("Copy Path") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(p, forType: .string)
                }
                Button("Close File") { close(p) }
            }
            Divider()
            Picker("Code Colours", selection: $themeID) {
                ForEach(CodeThemes.families) { f in Text(f.name).tag(f.id) }
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 24, height: 22)
                .contentShape(Rectangle())
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .foregroundStyle(Color(nsColor: t.gutterActiveInk))
        .help("Open, go to a line, code colours")
    }

    /// Where the file is, what came from outside lately, the caret's place,
    /// the language and whether it is saved.
    private func footer(_ doc: CodeDocument, _ t: CodeTheme) -> some View {
        HStack(spacing: 10) {
            Text(CodeReference.shown(doc.path, folder: owner.folder))
                .lineLimit(1)
                .truncationMode(.head)
            if let at = doc.outsideAt, !doc.marks.isEmpty {
                TimelineView(.periodic(from: .now, by: 15)) { ctx in
                    Label("Changed on disk " + Self.ago(at, now: ctx.date), systemImage: "circle.fill")
                        .labelStyle(DotLabel(ink: Theme.agent(slot: pane.slot)))
                }
            }
            Spacer(minLength: 6)
            Text(caret.selected > 0 ? "\(caret.selected) picked" : "Ln \(caret.line), Col \(caret.column)")
                .monospacedDigit()
            Text(doc.language.name)
            Text(state(doc))
                .foregroundStyle(doc.problem != nil || doc.conflict ? Color(nsColor: .systemRed) : Color(nsColor: t.gutterInk))
                .help(doc.problem ?? "")
        }
        .font(.system(size: 10.5))
        .foregroundStyle(Color(nsColor: t.gutterInk))
        .padding(.horizontal, 10)
        .frame(height: 22)
        .background(Color(nsColor: t.background))
        .overlay(alignment: .top) { Rectangle().fill(Color(nsColor: t.gutterInk).opacity(0.18)).frame(height: 1) }
    }

    private func state(_ doc: CodeDocument) -> String {
        if doc.problem != nil { return "Not saved" }
        if doc.conflict { return "Changed on disk too" }
        if doc.saving { return "Saving…" }
        if doc.dirty { return "Edited" }
        return doc.phase == .missing ? "Not on disk" : "Saved"
    }

    static func ago(_ at: Date, now: Date) -> String {
        let s = Int(now.timeIntervalSince(at))
        if s < 15 { return "just now" }
        if s < 60 { return "\(s / 15 * 15) s ago" }
        if s < 3600 { return "\(s / 60) min ago" }
        return "\(s / 3600) h ago"
    }

    /// The disk changed while you typed: keep yours, or take the disk's,
    /// which ⌘Z takes back.
    private func conflict(_ doc: CodeDocument, _ t: CodeTheme) -> some View {
        bar("\(doc.name) changed on disk while you were typing.", t) {
            Button("Use the Disk’s") { doc.takeTheirs() }
            Button("Keep Mine") { doc.keepMine() }
                .buttonStyle(.borderedProminent)
                .tint(Theme.agent(slot: pane.slot))
        }
    }

    private func missing(_ doc: CodeDocument, _ t: CodeTheme) -> some View {
        bar("\(doc.name) is gone from disk.", t) {
            Button("Close") { close(doc.path) }
            Button("Save It Again") { Task { await doc.save(over: true) } }
                .buttonStyle(.borderedProminent)
                .tint(Theme.agent(slot: pane.slot))
        }
    }

    private func bar(_ text: String, _ t: CodeTheme, @ViewBuilder buttons: () -> some View) -> some View {
        HStack(spacing: 10) {
            Text(text)
                .font(Theme.font(12, .semibold))
                .foregroundStyle(Theme.ink)
                .lineLimit(2)
            Spacer(minLength: 4)
            buttons()
        }
        .controlSize(.small)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.panel).shadow(radius: 3, y: 1))
        .padding(10)
    }

    private func note(_ text: String, _ t: CodeTheme, finder: String? = nil) -> some View {
        VStack(spacing: 10) {
            Text(text)
                .font(Theme.font(12))
                .multilineTextAlignment(.center)
            if let finder {
                Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: finder)]) }
                    .controlSize(.small)
            }
        }
        .foregroundStyle(Color(nsColor: t.gutterInk))
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: Doing

    private func open(_ path: String) {
        finding = false
        store.rolodex.open(path, on: pane.id, in: owner.id)
        store.rolodex.bump(pane.id)
    }

    private func close(_ path: String) {
        CodeDocs.shared.existing(path)?.saveNow()
        store.rolodex.close(file: path, on: pane.id, in: owner.id)
        CodeDocs.shared.keep(only: store.rolodex.openFiles)
        store.rolodex.bump(pane.id)
    }

    /// Checks the file against the disk each second while it shows and the
    /// app is in front.
    private func follow() async {
        guard !store.isFixture, shows, let path = pane.file else { return }
        while !Task.isCancelled {
            if NSApp.isActive, let doc = CodeDocs.shared.existing(path) { await doc.check() }
            try? await Task.sleep(nanoseconds: 1_000_000_000)
        }
    }
}

/// A dot in a colour before a label's words.
private struct DotLabel: LabelStyle {
    let ink: Color

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            Circle().fill(ink).frame(width: 6, height: 6)
            configuration.title
        }
    }
}

/// How a file and its lines are named to the user and the agent: from the
/// session's folder when it is in it.
enum CodeReference {
    static func shown(_ path: String, folder: String?) -> String {
        if let f = folder, !f.isEmpty {
            let base = f.hasSuffix("/") ? f : f + "/"
            if path.hasPrefix(base) { return String(path.dropFirst(base.count)) }
        }
        let home = NSHomeDirectory()
        return path.hasPrefix(home + "/") ? "~" + path.dropFirst(home.count) : path
    }

    /// As `internal/core/pane.go:12-20`, which an agent reads as those
    /// lines.
    static func text(_ path: String, lines: ClosedRange<Int>, folder: String?) -> String {
        let where_ = lines.lowerBound == lines.upperBound ? "\(lines.lowerBound)" : "\(lines.lowerBound)-\(lines.upperBound)"
        return shown(path, folder: folder) + ":" + where_
    }
}

/// Go to a line: a small field over the file.
@MainActor
private struct GoToLine: View {
    let theme: CodeTheme
    let done: (Int?) -> Void
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: 8) {
            Text("Go to line")
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(Color(nsColor: theme.gutterActiveInk))
            TextField("", text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: 12, design: .monospaced))
                .foregroundStyle(Color(nsColor: theme.foreground))
                .frame(width: 80)
                .focused($focused)
                .onSubmit { done(Int(text.trimmingCharacters(in: .whitespaces).split(separator: ":").first ?? "")) }
                .onKeyPress(.escape) {
                    done(nil)
                    return .handled
                }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Color(nsColor: theme.background))
            .shadow(color: .black.opacity(0.3), radius: 10, y: 3))
        .overlay(RoundedRectangle(cornerRadius: 9, style: .continuous).strokeBorder(Color(nsColor: theme.gutterInk).opacity(0.35)))
        .onAppear { focused = true }
        .onChange(of: focused) { _, f in if !f { done(nil) } }
    }
}

/// Open a file: a field that finds files of the session's folder by the
/// letters typed, in order, the file's own name first; ↑ and ↓ pick, ↩
/// opens and Esc goes back.
@MainActor
struct QuickOpen: View {
    let folder: String?
    let theme: CodeTheme
    let focusToken: Int
    let takesKeys: Bool
    let open: @MainActor (String) -> Void
    /// Closes it; none for the card's own when no file is open.
    var close: (@MainActor () -> Void)?
    @State private var query = ""
    @State private var files: [String] = []
    @State private var picked = 0
    @State private var loading = true
    @FocusState private var focused: Bool

    private var results: [String] { CodeFiles.best(files, query) }

    var body: some View {
        let list = results
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(Color(nsColor: theme.gutterInk))
                TextField("Open a file in \(folder.map { ($0 as NSString).lastPathComponent } ?? "this folder")", text: $query)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13))
                    .foregroundStyle(Color(nsColor: theme.foreground))
                    .focused($focused)
                    .onSubmit { if list.indices.contains(picked) { pick(list[picked]) } }
                    .onKeyPress(.downArrow) {
                        picked = min(picked + 1, max(0, list.count - 1))
                        return .handled
                    }
                    .onKeyPress(.upArrow) {
                        picked = max(picked - 1, 0)
                        return .handled
                    }
                    .onKeyPress(.escape) {
                        close?()
                        return close == nil ? .ignored : .handled
                    }
            }
            .padding(.horizontal, 12)
            .frame(height: 36)
            Rectangle().fill(Color(nsColor: theme.gutterInk).opacity(0.2)).frame(height: 1)
            if folder == nil {
                hint("This session has no folder to open files from.")
            } else if loading {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity).padding(16)
            } else if list.isEmpty {
                hint(query.isEmpty ? "No files here." : "No file matches “\(query)”.")
            } else {
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            ForEach(Array(list.enumerated()), id: \.element) { i, f in row(f, on: i == picked).id(i) }
                        }
                        .padding(.vertical, 4)
                    }
                    .onChange(of: picked) { _, p in proxy.scrollTo(p) }
                }
            }
            if close == nil {
                hint("Or ask the agent to show you some code: it opens here.")
            }
        }
        .task(id: folder) {
            guard let folder else { return }
            loading = true
            files = await CodeFiles.shared.list(folder)
            loading = false
        }
        .onChange(of: query) { picked = 0 }
        .onAppear { if takesKeys || close != nil { focused = true } }
        .onChange(of: focusToken) { focused = true }
    }

    private func row(_ f: String, on: Bool) -> some View {
        let name = (f as NSString).lastPathComponent
        let dir = (f as NSString).deletingLastPathComponent
        return HStack(spacing: 8) {
            Text(name)
                .font(.system(size: 12.5, weight: .medium))
                .foregroundStyle(Color(nsColor: theme.foreground))
            Text(dir)
                .font(.system(size: 11))
                .foregroundStyle(Color(nsColor: theme.gutterInk))
                .lineLimit(1)
                .truncationMode(.head)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .frame(height: 24)
        .background(Color(nsColor: on ? theme.selection : .clear))
        .contentShape(Rectangle())
        .onTapGesture { pick(f) }
    }

    private func hint(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 11.5))
            .foregroundStyle(Color(nsColor: theme.gutterInk))
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
    }

    private func pick(_ relative: String) {
        guard let folder else { return }
        open((folder as NSString).appendingPathComponent(relative))
    }
}

/// The files of a folder, for opening by name: git's list where the folder
/// is in a repository, tracked and untracked but not ignored; else a walk
/// that leaves hidden folders and the usual build folders out. At most
/// 50 000, read again after 15 seconds.
@MainActor
final class CodeFiles {
    static let shared = CodeFiles()
    private var cache: [String: (at: Date, files: [String])] = [:]

    func list(_ folder: String) async -> [String] {
        if let c = cache[folder], Date().timeIntervalSince(c.at) < 15 { return c.files }
        let files = await Task.detached(priority: .userInitiated) { Self.read(folder) }.value
        cache[folder] = (Date(), files)
        return files
    }

    nonisolated static func read(_ folder: String) -> [String] {
        if let out = git(folder) { return out }
        var files: [String] = []
        let skip: Set<String> = ["node_modules", "build", ".build", "DerivedData", "target", "dist", "vendor", "__pycache__"]
        let base = URL(fileURLWithPath: folder)
        guard let walk = FileManager.default.enumerator(at: base, includingPropertiesForKeys: [.isDirectoryKey],
                                                        options: [.skipsHiddenFiles, .skipsPackageDescendants]) else { return [] }
        for case let url as URL in walk {
            if skip.contains(url.lastPathComponent) {
                walk.skipDescendants()
                continue
            }
            if (try? url.resourceValues(forKeys: [.isDirectoryKey]))?.isDirectory == true { continue }
            files.append(String(url.path.dropFirst(base.path.count + 1)))
            if files.count >= 50_000 { break }
        }
        return files
    }

    /// git ls-files, or nil outside a repository or when git takes over five
    /// seconds.
    nonisolated private static func git(_ folder: String) -> [String]? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/git")
        p.arguments = ["-C", folder, "ls-files", "-co", "--exclude-standard", "-z"]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        do { try p.run() } catch { return nil }
        let read = Read()
        let done = DispatchSemaphore(value: 0)
        DispatchQueue.global(qos: .userInitiated).async {
            read.data = out.fileHandleForReading.readDataToEndOfFile()
            done.signal()
        }
        guard done.wait(timeout: .now() + 5) == .success else {
            p.terminate()
            return nil
        }
        p.waitUntilExit()
        guard p.terminationStatus == 0 else { return nil }
        return read.data.split(separator: 0).prefix(50_000).compactMap { String(data: $0, encoding: .utf8) }
    }

    /// What git printed, handed from the thread that read it.
    private final class Read: @unchecked Sendable {
        var data = Data()
    }

    /// The files that match what was typed, the best first, at most 60.
    nonisolated static func best(_ files: [String], _ query: String) -> [String] {
        let q = Array(query.lowercased().utf8.filter { $0 != 32 })
        guard !q.isEmpty else { return Array(files.sorted { $0.count < $1.count }.prefix(60)) }
        var scored: [(Int, String)] = []
        for f in files { if let s = score(f, q) { scored.append((s, f)) } }
        scored.sort { $0.0 != $1.0 ? $0.0 > $1.0 : $0.1.count < $1.1.count }
        return scored.prefix(60).map(\.1)
    }

    /// How well a path matches typed letters, nil for not at all: each
    /// letter must come after the last, and one in the file's own name, at
    /// a word's start or right after the one before scores more.
    nonisolated static func score(_ path: String, _ q: [UInt8]) -> Int? {
        let p = Array(path.lowercased().utf8)
        let name = (p.lastIndex(of: UInt8(ascii: "/")).map { $0 + 1 }) ?? 0
        // The whole query in the file's name first, else anywhere.
        for start in [name, 0] {
            var score = 0, at = start, last = -2
            var ok = true
            for c in q {
                while at < p.count, p[at] != c { at += 1 }
                if at == p.count {
                    ok = false
                    break
                }
                score += 1
                if at == last + 1 { score += 5 }
                if at >= name { score += 3 }
                if at == 0 || [UInt8(ascii: "/"), UInt8(ascii: "_"), UInt8(ascii: "-"), UInt8(ascii: ".")].contains(p[at - 1]) { score += 4 }
                last = at
                at += 1
            }
            if ok { return score + (start == name ? 10 : 0) - p.count / 16 }
        }
        return nil
    }
}

extension Store {
    /// Puts a file's lines in the shown agent's prompt, as
    /// `internal/core/pane.go:12-20 `, and gives the terminal the keyboard,
    /// to ask about them.
    func askAgent(about path: String, lines: ClosedRange<Int>, folder: String?) {
        guard pasteToTerminal(CodeReference.text(path, lines: lines, folder: folder) + " ") else {
            say("There’s no terminal to ask in yet.", error: true)
            return
        }
        giveMainKeys()
    }
}

/// A few lines of code in a theme, as Settings shows the code colours.
@MainActor
struct CodeSample: View {
    let theme: CodeTheme

    static let text = """
    // Greet says hello to someone by name.
    func Greet(name string, times int) string {
        if times > 3 {
            return "Hello, " + name + "!\\n"
        }
        return fmt.Sprintf("Hi %s", name)
    }
    """

    var body: some View {
        Text(Self.coloured(theme))
            .font(.system(size: 11.5, design: .monospaced))
            .lineSpacing(2)
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Color(nsColor: theme.background)))
            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.edge))
            .accessibilityLabel("A sample of code in \(theme.name)")
    }

    static func coloured(_ theme: CodeTheme) -> AttributedString {
        let ns = text as NSString
        let colorer = SyntaxColorer(language: CodeLanguage.detect(path: "sample.go", firstLine: nil))
        colorer.load(ns)
        let out = NSMutableAttributedString(string: text, attributes: [.foregroundColor: theme.foreground])
        colorer.tokens(in: NSRange(location: 0, length: ns.length)) { r, kind in
            out.addAttribute(.foregroundColor, value: theme.color(kind), range: r)
        }
        return (try? AttributedString(out, including: \.appKit)) ?? AttributedString(text)
    }
}
