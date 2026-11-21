import AppKit
import SwiftUI

// A Vault card in the panes beside a session (docs/MAC.md, Rolodex.swift):
// the Obsidian vault's notes, read and written by the app itself (the
// obsidian plugin, DESIGN §4.17), so Obsidian is the person's to have or
// not. A note shows in the code card's editor (CodeEditor.swift), in the
// code colours picked, its lines wrapped as prose, with Obsidian's
// [[links]], #tags and the rest coloured; it saves as you type and takes in
// what Obsidian, a sync or an agent changes on disk (CodeDocument.swift).
// ⌘-click a link to open its note on the card; a note a link names that
// isn't there yet is made, as Obsidian makes one. Along its top, the note's
// name and the ways about; along its foot, the notes that link to it.

@MainActor
struct VaultPane: View {
    @Environment(Store.self) private var store
    @Environment(\.colorScheme) private var scheme
    @AppStorage(CodeThemes.defaultsKey) private var themeID = CodeThemes.standard
    /// The vault's notes as a tree beside the note, for every Vault card.
    @AppStorage("vaultTree") private var tree = true
    /// The notes shown in Reading view, "r " and the path a line, or with
    /// the editor beside it, "b "; the newest 200. A note opens in the
    /// editor first, as Obsidian opens one (VaultReading.swift).
    @AppStorage("vaultLooks") private var looks = ""
    let owner: PaneOwner
    let pane: DeckPane
    let shows: Bool
    let keys: Bool
    let token: Int
    @State private var caret = CodeCaret()
    @State private var finding = false
    /// The notes shown before and after this one, as links were followed.
    @State private var back: [String] = []
    @State private var ahead: [String] = []
    /// The notes that link to the one on show.
    @State private var linked: [String] = []
    /// Bumped to read the vault's notes again.
    @State private var refresh = 0

    private var theme: CodeTheme { CodeThemes.theme(themeID, dark: scheme == .dark) }
    private var font: NSFont { NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular) }
    private var fontSize: CGFloat { max(9, store.fontSize - 1) }
    private var folder: String? { store.vault.folder }

    var body: some View {
        let t = theme
        VStack(spacing: 0) {
            header(t)
            Rectangle().fill(Color(nsColor: t.gutterInk).opacity(0.22)).frame(height: 1)
            content(t)
        }
        .background(Color(nsColor: t.background))
        .background { readingKeys.opacity(0).allowsHitTesting(false).accessibilityHidden(true) }
        .overlay(alignment: .top) {
            if finding, let folder {
                QuickOpen(folder: folder, theme: t, focusToken: token, takesKeys: true, shows: shows, open: { open($0) },
                          close: {
                              finding = false
                              store.rolodex.bump(pane.id)
                          }, only: VaultLinks.isNote, prompt: "Open a note in \(store.vault.result?.name ?? "the vault")")
                    .frame(maxWidth: 460, maxHeight: 360)
                    .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Color(nsColor: t.background))
                        .shadow(color: .black.opacity(0.3), radius: 12, y: 4))
                    .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color(nsColor: t.gutterInk).opacity(0.35)))
                    .padding(.top, 36)
                    .padding(.horizontal, 16)
            }
        }
        // Where the vault is, as the card shows and every few seconds.
        .task(id: shows) { await watch() }
        // The note on show against the disk, each second while it shows.
        .task(id: "\(pane.file ?? "")/\(shows)") { await follow() }
        .task(id: "\(pane.file ?? "")/\(refresh)/\(folder ?? "")") { await findLinked() }
        // A card with no note on show, as `irie ctl card vault` makes one,
        // shows the notepad's note while the notepad is kept in the vault.
        .task(id: "\(pane.file == nil)/\(store.vault.notesNote ?? "")/\(folder ?? "")") {
            if pane.file == nil, folder != nil, let n = store.vault.notesNote {
                store.rolodex.open(n, on: pane.id, in: owner.id)
            }
        }
        .onChange(of: shows) { _, on in
            guard !on else { return }
            if let p = pane.file { CodeDocs.shared.existing(p)?.saveNow() }
            finding = false
        }
        .onDisappear {
            if let p = pane.file { CodeDocs.shared.existing(p)?.saveNow() }
        }
    }

    // MARK: Parts

    private func header(_ t: CodeTheme) -> some View {
        let path = pane.file
        let notes = store.vault.notesNote
        return HStack(spacing: 2) {
            icon("sidebar.left", tree ? "Hide the vault’s notes" : "Show the vault’s notes beside the note", t, on: tree) { tree.toggle() }
                .disabled(folder == nil)
            icon("chevron.left", "Back to the note before", t) { go(back: true) }
                .disabled(back.isEmpty)
            icon("chevron.right", "On to the next note", t) { go(back: false) }
                .disabled(ahead.isEmpty)
            if let path, let folder {
                Text(VaultPane.title(path))
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(Color(nsColor: t.foreground))
                    .lineLimit(1)
                    .padding(.leading, 4)
                let where_ = VaultPane.place(path, in: folder)
                if !where_.isEmpty {
                    Text(where_)
                        .font(.system(size: 11))
                        .foregroundStyle(Color(nsColor: t.gutterInk))
                        .lineLimit(1)
                        .truncationMode(.head)
                }
            } else {
                Text(store.vault.result?.name ?? "Vault")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(Color(nsColor: t.foreground))
                    .padding(.leading, 4)
            }
            Spacer(minLength: 4)
            if let path, folder != nil {
                // Obsidian's view switcher: Reading view, and with ⌘ held,
                // Reading view beside the editor.
                let l = look(path)
                icon(l == .editing ? "book" : "book.fill", lookHelp(l), t, on: l != .editing) {
                    if NSEvent.modifierFlags.contains(.command) {
                        setLook(path, l == .both ? .editing : .both)
                    } else {
                        setLook(path, l == .reading ? .editing : .reading)
                    }
                }
            }
            if let notes {
                icon("checklist", "Your notepad, as kept in the vault", t, on: path == notes) { open(notes) }
            }
            icon("magnifyingglass", "Open a note by its name (⌘P)", t) { finding.toggle() }
                .disabled(folder == nil)
            icon("square.and.pencil", "A new note", t) { newNote() }
                .disabled(folder == nil)
            icon("arrow.up.right.square", obsidianHelp, t) {
                if let p = path ?? folder { store.openInObsidian(p) }
            }
            .disabled(folder == nil || store.vault.result?.opened != true || !VaultModel.obsidianHere)
            menu(t)
        }
        .padding(.horizontal, 6)
        .frame(height: 30)
    }

    private var obsidianHelp: String {
        if !VaultModel.obsidianHere { return "Obsidian isn’t on this Mac: the card reads and writes the vault itself" }
        if store.vault.result?.opened != true { return "Obsidian hasn’t opened this vault yet: in Obsidian, Open folder as vault" }
        return "Open in Obsidian"
    }

    private func icon(_ name: String, _ help: String, _ t: CodeTheme, on: Bool = false, _ act: @escaping () -> Void) -> some View {
        Button(action: act) {
            Image(systemName: name)
                .font(.system(size: 11, weight: .semibold))
                .frame(width: 24, height: 22)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .foregroundStyle(Color(nsColor: on ? t.foreground : t.gutterActiveInk))
        .help(help)
        .accessibilityLabel(help)
    }

    private func menu(_ t: CodeTheme) -> some View {
        Menu {
            Button("Open a Note…") { finding = true }.disabled(folder == nil)
            Button("New Note") { newNote() }.disabled(folder == nil)
            if let p = pane.file {
                Divider()
                Toggle("Reading View", isOn: Binding(get: { look(p) == .reading }, set: { setLook(p, $0 ? .reading : .editing) }))
                Toggle("Reading View Beside the Editor", isOn: Binding(get: { look(p) == .both }, set: { setLook(p, $0 ? .both : .editing) }))
                Divider()
                Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: p)]) }
                Button("Copy Path") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(p, forType: .string)
                }
            }
            Divider()
            let notesOn = store.plugins.first { $0.name == "obsidian" }?.parts.first { $0.name == "notes" }?.on ?? false
            Button(notesOn ? "Keep Notes in \(Brand.name) Alone" : "Keep Notes in the Vault Too") {
                store.setPlugin("obsidian", part: "notes", on: !notesOn)
            }
            Button("Choose Another Vault…") { Task { if await store.chooseVault() { refresh += 1 } } }
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
        .help("Notes, the vault, code colours")
    }

    @ViewBuilder
    private func content(_ t: CodeTheme) -> some View {
        let r = store.vault.result
        switch r?.state {
        case nil:
            if store.vault.served || store.isFixture {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                note("This \(Brand.name)’s core can’t show a vault yet. The next install brings it.", t)
            }
        case "plugin_off"?:
            note(r?.text ?? "The Obsidian plugin is off.", t) {
                Button("Turn the Obsidian Plugin On") { store.setPlugin("obsidian", on: true) }
            }
        case "ready"?:
            if let folder {
                GeometryReader { geo in
                    HStack(spacing: 0) {
                        if tree {
                            CodeTree(folder: folder, theme: t, shown: pane.file, open: { open($0) }, only: VaultLinks.isNote, bare: true, refresh: refresh)
                                .frame(width: min(240, max(140, geo.size.width * 0.28)))
                            Rectangle().fill(Color(nsColor: t.gutterInk).opacity(0.22)).frame(width: 1)
                        }
                        VStack(spacing: 0) { main(t, folder: folder) }
                    }
                }
            }
        default:
            welcome(r, t)
        }
    }

    /// No vault yet, or one that isn't there: choose one, or make one.
    private func welcome(_ r: ObsidianResult?, _ t: CodeTheme) -> some View {
        let first = (r?.vault ?? "").isEmpty
        return VStack(spacing: 12) {
            Image(systemName: "books.vertical")
                .font(.system(size: 26, weight: .light))
                .foregroundStyle(Color(nsColor: t.gutterInk))
            Text(first ? "Your Obsidian vault, beside your sessions" : "The vault isn’t there")
                .font(Theme.font(14, .semibold))
                .foregroundStyle(Color(nsColor: t.foreground))
            Text(first ? "A vault is a folder of Markdown notes. This card reads and writes it as Obsidian does, so Obsidian is yours to have or not, and keeps your Notes in it too, as \(Brand.name) Notes.md."
                : (r?.text ?? ""))
                .font(Theme.font(12))
                .foregroundStyle(Color(nsColor: t.gutterInk))
                .multilineTextAlignment(.center)
                .frame(maxWidth: 380)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 8) {
                Button(first ? "Choose Your Vault…" : "Choose Another Vault…") { Task { if await store.chooseVault() { refresh += 1 } } }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.agent(slot: pane.slot))
                Button("New Vault…") { Task { if await store.newVault() { refresh += 1 } } }
            }
            .controlSize(.regular)
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    /// The note on show and its foot, or a way to open one.
    @ViewBuilder
    private func main(_ t: CodeTheme, folder: String) -> some View {
        if let path = pane.file {
            let doc = CodeDocs.shared.existing(path) ?? CodeDocs.shared.doc(path, theme: t, font: font)
            ZStack(alignment: .top) {
                page(doc, t, folder: folder)
                if doc.conflict {
                    bar("\(VaultPane.title(path)) changed on disk while you were typing.", t) {
                        Button("Use the Disk’s") { doc.takeTheirs() }
                        Button("Keep Mine") { doc.keepMine() }
                            .buttonStyle(.borderedProminent)
                            .tint(Theme.agent(slot: pane.slot))
                    }
                }
                if doc.phase == .missing {
                    bar("\(VaultPane.title(path)) is gone from the vault.", t) {
                        Button("Save It Again") { Task { await doc.save(over: true) } }
                            .buttonStyle(.borderedProminent)
                            .tint(Theme.agent(slot: pane.slot))
                    }
                }
            }
            foot(doc, t, folder: folder)
        } else {
            note("Pick a note, find one by its name with ⌘P, or start a new one.", t) {
                HStack(spacing: 8) {
                    if let notes = store.vault.notesNote {
                        Button("Your Notepad’s Note") { open(notes) }
                    }
                    Button("New Note") { newNote() }
                }
            }
        }
    }

    /// The note in the editor, in Reading view, or in both: Reading view
    /// beside the editor when the card is wide, under it when narrow, the
    /// editor kept in its place so the typing goes on.
    @ViewBuilder
    private func page(_ doc: CodeDocument, _ t: CodeTheme, folder: String) -> some View {
        let l = look(doc.path)
        switch doc.phase {
        case .ready, .missing:
            if l == .reading {
                reading(doc, t, folder: folder, alone: true)
            } else {
                GeometryReader { geo in
                    let wide = geo.size.width >= 620
                    let layout = wide ? AnyLayout(HStackLayout(spacing: 0)) : AnyLayout(VStackLayout(spacing: 0))
                    layout {
                        editor(doc, t).frame(maxWidth: .infinity, maxHeight: .infinity)
                        if l == .both {
                            Rectangle().fill(Color(nsColor: t.gutterInk).opacity(0.22))
                                .frame(width: wide ? 1 : nil, height: wide ? nil : 1)
                            reading(doc, t, folder: folder, alone: false).frame(maxWidth: .infinity, maxHeight: .infinity)
                        }
                    }
                }
            }
        default:
            editor(doc, t)
        }
    }

    private func reading(_ doc: CodeDocument, _ t: CodeTheme, folder: String, alone: Bool) -> some View {
        VaultReading(doc: doc,
                     ctx: VaultReadingContext(theme: t, size: store.fontSize + 1, folder: folder, note: doc.path,
                                              accent: Theme.agent(slot: pane.slot), fixture: store.isFixture,
                                              open: { follow($0, from: doc.path) }, tick: { tick(doc, line: $0) }),
                     goTo: store.rolodex.goTo[pane.id], went: alone ? { g in store.rolodex.went(g, on: pane.id) } : nil)
            // A view of its own for each note, so one never shows another's
            // blocks while it reads its own.
            .id(ObjectIdentifier(doc))
    }

    @ViewBuilder
    private func editor(_ doc: CodeDocument, _ t: CodeTheme) -> some View {
        switch doc.phase {
        case .loading:
            ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
        case .ready, .missing:
            CodeEditorView(doc: doc, theme: t, fontSize: fontSize, mark: NSColor(Theme.agent(slot: pane.slot)),
                           focusToken: token, takesKeysOnStart: shows && keys, shows: shows,
                           goTo: store.rolodex.goTo[pane.id], caret: caret, actions: actions(doc), prose: true)
                .id(ObjectIdentifier(doc))
        case .notText:
            note("\(doc.name) isn’t a note this card can edit.", t)
        case .tooBig:
            note("\(doc.name) is over 8 MB, too big for the card.", t)
        case let .failed(why):
            note("\(doc.name) didn’t open: \(why)", t)
        }
    }

    private func actions(_ doc: CodeDocument) -> CodeActions {
        var a = CodeActions()
        a.save = { Task { await doc.save() } }
        a.quickOpen = { finding = true }
        a.went = { g in store.rolodex.went(g, on: pane.id) }
        a.openLink = { link in follow(link, from: doc.path) }
        // ⌘E as in Obsidian, and ⇧⌘V as the code card's pretty view.
        a.reading = { setLook(doc.path, look(doc.path) == .reading ? .editing : .reading) }
        a.pretty = { setLook(doc.path, look(doc.path) == .both ? .editing : .both) }
        if store.paneSession?.id == owner.id {
            a.ask = { lines in store.askAgent(about: doc.path, lines: lines, folder: owner.folder) }
        }
        a.copyReference = { lines in
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(CodeReference.text(doc.path, lines: lines, folder: owner.folder), forType: .string)
        }
        return a
    }

    /// The notes that link here, and whether it is saved.
    private func foot(_ doc: CodeDocument, _ t: CodeTheme, folder: String) -> some View {
        HStack(spacing: 10) {
            if linked.isEmpty {
                Text("No note links here")
            } else {
                Menu(linked.count == 1 ? "1 note links here" : "\(linked.count) notes link here") {
                    ForEach(linked, id: \.self) { n in
                        Button(VaultPane.title(n)) { open((folder as NSString).appendingPathComponent(n)) }
                    }
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
            }
            Spacer(minLength: 6)
            Text(look(doc.path) == .reading ? "Click a link to open it, a box to tick it" : "⌘-click a link to open it")
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
        return doc.phase == .missing ? "Not in the vault" : "Saved"
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

    private func note(_ text: String, _ t: CodeTheme) -> some View {
        note(text, t) { EmptyView() }
    }

    private func note(_ text: String, _ t: CodeTheme, @ViewBuilder buttons: () -> some View) -> some View {
        VStack(spacing: 10) {
            Text(text)
                .font(Theme.font(12))
                .multilineTextAlignment(.center)
                .frame(maxWidth: 380)
            buttons().controlSize(.small)
        }
        .foregroundStyle(Color(nsColor: t.gutterInk))
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: Reading view

    private enum Look { case editing, reading, both }

    private func look(_ path: String) -> Look {
        for line in looks.split(separator: "\n") where line.dropFirst(2) == path {
            return line.hasPrefix("b ") ? .both : .reading
        }
        return .editing
    }

    private func setLook(_ path: String, _ l: Look) {
        var lines = looks.split(separator: "\n").map(String.init).filter { $0.dropFirst(2) != path }
        switch l {
        case .editing: break
        case .reading: lines.append("r " + path)
        case .both: lines.append("b " + path)
        }
        if lines.count > 200 { lines.removeFirst(lines.count - 200) }
        looks = lines.joined(separator: "\n")
    }

    private func lookHelp(_ l: Look) -> String {
        switch l {
        case .editing: return "Reading view (⌘E); ⌘-click for it beside the editor (⇧⌘V)"
        case .reading: return "Back to editing (⌘E)"
        case .both: return "Reading view alone (⌘E); ⌘-click for the editor alone"
        }
    }

    /// ⌘E and ⌘P while the note shows in Reading view and the card has the
    /// keyboard, when the editor doesn't have it to take them itself.
    @ViewBuilder
    private var readingKeys: some View {
        if shows, keys, let p = pane.file, look(p) != .editing {
            Button("") { setLook(p, look(p) == .reading ? .editing : .reading) }
                .keyboardShortcut("e", modifiers: .command)
            Button("") { finding = true }
                .keyboardShortcut("p", modifiers: .command)
        }
    }

    /// A task's box clicked in Reading view: its mark in the note goes from
    /// [ ] to [x], or back to [ ], as typing would put it, so it saves, ⌘Z
    /// in the editor takes it back, and the notepad follows when the note
    /// is its own.
    private func tick(_ doc: CodeDocument, line: Int) {
        guard line < doc.lineCount else { return }
        let r = doc.lineRange(line)
        let text = doc.storage.mutableString.substring(with: r)
        guard let box = VaultMarkdown.checkbox(in: text) else { return }
        doc.replace(NSRange(location: r.location + box.range.location, length: box.range.length),
                    with: box.done ? " " : "x", named: box.done ? "Untick" : "Tick")
    }

    // MARK: Names

    /// A note's name as Obsidian shows it: its file's, without .md.
    nonisolated static func title(_ path: String) -> String {
        let name = (path as NSString).lastPathComponent
        return name.lowercased().hasSuffix(".md") ? String(name.dropLast(3)) : name
    }

    /// The folder a note is in, from the vault's; "" at its top.
    nonisolated static func place(_ path: String, in folder: String) -> String {
        guard path.hasPrefix(folder + "/") else { return (path as NSString).deletingLastPathComponent }
        return (String(path.dropFirst(folder.count + 1)) as NSString).deletingLastPathComponent
    }

    // MARK: Doing

    /// Shows a note on the card, the one before kept for Back.
    private func open(_ path: String, line: Int? = nil) {
        finding = false
        if let now = pane.file, now != path {
            back.append(now)
            ahead.removeAll()
        }
        store.rolodex.open(path, on: pane.id, in: owner.id, line: line)
        store.rolodex.bump(pane.id)
    }

    private func go(back b: Bool) {
        guard let now = pane.file else { return }
        if b, let to = back.popLast() {
            ahead.append(now)
            store.rolodex.open(to, on: pane.id, in: owner.id)
        } else if !b, let to = ahead.popLast() {
            back.append(now)
            store.rolodex.open(to, on: pane.id, in: owner.id)
        }
        store.rolodex.bump(pane.id)
    }

    /// Follows a link ⌘-clicked in a note: a page to a browser card, a
    /// note of the vault onto this card, at its heading or block, a file
    /// of the vault in the app the Mac opens it with, and a note that
    /// isn't there yet made first, as Obsidian makes one.
    private func follow(_ link: String, from: String) {
        if let u = URL(string: link), ["http", "https"].contains(u.scheme?.lowercased() ?? "") {
            if !store.showLink(link, in: owner.id) { NSWorkspace.shared.open(u) }
            return
        }
        guard let folder else { return }
        let target = VaultLinks.target(link)
        let here = from.hasPrefix(folder + "/") ? String(from.dropFirst(folder.count + 1)) : nil
        Task {
            let files = await CodeFiles.shared.list(folder).filter(VaultLinks.isFile)
            if let found = VaultLinks.resolve(target.name, from: here, files: files) {
                let path = (folder as NSString).appendingPathComponent(found)
                guard VaultLinks.isNote(found) else {
                    NSWorkspace.shared.open(URL(fileURLWithPath: path))
                    return
                }
                var line: Int?
                if target.heading != nil || target.block != nil, let text = try? String(contentsOfFile: path, encoding: .utf8) {
                    line = VaultLinks.line(of: target, in: text)
                }
                open(path, line: line)
                return
            }
            guard let made = VaultLinks.newNote(target.name) else { return }
            let path = (folder as NSString).appendingPathComponent(made)
            do {
                try FileManager.default.createDirectory(atPath: (path as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
                if !FileManager.default.fileExists(atPath: path) {
                    try Data().write(to: URL(fileURLWithPath: path), options: .withoutOverwriting)
                }
                CodeFiles.shared.forget(folder)
                refresh += 1
                open(path)
            } catch {
                store.say("Couldn’t make \(VaultPane.title(path)): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// A new note at the vault's top: Untitled, or the first Untitled N
    /// not taken, as Obsidian names one.
    private func newNote() {
        guard let folder else { return }
        let fm = FileManager.default
        var n = 0
        var path = (folder as NSString).appendingPathComponent("Untitled.md")
        while fm.fileExists(atPath: path) {
            n += 1
            path = (folder as NSString).appendingPathComponent("Untitled \(n).md")
        }
        do {
            try Data().write(to: URL(fileURLWithPath: path), options: .withoutOverwriting)
            CodeFiles.shared.forget(folder)
            refresh += 1
            open(path)
        } catch {
            store.say("Couldn’t make a new note: \(error.localizedDescription)", error: true)
        }
    }

    // MARK: Watching

    /// Asks the core where the vault is as the card shows, then every five
    /// seconds while it does, reading the notes again now and then.
    private func watch() async {
        guard shows, !store.isFixture else { return }
        var n = 0
        while !Task.isCancelled {
            await store.vault.look()
            if n > 0, n % 2 == 0, let folder {
                CodeFiles.shared.forget(folder)
                refresh += 1
            }
            n += 1
            try? await Task.sleep(nanoseconds: 5_000_000_000)
        }
    }

    /// Checks the note against the disk each second while it shows and the
    /// app is in front.
    private func follow() async {
        guard !store.isFixture, shows, let path = pane.file else { return }
        while !Task.isCancelled {
            if NSApp.isActive, let doc = CodeDocs.shared.existing(path) { await doc.check() }
            try? await Task.sleep(nanoseconds: 1_000_000_000)
        }
    }

    /// The notes that link to the one on show, read off the main thread.
    private func findLinked() async {
        guard let folder, let path = pane.file, path.hasPrefix(folder + "/") else {
            linked = []
            return
        }
        if store.isFixture {
            linked = Fixture.vaultLinked
            return
        }
        let note = String(path.dropFirst(folder.count + 1))
        let files = await CodeFiles.shared.list(folder).filter(VaultLinks.isFile)
        let found = await Task.detached(priority: .utility) { VaultLinks.backlinks(to: note, vault: folder, files: files) }.value
        guard !Task.isCancelled else { return }
        linked = found
    }
}
