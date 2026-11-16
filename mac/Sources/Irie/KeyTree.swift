import AppKit
import SwiftUI

/// A tree of keys with fn, as Emacs's prefix keys are (C-x C-s): Fn+X opens
/// it, and each key after, fn still held, goes a level down, until one does
/// something. Fn+X Fn+L Fn+R reverses the layout: X for execute, L for
/// layout, R for reverse. Hold fn and tap the letters, as Ctrl is held for
/// C-x C-s. A moment into a chord a hint over the terminal lists the keys
/// that come next (KeyHint). Esc, a click or the app losing the focus drops
/// the chord, and a key typed without fn drops it and types as it would
/// have. Only KeyRouter sees fn on a letter (Roots' principles §4.8), so
/// the menus name these chords in their help.
///
/// Your own chords go in keys.json beside your layouts (KeyFile), read
/// afresh whenever it changes, so an agent can add one.
struct KeyNode: Identifiable, Equatable {
    /// Its letter or digit, lower case.
    var key: String
    var name: String
    /// What it does; nil for a node that opens more keys.
    var action: KeyAction?
    var children: [KeyNode] = []

    var id: String { key }

    func child(_ key: String) -> KeyNode? { children.first { $0.key == key } }

    /// Adds a node at a path of keys, making the nodes on the way, and
    /// replacing one at the end; a node with neither an action nor a name
    /// of its own keeps what was there.
    @MainActor
    mutating func put(_ path: ArraySlice<String>, name: String?, action: KeyAction?) {
        guard let k = path.first else { return }
        var i = children.firstIndex { $0.key == k }
        if i == nil {
            children.append(KeyNode(key: k, name: k.uppercased()))
            i = children.count - 1
        }
        guard let at = i else { return }
        if path.count == 1 {
            if let action {
                children[at].action = action
                children[at].children = []
                children[at].name = name ?? action.name
            } else if let name {
                children[at].name = name
            }
        } else {
            // A key that did something now opens more.
            children[at].action = nil
            children[at].put(path.dropFirst(), name: name, action: action)
        }
    }
}

/// What a key of the tree does.
enum KeyAction: Equatable {
    /// The layout reversed, or back (Store.reversed).
    case reverse
    /// A layout, by its number among yours (1 for ⌃⌘1), its file's name or
    /// its own name, or a built-in one's, such as focus.
    case layout(String)
    /// Zen on or off (Zen.swift).
    case zen
    /// A menu item, by its menu's name and its own, as "View › Layout ›
    /// Focus".
    case menu([String])

    /// Read from a chord's "do": reverse, zen, layout:NAME or menu:PATH.
    init?(_ text: String) {
        let t = text.trimmingCharacters(in: .whitespaces)
        if t.lowercased() == "reverse" { self = .reverse; return }
        if t.lowercased() == "zen" { self = .zen; return }
        if let colon = t.firstIndex(of: ":") {
            let kind = t[..<colon].lowercased(), rest = t[t.index(after: colon)...].trimmingCharacters(in: .whitespaces)
            guard !rest.isEmpty else { return nil }
            if kind == "layout" { self = .layout(rest); return }
            if kind == "menu" {
                let path = MenuPath.split(rest)
                guard !path.isEmpty else { return nil }
                self = .menu(path)
                return
            }
        }
        return nil
    }

    /// Its name in the hint, unless the chord gives one.
    @MainActor
    var name: String {
        switch self {
        case .reverse: return "Reverse"
        case .zen: return "Zen"
        case let .layout(l): return KeyTree.layout(l)?.name ?? l
        case let .menu(path): return path.last ?? "Menu"
        }
    }
}

/// The tree as it is now: Irie's own keys, then yours over them.
@MainActor
enum KeyTree {
    /// Letters macOS keeps for itself with fn, which never reach the app
    /// (Apple's Mac keyboard shortcuts: Fn-A the Dock, Fn-C Control
    /// Center, Fn-D dictation, Fn-E emoji, Fn-H the desktop, Fn-N
    /// notifications, Fn-Q a Quick Note), and F, which enters full screen
    /// from the View menu.
    static let taken: Set<String> = ["a", "c", "d", "e", "f", "h", "n", "q"]

    /// Irie's own: X Execute › L Layout › R Reverse, and 1 to 9 for your
    /// first nine layouts, as ⌃⌘1 to ⌃⌘9.
    static func builtIn(layouts: [PaneLayout]) -> KeyNode {
        var layout = KeyNode(key: "l", name: "Layout", children: [KeyNode(key: "r", name: "Reverse", action: .reverse)])
        for (i, l) in layouts.prefix(9).enumerated() {
            layout.children.append(KeyNode(key: String(i + 1), name: l.name, action: .layout(String(i + 1))))
        }
        return KeyNode(key: "", name: "", children: [KeyNode(key: "x", name: "Execute", children: [layout])])
    }

    /// The root: Irie's keys with yours from keys.json put over them.
    static var root: KeyNode {
        var root = builtIn(layouts: LookBook.shared.layouts)
        for c in KeyFile.shared.chords {
            root.put(c.keys[...], name: c.name, action: c.action)
        }
        return root
    }

    /// The layout a chord names: by number among yours, by file name or
    /// name, yours first, then a built-in one.
    static func layout(_ name: String) -> PaneLayout? {
        let mine = LookBook.shared.layouts
        if let n = Int(name), (1...mine.count).contains(n) { return mine[n - 1] }
        let all = mine + PaneLayout.builtIns
        return all.first { $0.id == name } ?? all.first { $0.name.caseInsensitiveCompare(name) == .orderedSame }
    }

    /// How a run of keys reads: "Fn+X Fn+L".
    static func spoken(_ keys: [String]) -> String {
        keys.map { "Fn+" + $0.uppercased() }.joined(separator: " ")
    }
}

/// Your chords, from keys.json in the config folder ($XDG_CONFIG_HOME/irie):
///
///     {"chords": [
///       {"keys": "x l o", "do": "layout:focus"},
///       {"keys": "x s", "name": "Session"},
///       {"keys": "x s p", "do": "menu:Session › Park", "name": "Park"}
///     ]}
///
/// keys are the letters or digits after fn, one per key; do is reverse,
/// zen, layout:NAME or menu:PATH; a chord with no do names the keys it
/// opens. Read again whenever the file changes.
@MainActor
final class KeyFile {
    static let shared = KeyFile()

    struct Chord: Equatable {
        var keys: [String]
        var name: String?
        var action: KeyAction?
    }

    private struct File: Decodable {
        var chords: [Entry]?
    }

    private struct Entry: Decodable {
        var keys: String
        var `do`: String?
        var name: String?
    }

    static var url: URL { LookFiles.config.appendingPathComponent("keys.json") }

    /// What the file says, and what was wrong with it.
    private(set) var chords: [Chord] = []
    private(set) var problems: [String] = []
    private var stamp: Date?
    /// The problems already said, so each is said once.
    private var said: [String] = []

    /// Reads the file again if it changed since, and says what is wrong
    /// with it, once.
    func refresh(_ store: Store?) {
        let url = Self.url
        let made = (try? url.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
        guard made != stamp else { return }
        stamp = made
        (chords, problems) = made == nil ? ([], []) : Self.read(url)
        if !problems.isEmpty, problems != said {
            said = problems
            store?.say("keys.json: " + problems.joined(separator: "; "), error: true)
        }
    }

    /// The chords in a file, and what is wrong with any left out.
    static func read(_ url: URL) -> ([Chord], [String]) {
        guard let data = try? Data(contentsOf: url) else { return ([], ["it could not be read"]) }
        return parse(data)
    }

    static func parse(_ data: Data) -> ([Chord], [String]) {
        guard let file = try? JSONDecoder().decode(File.self, from: data) else {
            return ([], [#"it is not JSON of the form {"chords": [{"keys": "x l r", "do": "reverse"}]}"#])
        }
        var chords: [Chord] = []
        var problems: [String] = []
        for e in file.chords ?? [] {
            let keys = e.keys.lowercased().split(whereSeparator: { $0 == " " || $0 == "," || $0 == "+" })
                .map(String.init).filter { $0 != "fn" }
            if keys.isEmpty || keys.contains(where: { $0.count != 1 || !$0.first!.isASCII || !($0.first!.isLetter || $0.first!.isNumber) }) {
                problems.append("“\(e.keys)” is not keys, one letter or digit each")
                continue
            }
            if let k = keys.first(where: { KeyTree.taken.contains($0) }) {
                problems.append("“\(e.keys)”: macOS keeps Fn+\(k.uppercased()) for itself")
                continue
            }
            var action: KeyAction?
            if let d = e.do {
                guard let a = KeyAction(d) else {
                    problems.append("“\(e.keys)”: “\(d)” is not reverse, zen, layout:NAME or menu:PATH")
                    continue
                }
                action = a
            }
            chords.append(Chord(keys: keys, name: e.name, action: action))
        }
        return (chords, problems)
    }
}

/// The chord under way, as KeyRouter feeds it keys, and its hint.
@MainActor
@Observable
final class KeyChord {
    static let shared = KeyChord()

    /// The keys pressed so far, after fn.
    private(set) var path: [String] = []
    /// The hint shows: a moment after the chord starts, so a quick one
    /// never flashes it.
    private(set) var hintShown = false
    @ObservationIgnored private var hintSoon: DispatchWorkItem?
    @ObservationIgnored private var clicks: Any?
    @ObservationIgnored private var away: NSObjectProtocol?
    @ObservationIgnored private weak var store: Store?

    static let hintDelay: TimeInterval = 0.4

    var pending: Bool { !path.isEmpty }

    /// The node the keys so far lead to.
    var node: KeyNode? { path.isEmpty ? nil : Self.walk(KeyTree.root, path) }

    static func walk(_ root: KeyNode, _ path: [String]) -> KeyNode? {
        var n = root
        for k in path {
            guard let c = n.child(k) else { return nil }
            n = c
        }
        return n
    }

    /// Takes a key: true when the chord used it, false to let it go on as
    /// it would have.
    func route(_ event: NSEvent, store: Store) -> Bool {
        self.store = store
        let fn = event.modifierFlags.contains(.function)
        let mods = event.modifierFlags.intersection([.command, .control, .option, .shift])
        // A letter or a digit. Arrows and F-keys carry the fn flag too, so
        // only these count as keys with fn.
        let c = event.charactersIgnoringModifiers?.lowercased() ?? ""
        let letter = c.count == 1 && (c.first!.isLetter || c.first!.isNumber) && c.first!.isASCII ? c : nil
        guard pending else {
            guard fn, mods.isEmpty, let k = letter else { return false }
            KeyFile.shared.refresh(store)
            guard let n = KeyTree.root.child(k) else { return false }
            take(k, n)
            return true
        }
        // Held keys repeat; a chord takes each key once.
        if event.isARepeat, fn, letter != nil { return true }
        if event.keyCode == 53 { // Esc
            cancel()
            return true
        }
        guard fn, mods.isEmpty, let k = letter else {
            // Typed without fn: the chord is dropped, and the key goes on.
            cancel()
            return false
        }
        guard let here = node, let n = here.child(k) else {
            let before = KeyTree.spoken(path)
            cancel()
            store.say("\(before) Fn+\(k.uppercased()) does nothing. Wait a moment after \(before) to see its keys.", error: false, seconds: 4)
            NSSound.beep()
            return true
        }
        take(k, n)
        return true
    }

    private func take(_ k: String, _ n: KeyNode) {
        if let a = n.action {
            cancel()
            if let store { Self.run(a, store: store) }
            return
        }
        if path.isEmpty { watch() }
        path.append(k)
        if hintShown {
            announce()
        } else if hintSoon == nil {
            let soon = DispatchWorkItem { [weak self] in
                MainActor.assumeIsolated {
                    guard let self, self.pending else { return }
                    self.hintShown = true
                    self.announce()
                }
            }
            hintSoon = soon
            DispatchQueue.main.asyncAfter(deadline: .now() + Self.hintDelay, execute: soon)
        }
    }

    /// Drops the chord and its hint.
    func cancel() {
        hintSoon?.cancel()
        hintSoon = nil
        if !path.isEmpty { path = [] }
        if hintShown { hintShown = false }
        if let c = clicks { NSEvent.removeMonitor(c) }
        if let a = away { NotificationCenter.default.removeObserver(a) }
        clicks = nil
        away = nil
    }

    /// A click anywhere or the app losing the focus drops the chord.
    private func watch() {
        guard clicks == nil else { return }
        clicks = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]) { [weak self] event in
            MainActor.assumeIsolated { self?.cancel() }
            return event
        }
        away = NotificationCenter.default.addObserver(forName: NSApplication.didResignActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.cancel() }
        }
    }

    /// VoiceOver hears what comes next.
    private func announce() {
        guard let n = node else { return }
        let next = n.children.map { "\($0.key.uppercased()) \($0.name)" }.joined(separator: ", ")
        AccessibilityNotification.Announcement("\(KeyTree.spoken(path)): \(next)").post()
    }

    static func run(_ a: KeyAction, store: Store) {
        switch a {
        case .reverse:
            store.toggleReversed()
        case .zen:
            store.toggleZen()
        case let .layout(name):
            if let l = KeyTree.layout(name) {
                LookBook.shared.apply(l)
            } else {
                store.say("There is no layout “\(name)”.", error: true)
            }
        case let .menu(path):
            if !MenuPath.perform(path) {
                store.say("“\(path.joined(separator: " › "))” is not a menu item that can be chosen now.", error: true)
            }
        }
    }

    /// `--snapshot`'s: a chord part way, its hint showing.
    func showFixture(_ keys: [String]) {
        cancel()
        path = keys
        hintShown = !keys.isEmpty
    }
}

/// A menu item found by its menu's name and its own, and chosen as a click
/// on it would.
@MainActor
enum MenuPath {
    /// "View › Layout › Focus", or with ">" between.
    nonisolated static func split(_ text: String) -> [String] {
        text.replacingOccurrences(of: ">", with: "›").split(separator: "›")
            .map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    /// Chooses it if it is there and can be chosen now; whether it was.
    static func perform(_ path: [String]) -> Bool {
        guard var menu = NSApp.mainMenu, !path.isEmpty else { return false }
        func same(_ a: String, _ b: String) -> Bool {
            func plain(_ s: String) -> String { s.replacingOccurrences(of: "…", with: "").replacingOccurrences(of: "...", with: "").trimmingCharacters(in: .whitespaces) }
            return plain(a).caseInsensitiveCompare(plain(b)) == .orderedSame
        }
        for (i, title) in path.enumerated() {
            menu.update()
            guard let at = menu.items.firstIndex(where: { !$0.isSeparatorItem && same($0.title, title) }) else { return false }
            let item = menu.items[at]
            if i == path.count - 1 {
                guard item.isEnabled, item.submenu == nil else { return false }
                menu.performActionForItem(at: at)
                return true
            }
            guard let sub = item.submenu else { return false }
            menu = sub
        }
        return false
    }
}

/// The hint over the terminal while a chord waits for its next key: the
/// keys so far, then each key that comes next, with what it does or opens.
@MainActor
struct KeyHint: View {
    let chord: KeyChord

    var body: some View {
        ZStack {
            if chord.hintShown, let node = chord.node { panel(node) }
        }
        .animation(.easeOut(duration: 0.12), value: chord.hintShown)
    }

    private func panel(_ node: KeyNode) -> some View {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 4) {
                    ForEach(Array(chord.path.enumerated()), id: \.offset) { i, k in
                        if i > 0 {
                            Image(systemName: "chevron.right").font(.system(size: 9, weight: .semibold)).foregroundStyle(Theme.muted)
                        }
                        KeyCap(key: k)
                    }
                    Text(node.name)
                        .font(Theme.font(12, .semibold))
                        .foregroundStyle(Theme.ink)
                        .padding(.leading, 4)
                }
                Rectangle().fill(Theme.edge).frame(height: 1)
                VStack(alignment: .leading, spacing: 5) {
                    ForEach(node.children) { c in
                        HStack(spacing: 8) {
                            KeyCap(key: c.key)
                            Text(c.name)
                                .font(Theme.font(12))
                                .foregroundStyle(Theme.ink)
                                .lineLimit(1)
                            if c.action == nil {
                                Image(systemName: "ellipsis").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.muted)
                            }
                        }
                    }
                }
                Text("Hold fn for each key. Esc drops it.")
                    .font(Theme.font(10.5))
                    .foregroundStyle(Theme.muted)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .frame(minWidth: 200, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.field).shadow(color: Theme.shadow, radius: 8, y: 2))
            .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
            .fixedSize()
            .allowsHitTesting(false)
            .transition(.opacity)
            .accessibilityElement(children: .combine)
    }
}

/// A key as its cap: fn and the letter.
private struct KeyCap: View {
    let key: String

    var body: some View {
        HStack(spacing: 3) {
            Text("fn").font(Theme.font(9.5, .medium)).foregroundStyle(Theme.muted)
            Text(key.uppercased()).font(.system(size: 11.5, weight: .semibold, design: .monospaced)).foregroundStyle(Theme.ink)
        }
        .padding(.horizontal, 6)
        .frame(minWidth: 34, minHeight: 20)
        .background(RoundedRectangle(cornerRadius: 5, style: .continuous).fill(Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: 5, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
    }
}
