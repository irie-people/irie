import AppKit
import SwiftUI

// The panes beside a session's terminal, as a rolodex (docs/proposals/
// workspaces.md §4): the agent's terminal on the left, and on the right a
// wheel of cards, each a pane of the user's own: a shell in the session's
// folder, a browser, or a plugin's page such as the market map.
//
// ⌥Tab takes the keyboard from the terminal to the first card, then on to
// the next, left to right, turning the wheel to each, and back to the
// terminal after the last; ⌥⇧Tab goes the other way. The wheel turns the
// short way round, so after the last card the first comes up next, as on a
// real card wheel. The cards' index tabs stand left to right, the front
// card's in front of the others, and the cards behind stand higher the
// further back they are. A turn flips the front card over the rail toward
// you and the next one comes up behind it; a turn back brings the card
// before up over the rail. A scroll over the tabs or the rail turns the
// wheel a card a notch, with a tick on the trackpad, and the knob at the
// rail's end turns with it, and turns the wheel when dragged or clicked.
//
// Each session has its own cards, kept across launches. Whether they show,
// and how wide, is one choice for every session, so moving between sessions
// never resizes the agents, whose windows all take the slot's size (DESIGN
// §2.3); opening, closing or a release of the divider resizes them once, as
// the right sidebar does.

// MARK: Model

/// What a pane shows.
enum PaneKind: String, Codable, CaseIterable, Identifiable {
    case terminal, browser, markets

    var id: String { rawValue }

    var title: String {
        switch self {
        case .terminal: return "Terminal"
        case .browser: return "Browser"
        case .markets: return "Markets"
        }
    }

    var symbol: String {
        switch self {
        case .terminal: return "terminal"
        case .browser: return "globe"
        case .markets: return "chart.bar.doc.horizontal"
        }
    }

    /// The letter that picks it on a blank card.
    var letter: Character {
        switch self {
        case .terminal: return "t"
        case .browser: return "b"
        case .markets: return "m"
        }
    }

    /// What it is, in a line, on a blank card.
    var about: String {
        switch self {
        case .terminal: return "Your own shell in this session’s folder"
        case .browser: return "A page: your app, docs, anything"
        case .markets: return "Your market map"
        }
    }

    /// The plugin it comes with, which must be on for it to be offered.
    var plugin: String? { self == .markets ? "markets" : nil }

    /// The kinds on offer with these plugins: a plugin's only while it is on.
    static func offered(_ plugins: [PluginInfo], all: Bool = false) -> [PaneKind] {
        allCases.filter { k in k.plugin.map { p in all || plugins.contains { $0.name == p && $0.on } } ?? true }
    }
}

/// One card of the wheel.
struct DeckPane: Codable, Equatable, Identifiable {
    /// Its id, which also names its shell on the dock server
    /// (internal/core/pane.go).
    var id: String
    var kind: PaneKind
    /// Its colour: one of the agent colours, far round the colour wheel
    /// from the card before it.
    var slot: Int
    /// A browser's page, and its title.
    var url: String?
    var title: String?

    /// What its index tab says.
    var label: String {
        if let t = title?.trimmingCharacters(in: .whitespacesAndNewlines), !t.isEmpty { return t }
        if kind == .browser, let host = url.flatMap(URL.init(string:))?.host { return host }
        return kind.title
    }

    static func newID() -> String {
        let chars = Array("abcdefghijklmnopqrstuvwxyz0123456789")
        return String((0..<10).map { _ in chars.randomElement() ?? "a" })
    }
}

/// A session's cards, left to right, and the one in front.
struct Deck: Codable, Equatable {
    var panes: [DeckPane] = []
    var front: String?

    /// The card in front: the one named, or else the first.
    var frontPane: DeckPane? { panes.first { $0.id == front } ?? panes.first }
    var frontIndex: Int? { frontPane.flatMap { f in panes.firstIndex { $0.id == f.id } } }

    /// How far behind the front card a card stands: 0 for the front one.
    func depth(of i: Int) -> Int {
        guard let f = frontIndex, !panes.isEmpty else { return 0 }
        return (i - f + panes.count) % panes.count
    }
}

/// One notch of the wheel on show: the card that leaves the front, and the
/// one that comes to it.
struct Flip: Equatable, Identifiable {
    var id: Int
    /// The session whose wheel turns: another's never shows on this one.
    var session: String
    /// The card that leaves the front; nil for a blank card.
    var from: DeckPane?
    var to: DeckPane?
    var forward: Bool
    var start: Date
    var duration: Double
    /// Where the two cards' tabs stand, from the card file's left.
    var fromTab: CGFloat
    var toTab: CGFloat

    func progress(at date: Date) -> Double { min(max(date.timeIntervalSince(start) / duration, 0), 1) }

    /// When the live pane changes under the flap: early in a turn forward,
    /// once the falling card covers the front; late in a turn back, as the
    /// rising card lands.
    var swap: Double { forward ? 0.14 : 0.8 }

    /// The flap's tilt, in degrees about the rail, 0 standing up: a card
    /// turned forward falls toward you, a card turned back rises from you.
    func tilt(_ p: Double) -> Double {
        if forward {
            let t = max(0, (p - 0.06) / 0.94)
            return -96 * t * t
        }
        let t = min(1, p / 0.8)
        // It lands with a small bounce.
        let c1 = 1.25, c3 = c1 + 1, u = t - 1
        return -96 * (1 - (1 + c3 * u * u * u + c1 * u * u))
    }

    /// How much of the flap shows: a turn forward fades the falling card in
    /// over the live one first; a turn back fades the landed card into the
    /// live one under it.
    func cover(_ p: Double) -> Double {
        if forward { return min(1, p / swap) }
        return p < swap ? 1 : max(0, 1 - (p - swap) / (1 - swap))
    }
}

/// The rolodex's state: whether it shows and how wide, each session's cards,
/// where the keyboard is, and the turns on show.
@MainActor
@Observable
final class RolodexModel {
    /// The keyboard's place on a blank card.
    static let blank = "+blank"
    /// How long one notch takes, alone.
    static let flipTime = 0.4
    /// How it slides in from the window's edge, and out.
    static let slide = Animation.spring(response: 0.42, dampingFraction: 0.82)
    /// How the cards behind settle after a turn, and the knob with them.
    static let settle = Animation.spring(response: 0.34, dampingFraction: 0.7)
    static let startShare = 0.42

    private static let openKey = "rolodexOpen"
    private static let shareKey = "rolodexShare"
    private static let decksKey = "rolodexDecks"
    private static let recentKey = "rolodexRecent"

    /// Whether the cards show beside the terminal, for every session, and
    /// their share of the room beside the right sidebar.
    var open = UserDefaults.standard.bool(forKey: RolodexModel.openKey) {
        didSet { if !fixture, open != oldValue { UserDefaults.standard.set(open, forKey: Self.openKey) } }
    }
    var share = RolodexModel.savedShare() {
        didSet { if !fixture, share != oldValue { UserDefaults.standard.set(share, forKey: Self.shareKey) } }
    }
    /// Sliding in or out, over the terminal's edge: the terminal keeps no
    /// room for it meanwhile, so its width changes once.
    var sliding = false
    @ObservationIgnored private var slides = 0
    /// Each session's cards, by session.
    private(set) var decks: [String: Deck] = RolodexModel.savedDecks()
    /// Where the keyboard is: a card's id, blank, or nil for the terminal,
    /// the conversation or anything else.
    var keys: String?
    /// The turns on show, the oldest first, and the card whose live pane
    /// shows meanwhile.
    private(set) var flips: [Flip] = []
    private(set) var swapped: String?
    @ObservationIgnored private var flipCount = 0
    /// The knob's turn, in degrees.
    var knob = 0.0
    /// Bumped to give a card the keyboard, by card.
    private(set) var focus: [String: Int] = [:]
    /// The core runs panes' shells (op pane).
    var canShell = false
    /// The width a drag of the divider asks for, while it lasts.
    var guide: Double?
    /// The pages typed lately, for a new browser card's start.
    private(set) var recent: [String] = UserDefaults.standard.stringArray(forKey: RolodexModel.recentKey) ?? []
    /// `--snapshot`'s: nothing saved, nothing moves, and a turn drawn still
    /// at this point.
    @ObservationIgnored var fixture = false
    var still: Double?
    /// The blank card has the keyboard.
    @ObservationIgnored var blankHasKeys = false
    /// The live pane's place in the window, and the bands a scroll turns the
    /// wheel over: the tabs and the rail.
    @ObservationIgnored weak var frontProbe: NSView?
    @ObservationIgnored let railProbes = NSHashTable<NSView>.weakObjects()
    @ObservationIgnored private var spin: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var monitors: [Any] = []
    @ObservationIgnored private var scrolled: CGFloat = 0
    @ObservationIgnored private var lastNotch = Date.distantPast

    /// Whether the terminal makes room and sits beside it.
    var docked: Bool { open && !sliding }

    var turning: Bool { !flips.isEmpty || spin != nil }

    func deck(_ session: String) -> Deck { decks[session] ?? Deck() }

    /// The card whose live pane shows: the one under the flap during a turn,
    /// else the front one.
    func visible(in deck: Deck) -> String? {
        if !flips.isEmpty, let s = swapped, deck.panes.contains(where: { $0.id == s }) { return s }
        return deck.frontPane?.id
    }

    /// A card's keyboard token.
    func token(_ id: String) -> Int { focus[id] ?? 0 }

    /// Gives a card the keyboard, once its live pane shows.
    func bump(_ id: String) {
        focus[id, default: 0] += 1
        if id != Self.blank, !fixture { settle(id) }
    }

    /// A card that took no keyboard, such as a map still loading or a shell
    /// not yet up, keeps its place all the same: the window holds the
    /// keyboard itself, so typing reaches no agent unseen and the next ⌥Tab
    /// goes on from the card.
    private func settle(_ id: String) {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
            MainActor.assumeIsolated {
                guard let self, self.keys == id, !self.blankHasKeys, let w = WindowPanes.window,
                      w.firstResponder is NSView, self.place(in: w, deck: Deck()) == nil else { return }
                w.makeFirstResponder(nil)
            }
        }
    }

    /// Stops a spin under way, as another turn or a key starts.
    func stopSpin() {
        spin?.cancel()
        spin = nil
    }

    // MARK: Showing

    /// Slides the wheel in from the window's edge, or out. With Reduce
    /// Motion on, in one step.
    func setOpen(_ on: Bool) {
        guard on != open else { return }
        slides += 1
        let n = slides
        if fixture || NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
            quietly { sliding = false; open = on }
            return
        }
        quietly { sliding = true }
        withAnimation(Self.slide) { open = on } completion: { [weak self] in
            guard let self, self.slides == n else { return }
            self.quietly { self.sliding = false }
        }
    }

    func setShare(_ s: Double) {
        quietly { share = min(max(s, 0.2), 0.75) }
    }

    /// The wheel's width in a room as wide as room, leaving the terminal at
    /// least keep, down to its own narrowest.
    static func width(share: Double, in room: CGFloat, keep: CGFloat) -> CGFloat {
        let want = room * CGFloat(share)
        return max(min(want, room - keep), min(260, room * 0.5)).rounded()
    }

    private func quietly(_ work: () -> Void) {
        var t = Transaction(animation: nil)
        t.disablesAnimations = true
        withTransaction(t, work)
    }

    // MARK: Cards

    /// A new card at the right end of a session's wheel, in a colour far
    /// round from the card before it and from the session's own.
    @discardableResult
    func add(_ kind: PaneKind, to session: String, avoiding own: Int?, url: String? = nil) -> DeckPane {
        var d = deck(session)
        var slot = d.panes.last.map { ($0.slot + 3) % Theme.agentSlots } ?? ((own ?? 0) + 3) % Theme.agentSlots
        if slot == own { slot = (slot + 1) % Theme.agentSlots }
        let p = DeckPane(id: DeckPane.newID(), kind: kind, slot: slot, url: url)
        d.panes.append(p)
        if d.panes.count == 1 { d.front = p.id }
        withAnimation(fixture ? nil : Self.settle) { decks[session] = d }
        saveSoon()
        return p
    }

    /// Takes a card off a session's wheel.
    func remove(_ pane: String, from session: String) {
        var d = deck(session)
        guard let i = d.panes.firstIndex(where: { $0.id == pane }) else { return }
        let wasFront = d.frontPane?.id == pane
        d.panes.remove(at: i)
        if wasFront { d.front = d.panes.isEmpty ? nil : d.panes[i % d.panes.count].id }
        withAnimation(fixture ? nil : Self.settle) { decks[session] = d }
        focus[pane] = nil
        if keys == pane { keys = nil }
        saveSoon()
    }

    /// Moves a card one place left (-1) or right (+1).
    func move(_ pane: String, in session: String, by delta: Int) {
        var d = deck(session)
        guard let i = d.panes.firstIndex(where: { $0.id == pane }), d.panes.indices.contains(i + delta) else { return }
        d.panes.swapAt(i, i + delta)
        withAnimation(fixture ? nil : Self.settle) { decks[session] = d }
        saveSoon()
    }

    /// Keeps a browser card's page and title, for its tab and the next launch.
    func update(_ pane: String, in session: String, url: String?, title: String?) {
        guard var d = decks[session], let i = d.panes.firstIndex(where: { $0.id == pane }) else { return }
        guard d.panes[i].url != url || d.panes[i].title != title else { return }
        d.panes[i].url = url
        d.panes[i].title = title
        decks[session] = d
        saveSoon()
    }

    /// Notes a page typed into a browser card, the newest first.
    func noteRecent(_ url: String) {
        var r = recent.filter { $0 != url }
        r.insert(url, at: 0)
        recent = Array(r.prefix(6))
        if !fixture { UserDefaults.standard.set(recent, forKey: Self.recentKey) }
    }

    /// Drops the wheels of sessions that are gone, once there are many.
    func prune(keeping live: Set<String>) {
        guard decks.count > 120 else { return }
        decks = decks.filter { live.contains($0.key) }
        saveSoon()
    }

    // MARK: Turning

    /// Turns a session's wheel to a card, a notch at a time, the short way
    /// round (prefer settles a tie), each notch quicker the further it goes.
    /// With keys the card takes the keyboard as it comes to the front.
    func turn(to target: String, in session: String, prefer forward: Bool = true, keys giveKeys: Bool, layout: @escaping (Int) -> CGFloat) {
        let d = deck(session)
        guard let t = d.panes.firstIndex(where: { $0.id == target }) else { return }
        stopSpin()
        if giveKeys { keys = target }
        guard let f = d.frontIndex, f != t else {
            if giveKeys { bump(target) }
            return
        }
        let n = d.panes.count
        let ahead = (t - f + n) % n, back = (f - t + n) % n
        let fwd = ahead == back ? forward : ahead < back
        let steps = fwd ? ahead : back
        let each = steps == 1 ? Self.flipTime : max(0.16, Self.flipTime / Double(steps).squareRoot())
        // The first notch at once, on the key; the rest of a spin after it.
        notch(in: session, forward: fwd, duration: each, keys: giveKeys && steps == 1 ? target : nil, layout: layout)
        guard steps > 1 else { return }
        spin = Task { [weak self] in
            for k in 1..<steps {
                try? await Task.sleep(nanoseconds: UInt64(each * 0.55 * 1e9))
                guard let self, !Task.isCancelled else { return }
                self.notch(in: session, forward: fwd, duration: each, keys: giveKeys && k == steps - 1 ? target : nil, layout: layout)
            }
            self?.spin = nil
        }
    }

    /// One notch: the next card comes to the front, or the one before.
    func notch(in session: String, forward: Bool, duration: Double = RolodexModel.flipTime, keys target: String?, layout: (Int) -> CGFloat) {
        var d = deck(session)
        guard let f = d.frontIndex, d.panes.count > 1 else { return }
        let n = d.panes.count
        let ti = (f + (forward ? 1 : n - 1)) % n
        let from = d.panes[f], to = d.panes[ti]
        d.front = to.id
        let calm = fixture || NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
        withAnimation(calm ? nil : Self.settle) {
            decks[session] = d
            knob += (forward ? 1 : -1) * 360 / Double(max(n, 8))
        }
        saveSoon()
        play(Flip(id: 0, session: session, from: from, to: to, forward: forward, start: Date(), duration: duration,
                  fromTab: layout(f), toTab: layout(ti)), calm: calm, keys: target)
    }

    /// Shows a turn: the flap falls or rises, the live pane changes under it
    /// at its moment, and a card owed the keyboard takes it then.
    func play(_ flip: Flip, calm: Bool, keys target: String?) {
        flipCount += 1
        let id = flipCount
        if calm {
            flips = []
            swapped = nil
            if let target, keys == target { bump(target) }
            return
        }
        var f = flip
        f.id = id
        swapped = flip.from?.id ?? flip.to?.id
        flips = (flips + [f]).suffix(4)
        lastNotch = Date()
        Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(f.swap * f.duration * 1e9))
            guard let self else { return }
            if self.flipCount == id { self.swapped = f.to?.id }
            // Only while the keyboard is still meant for it: a key or a
            // click since may have sent it elsewhere.
            if let target, self.flipCount == id, self.keys == target { self.bump(target) }
            try? await Task.sleep(nanoseconds: UInt64((1 - f.swap) * f.duration * 1e9 + 30_000_000))
            self.flips.removeAll { $0.id == id }
            if self.flips.isEmpty { self.swapped = nil }
        }
    }

    // MARK: The keyboard

    /// Where the keyboard is now: on the front card (its id, or blank), or
    /// nil for the terminal or anywhere else. While the wheel turns, where
    /// the last key sent it.
    func keysNow(in window: NSWindow?, deck: Deck) -> String? {
        if turning { return keys }
        return place(in: window, deck: deck)
    }

    /// Where the window's keyboard is, whatever the wheel is doing: on the
    /// front card, or nil for anywhere else; where the last key sent it while
    /// the window itself holds it.
    private func place(in window: NSWindow?, deck: Deck) -> String? {
        if blankHasKeys { return Self.blank }
        guard let window, let r = window.firstResponder as? NSView else { return keys }
        guard open, let probe = frontProbe, probe.window === window else { return nil }
        let a = r.convert(r.bounds, to: nil), b = probe.convert(probe.bounds, to: nil)
        if b.insetBy(dx: -4, dy: -40).contains(CGPoint(x: a.midX, y: a.midY)), a.width <= b.width + 8, a.height <= b.height + 60 {
            return deck.frontPane?.id ?? Self.blank
        }
        return nil
    }

    /// Watches clicks, which move the keyboard, and scrolls over the tabs
    /// and the rail, which turn the wheel.
    func install(_ store: Store) {
        guard monitors.isEmpty else { return }
        if let m = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel, handler: { [weak self, weak store] event in
            guard let self, let store else { return event }
            return MainActor.assumeIsolated { self.scroll(event, store: store) ? nil : event }
        }) { monitors.append(m) }
        if let m = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown], handler: { [weak self, weak store] event in
            guard let self, let store else { return event }
            let window = event.window
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    // A click counts even mid-turn: it sends the keyboard
                    // where it lands, and a turn under way then keeps it.
                    guard let s = store.paneSession, window != nil, window === WindowPanes.window else { return }
                    let k = self.place(in: window, deck: self.deck(s.id))
                    if k != self.keys { self.keys = k }
                }
            }
            return event
        }) { monitors.append(m) }
    }

    /// A scroll over the tabs or the rail turns the wheel a notch per step,
    /// with a tick on the trackpad while fingers are on it.
    private func scroll(_ event: NSEvent, store: Store) -> Bool {
        guard open, overRail(event), let s = store.paneSession else { return false }
        let d = deck(s.id)
        guard d.panes.count > 1 else { return true }
        if event.phase == .began || event.phase == .mayBegin { scrolled = 0 }
        let dy = event.scrollingDeltaY, dx = event.scrollingDeltaX
        scrolled += abs(dy) >= abs(dx) ? dy : dx
        let unit: CGFloat = event.hasPreciseScrollingDeltas ? 30 : 1
        scrolled = min(max(scrolled, -unit * 2.5), unit * 2.5)
        guard abs(scrolled) >= unit, Date().timeIntervalSince(lastNotch) > 0.07 else { return true }
        let forward = scrolled < 0
        scrolled += forward ? unit : -unit
        let follow = keys != nil && keys != Self.blank
        let next = d.panes[((d.frontIndex ?? 0) + (forward ? 1 : d.panes.count - 1)) % d.panes.count].id
        stopSpin()
        if follow { keys = next }
        notch(in: s.id, forward: forward, duration: 0.26, keys: follow ? next : nil, layout: store.tabPlace(for: s.id))
        if event.phase != [] { NSHapticFeedbackManager.defaultPerformer.perform(.alignment, performanceTime: .now) }
        return true
    }

    private func overRail(_ event: NSEvent) -> Bool {
        railProbes.allObjects.contains { v in
            guard let w = v.window, w === event.window else { return false }
            return v.bounds.contains(v.convert(event.locationInWindow, from: nil))
        }
    }

    // MARK: Saving

    private func saveSoon() {
        guard !fixture else { return }
        saving?.cancel()
        saving = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 400_000_000)
            guard let self, !Task.isCancelled else { return }
            let kept = self.decks.filter { !$0.value.panes.isEmpty }
            if let data = try? JSONEncoder().encode(kept) { UserDefaults.standard.set(data, forKey: Self.decksKey) }
        }
    }

    private static func savedDecks() -> [String: Deck] {
        guard let data = UserDefaults.standard.data(forKey: decksKey),
              let d = try? JSONDecoder().decode([String: Deck].self, from: data) else { return [:] }
        return d
    }

    private static func savedShare() -> Double {
        let v = UserDefaults.standard.double(forKey: shareKey)
        return v >= 0.2 && v <= 0.75 ? v : startShare
    }

    /// `--snapshot`'s wheel: the fixture's session with a card of each kind,
    /// that kind in front, or a blank card for none; or, closed, none.
    func loadFixture(session: String, front: PaneKind?, open on: Bool = true) {
        fixture = true
        open = on
        sliding = false
        flips = []
        swapped = nil
        still = nil
        keys = nil
        guard on, let front else {
            decks = [:]
            return
        }
        let panes = [
            DeckPane(id: "fixtureterm", kind: .terminal, slot: 1),
            DeckPane(id: "fixtureweb", kind: .browser, slot: 4, url: "http://localhost:5173/", title: "Acme · Sign in"),
            DeckPane(id: "fixturemap", kind: .markets, slot: 2),
        ]
        decks = [session: Deck(panes: panes, front: panes.first { $0.kind == front }?.id)]
        keys = panes.first { $0.kind == front }?.id
    }

    /// `--snapshot`'s turn, drawn still at a point of it: forward from the
    /// front card to the next, or back to the one before.
    func freezeFixtureTurn(session: String, forward: Bool, at progress: Double, size: CGSize) {
        let d = deck(session)
        guard let f = d.frontIndex, d.panes.count > 1 else { return }
        let n = d.panes.count
        let t = (f + (forward ? 1 : n - 1)) % n
        let lay = RolodexLayout(size: size, count: n)
        var after = d
        after.front = d.panes[t].id
        decks[session] = after
        flips = [Flip(id: 1, session: session, from: d.panes[f], to: d.panes[t], forward: forward, start: .distantPast, duration: 1,
                      fromTab: lay.tabX(f), toTab: lay.tabX(t))]
        let flip = flips[0]
        swapped = progress < flip.swap ? d.panes[f].id : d.panes[t].id
        still = progress
    }
}

// MARK: Store

/// The pane op's arguments and answer (internal/ipc/pane.go).
struct PaneArgs: Encodable {
    var session: String
    var pane: String
    var start: Bool?
    var restart: Bool?
    var end: Bool?
}

struct PaneShell: Decodable, Equatable {
    var state: String
    var exit: Int?
    var tmux: String?
    var socket: String?
    var target: String?
    var cwd: String?
}

extension Store {
    /// The session whose cards show: the selected one of Irie's own.
    var paneSession: Session? { outsideSelection == nil ? selectedRow : nil }

    /// The kinds of card on offer: the plugins' only while they are on.
    var paneKinds: [PaneKind] { PaneKind.offered(plugins, all: isFixture) }

    /// Where the tabs of a session's cards stand, by place, in the wheel's
    /// width as last drawn.
    func tabPlace(for session: String) -> (Int) -> CGFloat {
        let n = rolodex.deck(session).panes.count
        let w = RolodexLayout.lastSize
        return { i in RolodexLayout(size: w, count: n).tabX(i) }
    }

    /// View › Show Panes (⌥⌘P), and the toolbar's button. Closing hands the
    /// keyboard back to the terminal.
    func togglePanes() {
        let opening = !rolodex.open
        rolodex.setOpen(opening)
        if !opening, rolodex.keys != nil { giveMainKeys() }
    }

    /// Gives the keyboard back to the terminal, or to the message box while
    /// the conversation shows.
    func giveMainKeys() {
        rolodex.keys = nil
        if showTerminal { focusTerminal += 1 } else { focusMessage += 1 }
    }

    /// ⌥Tab (+1) and ⌥⇧Tab (-1): the keyboard goes from the terminal to the
    /// first card, then to each card in turn, left to right, and back to the
    /// terminal after the last. A closed wheel slides in first.
    func cyclePanes(_ delta: Int, in window: NSWindow?) {
        guard let s = paneSession, window == nil || window === WindowPanes.window else { return }
        let r = rolodex
        let d = r.deck(s.id)
        let at = r.open ? r.keysNow(in: window, deck: d) : nil
        if !r.open { r.setOpen(true) }
        if d.panes.isEmpty {
            if at == RolodexModel.blank {
                giveMainKeys()
            } else {
                r.keys = RolodexModel.blank
                r.bump(RolodexModel.blank)
            }
            return
        }
        let stops: [String?] = [nil] + d.panes.map(\.id)
        let i = at == RolodexModel.blank ? 0 : (stops.firstIndex(of: at) ?? 0)
        guard let next = stops[(i + delta + stops.count) % stops.count] else {
            giveMainKeys()
            return
        }
        r.turn(to: next, in: s.id, prefer: delta > 0, keys: true, layout: tabPlace(for: s.id))
    }

    /// A click on a card's tab: the wheel turns to it, and it takes the
    /// keyboard.
    func showPane(_ id: String) {
        guard let s = paneSession else { return }
        rolodex.turn(to: id, in: s.id, keys: true, layout: tabPlace(for: s.id))
    }

    /// Adds a card at the wheel's right end and turns to it. From a blank
    /// card, the blank card flips away.
    func newPane(_ kind: PaneKind) {
        guard let s = paneSession else { return }
        let r = rolodex
        if !r.open { r.setOpen(true) }
        let was = r.deck(s.id)
        let own = s.hue.map { ($0 - 1) % Theme.agentSlots }
        r.prune(keeping: Set(sessions.map(\.id) + closed.map(\.id)))
        let p = r.add(kind, to: s.id, avoiding: own)
        if was.panes.isEmpty {
            r.keys = p.id
            let x = tabPlace(for: s.id)(0)
            // The blank card falls from where it stood, already over the new
            // card's live pane.
            r.play(Flip(id: 0, session: s.id, from: nil, to: p, forward: true, start: Date().addingTimeInterval(-0.06),
                        duration: RolodexModel.flipTime, fromTab: x, toTab: x),
                   calm: r.fixture || NSWorkspace.shared.accessibilityDisplayShouldReduceMotion, keys: p.id)
        } else {
            r.turn(to: p.id, in: s.id, keys: true, layout: tabPlace(for: s.id))
        }
    }

    /// Closes a card: its shell ends, and when it was in front it flips away
    /// and the next card comes up.
    func closePane(_ id: String) {
        guard let s = paneSession else { return }
        let r = rolodex
        let d = r.deck(s.id)
        guard let i = d.panes.firstIndex(where: { $0.id == id }) else { return }
        let p = d.panes[i]
        let hadKeys = r.keys == id
        if p.kind == .terminal, r.canShell, !isFixture {
            Task { _ = try? await client.call("pane", PaneArgs(session: s.id, pane: id, end: true), timeout: 10, as: PaneShell.self) }
        }
        if p.kind == .browser { WebPool.shared.drop(id) }
        let place = tabPlace(for: s.id)
        let wasFront = d.frontPane?.id == id
        r.remove(id, from: s.id)
        guard wasFront else { return }
        let after = r.deck(s.id)
        let next = after.frontPane
        if hadKeys, let n = next { r.keys = n.id }
        let calm = r.fixture || NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
        // The closed card falls from where it stood, already over the live pane.
        let nextTab = next.flatMap { n in after.panes.firstIndex { $0.id == n.id } }
            .map { RolodexLayout(size: RolodexLayout.lastSize, count: after.panes.count).tabX($0) } ?? place(0)
        let flip = Flip(id: 0, session: s.id, from: p, to: next, forward: true, start: Date().addingTimeInterval(-0.06),
                        duration: RolodexModel.flipTime, fromTab: place(i), toTab: nextTab)
        r.play(flip, calm: calm, keys: hadKeys ? next?.id : nil)
        // With no card left, the keyboard goes back to the terminal.
        if hadKeys, next == nil { giveMainKeys() }
    }

    /// Starts, restarts or ends a card's shell, or asks how it stands.
    func paneShell(_ args: PaneArgs) async throws -> PaneShell {
        try await client.call("pane", args, timeout: 15, as: PaneShell.self)
    }
}

// MARK: Layout

/// Where the parts of the wheel go in its frame.
struct RolodexLayout {
    let size: CGSize
    /// How many cards; 0 draws a blank one.
    let count: Int

    static let tabHeight: CGFloat = 25
    /// How much higher each card behind stands than the one before it.
    static let step: CGFloat = 6
    /// The depths drawn apart; deeper cards stand with the last.
    static let deepest = 2
    /// How much narrower each card behind is, a side.
    static let inset: CGFloat = 7
    /// The rail's band at the foot.
    static let rail: CGFloat = 22
    /// Room right of the cards, for the knob and the + button.
    static let knobRoom: CGFloat = 38
    static let radius: CGFloat = 13
    static let slope: CGFloat = 8

    /// The size the wheel was last drawn at, for the tabs' places as a key
    /// turns it.
    @MainActor static var lastSize = CGSize(width: 480, height: 600)

    var left: CGFloat { 14 }
    var cardsWidth: CGFloat { max(100, size.width - left - Self.knobRoom) }
    var railY: CGFloat { size.height - Self.rail / 2 }

    /// The front card, its tab included.
    var front: CGRect {
        let top = Self.step * CGFloat(Self.deepest)
        return CGRect(x: left, y: top, width: cardsWidth, height: max(120, railY + 5 - top))
    }

    /// The front card's body, under its tab.
    var body: CGRect {
        CGRect(x: front.minX, y: front.minY + Self.tabHeight, width: front.width, height: front.height - Self.tabHeight)
    }

    /// Where the live pane goes, inside the body, clear of the rail's notch.
    var content: CGRect {
        CGRect(x: body.minX + 8, y: body.minY + 8, width: body.width - 16, height: max(60, body.height - 8 - 16))
    }

    /// A card at a depth behind the front one.
    func card(depth d: Int) -> CGRect {
        let k = CGFloat(min(d, Self.deepest))
        return CGRect(x: front.minX + k * Self.inset, y: front.minY - k * Self.step, width: front.width - 2 * k * Self.inset, height: front.height)
    }

    /// The tabs' width: as many fit side by side as there are cards, at most
    /// 156 points and at least 74.
    var tabWidth: CGFloat {
        let room = cardsWidth - 2 * (Self.radius + 4)
        return min(156, max(74, room / CGFloat(max(count, 3)) + 12))
    }

    /// Where the ith card's tab starts, from the wheel's left, left to right
    /// by place, overlapping when the cards are many.
    func tabX(_ i: Int) -> CGFloat {
        let start = left + Self.radius + 4
        guard count > 1 else { return start }
        let room = cardsWidth - 2 * (Self.radius + 4) - tabWidth
        let step = min(tabWidth - 4, room / CGFloat(count - 1))
        return start + CGFloat(i) * max(step, 0)
    }
}

// MARK: Shapes

/// An index card: a body with rounded corners and a tab on its top edge,
/// and the notch in its foot that the rail runs through.
struct IndexCard: Shape {
    /// The tab's left and width, from the card's left.
    var tabX: CGFloat
    var tabWidth: CGFloat
    var notch = true

    var animatableData: AnimatablePair<CGFloat, CGFloat> {
        get { AnimatablePair(tabX, tabWidth) }
        set { tabX = newValue.first; tabWidth = newValue.second }
    }

    func path(in rect: CGRect) -> Path {
        let r = RolodexLayout.radius, s = RolodexLayout.slope
        let top = rect.minY, y0 = rect.minY + RolodexLayout.tabHeight
        let x1 = min(max(rect.minX + tabX, rect.minX + r + 2), rect.maxX - r - 2 - 40)
        let x2 = min(x1 + tabWidth, rect.maxX - r - 2)
        var p = Path()
        p.move(to: CGPoint(x: rect.minX, y: y0 + r))
        p.addArc(tangent1End: CGPoint(x: rect.minX, y: y0), tangent2End: CGPoint(x: rect.minX + r, y: y0), radius: r)
        // The tab: a soft slope up, its top, and a slope down.
        p.addArc(tangent1End: CGPoint(x: x1, y: y0), tangent2End: CGPoint(x: x1 + s, y: top), radius: 5)
        p.addArc(tangent1End: CGPoint(x: x1 + s, y: top), tangent2End: CGPoint(x: x2 - s, y: top), radius: 7)
        p.addArc(tangent1End: CGPoint(x: x2 - s, y: top), tangent2End: CGPoint(x: x2, y: y0), radius: 7)
        p.addArc(tangent1End: CGPoint(x: x2, y: y0), tangent2End: CGPoint(x: rect.maxX, y: y0), radius: 5)
        p.addArc(tangent1End: CGPoint(x: rect.maxX, y: y0), tangent2End: CGPoint(x: rect.maxX, y: rect.maxY), radius: r)
        p.addArc(tangent1End: CGPoint(x: rect.maxX, y: rect.maxY), tangent2End: CGPoint(x: rect.minX, y: rect.maxY), radius: r)
        if notch {
            // The slot the rail runs through, in the middle of the foot.
            let cx = rect.midX, w: CGFloat = 17, d: CGFloat = 10
            p.addLine(to: CGPoint(x: cx + w + 4, y: rect.maxY))
            p.addArc(tangent1End: CGPoint(x: cx + w, y: rect.maxY), tangent2End: CGPoint(x: cx + w, y: rect.maxY - d), radius: 3)
            p.addArc(tangent1End: CGPoint(x: cx + w, y: rect.maxY - d), tangent2End: CGPoint(x: cx - w, y: rect.maxY - d), radius: 6)
            p.addArc(tangent1End: CGPoint(x: cx - w, y: rect.maxY - d), tangent2End: CGPoint(x: cx - w, y: rect.maxY), radius: 6)
            p.addArc(tangent1End: CGPoint(x: cx - w, y: rect.maxY), tangent2End: CGPoint(x: cx - w - 4, y: rect.maxY), radius: 3)
        }
        p.addArc(tangent1End: CGPoint(x: rect.minX, y: rect.maxY), tangent2End: CGPoint(x: rect.minX, y: y0), radius: r)
        p.closeSubpath()
        return p
    }
}

/// The ruled lines of an index card: a coloured line under its head, then
/// faint ones down the card.
private struct Rules: View {
    let ink: Color
    /// How far down the coloured line under the card's head is.
    var head: CGFloat = 30

    var body: some View {
        Canvas { ctx, size in
            var line = Path()
            line.move(to: CGPoint(x: 0, y: head))
            line.addLine(to: CGPoint(x: size.width, y: head))
            ctx.stroke(line, with: .color(ink.opacity(0.45)), lineWidth: 1.2)
            var y: CGFloat = head + 24
            var lines = Path()
            while y < size.height - 10 {
                lines.move(to: CGPoint(x: 0, y: y))
                lines.addLine(to: CGPoint(x: size.width, y: y))
                y += 24
            }
            ctx.stroke(lines, with: .color(Theme.edge.opacity(0.8)), lineWidth: 0.7)
        }
        .allowsHitTesting(false)
    }
}

// MARK: The wheel

/// What the wheel needs of the session it shows, and no more: the session's
/// row changes at every new detail or token, which then leaves the wheel be.
struct PaneOwner: Equatable {
    let id: String
    /// The other machine it runs on, if any.
    let peer: String?
    /// Its colour, for the knob's dot.
    let ink: Color

    init(_ s: Session) {
        id = s.id
        peer = s.peerName
        ink = Look.edge(s)
    }
}

/// The rolodex beside the terminal, for the session on show.
@MainActor
struct RolodexView: View {
    @Environment(Store.self) private var store

    var body: some View {
        let r = store.rolodex
        let owner = store.paneSession.map(PaneOwner.init)
        RolodexWheel(owner: owner)
            .onAppear { r.install(store) }
            // A key that sends the keyboard to the terminal takes it off the cards.
            .onChange(of: store.focusTerminal) { if r.keys != nil, !r.turning { r.keys = nil } }
            .onChange(of: store.focusMessage) { if r.keys != nil, !r.turning { r.keys = nil } }
            // Another session's wheel stops a spin of this one's.
            .onChange(of: owner?.id) { r.stopSpin() }
    }
}

/// The wheel: the cards behind, the front card with its live pane, the
/// flaps of turns on show, the rail and its knob, and the + that adds a card.
@MainActor
private struct RolodexWheel: View {
    let owner: PaneOwner?
    @Environment(Store.self) private var store
    @State private var hoveredTab: String?
    @State private var knobDrag: CGFloat?
    @State private var knobTurned = false

    var body: some View {
        let r = store.rolodex
        let deck = owner.map { r.deck($0.id) } ?? Deck()
        GeometryReader { geo in
            let lay = RolodexLayout(size: geo.size, count: deck.panes.count)
            ZStack(alignment: .topLeading) {
                rail(lay)
                // The cards behind, the furthest first, each with its tab.
                ForEach(Array(deck.panes.enumerated()).filter { deck.depth(of: $0.offset) > 0 }, id: \.element.id) { i, p in
                    backCard(p, place: i, depth: deck.depth(of: i), lay: lay)
                }
                frontCard(deck, lay: lay)
                live(deck, lay: lay)
                flaps(lay)
                plus(lay)
                // The bands a scroll turns the wheel over; a turn drawn still
                // has no window to scroll in.
                if r.still == nil {
                    Probe { r.railProbes.add($0) }
                        .frame(width: lay.size.width, height: lay.body.minY + 2)
                        .allowsHitTesting(false)
                    Probe { r.railProbes.add($0) }
                        .frame(width: lay.size.width, height: RolodexLayout.rail + 6)
                        .offset(y: lay.size.height - RolodexLayout.rail - 6)
                        .allowsHitTesting(false)
                }
            }
            .onAppear { RolodexLayout.lastSize = geo.size }
            .onChange(of: geo.size) { _, s in RolodexLayout.lastSize = s }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Panes, \(deck.panes.count) " + (deck.panes.count == 1 ? "card" : "cards"))
    }

    // MARK: Cards

    private func backCard(_ p: DeckPane, place i: Int, depth d: Int, lay: RolodexLayout) -> some View {
        let rect = lay.card(depth: d)
        let tabX = lay.tabX(i) - rect.minX
        let shape = IndexCard(tabX: tabX, tabWidth: lay.tabWidth, notch: false)
        let fade = d > RolodexLayout.deepest ? 0.75 : 1.0
        return ZStack(alignment: .topLeading) {
            shape.fill(Theme.agentWash(slot: p.slot))
                .overlay(shape.fill(Theme.panel.opacity(0.35)))
                .overlay(shape.stroke(Theme.edge, lineWidth: 1))
                .shadow(color: .black.opacity(0.08), radius: 3, y: -1)
                .allowsHitTesting(false)
            tabLabel(p, front: false, width: lay.tabWidth)
                .offset(x: tabX)
        }
        .frame(width: rect.width, height: rect.height)
        .offset(x: rect.minX, y: rect.minY)
        .opacity(fade)
        .zIndex(Double(-d))
    }

    @ViewBuilder
    private func frontCard(_ deck: Deck, lay: RolodexLayout) -> some View {
        let r = store.rolodex
        let p = deck.frontPane
        let i = deck.frontIndex ?? 0
        let tabX = lay.tabX(i) - lay.front.minX
        let shape = IndexCard(tabX: tabX, tabWidth: lay.tabWidth)
        let keys = r.keys != nil && r.keys == (p?.id ?? RolodexModel.blank)
        let ink = p.map { Theme.agent(slot: $0.slot) } ?? Theme.muted
        ZStack(alignment: .topLeading) {
            shape.fill(Theme.panel)
                .shadow(color: keys ? ink.opacity(0.35) : .black.opacity(0.16), radius: keys ? 12 : 8, y: 2)
                .allowsHitTesting(false)
            // The tab, in the card's colour.
            Rectangle()
                .fill(p.map { Theme.agentWash(slot: $0.slot) } ?? Theme.field)
                .frame(height: RolodexLayout.tabHeight + 1)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                .clipShape(shape)
                .allowsHitTesting(false)
            shape.stroke(keys ? ink : Theme.edge, lineWidth: keys ? 1.6 : 1)
                .allowsHitTesting(false)
            if let p {
                tabLabel(p, front: true, width: lay.tabWidth)
                    .offset(x: tabX)
            } else {
                blankTab(width: lay.tabWidth)
                    .offset(x: tabX)
            }
            // The live pane's ground, while it shows, and what a blank card says.
            if let p {
                RoundedRectangle(cornerRadius: 9, style: .continuous)
                    .fill(p.kind == .terminal ? Color(nsColor: Theme.terminalBackground) : Theme.card)
                    .frame(width: lay.content.width, height: lay.content.height)
                    .offset(x: lay.content.minX - lay.front.minX, y: lay.content.minY - lay.front.minY)
                    .allowsHitTesting(false)
            } else {
                BlankCard(hasSession: owner != nil, lay: lay)
                    .frame(width: lay.body.width, height: lay.body.height)
                    .offset(y: RolodexLayout.tabHeight)
            }
        }
        .frame(width: lay.front.width, height: lay.front.height)
        .offset(x: lay.front.minX, y: lay.front.minY)
        .zIndex(1)
    }

    /// The live panes, one on show, the rest kept under it, so a terminal
    /// stays attached and a page stays loaded as the wheel turns.
    @ViewBuilder
    private func live(_ deck: Deck, lay: RolodexLayout) -> some View {
        let r = store.rolodex
        let shown = r.visible(in: deck)
        let c = lay.content
        if let owner {
            ZStack(alignment: .topLeading) {
                ForEach(deck.panes) { p in
                    let on = shown == p.id
                    PaneContent(owner: owner, pane: p, shows: on && r.open, keys: r.keys == p.id, token: r.token(p.id))
                        .frame(width: c.width, height: c.height)
                        .opacity(on ? 1 : 0)
                        .allowsHitTesting(on)
                        .accessibilityHidden(!on)
                }
            }
            .frame(width: c.width, height: c.height)
            // Where the live pane is in the window; a turn drawn still has
            // no window, and its renderer would paint the probe in.
            .background {
                if r.still == nil { Probe { r.frontProbe = $0 }.allowsHitTesting(false) }
            }
            .offset(x: c.minX, y: c.minY)
            .transaction { $0.animation = nil }
            .zIndex(2)
        }
    }

    /// The flaps of the turns on show: a turn forward draws the oldest on top,
    /// as the card that fell first is nearest you; a turn back, the newest.
    @ViewBuilder
    private func flaps(_ lay: RolodexLayout) -> some View {
        let r = store.rolodex
        let flips = r.flips.filter { $0.session == owner?.id }
        if let still = r.still, let f = flips.last {
            FlapView(flip: f, progress: still, lay: lay)
                .zIndex(5)
        } else if !flips.isEmpty {
            TimelineView(.animation) { ctx in
                ZStack(alignment: .topLeading) {
                    ForEach(flips.first?.forward == false ? flips : flips.reversed()) { f in
                        FlapView(flip: f, progress: f.progress(at: ctx.date), lay: lay)
                    }
                }
            }
            .allowsHitTesting(false)
            .zIndex(5)
        }
    }

    private func tabLabel(_ p: DeckPane, front: Bool, width: CGFloat) -> some View {
        let hovered = hoveredTab == p.id
        return HStack(spacing: 5) {
            Image(systemName: p.kind.symbol)
                .font(.system(size: 10.5, weight: .semibold))
            Text(p.label)
                .font(Theme.font(11.5, front ? .bold : .medium))
                .lineLimit(1)
                .truncationMode(.tail)
            Spacer(minLength: 0)
            if hovered {
                Button { store.closePane(p.id) } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 8.5, weight: .bold))
                        .frame(width: 16, height: 16)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Close this pane")
                .accessibilityLabel("Close \(p.label)")
            }
        }
        .foregroundStyle(front ? Theme.agentInk(slot: p.slot) : Theme.muted)
        .padding(.leading, RolodexLayout.slope + 4)
        .padding(.trailing, RolodexLayout.slope)
        .frame(width: width, height: RolodexLayout.tabHeight)
        .contentShape(Rectangle())
        .offset(y: hovered && !front ? -2 : 0)
        .animation(.easeOut(duration: 0.12), value: hovered)
        .onHover { inside in hoveredTab = inside ? p.id : (hoveredTab == p.id ? nil : hoveredTab) }
        .onTapGesture { store.showPane(p.id) }
        .contextMenu { tabMenu(p) }
        .help("\(p.label) (⌥Tab walks the panes)")
        .accessibilityElement(children: .combine)
        .accessibilityLabel(p.label)
        .accessibilityAddTraits(front ? [.isButton, .isSelected] : [.isButton])
    }

    @ViewBuilder
    private func tabMenu(_ p: DeckPane) -> some View {
        Button("Show") { store.showPane(p.id) }
        Divider()
        if let o = owner {
            Button("Move Left") { store.rolodex.move(p.id, in: o.id, by: -1) }
            Button("Move Right") { store.rolodex.move(p.id, in: o.id, by: 1) }
        }
        if p.kind == .browser, let u = p.url.flatMap(URL.init(string:)) {
            Divider()
            Button("Open in Your Browser") { NSWorkspace.shared.open(u) }
            Button("Copy Address") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(u.absoluteString, forType: .string)
            }
        }
        Divider()
        Button("Close Pane") { store.closePane(p.id) }
    }

    private func blankTab(width: CGFloat) -> some View {
        HStack(spacing: 5) {
            Image(systemName: "plus")
                .font(.system(size: 10, weight: .bold))
            Text("New card")
                .font(Theme.font(11.5, .bold))
            Spacer(minLength: 0)
        }
        .foregroundStyle(Theme.muted)
        .padding(.leading, RolodexLayout.slope + 4)
        .frame(width: width, height: RolodexLayout.tabHeight)
    }

    // MARK: Rail

    /// The rail along the foot, which runs through the cards' notch, with a
    /// cap at its left end and the knob at its right.
    private func rail(_ lay: RolodexLayout) -> some View {
        let y = lay.railY
        let ink = owner?.ink ?? Theme.accent
        return ZStack(alignment: .topLeading) {
            // The rod: lit along its top, shaded under, as round metal is.
            Capsule()
                .fill(LinearGradient(colors: [Theme.muted.opacity(0.3), Theme.muted.opacity(0.7), Theme.muted.opacity(0.95)],
                                     startPoint: .top, endPoint: .bottom))
                .overlay(Capsule().fill(Theme.panel.opacity(0.55)).frame(height: 1).padding(.horizontal, 3).offset(y: -1.5))
                .shadow(color: .black.opacity(0.15), radius: 1.5, y: 1)
                .frame(width: lay.size.width - 20, height: 6)
                .offset(x: 6, y: y - 3)
            // The stop at its left end.
            Circle()
                .fill(Theme.panel)
                .overlay(Circle().strokeBorder(Theme.muted.opacity(0.6), lineWidth: 1.5))
                .overlay(Circle().fill(Theme.muted.opacity(0.6)).frame(width: 3, height: 3))
                .frame(width: 13, height: 13)
                .offset(x: 2, y: y - 6.5)
            Knob(angle: store.rolodex.knob, ink: ink)
                .frame(width: 26, height: 26)
                .contentShape(Circle())
                .gesture(knobGesture())
                .help("Turn the wheel: click, drag, or scroll over the tabs or the rail")
                .offset(x: lay.size.width - 28, y: y - 13)
                .accessibilityLabel("Turn to the next pane")
                .accessibilityAddTraits(.isButton)
        }
        .zIndex(-10)
    }

    /// A drag of the knob turns the wheel a notch per 18 points; a click
    /// turns it one notch forward.
    private func knobGesture() -> some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { g in
                guard let s = owner?.id else { return }
                let moved = g.translation.height - g.translation.width
                let base = knobDrag ?? 0
                if knobDrag == nil { knobDrag = 0 }
                if abs(moved - base) >= 18 {
                    knobDrag = moved
                    knobTurned = true
                    turnOne(s, forward: moved > base)
                }
            }
            .onEnded { _ in
                if !knobTurned, let s = owner?.id { turnOne(s, forward: true) }
                knobDrag = nil
                knobTurned = false
            }
    }

    private func turnOne(_ s: String, forward: Bool) {
        let r = store.rolodex
        let d = r.deck(s)
        guard d.panes.count > 1, let f = d.frontIndex else { return }
        let next = d.panes[(f + (forward ? 1 : d.panes.count - 1)) % d.panes.count].id
        let follow = r.keys != nil && r.keys != RolodexModel.blank
        r.stopSpin()
        if follow { r.keys = next }
        r.notch(in: s, forward: forward, duration: 0.3, keys: follow ? next : nil, layout: store.tabPlace(for: s))
    }

    /// The + above the knob: a new card of any kind on offer.
    @ViewBuilder
    private func plus(_ lay: RolodexLayout) -> some View {
        let mark = Image(systemName: "plus")
            .font(.system(size: 11, weight: .bold))
            .foregroundStyle(Theme.muted)
        Group {
            if store.rolodex.still != nil {
                // A turn drawn still has no menus to open.
                mark
            } else {
                Menu {
                    ForEach(store.paneKinds) { k in
                        Button { store.newPane(k) } label: { Label("New \(k.title) Pane", systemImage: k.symbol) }
                    }
                } label: {
                    mark
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
            }
        }
        .frame(width: 24, height: 24)
        .background(Circle().fill(Theme.panel).overlay(Circle().strokeBorder(Theme.edge, lineWidth: 1)))
        .help("Add a pane: a terminal, a browser or a plugin’s page")
        .disabled(owner == nil)
        .offset(x: lay.size.width - 27, y: lay.front.minY + 1)
        .zIndex(6)
    }
}

/// The knob at the rail's end: ridges round its rim, and a dot in the
/// session's colour, which turns a notch for every card.
private struct Knob: View {
    let angle: Double
    let ink: Color

    var body: some View {
        ZStack {
            Circle()
                .fill(Theme.panel)
                .overlay(Circle().strokeBorder(Theme.edge, lineWidth: 1))
                .shadow(color: .black.opacity(0.18), radius: 2, y: 1)
            ForEach(0..<12, id: \.self) { i in
                Capsule()
                    .fill(Theme.muted.opacity(0.45))
                    .frame(width: 1.5, height: 4)
                    .offset(y: -8.5)
                    .rotationEffect(.degrees(Double(i) * 30))
            }
            Circle()
                .fill(ink)
                .frame(width: 5, height: 5)
                .offset(y: -4.5)
        }
        .rotationEffect(.degrees(angle))
    }
}

/// A flap: the face of a card turning over the rail, the card leaving the
/// front in a turn forward, the card coming to it in a turn back.
private struct FlapView: View {
    let flip: Flip
    let progress: Double
    let lay: RolodexLayout

    var body: some View {
        let pane = flip.forward ? flip.from : flip.to
        let tab = flip.forward ? flip.fromTab : flip.toTab
        let tilt = flip.tilt(progress)
        let shade = min(0.45, abs(sin(tilt * .pi / 180)) * 0.5)
        CardFace(pane: pane, tabX: tab - lay.front.minX, lay: lay)
            .overlay(IndexCard(tabX: tab - lay.front.minX, tabWidth: lay.tabWidth).fill(Color.black.opacity(shade)))
            .frame(width: lay.front.width, height: lay.front.height)
            .rotation3DEffect(.degrees(tilt), axis: (x: 1, y: 0, z: 0), anchor: .bottom, perspective: 0.42)
            .opacity(flip.cover(progress))
            .offset(x: lay.front.minX, y: lay.front.minY)
    }
}

/// A card's face, as it shows while it turns: its tab, its ruled lines, and
/// its kind large in its colour.
private struct CardFace: View {
    let pane: DeckPane?
    let tabX: CGFloat
    let lay: RolodexLayout

    var body: some View {
        let shape = IndexCard(tabX: tabX, tabWidth: lay.tabWidth)
        let ink = pane.map { Theme.agentInk(slot: $0.slot) } ?? Theme.muted
        ZStack(alignment: .topLeading) {
            shape.fill(Theme.panel)
            Rectangle()
                .fill(pane.map { Theme.agentWash(slot: $0.slot) } ?? Theme.field)
                .frame(height: RolodexLayout.tabHeight + 1)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                .clipShape(shape)
            Rules(ink: pane.map { Theme.agent(slot: $0.slot) } ?? Theme.muted)
                .frame(width: lay.body.width - 24, height: lay.body.height - 20)
                .offset(x: 12, y: RolodexLayout.tabHeight + 4)
            shape.stroke(Theme.edge, lineWidth: 1)
            HStack(spacing: 5) {
                Image(systemName: pane?.kind.symbol ?? "plus")
                    .font(.system(size: 10.5, weight: .semibold))
                Text(pane?.label ?? "New card")
                    .font(Theme.font(11.5, .bold))
                    .lineLimit(1)
                Spacer(minLength: 0)
            }
            .foregroundStyle(ink)
            .padding(.leading, RolodexLayout.slope + 4)
            .frame(width: lay.tabWidth, height: RolodexLayout.tabHeight)
            .offset(x: tabX)
            VStack(spacing: 10) {
                Image(systemName: pane?.kind.symbol ?? "rectangle.stack")
                    .font(.system(size: 38, weight: .light))
                Text(pane?.label ?? "New card")
                    .font(Theme.font(17, .bold))
                    .lineLimit(1)
            }
            .foregroundStyle(ink)
            .frame(width: lay.body.width, height: lay.body.height)
            .offset(y: RolodexLayout.tabHeight)
        }
    }
}

/// A blank card: what a new card can be, a key for each.
@MainActor
private struct BlankCard: View {
    @Environment(Store.self) private var store
    let hasSession: Bool
    let lay: RolodexLayout
    @FocusState private var focused: Bool

    var body: some View {
        let r = store.rolodex
        ZStack(alignment: .topLeading) {
            Rules(ink: Theme.accent, head: 46)
                .padding(.horizontal, 12)
                .padding(.top, 4)
                .padding(.bottom, 18)
            VStack(alignment: .leading, spacing: 12) {
                Text(hasSession ? "What goes on this card?" : "Pick a session")
                    .font(Theme.font(15, .bold))
                    .foregroundStyle(Theme.ink)
                    .padding(.top, 8)
                if hasSession {
                    ForEach(store.paneKinds) { k in choice(k) }
                    Text("Press its letter, or ⌥Tab to go back to the terminal.")
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.muted)
                        .padding(.top, 4)
                } else {
                    Text("Each session keeps its own cards: a shell in its folder, a page, a plugin’s map.")
                        .font(Theme.font(12))
                        .foregroundStyle(Theme.muted)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 22)
            .padding(.top, 12)
        }
        .focusable(hasSession)
        .focused($focused)
        .focusEffectDisabled()
        .onKeyPress { press in
            guard let k = store.paneKinds.first(where: { String($0.letter) == press.characters.lowercased() }) else { return .ignored }
            store.newPane(k)
            return .handled
        }
        .onChange(of: focused) { _, f in r.blankHasKeys = f }
        .onChange(of: r.token(RolodexModel.blank)) { focused = true }
        .onAppear { if r.keys == RolodexModel.blank { focused = true } }
        .onDisappear { r.blankHasKeys = false }
    }

    private func choice(_ k: PaneKind) -> some View {
        Button { store.newPane(k) } label: {
            HStack(spacing: 10) {
                Text(String(k.letter).uppercased())
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 20, height: 20)
                    .background(RoundedRectangle(cornerRadius: 5, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
                Image(systemName: k.symbol)
                    .font(.system(size: 13, weight: .semibold))
                    .frame(width: 18)
                VStack(alignment: .leading, spacing: 1) {
                    Text(k.title).font(Theme.font(13, .semibold)).foregroundStyle(Theme.ink)
                    Text(k.about).font(Theme.font(11)).foregroundStyle(Theme.muted)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .frame(maxWidth: 340)
            .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card.opacity(0.85)))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .foregroundStyle(Theme.accentInk)
        .help("New \(k.title) pane (\(String(k.letter).uppercased()))")
    }
}

/// The wheel's leading edge, the divider from the terminal, which a drag
/// moves. A guide shows where it would go, and the width applies on release:
/// one resize of the terminal, and of every agent, not one per frame.
@MainActor
struct RolodexEdge: View {
    @Environment(Store.self) private var store
    /// The room beside the right sidebar, the wheel's width in it, and what
    /// the terminal keeps of it.
    let room: CGFloat
    let drawn: CGFloat
    let keep: CGFloat
    @State private var hovering = false

    var body: some View {
        let r = store.rolodex
        Color.clear
            .frame(width: 10)
            .overlay(Capsule().fill(Theme.edge).frame(width: 3, height: 34).opacity(hovering || r.guide != nil ? 1 : 0))
            .contentShape(Rectangle())
            .onHover { inside in
                hovering = inside
                if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
            }
            .gesture(DragGesture(minimumDistance: 1, coordinateSpace: .global)
                .onChanged { g in
                    let share = Double((drawn - g.translation.width) / max(room, 1))
                    r.guide = Double(RolodexModel.width(share: min(max(share, 0.2), 0.75), in: room, keep: keep))
                }
                .onEnded { _ in
                    if let g = r.guide { r.setShare(g / Double(max(room, 1))) }
                    r.guide = nil
                })
            .onTapGesture(count: 2) { r.setShare(RolodexModel.startShare) }
            .help("Drag to make the panes wider or narrower; double-click for their first width")
            .accessibilityHidden(true)
    }
}

/// A view that hands over its NSView, for the wheel to find where the live
/// pane and its scroll bands are in the window. It takes no clicks.
private struct Probe: NSViewRepresentable {
    let found: (NSView) -> Void

    func makeNSView(context _: Context) -> NSView {
        let v = ProbeView()
        DispatchQueue.main.async { found(v) }
        return v
    }

    func updateNSView(_: NSView, context _: Context) {}

    private final class ProbeView: NSView {
        override func hitTest(_: NSPoint) -> NSView? { nil }
    }
}

// MARK: Panes

/// One card's live pane, by its kind.
@MainActor
private struct PaneContent: View {
    let owner: PaneOwner
    let pane: DeckPane
    let shows: Bool
    /// The keyboard is meant for this card: it takes it as it first shows.
    let keys: Bool
    let token: Int

    var body: some View {
        switch pane.kind {
        case .terminal: ShellPane(owner: owner, pane: pane, shows: shows, keys: keys, token: token)
        case .browser: BrowserPane(session: owner.id, pane: pane, shows: shows, keys: keys, token: token)
        case .markets: MarketsPane(shows: shows, keys: keys, token: token)
        }
    }
}

/// A card's own shell, in the session's folder: a tmux client of its session
/// on the dock server. When the shell exits its last screen stays, with New
/// Shell and Close.
@MainActor
private struct ShellPane: View {
    @Environment(Store.self) private var store
    let owner: PaneOwner
    let pane: DeckPane
    let shows: Bool
    let keys: Bool
    let token: Int
    @State private var shell: PaneShell?
    @State private var problem: String?

    var body: some View {
        ZStack(alignment: .bottom) {
            if store.isFixture {
                FixtureScreen(lines: ["~/src/website  (fix-login-test)", "❯ npm run dev", "", "  VITE ready in 312 ms", "  ➜  Local:   http://localhost:5173/",
                                      "", "❯ git log --oneline -3", "a1b2c3d Wait for the session write", "9f8e7d6 Add a helper that reads it back"])
                    .padding(8)
            } else if let peer = owner.peer {
                note("A shell beside a session on \(peer) runs there; that comes later.")
            } else if !store.rolodex.canShell {
                note("This \(Brand.name)’s core can’t run a pane’s shell yet. The next install brings it.")
            } else if let s = shell, s.state != "stopped", let target = s.target, let tmux = s.tmux, let socket = s.socket {
                // A card the keyboard was sent to before its shell was up
                // takes the keyboard as its terminal starts.
                SplitTerminalView(tmux: tmux, socket: socket, target: "=" + target, fontSize: max(9, store.fontSize - 1),
                                  focusToken: token, followsFocus: true, takesKeysOnStart: shows && keys, shows: shows)
                    .padding(6)
                if s.state == "exited" { ended(s) }
            } else if let problem {
                note(problem)
            } else {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        // Again as it comes to the front or leaves it, since it looks while
        // it shows, and once the core can run its shell.
        .task(id: "\(owner.id)/\(pane.id)/\(shows)/\(store.rolodex.canShell)") { await follow() }
    }

    private func note(_ text: String) -> some View {
        Text(text)
            .font(Theme.font(12))
            .foregroundStyle(Color(nsColor: Theme.terminalForeground).opacity(0.7))
            .multilineTextAlignment(.center)
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func ended(_ s: PaneShell) -> some View {
        HStack(spacing: 10) {
            Text(s.exit.map { $0 == 0 ? "The shell ended." : "The shell ended with code \($0)." } ?? "The shell ended.")
                .font(Theme.font(12, .semibold))
                .foregroundStyle(Theme.ink)
            Spacer(minLength: 4)
            Button("New Shell") { act(PaneArgs(session: owner.id, pane: pane.id, restart: true)) }
                .buttonStyle(.borderedProminent)
                .tint(Theme.agent(slot: pane.slot))
            Button("Close Pane") { store.closePane(pane.id) }
        }
        .controlSize(.small)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.panel).shadow(radius: 3, y: 1))
        .padding(10)
    }

    /// Starts the shell when none runs, then, while the card shows, looks
    /// every few seconds to catch it ending. Each look starts it again if
    /// it has gone, as after a start that failed; one that ended keeps its
    /// last screen until New Shell.
    private func follow() async {
        guard !store.isFixture, owner.peer == nil, store.rolodex.canShell else { return }
        await load(PaneArgs(session: owner.id, pane: pane.id, start: true))
        while shows, !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 4_000_000_000)
            if Task.isCancelled { return }
            await load(PaneArgs(session: owner.id, pane: pane.id, start: true))
        }
    }

    private func load(_ args: PaneArgs) async {
        do {
            let s = try await store.paneShell(args)
            if s != shell { shell = s }
            problem = nil
        } catch {
            problem = "The shell didn’t start: \(error.localizedDescription)"
        }
    }

    private func act(_ args: PaneArgs) {
        Task {
            await load(args)
            store.rolodex.bump(pane.id)
        }
    }
}

/// The market map, in a card (Markets.swift).
@MainActor
private struct MarketsPane: View {
    @Environment(Store.self) private var store
    let shows: Bool
    let keys: Bool
    let token: Int

    var body: some View {
        if store.isFixture {
            MapSketch()
        } else {
            MarketsWindow(embedded: true, focus: token, takesKeys: shows && keys)
        }
    }
}

/// What the snapshots show for the market map.
private struct MapSketch: View {
    var body: some View {
        Canvas { ctx, size in
            let dots: [(CGFloat, CGFloat, CGFloat)] = [(0.22, 0.30, 26), (0.55, 0.26, 18), (0.74, 0.48, 30), (0.36, 0.62, 22), (0.62, 0.74, 14)]
            var lines = Path()
            for (a, b) in [(0, 1), (1, 2), (0, 3), (3, 4), (2, 4)] {
                lines.move(to: CGPoint(x: dots[a].0 * size.width, y: dots[a].1 * size.height))
                lines.addLine(to: CGPoint(x: dots[b].0 * size.width, y: dots[b].1 * size.height))
            }
            ctx.stroke(lines, with: .color(Theme.edge), lineWidth: 1.5)
            for (i, d) in dots.enumerated() {
                let r = CGRect(x: d.0 * size.width - d.2, y: d.1 * size.height - d.2, width: d.2 * 2, height: d.2 * 2)
                ctx.fill(Path(ellipseIn: r), with: .color(Theme.agent(slot: i % Theme.agentSlots).opacity(0.75)))
            }
        }
        .padding(12)
    }
}
