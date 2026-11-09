import AppKit
import Observation
import SwiftUI

// Your own themes and layouts: View > Theme > Make a Theme…, View > Layout
// and Settings > Appearance (ThemeMaker.swift draws the window). Each is a
// small JSON file in Irie's config folder, beside config.toml, so you can
// copy one to another Mac, share it, keep it with your dotfiles or ask an
// agent to write one:
//
//   themes/<id>.json   a palette: its name, the palette it started from, and
//                      its light and dark colours by role, as "#RRGGBB". A
//                      role it leaves out keeps the colour of the palette it
//                      started from.
//   layouts/<id>.json  the window's panes: what the right side shows, the
//                      sidebar, the details panel, the right sidebar and the
//                      terminal's text size, and a theme if you like. A part
//                      it leaves out stays as it is.
//
// The app reads both folders as it starts, again when a file in them comes,
// goes or is replaced, and whenever it becomes the active app, so a file an
// agent writes shows in the menus at once.

/// One colour of a palette: its name in a theme file, what Make a Theme
/// calls it, and how the picks of Make a Theme move it.
struct ColorRole: Identifiable {
    let key: String
    let path: WritableKeyPath<Swatches, UInt32>
    let title: String
    let kind: Kind
    var id: String { key }

    enum Kind: String, CaseIterable {
        /// Grounds, hairlines and text, which the tint moves.
        case neutral
        /// The accent's colours, which the accent moves.
        case accent
        /// DESIGN §5.3's state colours, which keep their meaning.
        case state
        /// The sun of Irie's mark.
        case sun

        var title: String {
            switch self {
            case .neutral: return "Grounds and text"
            case .accent: return "Accent"
            case .state: return "States"
            case .sun: return "Mark"
            }
        }
    }

    /// Every colour a palette has, in the order a theme file lists them.
    static let all: [ColorRole] = [
        .init(key: "sidebar", path: \.sidebar, title: "Sidebar", kind: .neutral),
        .init(key: "ground", path: \.ground, title: "Window", kind: .neutral),
        .init(key: "card", path: \.card, title: "Card", kind: .neutral),
        .init(key: "panel", path: \.panel, title: "Details panel", kind: .neutral),
        .init(key: "field", path: \.field, title: "Field", kind: .neutral),
        .init(key: "edge", path: \.edge, title: "Hairline", kind: .neutral),
        .init(key: "hover", path: \.hover, title: "Card under the pointer", kind: .neutral),
        .init(key: "ink", path: \.ink, title: "Text", kind: .neutral),
        .init(key: "muted", path: \.muted, title: "Quiet text", kind: .neutral),
        .init(key: "selection", path: \.selection, title: "Selected card", kind: .accent),
        .init(key: "accent", path: \.accent, title: "Accent", kind: .accent),
        .init(key: "accent_ink", path: \.accentInk, title: "Accent text", kind: .accent),
        .init(key: "accent_fill", path: \.accentFill, title: "Start button", kind: .accent),
        .init(key: "on_accent", path: \.onAccent, title: "Arrow on the start button", kind: .accent),
        .init(key: "waves", path: \.waves, title: "Waves", kind: .accent),
        .init(key: "needs", path: \.needs, title: "Needs you", kind: .state),
        .init(key: "needs_tint", path: \.needsTint, title: "Card that needs you", kind: .state),
        .init(key: "done", path: \.done, title: "Done", kind: .state),
        .init(key: "paused", path: \.paused, title: "Paused", kind: .state),
        .init(key: "frozen", path: \.frozen, title: "Frozen", kind: .state),
        .init(key: "sun", path: \.sun, title: "Sun", kind: .sun),
    ]
}

extension Swatches {
    /// The colours by their names in a theme file, as "#RRGGBB": what the
    /// core's `theme` op takes for a palette of your own.
    var hexes: [String: String] {
        Dictionary(uniqueKeysWithValues: ColorRole.all.map { ($0.key, Self.hex(self[keyPath: $0.path])) })
    }

    static func hex(_ v: UInt32) -> String { String(format: "#%06X", v & 0xFFFFFF) }

    /// "#RRGGBB" or "RRGGBB" as a colour.
    static func parse(_ s: String) -> UInt32? {
        var t = Substring(s.trimmingCharacters(in: .whitespaces))
        if t.hasPrefix("#") { t = t.dropFirst() }
        guard t.count == 6, t.allSatisfy(\.isHexDigit), let v = UInt32(t, radix: 16) else { return nil }
        return v
    }

    /// These colours with those a theme file gives put in their place, and
    /// what the file has that is no colour or no role.
    func with(_ roles: [String: String]?) -> (Swatches, [String]) {
        var s = self
        var bad: [String] = []
        for (k, v) in roles ?? [:] {
            guard let role = ColorRole.all.first(where: { $0.key == k }) else {
                bad.append("\(k) is no colour of a theme")
                continue
            }
            guard let c = Self.parse(v) else {
                bad.append("\(k) = \(v) is not #RRGGBB")
                continue
            }
            s[keyPath: role.path] = c
        }
        return (s, bad.sorted())
    }
}

/// A theme you made: its palette, and the palette it started from.
struct CustomTheme: Identifiable, Equatable {
    var palette: Palette
    var basedOn: String
    var file: URL
    var id: String { palette.id }
    var name: String { palette.name }
}

/// The window's panes as you arranged them, to come back to (View > Layout).
/// A part it leaves out stays as it is when it is applied.
struct PaneLayout: Codable, Equatable, Identifiable {
    /// Its file's name without .json, or a built-in layout's own.
    var id = ""
    var name: String
    var builtIn = false
    /// What the right side shows: "conversation" or "terminal".
    var view: String?
    var sidebar: Sidebar?
    var details: Details?
    var drawer: Drawer?
    /// The terminal's text size, in points.
    var textSize: Double?
    /// A palette to change to with it.
    var theme: String?

    struct Sidebar: Codable, Equatable {
        var shown: Bool
        var width: Double?
    }

    /// The details panel at the foot of the sidebar: one line, or open this
    /// tall.
    struct Details: Codable, Equatable {
        var minimized: Bool
        var height: Double?
    }

    /// The right sidebar (RightSidebar.swift): open or not, on which tab,
    /// and how wide.
    struct Drawer: Codable, Equatable {
        var open: Bool
        var tab: String?
        var width: Double?
    }

    enum CodingKeys: String, CodingKey {
        case name, view, sidebar, details, drawer, theme
        case textSize = "text_size"
    }

    /// Layouts every Irie has, after yours in View > Layout. They leave the
    /// widths, the details panel and the text size as they are.
    static let builtIns: [PaneLayout] = [
        PaneLayout(id: "conversation", name: "Conversation", builtIn: true, view: "conversation",
                   sidebar: .init(shown: true), drawer: .init(open: false)),
        PaneLayout(id: "terminal", name: "Terminal", builtIn: true, view: "terminal",
                   sidebar: .init(shown: true), drawer: .init(open: false)),
        PaneLayout(id: "review", name: "Review", builtIn: true, view: "conversation",
                   sidebar: .init(shown: true), drawer: .init(open: true, tab: SideTab.review.rawValue)),
        PaneLayout(id: "focus", name: "Focus", builtIn: true, view: "terminal",
                   sidebar: .init(shown: false), details: .init(minimized: true), drawer: .init(open: false)),
    ]

    /// The window as it is now, under a name.
    @MainActor
    static func current(_ store: Store, name: String, theme: String? = nil) -> PaneLayout {
        let d = UserDefaults.standard
        var l = PaneLayout(name: name)
        l.view = store.showTerminal ? "terminal" : "conversation"
        if let s = WindowPanes.sidebar {
            l.sidebar = .init(shown: s.shown, width: s.shown ? s.width.rounded() : nil)
        }
        let height = d.object(forKey: DetailsPanel.heightKey) as? Double ?? DetailsPanel.compact
        l.details = .init(minimized: d.bool(forKey: DetailsPanel.minimizedKey), height: height.rounded())
        l.drawer = .init(open: store.side.open, tab: store.side.tab.rawValue, width: store.side.width.rounded())
        l.textSize = Double(store.fontSize)
        l.theme = theme
        return l
    }

    /// Arranges the window so.
    @MainActor
    func apply(_ store: Store) {
        let d = UserDefaults.standard
        if let t = theme, Palette.exists(t) { d.set(t, forKey: Palette.defaultsKey) }
        if let v = view { store.showTerminal = v == "terminal" }
        if let s = sidebar { WindowPanes.setSidebar(shown: s.shown, width: s.width) }
        if let x = details {
            d.set(x.minimized, forKey: DetailsPanel.minimizedKey)
            if let h = x.height { d.set(max(h, 58), forKey: DetailsPanel.heightKey) }
        }
        if let r = drawer {
            if let w = r.width { store.setSideWidth(w) }
            let tab = r.tab.flatMap(SideTab.init(rawValue:)).flatMap { SideTab.built.contains($0) ? $0 : nil } ?? store.side.tab
            if r.open { store.openSide(tab) } else { store.closeSide() }
        }
        if let t = textSize { store.setFontSize(CGFloat(t)) }
    }

    /// What it sets, in a few words, for its row in Make a Theme.
    var summary: String {
        var parts: [String] = []
        if let v = view { parts.append(v == "terminal" ? "Terminal" : "Conversation") }
        if let s = sidebar { parts.append(s.shown ? (s.width.map { "sidebar \(Int($0)) pt" } ?? "sidebar") : "no sidebar") }
        if let r = drawer, r.open {
            let tab = r.tab.flatMap(SideTab.init(rawValue:))?.title ?? "right sidebar"
            parts.append(tab.lowercased() + (r.width.map { " \(Int($0)) pt" } ?? ""))
        }
        if details?.minimized == true { parts.append("details in a line") }
        if let t = textSize { parts.append("\(Int(t)) pt text") }
        if let t = theme { parts.append(Palette.named(t).name) }
        return parts.joined(separator: " · ")
    }
}

/// The main window's sidebar, which SwiftUI's NavigationSplitView keeps as
/// the first item of an AppKit split view: whether it shows, and its width.
@MainActor
enum WindowPanes {
    /// Irie's own window, as ContentView finds it.
    static weak var window: NSWindow?

    private static func controller() -> NSSplitViewController? {
        guard let root = window?.contentView else { return nil }
        var found: [NSSplitViewController] = []
        var stack: [NSView] = [root]
        while let v = stack.popLast() {
            if let s = v as? NSSplitView, let c = s.delegate as? NSSplitViewController, c.splitViewItems.count >= 2 {
                if c.splitViewItems[0].behavior == .sidebar { return c }
                found.append(c)
            }
            stack.append(contentsOf: v.subviews)
        }
        return found.first
    }

    static var sidebar: (shown: Bool, width: Double)? {
        guard let c = controller() else { return nil }
        let item = c.splitViewItems[0]
        return (!item.isCollapsed, Double(item.viewController.view.frame.width))
    }

    /// Shows or hides the sidebar as its toggle does, then moves its edge.
    static func setSidebar(shown: Bool, width: Double?) {
        guard let c = controller() else { return }
        let item = c.splitViewItems[0]
        let toggled = item.isCollapsed == shown
        if toggled { c.toggleSidebar(nil) }
        guard shown, let w = width else { return }
        // After the toggle's own animation, which would undo the move.
        DispatchQueue.main.asyncAfter(deadline: .now() + (toggled ? 0.4 : 0)) {
            c.splitView.setPosition(CGFloat(w), ofDividerAt: 0)
        }
    }

    /// The window's width, for the drawing of the layout.
    static var width: Double? { window.map { Double($0.frame.width) } }
}

/// Where your themes and layouts live: $XDG_CONFIG_HOME/irie, or
/// ~/.config/irie, as internal/paths has it.
enum LookFiles {
    static var config: URL {
        let env = ProcessInfo.processInfo.environment
        if let x = env["XDG_CONFIG_HOME"], x.hasPrefix("/") {
            return URL(fileURLWithPath: x).appendingPathComponent("irie")
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".config/irie")
    }

    static var themes: URL { config.appendingPathComponent("themes") }
    static var layouts: URL { config.appendingPathComponent("layouts") }

    /// A theme file as written: everything in it optional, so a hand-made
    /// file can give a name and a colour or two.
    private struct ThemeFile: Codable {
        var name: String?
        var basedOn: String?
        var light: [String: String]?
        var dark: [String: String]?

        enum CodingKeys: String, CodingKey {
            case name, light, dark
            case basedOn = "based_on"
        }
    }

    /// The JSON files of a folder, oldest first, so a new one goes last.
    private static func files(_ dir: URL) -> [URL] {
        let keys: [URLResourceKey] = [.creationDateKey]
        let urls = (try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: keys, options: [.skipsHiddenFiles])) ?? []
        func made(_ u: URL) -> Date { (try? u.resourceValues(forKeys: Set(keys)).creationDate) ?? .distantPast }
        return urls.filter { $0.pathExtension.lowercased() == "json" }
            .sorted { (made($0), $0.lastPathComponent) < (made($1), $1.lastPathComponent) }
    }

    /// Every theme in the folder, and what was wrong with any that could
    /// not be read whole. One named like a palette of Irie's is left out.
    static func readThemes() -> (themes: [CustomTheme], problems: [String]) {
        var themes: [CustomTheme] = []
        var problems: [String] = []
        for url in files(LookFiles.themes) {
            let id = url.deletingPathExtension().lastPathComponent
            guard !Palette.all.contains(where: { $0.id == id }) else {
                problems.append("\(url.lastPathComponent): \(id) is the name of one of Irie's palettes")
                continue
            }
            guard let data = try? Data(contentsOf: url), let f = try? JSONDecoder().decode(ThemeFile.self, from: data) else {
                problems.append("\(url.lastPathComponent) is not a theme's JSON")
                continue
            }
            let base = Palette.all.first { $0.id == f.basedOn } ?? Palette.standard
            let (light, badLight) = base.light.with(f.light)
            let (dark, badDark) = base.dark.with(f.dark)
            problems += (badLight.map { "light \($0)" } + badDark.map { "dark \($0)" }).map { "\(url.lastPathComponent): \($0)" }
            let given = (f.name ?? "").trimmingCharacters(in: .whitespaces)
            let name = given.isEmpty ? id : given
            themes.append(CustomTheme(palette: Palette(id: id, name: name, light: light, dark: dark), basedOn: base.id, file: url))
        }
        return (themes, problems)
    }

    static func readLayouts() -> (layouts: [PaneLayout], problems: [String]) {
        var layouts: [PaneLayout] = []
        var problems: [String] = []
        for url in files(LookFiles.layouts) {
            guard let data = try? Data(contentsOf: url), var l = try? JSONDecoder().decode(PaneLayout.self, from: data) else {
                problems.append("\(url.lastPathComponent) is not a layout's JSON")
                continue
            }
            l.id = url.deletingPathExtension().lastPathComponent
            layouts.append(l)
        }
        return (layouts, problems)
    }

    /// A theme as a file: every colour, light then dark, in the order of
    /// ColorRole.all, so a hand edit finds them in groups.
    static func json(_ t: CustomTheme) -> Data {
        func string(_ s: String) -> String {
            let e = JSONEncoder()
            e.outputFormatting = .withoutEscapingSlashes
            return (try? e.encode(s)).flatMap { String(data: $0, encoding: .utf8) } ?? "\"\""
        }
        func set(_ s: Swatches) -> String {
            ColorRole.all.map { "    \"\($0.key)\": \"\(Swatches.hex(s[keyPath: $0.path]))\"" }.joined(separator: ",\n")
        }
        let text = "{\n  \"name\": \(string(t.name)),\n  \"based_on\": \(string(t.basedOn)),\n"
            + "  \"light\": {\n\(set(t.palette.light))\n  },\n  \"dark\": {\n\(set(t.palette.dark))\n  }\n}\n"
        return Data(text.utf8)
    }

    static func json(_ l: PaneLayout) throws -> Data {
        let e = JSONEncoder()
        e.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        return try e.encode(l) + Data("\n".utf8)
    }

    /// A file name for a new theme or layout from its name: lower case,
    /// words joined by dashes, and a number after it while that is taken.
    static func newID(_ name: String, taken: (String) -> Bool) -> String {
        var slug = ""
        for ch in name.lowercased() {
            if ch.isLetter || ch.isNumber, ch.isASCII { slug.append(ch) } else if !slug.hasSuffix("-") { slug.append("-") }
        }
        slug = String(slug.trimmingCharacters(in: CharacterSet(charactersIn: "-")).prefix(48))
        if slug.isEmpty { slug = "mine" }
        var id = slug
        var n = 2
        while taken(id) {
            id = "\(slug)-\(n)"
            n += 1
        }
        return id
    }

    static func write(_ data: Data, to url: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: url, options: .atomic)
    }

    /// Puts a file in the Trash, where it can be got back.
    static func trash(_ url: URL) throws {
        try FileManager.default.trashItem(at: url, resultingItemURL: nil)
    }
}

/// Your themes and layouts as the app has them, kept in step with their
/// folders, and the theme Make a Theme tries on the window.
@MainActor
@Observable
final class LookBook {
    static let shared = LookBook()

    /// Your themes, oldest first.
    private(set) var themes: [CustomTheme] = []
    /// Your layouts, oldest first: the first nine take ⌃⌘1 to ⌃⌘9.
    private(set) var layouts: [PaneLayout] = []
    /// Files that could not be read whole, and why.
    private(set) var problems: [String] = []
    /// Bumped when the colours on show change while the palette's name
    /// stays: a theme tried on, or one of yours changed. The window's
    /// views read it, so they draw again.
    private(set) var revision = 0
    /// What Make a Theme opens on next (ThemeMaker.swift).
    var request: Request?

    enum Request: Equatable {
        /// A new theme, starting from the palette in use.
        case new
        /// One of yours, to change.
        case edit(String)
        /// The Layouts page, with the name of a new one to type, or not.
        case layouts(saving: Bool)
    }

    @ObservationIgnored private weak var store: Store?
    @ObservationIgnored private var sources: [DispatchSourceFileSystemObject] = []
    @ObservationIgnored private var reload: DispatchWorkItem?
    @ObservationIgnored private var started = false
    @ObservationIgnored private var fixture = false

    private init() {
        read()
    }

    /// Starts following the folders, once; the store hears when the
    /// palette in use changed, for the status line.
    func start(_ store: Store) {
        self.store = store
        guard !started, !fixture else { return }
        started = true
        watch()
        NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.changed() }
        }
    }

    /// `--snapshot`'s theme and layouts, in place of the files.
    func useFixture() {
        fixture = true
        let start = Palette.evergreen
        var light = start.light, dark = start.dark
        light.accent = 0x2D5F7A; light.accentInk = 0x234E66; light.accentFill = 0x93B5C9; light.selection = 0xD8E3EA; light.waves = 0x5A8AA6
        dark.accent = 0x96C3DB; dark.accentInk = 0xAFD2E5; dark.accentFill = 0x96C3DB; dark.selection = 0x283A46; dark.waves = 0x96C3DB
        themes = [CustomTheme(palette: Palette(id: "harbour", name: "Harbour", light: light, dark: dark), basedOn: start.id,
                              file: URL(fileURLWithPath: "/home/alice/.config/irie/themes/harbour.json"))]
        layouts = [
            PaneLayout(id: "pairing", name: "Pairing", view: "terminal", sidebar: .init(shown: true, width: 300),
                       details: .init(minimized: false, height: 120), drawer: .init(open: true, tab: "helpers", width: 380), textSize: 14),
            PaneLayout(id: "night", name: "Night", view: "conversation", sidebar: .init(shown: true, width: 290),
                       details: .init(minimized: true), drawer: .init(open: false), textSize: 13, theme: "harbour"),
        ]
        problems = []
        Palette.custom = themes.map(\.palette)
        revision += 1
    }

    /// Reads both folders again; a theme in use that changed is drawn and
    /// sent to the core again.
    private func read() {
        let (t, tp) = LookFiles.readThemes()
        let (l, lp) = LookFiles.readLayouts()
        let before = themes
        if t != themes { themes = t }
        if l != layouts { layouts = l }
        if tp + lp != problems { problems = tp + lp }
        Palette.custom = t.map(\.palette)
        if t != before {
            revision += 1
            let chosen = Palette.chosen
            if before.first(where: { $0.id == chosen }) != t.first(where: { $0.id == chosen }) { store?.sendTheme(chosen) }
        }
    }

    /// A file came, went or changed: read the folders soon, once for a
    /// burst of changes.
    private func changed() {
        guard !fixture else { return }
        reload?.cancel()
        let work = DispatchWorkItem { [weak self] in
            MainActor.assumeIsolated {
                self?.read()
                self?.watch()
            }
        }
        reload = work
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2, execute: work)
    }

    /// Watches the config folder and both of its folders, those that are
    /// there: a file made, removed or renamed in them writes the folder.
    private func watch() {
        for s in sources { s.cancel() }
        sources = []
        for dir in [LookFiles.config, LookFiles.themes, LookFiles.layouts] {
            let fd = open(dir.path, O_EVTONLY)
            guard fd >= 0 else { continue }
            let s = DispatchSource.makeFileSystemObjectSource(fileDescriptor: fd, eventMask: [.write, .delete, .rename, .link], queue: .main)
            s.setEventHandler { [weak self] in MainActor.assumeIsolated { self?.changed() } }
            s.setCancelHandler { close(fd) }
            s.resume()
            sources.append(s)
        }
    }

    // MARK: Themes

    /// Shows a palette on the window in place of the one in use while Make
    /// a Theme has it, or puts the one in use back.
    func tryOn(_ p: Palette?) {
        guard Theme.trying != p else { return }
        Theme.trying = p
        revision += 1
    }

    func theme(_ id: String) -> CustomTheme? { themes.first { $0.id == id } }

    /// Writes a theme you made, a new one or one of yours changed, and
    /// makes it the theme in use.
    @discardableResult
    func save(_ p: Palette, basedOn: String, as id: String?) throws -> String {
        let id = id ?? LookFiles.newID(p.name) { candidate in
            Palette.all.contains { $0.id == candidate } || FileManager.default.fileExists(atPath: LookFiles.themes.appendingPathComponent(candidate + ".json").path)
        }
        let url = LookFiles.themes.appendingPathComponent(id + ".json")
        let theme = CustomTheme(palette: Palette(id: id, name: p.name, light: p.light, dark: p.dark), basedOn: basedOn, file: url)
        try LookFiles.write(LookFiles.json(theme), to: url)
        read()
        watch()
        UserDefaults.standard.set(id, forKey: Palette.defaultsKey)
        store?.sendTheme(id)
        return id
    }

    /// Puts a theme of yours in the Trash. In use, the palette it started
    /// from takes its place.
    func delete(theme id: String) throws {
        guard let t = theme(id) else { return }
        try LookFiles.trash(t.file)
        if Palette.chosen == id { UserDefaults.standard.set(t.basedOn, forKey: Palette.defaultsKey) }
        read()
    }

    // MARK: Layouts

    /// Saves the window's layout under a name: a new one, or in place of
    /// the one of yours with that id.
    @discardableResult
    func save(_ layout: PaneLayout, as id: String? = nil) throws -> String {
        let id = id ?? LookFiles.newID(layout.name) { candidate in
            FileManager.default.fileExists(atPath: LookFiles.layouts.appendingPathComponent(candidate + ".json").path)
        }
        var l = layout
        l.id = id
        try LookFiles.write(LookFiles.json(l), to: LookFiles.layouts.appendingPathComponent(id + ".json"))
        read()
        watch()
        return id
    }

    func delete(layout id: String) throws {
        let url = LookFiles.layouts.appendingPathComponent(id + ".json")
        if FileManager.default.fileExists(atPath: url.path) { try LookFiles.trash(url) }
        read()
    }

    /// Applies a layout of yours or a built-in one, and says which, as a
    /// key such as ⌃⌘2 does it unseen.
    func apply(_ l: PaneLayout) {
        guard let store else { return }
        l.apply(store)
        store.say("The “\(l.name)” layout.", error: false, seconds: 2)
    }

    /// The file of a layout of yours.
    func file(layout id: String) -> URL { LookFiles.layouts.appendingPathComponent(id + ".json") }
}

extension Palette {
    /// Your own palettes (Looks.swift), after Irie's in View > Theme. They
    /// are read the first time a palette is looked up, and LookBook keeps
    /// them up to date.
    static var custom: [Palette] = LookFiles.readThemes().themes.map(\.palette)

    /// Irie's palettes, then yours.
    static var choices: [Palette] { all + custom }

    static func exists(_ id: String) -> Bool { choices.contains { $0.id == id } }

    /// The palette chosen in View > Theme, as the app keeps it, whatever
    /// Make a Theme tries on meanwhile.
    static var chosen: String { UserDefaults.standard.string(forKey: defaultsKey) ?? standard.id }

    /// Whether it is one of yours.
    var isCustom: Bool { !Palette.all.contains { $0.id == id } }
}

/// View > Theme and View > Layout.
@MainActor
struct LookMenus: View {
    let store: Store
    @Binding var theme: String
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        let looks = LookBook.shared
        Menu("Theme") {
            Picker("Theme", selection: $theme) {
                ForEach(Palette.all) { p in Text(p.name).tag(p.id) }
                if !looks.themes.isEmpty {
                    Divider()
                    ForEach(looks.themes) { t in Text(t.name).tag(t.id) }
                }
            }
            .pickerStyle(.inline)
            .labelsHidden()
            Divider()
            Button("Make a Theme…") { open(.new) }
            if let t = looks.theme(theme) {
                Button("Change “\(t.name)”…") { open(.edit(t.id)) }
            }
        }
        Menu("Layout") {
            ForEach(Array(looks.layouts.enumerated()), id: \.element.id) { i, l in
                if i < 9 {
                    Button(l.name) { looks.apply(l) }
                        .keyboardShortcut(KeyEquivalent(Character(String(i + 1))), modifiers: [.control, .command])
                } else {
                    Button(l.name) { looks.apply(l) }
                }
            }
            if !looks.layouts.isEmpty { Divider() }
            ForEach(PaneLayout.builtIns) { l in
                Button(l.name) { looks.apply(l) }
            }
            Divider()
            Button("Save the Window’s Layout…") { open(.layouts(saving: true)) }
            Button("Arrange Layouts…") { open(.layouts(saving: false)) }
        }
    }

    private func open(_ r: LookBook.Request) {
        LookBook.shared.request = r
        openWindow(id: ThemeMaker.windowID)
    }
}
