import AppKit
import SwiftUI

/// The look-ahead at keys: an alert-like pop-up over the window that shows
/// what each key does next from where the keyboard is. It shows a moment
/// into a chord with fn (KeyTree.swift); after Ctrl+X in a Claude Code
/// terminal, which Claude Code reads as the start of a chord of its own, so
/// S there is Send Now; and while a modifier is held alone: fn shows the
/// tree's root, ⌘, ⌃ and ⌥ what they do here, the place with the keyboard
/// first (a code card's S is Save, Claude Code's ⌃T its to-do list), then
/// Irie's own, read from the menus as they are. Its edges are blurred: it
/// shows what the window can do, not a part of it, and it takes no clicks
/// and no keys. A key still does what it does, as if the pop-up were not
/// there.
struct PeekRow: Identifiable, Equatable {
    /// The key as its cap shows it: "S", "⌃S", "Tab", "1–9".
    var key: String
    var name: String
    /// It opens more keys rather than acting.
    var opens = false

    var id: String { key + "\u{1}" + name }
}

struct PeekGroup: Identifiable, Equatable {
    /// Whose keys: "Claude Code", "This code card", "Irie".
    var head: String
    var rows: [PeekRow]

    var id: String { head }
}

/// One showing of the pop-up.
struct PeekSheet: Equatable {
    /// The keys so far, each as a cap: ["fn"], ["⌃X"], ["fn X", "fn L"].
    var keys: [String]
    var title: String
    /// Where the keyboard is, when it matters: "In the terminal, Claude Code".
    var place: String?
    var groups: [PeekGroup]
    var footer: String

    var isEmpty: Bool { groups.allSatisfy(\.rows.isEmpty) }
}

/// Where the window's keyboard is, as far as keys go.
enum PeekPlace: Equatable {
    /// The shown session's terminal; whether Claude Code runs in it.
    case agent(claude: Bool)
    /// A shell in the panes.
    case shell
    /// A code card's editor (CodeEditor.swift).
    case code
    /// A Vault card's note, in the same editor.
    case vault
    /// A field: the message box, the task bar, a name.
    case text
    case other

    @MainActor
    static func of(_ window: NSWindow?, store: Store) -> PeekPlace {
        guard let r = window?.firstResponder else { return .other }
        if let t = r as? CodeTextView { return t.prose ? .vault : .code }
        if r is TerminalScreen {
            // A card with the keyboard is a pane's own shell; else the
            // agent's terminal, or a teammate's beside it.
            if store.rolodex.keys != nil { return .shell }
            let agent = store.selectedRow?.agent ?? "claude"
            return .agent(claude: agent.lowercased().hasPrefix("claude"))
        }
        if r is NSTextView || r is NSTextField { return .text }
        return .other
    }

    var words: String? {
        switch self {
        case let .agent(claude): return claude ? "In the terminal, with Claude Code" : "In the terminal"
        case .shell: return "In a shell in the panes"
        case .code: return "In the code card"
        case .vault: return "In the vault card"
        case .text: return "In a text field"
        case .other: return nil
        }
    }
}

/// The pop-up's state, as KeyRouter feeds it keys and modifier changes, and
/// KeyChord its chords.
@MainActor
@Observable
final class KeyPeek {
    static let shared = KeyPeek()

    /// What shows, if anything.
    private(set) var sheet: PeekSheet?

    /// Why it shows, which says what ends it.
    private enum Cause { case chord, hold, program }
    @ObservationIgnored private var cause: Cause?
    @ObservationIgnored private var holdSoon: DispatchWorkItem?
    @ObservationIgnored private var programEnds: DispatchWorkItem?
    @ObservationIgnored private var held: NSEvent.ModifierFlags = []
    @ObservationIgnored private var heldAt = NSPoint.zero
    /// Something showed as the last key came: a chord it starts shows its
    /// keys at once, as the person is reading them already.
    @ObservationIgnored private(set) var showedAtKey = false

    /// Settings › Tabs: show what a modifier does while it is held alone.
    static let holdKey = "keyPeekHold"
    static var holds: Bool { UserDefaults.standard.object(forKey: holdKey) as? Bool ?? true }

    /// How long a modifier is held alone before its keys show: fn, which
    /// nothing else holds alone, a moment; the others longer, as ⌘ is held
    /// to see a terminal's links.
    static func holdDelay(_ m: NSEvent.ModifierFlags) -> TimeInterval { m == .function ? 0.5 : 1.0 }

    /// Claude Code drops a chord three seconds after its first key.
    static let programChord: TimeInterval = 3

    // MARK: Chords with fn

    /// A chord with fn shows its next keys, or nothing once it ends.
    func chord(_ path: [String], node: KeyNode?) {
        guard let node, !path.isEmpty else {
            if cause == .chord { hide() }
            return
        }
        cancelHold()
        cause = .chord
        sheet = PeekSheet(keys: path.map { "fn " + KeyCatalog.cap($0) }, title: node.name, place: nil,
                          groups: [PeekGroup(head: "", rows: node.children.map { PeekRow(key: KeyCatalog.cap($0.key), name: $0.name, opens: $0.action == nil) })],
                          footer: "Hold fn for each key. Esc drops it.")
    }

    // MARK: A modifier held alone

    /// A modifier went down or up. Held alone a moment, its keys show; let
    /// go, they go.
    func flags(_ event: NSEvent, window: NSWindow?, store: Store) {
        let mods = event.modifierFlags.intersection([.command, .control, .option, .shift, .function])
        guard mods != held else { return }
        held = mods
        holdSoon?.cancel()
        holdSoon = nil
        guard Self.holds, !KeyChord.shared.pending, !mods.isEmpty, mods != .shift else {
            if cause == .hold { hide() }
            return
        }
        // Already showing: the new set shows at once, as ⇧ joins ⌘.
        if cause == .hold {
            showHold(mods, window: window, store: store)
            return
        }
        guard cause == nil else { return }
        heldAt = NSEvent.mouseLocation
        let soon = DispatchWorkItem { [weak self, weak window] in
            MainActor.assumeIsolated {
                guard let self, self.held == mods, self.cause == nil else { return }
                // A pointer on the move is a ⌘ held to see the terminal's
                // links, not to read keys.
                let p = NSEvent.mouseLocation
                guard abs(p.x - self.heldAt.x) < 6, abs(p.y - self.heldAt.y) < 6 else { return }
                self.showHold(mods, window: window, store: store)
            }
        }
        holdSoon = soon
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.holdDelay(mods), execute: soon)
    }

    private func showHold(_ mods: NSEvent.ModifierFlags, window: NSWindow?, store: Store) {
        let place = PeekPlace.of(window, store: store)
        let s = KeyCatalog.held(mods, place: place, store: store)
        if s.isEmpty {
            if cause == .hold { hide() }
            return
        }
        cause = .hold
        sheet = s
    }

    private func cancelHold() {
        holdSoon?.cancel()
        holdSoon = nil
    }

    // MARK: Keys as they go by

    /// Every key the window gets, before anything acts on it; it never
    /// takes one. A key ends what a held modifier showed, ends Claude Code's
    /// chord after its second key, and Ctrl+X in its terminal starts one.
    func key(_ event: NSEvent, window: NSWindow?, store: Store) {
        showedAtKey = sheet != nil
        cancelHold()
        if cause == .hold { hide() }
        if cause == .program {
            hide()
            return
        }
        let mods = event.modifierFlags.intersection([.command, .control, .option, .shift])
        guard mods == .control, event.charactersIgnoringModifiers?.lowercased() == "x", !event.isARepeat,
              case .agent(claude: true) = PeekPlace.of(window, store: store) else { return }
        cause = .program
        sheet = KeyCatalog.claudeChord(store: store)
        let ends = DispatchWorkItem { [weak self] in
            MainActor.assumeIsolated { if self?.cause == .program { self?.hide() } }
        }
        programEnds = ends
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.programChord, execute: ends)
    }

    /// A click, or the app losing the focus, ends whatever shows but a
    /// chord with fn, which KeyChord ends itself.
    func drop() {
        cancelHold()
        held = []
        if cause == .hold || cause == .program { hide() }
    }

    func hide() {
        programEnds?.cancel()
        programEnds = nil
        cause = nil
        if sheet != nil { sheet = nil }
    }

    /// `--snapshot`'s.
    func showFixture(_ s: PeekSheet?) {
        hide()
        sheet = s
    }
}

/// What the keys do, by where the keyboard is and the modifiers held.
@MainActor
enum KeyCatalog {
    /// A key as its cap shows it.
    static func cap(_ key: String) -> String {
        switch key {
        case "\t": return "Tab"
        case "\r": return "Return"
        case " ": return "Space"
        case "\u{1b}": return "Esc"
        case "\u{7f}", "\u{8}": return "Delete"
        case String(UnicodeScalar(NSUpArrowFunctionKey)!): return "↑"
        case String(UnicodeScalar(NSDownArrowFunctionKey)!): return "↓"
        case String(UnicodeScalar(NSLeftArrowFunctionKey)!): return "←"
        case String(UnicodeScalar(NSRightArrowFunctionKey)!): return "→"
        default: return key.uppercased()
        }
    }

    /// The marks of a set of modifiers, in Apple's order: ⌃⌥⇧⌘.
    static func marks(_ m: NSEvent.ModifierFlags) -> String {
        var s = ""
        if m.contains(.function) { s += "fn " }
        if m.contains(.control) { s += "⌃" }
        if m.contains(.option) { s += "⌥" }
        if m.contains(.shift) { s += "⇧" }
        if m.contains(.command) { s += "⌘" }
        return s
    }

    /// The pop-up for a modifier held alone.
    static func held(_ mods: NSEvent.ModifierFlags, place: PeekPlace, store: Store) -> PeekSheet {
        if mods == .function { return root() }
        var groups: [PeekGroup] = []
        // The place's own keys first, then Irie's, which Irie catches
        // first where both have one.
        let mine = irie(mods)
        let taken = Set(mine.map(\.key))
        switch place {
        case .agent(claude: true):
            groups.append(PeekGroup(head: "Claude Code", rows: claude(mods).filter { !taken.contains($0.key) }))
        case .code:
            groups.append(PeekGroup(head: "This code card", rows: code(mods).filter { !taken.contains($0.key) }))
        case .vault:
            groups.append(PeekGroup(head: "This vault card", rows: code(mods).filter { !taken.contains($0.key) }))
        default:
            break
        }
        groups.append(PeekGroup(head: Brand.name, rows: mine))
        groups = groups.filter { !$0.rows.isEmpty }
        return PeekSheet(keys: [marks(mods).trimmingCharacters(in: .whitespaces)], title: "With \(marks(mods).trimmingCharacters(in: .whitespaces)) held",
                         place: place.words, groups: groups, footer: "Press a key to use it, or let go.")
    }

    /// Fn alone: the tree's root (KeyTree.swift), and the keys with fn that
    /// KeyRouter catches itself.
    static func root() -> PeekSheet {
        var rows = KeyTree.root.children.map { PeekRow(key: cap($0.key), name: $0.name, opens: $0.action == nil) }
        rows.append(PeekRow(key: "Tab", name: "Next space"))
        rows.append(PeekRow(key: "⇧Tab", name: "Previous space"))
        return PeekSheet(keys: ["fn"], title: "With fn held", place: nil, groups: [PeekGroup(head: "", rows: rows)],
                         footer: "Hold fn for each key of a chord.")
    }

    // MARK: Irie's own

    /// The menus' keys with exactly these modifiers, as the menus have them
    /// now, and those KeyRouter catches that no menu shows.
    static func irie(_ mods: NSEvent.ModifierFlags) -> [PeekRow] {
        var rows: [PeekRow] = []
        var seen = Set<String>()
        func add(_ r: PeekRow) {
            guard !seen.contains(r.key) else { return }
            seen.insert(r.key)
            rows.append(r)
        }
        for r in router(mods) { add(r) }
        var digits: [String] = []
        walk(NSApp.mainMenu, path: []) { item, path in
            var m = item.keyEquivalentModifierMask.intersection([.command, .control, .option, .shift])
            var key = item.keyEquivalent
            // An upper-case letter is the letter with ⇧.
            if key.count == 1, let c = key.first, c.isLetter, c.isUppercase {
                m.insert(.shift)
                key = key.lowercased()
            }
            guard m == mods, !key.isEmpty else { return }
            if key.count == 1, let c = key.first, c.isNumber, path.last == "Sessions" {
                digits.append(key)
                return
            }
            let name = item.title.isEmpty ? (path.last ?? "") : item.title
            add(PeekRow(key: cap(key), name: name.replacingOccurrences(of: "…", with: "")))
        }
        if !digits.isEmpty { add(PeekRow(key: "1–\(digits.count)", name: "Show that session")) }
        return rows
    }

    /// Each enabled item with a key, depth first, with the names of the
    /// menus it is in.
    private static func walk(_ menu: NSMenu?, path: [String], _ each: (NSMenuItem, [String]) -> Void) {
        guard let menu else { return }
        menu.update()
        for item in menu.items where !item.isSeparatorItem && !item.isHidden {
            if let sub = item.submenu {
                walk(sub, path: path + [item.title], each)
            } else if item.isEnabled, !item.keyEquivalent.isEmpty {
                each(item, path)
            }
        }
    }

    /// Keys KeyRouter catches that no menu item names (IrieApp.swift).
    static func router(_ mods: NSEvent.ModifierFlags) -> [PeekRow] {
        switch mods {
        case .control: return [PeekRow(key: "1–9", name: "Show that space")]
        case [.control, .option]: return [PeekRow(key: "1–9", name: "Send the card to that space")]
        default: return []
        }
    }

    // MARK: A code card

    /// The code card's editor's own keys (CodeEditor.swift).
    static func code(_ mods: NSEvent.ModifierFlags) -> [PeekRow] {
        switch mods {
        case .command:
            return [PeekRow(key: "S", name: "Save"), PeekRow(key: "P", name: "Open a file"), PeekRow(key: "L", name: "Go to a line"),
                    PeekRow(key: "F", name: "Find"), PeekRow(key: "G", name: "Find next"), PeekRow(key: "E", name: "Find the selection"),
                    PeekRow(key: "/", name: "Comment out, or back in"), PeekRow(key: "]", name: "Indent"), PeekRow(key: "[", name: "Outdent"),
                    PeekRow(key: "W", name: "Close the file")]
        case [.command, .shift]:
            return [PeekRow(key: "G", name: "Find previous")]
        case [.command, .option]:
            return [PeekRow(key: "F", name: "Find and replace")]
        default:
            return []
        }
    }

    // MARK: Claude Code

    /// Claude Code's keys with these modifiers, its defaults with the
    /// person's own keybindings.json over them (code.claude.com/docs/en/
    /// keybindings).
    static func claude(_ mods: NSEvent.ModifierFlags) -> [PeekRow] {
        let mine = ClaudeKeys.shared.single(mods)
        switch mods {
        case .control:
            return ClaudeKeys.merge([
                PeekRow(key: "C", name: "Stop what it does"), PeekRow(key: "D", name: "Quit (twice)"),
                PeekRow(key: "T", name: "Its to-do list"), PeekRow(key: "O", name: "The full transcript"),
                PeekRow(key: "R", name: "Search past prompts"), PeekRow(key: "G", name: "Write in your editor"),
                PeekRow(key: "S", name: "Put the draft aside"), PeekRow(key: "J", name: "A new line"),
                PeekRow(key: "B", name: "Run in the background"), PeekRow(key: "V", name: "Paste an image"),
                PeekRow(key: "L", name: "Draw the screen again"), PeekRow(key: "_", name: "Undo"),
                PeekRow(key: "X", name: "More keys", opens: true),
            ], mine)
        case .option:
            return ClaudeKeys.merge([
                PeekRow(key: "P", name: "Pick the model"), PeekRow(key: "O", name: "Fast mode"), PeekRow(key: "T", name: "Thinking"),
            ], mine)
        case .shift:
            return []
        default:
            return mine
        }
    }

    /// Ctrl+X in Claude Code: the second keys of its chords.
    static func claudeChord(store _: Store) -> PeekSheet {
        let rows = ClaudeKeys.merge([
            PeekRow(key: "⌃S", name: "Send now"),
            PeekRow(key: "Return", name: "Queue it"),
            PeekRow(key: "⌃E", name: "Write in your editor"),
            PeekRow(key: "⌃K", name: "Stop background agents"),
            PeekRow(key: "⌃B", name: "Run in the background"),
            PeekRow(key: "Tab", name: "Into the band above"),
            PeekRow(key: "⌃A", name: "Fold the band above"),
            PeekRow(key: "B", name: "Diff: the next base"),
            PeekRow(key: "← ↑", name: "Pane: more room"),
            PeekRow(key: "→ ↓", name: "Pane: less room"),
            PeekRow(key: "X", name: "Close the pane"),
        ], ClaudeKeys.shared.chord("ctrl+x"))
        return PeekSheet(keys: ["⌃X"], title: "Claude Code waits for a key", place: PeekPlace.agent(claude: true).words,
                         groups: [PeekGroup(head: "Claude Code", rows: rows)], footer: "Within three seconds, or it lets the chord go.")
    }
}

/// The person's own Claude Code keys, from ~/.claude/keybindings.json, read
/// again whenever it changes: a key bound there replaces the default with
/// the action's name, and null takes it away.
@MainActor
final class ClaudeKeys {
    static let shared = ClaudeKeys()

    private var stamp: Date?
    /// Each key string as written, lower case, and its action, nil for none.
    private var bound: [String: String?] = [:]

    private static var url: URL {
        let env = ProcessInfo.processInfo.environment
        let dir = env["CLAUDE_CONFIG_DIR"].map { URL(fileURLWithPath: $0) }
            ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".claude")
        return dir.appendingPathComponent("keybindings.json")
    }

    private func refresh() {
        let made = (try? Self.url.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
        guard made != stamp else { return }
        stamp = made
        bound = [:]
        guard made != nil, let data = try? Data(contentsOf: Self.url),
              let top = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let blocks = top["bindings"] as? [[String: Any]] else { return }
        for b in blocks {
            guard let ctx = b["context"] as? String, ["Global", "Chat", "Task"].contains(ctx),
                  let keys = b["bindings"] as? [String: Any] else { continue }
            for (k, v) in keys {
                bound[k.lowercased().trimmingCharacters(in: .whitespaces)] = .some(v as? String)
            }
        }
    }

    /// Bindings of one key with these modifiers, as rows.
    func single(_ mods: NSEvent.ModifierFlags) -> [PeekRow] {
        refresh()
        let want = Self.words(mods)
        return bound.compactMap { k, v in
            guard !k.contains(" "), let plus = k.lastIndex(of: "+"), String(k[..<plus]) == want else { return nil }
            return PeekRow(key: KeyCatalog.cap(String(k[k.index(after: plus)...])), name: v.map { Self.name($0) } ?? "")
        }
    }

    /// Bindings of chords that start with this key, by their second key.
    func chord(_ first: String) -> [PeekRow] {
        refresh()
        return bound.compactMap { k, v in
            let parts = k.split(separator: " ").map(String.init)
            guard parts.count == 2, parts[0] == first else { return nil }
            return PeekRow(key: Self.cap(parts[1]), name: v.map { Self.name($0) } ?? "")
        }
    }

    /// The defaults with the person's over them; an empty name takes a key
    /// away.
    static func merge(_ defaults: [PeekRow], _ mine: [PeekRow]) -> [PeekRow] {
        var rows = defaults
        for m in mine {
            rows.removeAll { $0.key == m.key }
            if !m.name.isEmpty { rows.append(m) }
        }
        return rows
    }

    private static func words(_ m: NSEvent.ModifierFlags) -> String {
        var w: [String] = []
        if m.contains(.control) { w.append("ctrl") }
        if m.contains(.option) { w.append("meta") }
        if m.contains(.shift) { w.append("shift") }
        if m.contains(.command) { w.append("cmd") }
        return w.joined(separator: "+")
    }

    /// "ctrl+s" as a cap: "⌃S".
    private static func cap(_ k: String) -> String {
        var marks = ""
        var parts = k.split(separator: "+").map(String.init)
        let key = parts.popLast() ?? k
        for p in parts {
            switch p {
            case "ctrl", "control": marks += "⌃"
            case "alt", "opt", "option", "meta": marks += "⌥"
            case "shift": marks += "⇧"
            case "cmd", "command", "super", "win": marks += "⌘"
            default: break
            }
        }
        let names = ["enter": "Return", "return": "Return", "tab": "Tab", "escape": "Esc", "esc": "Esc", "space": "Space",
                     "up": "↑", "down": "↓", "left": "←", "right": "→"]
        return marks + (names[key] ?? key.uppercased())
    }

    /// An action's name in plain words: the part after its colon, split at
    /// its capitals.
    private static func name(_ action: String) -> String {
        let a = action.split(separator: ":").last.map(String.init) ?? action
        var out = ""
        for c in a {
            if c.isUppercase { out += " " + c.lowercased() } else { out.append(c) }
        }
        return out.prefix(1).uppercased() + out.dropFirst()
    }
}

/// The pop-up, alert-like: across the window's middle in its upper third,
/// a frosted panel whose edges blur into the window. It takes no clicks.
@MainActor
struct KeyPeekView: View {
    let peek: KeyPeek
    @Environment(\.accessibilityReduceTransparency) private var solid
    @Environment(\.accessibilityReduceMotion) private var still
    @Environment(\.colorSchemeContrast) private var contrast

    var body: some View {
        GeometryReader { geo in
            ZStack {
                if let s = peek.sheet {
                    panel(s, height: geo.size.height)
                        .transition(still ? .opacity : .opacity.combined(with: .scale(scale: 0.97)))
                        .padding(.top, geo.size.height * 0.16)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                }
            }
            .animation(.easeOut(duration: 0.14), value: peek.sheet)
        }
        .allowsHitTesting(false)
    }

    private func panel(_ s: PeekSheet, height: CGFloat) -> some View {
        let shape = RoundedRectangle(cornerRadius: 16, style: .continuous)
        let many = s.groups.reduce(0) { $0 + $1.rows.count } > 9
        return VStack(spacing: 10) {
            // The keys so far, large, where an alert has its icon.
            HStack(spacing: 6) {
                ForEach(Array(s.keys.enumerated()), id: \.offset) { i, k in
                    if i > 0 { Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Theme.muted) }
                    PeekCap(text: k, large: true)
                }
            }
            VStack(spacing: 2) {
                Text(s.title).font(Theme.font(13, .bold)).foregroundStyle(Theme.ink)
                if let p = s.place { Text(p).font(Theme.font(11)).foregroundStyle(Theme.muted) }
            }
            .multilineTextAlignment(.center)
            VStack(alignment: .leading, spacing: 10) {
                ForEach(s.groups) { g in
                    VStack(alignment: .leading, spacing: 5) {
                        if s.groups.count > 1 {
                            Text(g.head).font(Theme.font(10.5, .semibold)).foregroundStyle(Theme.muted)
                        }
                        // At most as many as fit a short window; the rest
                        // are in the menus.
                        let most = height < 640 ? 12 : 24
                        LazyVGrid(columns: Array(repeating: GridItem(.flexible(minimum: 150), spacing: 14, alignment: .leading), count: many ? 2 : 1),
                                  alignment: .leading, spacing: 5) {
                            ForEach(g.rows.prefix(most)) { r in
                                HStack(spacing: 8) {
                                    PeekCap(text: r.key, large: false)
                                    Text(r.name).font(Theme.font(12)).foregroundStyle(Theme.ink).lineLimit(1)
                                    if r.opens {
                                        Image(systemName: "ellipsis").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.muted)
                                    }
                                }
                            }
                        }
                        if g.rows.count > most {
                            Text("And \(g.rows.count - most) more, in the menus").font(Theme.font(10.5)).foregroundStyle(Theme.muted)
                        }
                    }
                }
            }
            Text(s.footer).font(Theme.font(10.5)).foregroundStyle(Theme.muted)
        }
        .padding(.horizontal, 22)
        .padding(.vertical, 18)
        .frame(width: many ? 540 : 300)
        .background {
            if solid {
                shape.fill(Theme.field)
            } else {
                // Frosted, in the palette's own field colour, with no line
                // round it: its ground runs on past its edge and blurs into
                // the window, so the edge is soft all round.
                ZStack {
                    shape.fill(Theme.field.opacity(0.6))
                        .padding(-10)
                        .blur(radius: 14)
                    shape.fill(.regularMaterial)
                        .overlay(shape.fill(Theme.field.opacity(0.6)))
                        .mask { shape.inset(by: 2).blur(radius: 3) }
                }
                .shadow(color: Theme.shadow, radius: 24, y: 8)
            }
        }
        .overlay { if solid || contrast == .increased { shape.strokeBorder(Theme.edge, lineWidth: 1) } }
        .accessibilityElement(children: .combine)
    }
}

/// A key as its cap: "fn X", "⌃S", "Tab".
private struct PeekCap: View {
    let text: String
    let large: Bool

    var body: some View {
        Text(text)
            .font(.system(size: large ? 15 : 11.5, weight: .semibold, design: .rounded))
            .foregroundStyle(Theme.ink)
            .padding(.horizontal, large ? 10 : 6)
            .frame(minWidth: large ? 44 : 30, minHeight: large ? 32 : 20)
            .background(RoundedRectangle(cornerRadius: large ? 8 : 5, style: .continuous).fill(Theme.panel))
            .overlay(RoundedRectangle(cornerRadius: large ? 8 : 5, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
    }
}
