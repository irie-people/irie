import AppKit
import Observation
import SwiftUI

// Zen is a calm place, where the person comes before the work
// (docs/proposals/zen.md, asked for on 2026-11-21). The core keeps your zen,
// the settings below and the calm ones (a gentle agent, rests, a word late
// at night), as versions: each change shows at once and becomes a new
// version a moment later, and any earlier zen can come back (View › Your
// Zen…, ZenHistory.swift). Zen tells the core when it starts, ends or shows
// another session, so the core can ask each agent zen shows to be gentle,
// and draws the one thing the core offers at a time: a step of its path, a
// rest with a breath to follow, or a word at night (ZenCalm.swift). Other
// sessions that need you come as one soft line.
//
// Zen (Fn+Z, View › Enter Zen Mode): the session on show and its own
// parts alone, full screen, in a layout of zen's own, apart from View ›
// Layout's. Its parts are its terminal or conversation; its card, which
// becomes a header bubble over the terminal's top-left corner; and its
// panes, as it has them. Zen hides the rest: the other cards and the
// sidebar round them (search, details, notes, task bar), the title bar's
// toolbar, the right sidebar, which is the same for every session, and the
// strip under the terminal. It moves and closes nothing, so leaving it gives
// the window back as it was. The terminal stays the same view all along, so
// its tmux client never restarts (asked for on 2026-11-15; the card and the
// panes, and the card as a bubble, the same day).
//
// The three layouts arrange those parts for three ways of working:
//
// - Centred, the first: the terminal in a column of easy reading width in
//   the middle of the screen. A full screen's terminal is far wider than a
//   line of text should be, and an agent's words wrap at the terminal's
//   width; VS Code's zen centres its editor too (PROVENANCE.md).
// - Edge to Edge: the terminal across the screen, for the widest diffs,
//   logs and tables, and Claude Code's diff panel, which docks from 144
//   columns.
// - Keep Watch: Edge to Edge, with the other sessions listed down the left
//   edge, red when one needs you, so an agent waiting on a question is never
//   lost behind the one you watch.
//
// Apple's guidance for full screen shapes the rest: the system's own full
// screen, entered and left only by the person, with the menu bar a move of
// the pointer away. Asked for on 2026-11-15.

/// Zen's layouts, in the order Settings and View › Zen Layout list them.
enum ZenLayout: String, CaseIterable, Identifiable {
    case centred
    case edge
    case watch

    var id: String { rawValue }

    var title: String {
        switch self {
        case .centred: return "Centred"
        case .edge: return "Edge to Edge"
        case .watch: return "Keep Watch"
        }
    }

    /// What it is for, in a line.
    var blurb: String {
        switch self {
        case .centred:
            return "The terminal in a column of easy reading width in the middle of the screen. For reading an agent’s work and writing to it."
        case .edge:
            return "The terminal across the whole screen. The most room, for wide diffs, logs and tables."
        case .watch:
            return "Your other sessions listed down the left edge, red when one needs you. Stay on one without losing the rest; a click shows another."
        }
    }
}

/// Whether zen is on, and its settings, which the app keeps in its
/// defaults: the layout, Centred's width, whether zen fills the screen, and
/// zen's own text size.
@MainActor
@Observable
final class ZenModel {
    static let onKey = "zenOn"
    static let layoutKey = "zenLayout"
    static let columnsKey = "zenColumns"
    static let textKey = "zenTextSize"
    static let screenKey = "zenFullScreen"
    static let windowKey = "zenWindow"
    static let movedKey = "zenMoved"

    /// How long the app waits after a change before it sends it, so three
    /// presses of ⌘+ make one version, not three.
    static let gather: TimeInterval = 1.2

    /// The widths Centred offers, in columns of text, and the one it starts
    /// with.
    static let widths = [80, 100, 120]
    static let standardColumns = 100
    /// Keep Watch's list of the other sessions: a column as wide as the
    /// sidebar zen found, within the sidebar's own bounds, or the sidebar's
    /// first width.
    static let standardWatchColumn: CGFloat = 290
    static func watchColumn(for sidebar: PaneLayout.Sidebar?) -> CGFloat {
        guard let s = sidebar, s.shown, let w = s.width else { return standardWatchColumn }
        return min(max(CGFloat(w), 260), 420)
    }

    /// Zen is on.
    fileprivate(set) var on = false
    /// How wide Keep Watch's list is, while zen is on.
    fileprivate(set) var watchColumn = ZenModel.standardWatchColumn
    var layout = ZenLayout(rawValue: UserDefaults.standard.string(forKey: ZenModel.layoutKey) ?? "") ?? .centred {
        didSet {
            guard layout != oldValue, !fixture else { return }
            UserDefaults.standard.set(layout.rawValue, forKey: Self.layoutKey)
            keep(ZenChange(layout: layout.rawValue))
        }
    }
    /// How many columns of text wide Centred's column is.
    var columns = ZenModel.savedColumns() {
        didSet {
            guard columns != oldValue, !fixture else { return }
            UserDefaults.standard.set(columns, forKey: Self.columnsKey)
            keep(ZenChange(columns: columns))
        }
    }
    /// Zen takes the window full screen, onto a desktop of its own; off, it
    /// stays in the window.
    var fullScreen = UserDefaults.standard.object(forKey: ZenModel.screenKey) as? Bool ?? true {
        didSet {
            guard fullScreen != oldValue, !fixture else { return }
            UserDefaults.standard.set(fullScreen, forKey: Self.screenKey)
            keep(ZenChange(fullScreen: fullScreen))
        }
    }
    /// Zen's own text size, which ⌘+ and ⌘- change while zen is on; nil
    /// keeps the window's.
    var textSize = ZenModel.savedText() {
        didSet {
            guard textSize != oldValue, !fixture else { return }
            if let t = textSize { UserDefaults.standard.set(t, forKey: Self.textKey) } else { UserDefaults.standard.removeObject(forKey: Self.textKey) }
            keep(ZenChange(textSize: textSize.map { Int($0.rounded()) } ?? 0))
        }
    }
    /// The calm settings, which only the core keeps: whether zen asks the
    /// agent to be gentle, the minutes before a rest (0 for none), and the
    /// word late at night.
    fileprivate(set) var gentle = true
    fileprivate(set) var breaks = 0
    fileprivate(set) var night = true
    /// Your zen's newest version, nil before the first.
    fileprivate(set) var head: String?
    /// The one thing zen offers now, until it is answered.
    var offer: ZenOffer?
    /// The step whose change is on trial: what shows is your zen with it.
    fileprivate(set) var trying: String?
    /// View › Your Zen…, the graph explorer of your zen's versions.
    var historyShown = false
    /// The core keeps your zen: it lists the zen op.
    @ObservationIgnored var kept = false
    /// The app's own zen settings went to the core as its first version.
    @ObservationIgnored var moved = UserDefaults.standard.bool(forKey: ZenModel.movedKey) {
        didSet { if moved, !fixture { UserDefaults.standard.set(true, forKey: Self.movedKey) } }
    }
    /// Sends a change to the core (Store.changeZen).
    @ObservationIgnored var send: (@MainActor (ZenChange) -> Void)?
    /// Changes not sent yet, and calls still on their way: until both are
    /// done, the header's zen is older than what shows.
    @ObservationIgnored fileprivate var pending = ZenChange()
    @ObservationIgnored fileprivate var pendingGen = 0
    @ObservationIgnored var sending = 0
    /// Taking the core's zen, which is no change to send back.
    @ObservationIgnored fileprivate var taking = false
    /// `--snapshot`'s versions for Your Zen.
    @ObservationIgnored var fixtureVersions: [ZenVersion] = []
    /// Bumped each time zen starts: the window says for a moment how to
    /// leave it.
    fileprivate(set) var started = 0
    /// Zen took the window full screen, so leaving it gives the screen back;
    /// a window that was full screen already stays so.
    @ObservationIgnored fileprivate var tookScreen = false
    /// The window as zen found it, to put back. It is kept, so a zen that
    /// comes back after a relaunch still knows.
    @ObservationIgnored fileprivate var found: Found?
    /// Back after a relaunch behind the person's work: full screen once they
    /// come back to the app, never before (Desktops.swift, "Out of sight"),
    /// and how to leave it said then.
    @ObservationIgnored fileprivate var screenOnReturn = false
    @ObservationIgnored fileprivate var hintOnReturn = false
    /// The window is on its way into full screen or out of it, and one
    /// leaving zen meanwhile, which takes it out again once it is in.
    @ObservationIgnored fileprivate var moving = false
    @ObservationIgnored fileprivate var moves = 0
    @ObservationIgnored fileprivate var outOnArrival = false
    @ObservationIgnored fileprivate var watching: [NSObjectProtocol] = []
    @ObservationIgnored fileprivate weak var window: NSWindow?
    /// `--snapshot`'s: nothing is saved.
    @ObservationIgnored var fixture = false

    private static func savedColumns() -> Int {
        let n = UserDefaults.standard.integer(forKey: columnsKey)
        return widths.contains(n) ? n : standardColumns
    }

    private static func savedText() -> Double? {
        let v = UserDefaults.standard.double(forKey: textKey)
        return v >= 9 && v <= 28 ? v : nil
    }

    fileprivate func keepOn() {
        guard !fixture else { return }
        UserDefaults.standard.set(on, forKey: Self.onKey)
    }

    /// What zen hides of the window as it found it: whether the sidebar
    /// showed, and how wide, and whether the toolbar did.
    struct Found {
        var sidebar: PaneLayout.Sidebar?
        var toolbar = true
    }

    fileprivate func keepFound() {
        guard !fixture else { return }
        guard let f = found else { return UserDefaults.standard.removeObject(forKey: Self.windowKey) }
        var d: [String: Any] = ["toolbar": f.toolbar]
        if let s = f.sidebar {
            d["shown"] = s.shown
            if let w = s.width { d["width"] = w }
        }
        UserDefaults.standard.set(d, forKey: Self.windowKey)
    }

    fileprivate static func savedFound() -> Found {
        guard let d = UserDefaults.standard.dictionary(forKey: windowKey) else { return Found() }
        let sidebar = (d["shown"] as? Bool).map { PaneLayout.Sidebar(shown: $0, width: d["width"] as? Double) }
        return Found(sidebar: sidebar, toolbar: d["toolbar"] as? Bool ?? true)
    }

    /// Keeps a change for the core, sent a moment after the last one.
    fileprivate func keep(_ change: ZenChange) {
        guard kept, !taking, send != nil else { return }
        pending.merge(change)
        pendingGen += 1
        let gen = pendingGen
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.gather) { [weak self] in
            MainActor.assumeIsolated {
                guard let self, self.pendingGen == gen, !self.pending.isEmpty else { return }
                let ch = self.pending
                self.pending = ZenChange()
                self.send?(ch)
            }
        }
    }

    /// Your zen as the app shows it, whole, as a change.
    var current: ZenChange {
        ZenChange(layout: layout.rawValue, columns: columns, textSize: textSize.map { Int($0.rounded()) } ?? 0,
                  fullScreen: fullScreen, gentle: gentle, breaks: breaks, night: night)
    }

    /// The app's zen settings from before the core kept them, if any were
    /// ever changed.
    static var hadSettings: Bool {
        [layoutKey, columnsKey, textKey, screenKey].contains { UserDefaults.standard.object(forKey: $0) != nil }
    }

    /// Zen was on when the app last ran, and comes back once it has
    /// launched (ZenResume): until then, zen is not over.
    var resuming: Bool { !on && !fixture && UserDefaults.standard.bool(forKey: Self.onKey) }

    /// Takes the core's zen: what it offers and its newest version, and the
    /// settings, unless a change of the person's is still on its way.
    fileprivate func take(_ z: ZenState) {
        if offer != z.offer { offer = z.offer }
        if head != z.head { head = z.head }
        if trying != z.trying { trying = z.trying }
        guard pending.isEmpty, sending == 0 else { return }
        taking = true
        defer { taking = false }
        let p = z.profile
        if let l = ZenLayout(rawValue: p.layout), l != layout { layout = l }
        if Self.widths.contains(p.columns), p.columns != columns { columns = p.columns }
        let text: Double? = p.textSize >= 9 && p.textSize <= 28 ? Double(p.textSize) : nil
        if text != textSize { textSize = text }
        if p.fullScreen != fullScreen { fullScreen = p.fullScreen }
        if p.gentle != gentle { gentle = p.gentle }
        if p.breaks != breaks { breaks = p.breaks }
        if p.night != night { night = p.night }
    }

    /// Draws a layout for `--snapshot`, or none, with no window to change.
    func showFixture(_ l: ZenLayout?) {
        fixture = true
        if let l { layout = l }
        on = l != nil
    }
}

/// Zen comes back after a relaunch, as an install's or the freeze dog's, as
/// it does in VS Code: only in the app's own run, never in a preview, a
/// snapshot or a film, which read the same settings.
@MainActor
enum ZenResume {
    private static var launched = false
    private static var done = false
    private static var waiting: (@MainActor () -> Void)?

    /// The app has launched (AppDelegate).
    static func launch() {
        guard !Preview.isOn, !WindowPlace.away else { return }
        launched = true
        let work = waiting
        waiting = nil
        work?()
    }

    /// Runs once the app has launched, at once if it has.
    static func once(_ work: @escaping @MainActor () -> Void) {
        guard !done else { return }
        let run: @MainActor () -> Void = {
            done = true
            work()
        }
        if launched { run() } else { waiting = run }
    }
}

extension Store {
    /// Fn+Z and View › Enter Zen Mode: zen starts, or ends and gives the
    /// window back as it was. A press while the window goes into full
    /// screen or out of it waits for none: the system's own toggle does the
    /// same.
    func toggleZen() {
        guard !zen.moving else { return }
        if zen.on { leaveZen() } else { enterZen() }
    }

    /// Zen starts: the sidebar folds away and the toolbar goes, the session
    /// on show takes the keyboard, and the window goes full screen unless it
    /// is so already or zen stays in the window.
    func enterZen() {
        guard !zen.on, !isFixture, let window = WindowPanes.window else { return }
        zen.found = ZenModel.Found(sidebar: WindowPanes.sidebar.map { PaneLayout.Sidebar(shown: $0.shown, width: $0.shown ? $0.width.rounded() : nil) },
                                   toolbar: window.toolbar?.isVisible ?? true)
        zen.keepFound()
        zen.watchColumn = ZenModel.watchColumn(for: zen.found?.sidebar)
        begin(in: window)
        if zen.fullScreen, !window.styleMask.contains(.fullScreen) {
            zen.tookScreen = true
            window.toggleFullScreen(nil)
        }
    }

    /// Zen as the app left it, after a relaunch. What it puts back is what
    /// it found before; full screen waits until the person is in the app, so
    /// a relaunch behind their work never takes them to it.
    func resumeZen(in window: NSWindow) {
        guard !zen.on, !isFixture, UserDefaults.standard.bool(forKey: ZenModel.onKey) else { return }
        zen.found = ZenModel.savedFound()
        zen.watchColumn = ZenModel.watchColumn(for: zen.found?.sidebar)
        // How to leave it is said once the person is here to read it.
        begin(in: window, hint: NSApp.isActive)
        if zen.fullScreen, !window.styleMask.contains(.fullScreen) {
            if NSApp.isActive { fullScreenSoon() } else { zen.screenOnReturn = true }
        } else if !NSApp.isActive {
            zen.hintOnReturn = true
        }
    }

    private func begin(in window: NSWindow, hint: Bool = true) {
        watchZen(window)
        // At once, not slid away: the window's own move into full screen
        // is the motion.
        var t = Transaction()
        t.disablesAnimations = true
        withTransaction(t) { zen.on = true }
        if hint { zen.started += 1 } else { zen.hintOnReturn = true }
        zen.keepOn()
        useZenText(true)
        foldWindowForZen()
        // A field in the sidebar or a pane that had the keyboard is out of
        // sight now.
        giveMainKeys()
        sayZen()
    }

    /// Zen ends: the sidebar comes back at its width and the toolbar with
    /// it, the window out of full screen if zen took it there, and the rest
    /// as it was, as nothing changed it. `sidebar` false leaves the sidebar to a layout put on
    /// next (Looks.swift); `screen` false is for a window that left full
    /// screen by itself, as by its green button.
    func leaveZen(sidebar: Bool = true, screen: Bool = true) {
        guard zen.on else { return }
        var t = Transaction()
        t.disablesAnimations = true
        withTransaction(t) { zen.on = false }
        zen.keepOn()
        zen.screenOnReturn = false
        zen.hintOnReturn = false
        zen.historyShown = false
        useZenText(false)
        sayZen()
        let found = zen.found
        zen.found = nil
        zen.keepFound()
        WindowPanes.window?.toolbar?.isVisible = found?.toolbar ?? true
        if sidebar, let s = found?.sidebar, s.shown { WindowPanes.setSidebar(shown: true, width: s.width) }
        guard zen.tookScreen else { return }
        zen.tookScreen = false
        guard screen, let w = WindowPanes.window, w.styleMask.contains(.fullScreen) else { return }
        if zen.moving { zen.outOnArrival = true } else { w.toggleFullScreen(nil) }
    }

    /// Zen gives way to a part of the window it hides that the person asks
    /// for, such as the right sidebar or the session list: zen ends, and the
    /// part shows as asked. Whether zen was on. The session's own panes are
    /// zen's too, so they never end it.
    @discardableResult
    func zenGivesWay() -> Bool {
        guard zen.on else { return false }
        leaveZen()
        return true
    }

    /// Does what needs the sidebar, such as Find a Session or Type a Task,
    /// once zen has given it back.
    func afterZen(_ work: @escaping @MainActor () -> Void) {
        guard zenGivesWay() else { return work() }
        // The sidebar slides back in first (WindowPanes.setSidebar).
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { MainActor.assumeIsolated(work) }
    }

    /// Settings › Appearance › Zen: whether zen fills the screen, at once
    /// while zen is on.
    func setZenFullScreen(_ on: Bool) {
        zen.fullScreen = on
        zenScreen(on)
    }

    /// The window into full screen or out of it, as zen's setting says,
    /// while zen is on.
    private func zenScreen(_ on: Bool) {
        guard zen.on, !zen.moving, let w = WindowPanes.window else { return }
        let full = w.styleMask.contains(.fullScreen)
        if on, !full {
            zen.tookScreen = true
            w.toggleFullScreen(nil)
        } else if !on, full, zen.tookScreen {
            zen.tookScreen = false
            w.toggleFullScreen(nil)
        }
    }

    /// Settings › Appearance › Zen: zen's own text size, or nil for the
    /// window's, at once while zen is on.
    func setZenText(_ size: Double?) {
        zen.textSize = size.map { min(max($0, 9), 28) }
        if zen.on { useZenText(true) }
    }

    /// Tells the core whether zen shows, and which session: it asks each
    /// agent zen shows to be gentle, and offers what is due. A preview, a
    /// snapshot or a film leave that to the installed app, whose sessions
    /// they share.
    func sayZen() {
        guard zen.kept, !isFixture, !Preview.isOn, !zen.resuming else { return }
        callZen(ZenArgs(on: zen.on, session: zen.on ? selection : nil, windowText: Int(windowFontSize.rounded())), what: nil)
    }

    /// Changes your zen: it shows at once, and the core keeps it as a new
    /// version.
    func changeZen(_ change: ZenChange, why: String? = nil) {
        guard zen.kept, !isFixture, !Preview.isOn, !change.isEmpty else { return }
        callZen(ZenArgs(set: change, why: why), what: "change your zen")
    }

    /// Settings › Zen's calm settings, shown at once.
    func setZenCalm(gentle: Bool? = nil, breaks: Int? = nil, night: Bool? = nil) {
        if let gentle { zen.gentle = gentle }
        if let breaks { zen.breaks = breaks }
        if let night { zen.night = night }
        changeZen(ZenChange(gentle: gentle, breaks: breaks, night: night))
    }

    /// Answers what zen offers: its first button or its second. A step
    /// that shows your zen's history opens it.
    func answerZen(_ offer: ZenOffer, yes: Bool) {
        if yes, offer.show == "history" { zen.historyShown = true }
        guard !isFixture, !Preview.isOn else {
            zen.offer = nil
            return
        }
        zen.offer = nil
        callZen(ZenArgs(offer: offer.id, answer: yes ? "yes" : "no"), what: nil)
    }

    /// Back to an earlier zen, as a new version: history is never rewritten.
    func restoreZen(_ version: ZenVersion) {
        guard !isFixture, !Preview.isOn else { return }
        callZen(ZenArgs(restore: version.name), what: "go back to that zen")
    }

    /// Your zen's versions, newest first.
    func zenVersions() async -> [ZenVersion] {
        guard zen.kept, !isFixture else { return zen.fixtureVersions }
        let r = try? await client.call("zen", ZenArgs(log: 1000), timeout: 5, as: ZenResult.self)
        return r?.versions ?? []
    }

    /// One call of the zen op. Until it answers, the header's zen may be
    /// older than what shows, so the model leaves the settings alone; the
    /// answer's zen is taken after.
    private func callZen(_ args: ZenArgs, what: String?) {
        zen.sending += 1
        Task {
            var answer: ZenResult?
            do {
                answer = try await client.call("zen", args, timeout: 5, as: ZenResult.self)
            } catch {
                if let what { say("Couldn’t \(what): \(error.localizedDescription)", error: true) }
            }
            zen.sending -= 1
            if let z = answer?.zen { takeZen(z) }
        }
    }

    /// Takes zen from the core's header or an answer. The app's own zen
    /// settings from before the core kept them become its first version,
    /// once; a change from the core, such as a step tried or an agent's
    /// `irie ctl zen set`, shows at once.
    func takeZen(_ z: ZenState) {
        if z.head != nil, !zen.moved { zen.moved = true }
        if z.head == nil, zen.kept, !zen.moved, !isFixture, !Preview.isOn, ZenModel.hadSettings {
            zen.moved = true
            changeZen(zen.current, why: "Your zen as it was")
            return
        }
        let text = zen.textSize, full = zen.fullScreen
        zen.take(z)
        if zen.on, zen.textSize != text { useZenText(true) }
        if zen.fullScreen != full { zenScreen(zen.fullScreen) }
    }

    /// Points a column of the terminal takes for each point of text:
    /// Ghostty's own cell, or a monospaced font's 0.6 em. Centred's column
    /// is its columns of text wide at any text size.
    var cellRatio: CGFloat {
        #if canImport(GhosttyKit)
        if let r = (terminalView as? GhosttyTerminalView)?.cellRatio, r > 0.3, r < 1.2 { return r }
        #endif
        return 0.6
    }

    /// Watches the window go into full screen and out of it, and the app
    /// come to the front, for zen.
    func watchZen(_ window: NSWindow) {
        guard zen.window !== window else { return }
        zen.window = window
        let nc = NotificationCenter.default
        for o in zen.watching { nc.removeObserver(o) }
        func watch(_ name: Notification.Name, of object: AnyObject?, _ act: @escaping @MainActor (Store) -> Void) -> NSObjectProtocol {
            nc.addObserver(forName: name, object: object, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { if let self { act(self) } }
            }
        }
        zen.watching = [
            watch(NSWindow.willEnterFullScreenNotification, of: window) { $0.zenMoves() },
            watch(NSWindow.willExitFullScreenNotification, of: window) { $0.zenMoves() },
            watch(NSWindow.didEnterFullScreenNotification, of: window) { s in
                s.zen.moving = false
                // Zen ended on the way in: out again.
                if s.zen.outOnArrival {
                    s.zen.outOnArrival = false
                    if !s.zen.on, let w = s.zen.window { w.toggleFullScreen(nil) }
                }
            },
            watch(NSWindow.didExitFullScreenNotification, of: window) { s in
                s.zen.moving = false
                s.zen.outOnArrival = false
                // Out of the full screen zen took by another way, as the
                // green button or ⌃⌘F: zen ends too.
                if s.zen.on, s.zen.tookScreen {
                    s.zen.tookScreen = false
                    s.leaveZen(screen: false)
                }
            },
            watch(NSApplication.didBecomeActiveNotification, of: nil) { s in
                if s.zen.hintOnReturn {
                    s.zen.hintOnReturn = false
                    if s.zen.on { s.zen.started += 1 }
                }
                guard s.zen.screenOnReturn else { return }
                s.zen.screenOnReturn = false
                s.fullScreenSoon()
            },
        ]
    }

    /// The window starts into full screen or out of it. Should its end
    /// never come, as when macOS refuses, zen takes presses again soon.
    private func zenMoves() {
        zen.moving = true
        zen.moves += 1
        let n = zen.moves
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { [weak self] in
            MainActor.assumeIsolated {
                guard let self, self.zen.moves == n else { return }
                self.zen.moving = false
            }
        }
    }

    /// Takes the window full screen for zen once it has settled, while zen
    /// still wants it.
    private func fullScreenSoon() {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak self] in
            MainActor.assumeIsolated {
                guard let self, self.zen.on, self.zen.fullScreen, !self.zen.moving,
                      let w = WindowPanes.window, !w.styleMask.contains(.fullScreen) else { return }
                self.zen.tookScreen = true
                w.toggleFullScreen(nil)
            }
        }
    }

    /// Folds the sidebar away and hides the toolbar, once the window has
    /// them: after a relaunch zen starts before it may. The toolbar is
    /// hidden as View › Hide Toolbar hides it, which SwiftUI's own redraws of
    /// its items leave hidden; its visibility modifier, tried first, left a
    /// toolbar SwiftUI bridged into the window in sight.
    private func foldWindowForZen(tries: Int = 12) {
        guard zen.on else { return }
        let toolbar = WindowPanes.window?.toolbar
        toolbar?.isVisible = false
        let sidebar = WindowPanes.sidebar
        if let s = sidebar, s.shown { WindowPanes.setSidebar(shown: false, width: nil) }
        guard toolbar == nil || sidebar == nil, tries > 0 else { return }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
            MainActor.assumeIsolated { self?.foldWindowForZen(tries: tries - 1) }
        }
    }
}

#if canImport(GhosttyKit)
extension GhosttyTerminalView {
    /// A cell's width in points for each point of text, as Ghostty lays out
    /// the font in use; nil before the surface is there.
    var cellRatio: CGFloat? {
        // The cells across a wide band, so a cell's rounding to whole
        // pixels counts for little.
        guard let s = grid(width: 4000), fontSize > 0, s.columns > 0 else { return nil }
        return 4000 / CGFloat(s.columns) / fontSize
    }
}
#endif

// MARK: Views

/// Keep Watch's column at the screen's left edge, on the sidebar's ground:
/// the other sessions, a line each.
struct ZenWatchColumn: View {
    var body: some View {
        ZenOthers()
            .padding(.top, 24)
            .padding(.horizontal, 12)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(Theme.sidebar.ignoresSafeArea())
    }
}

/// Keep Watch's other sessions: each one the sidebar draws but the one on
/// show, in its order, a line each, its mark and name, and in red what it
/// needs while it needs you. A count over them goes to the first that needs
/// you, as the title bar's does; a click shows one, still in zen.
struct ZenOthers: View {
    @Environment(Store.self) private var store

    var body: some View {
        let shown = store.selection
        let others = store.drawnOrder.filter { $0.id != shown }
        let needing = others.filter { Look($0).loud }
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text("Other sessions")
                    .font(Theme.font(11, .semibold))
                    .foregroundStyle(Theme.muted)
                Spacer(minLength: 0)
                if let first = needing.first {
                    // Calm, as zen is: the red mark says it needs you
                    // (Roots §1.5), the words say it softly.
                    Button { store.show(first.id, focus: true) } label: {
                        HStack(spacing: 4) {
                            StateMark(look: .needs, size: 8)
                            Text(needing.count == 1 ? "1 would like you" : "\(needing.count) would like you")
                                .font(Theme.font(10.5, .medium))
                                .monospacedDigit()
                        }
                        .foregroundStyle(Theme.ink)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 1.5)
                        .background(Capsule().fill(Theme.hover))
                        .contentShape(Capsule())
                    }
                    .buttonStyle(.plain)
                    .help("Show the first that would like you, when you’re ready")
                }
            }
            .padding(.horizontal, 6)
            ScrollView(.vertical, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 1) {
                    ForEach(others) { s in
                        ZenOtherRow(row: .init(s)) { store.show(s.id, focus: true) }
                            .equatable()
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Other sessions")
    }
}

/// One of Keep Watch's other sessions. Its inputs are plain values, so a
/// row's news redraws only the lines it changes.
private struct ZenOtherRow: View, Equatable {
    struct Row: Equatable {
        let name: String
        let look: Look
        let label: String
        let tint: Color?
        let quiet: Bool

        @MainActor
        init(_ s: Session) {
            name = s.name
            look = Look(s)
            label = Look.label(s)
            tint = Look.agentTint(s)
            quiet = look.quiet || look == .done && s.isParked
        }
    }

    let row: Row
    let action: () -> Void
    @State private var hovering = false

    static func == (a: Self, b: Self) -> Bool { a.row == b.row }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        Button(action: action) {
            HStack(spacing: 8) {
                StateMark(look: row.look, size: 12, tint: row.tint)
                Text(row.name)
                    .font(Theme.font(12.5, row.quiet ? .regular : .medium))
                    .foregroundStyle(row.quiet ? Theme.muted : Theme.ink)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 4)
                if row.look.loud {
                    // Zen's calm: the mark is red, the words plain.
                    Text(row.label)
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 4)
            .background { if hovering { shape.fill(Theme.hover) } }
            .contentShape(shape)
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("\(row.name) · \(row.label)")
        .accessibilityLabel("\(row.name), \(row.label)")
    }
}

/// What zen says for a moment, in a capsule: how to leave it as it starts,
/// as a browser says how to leave its full screen, or a session's name.
struct ZenHint: View {
    static let leave = "Press Fn+Z to leave zen"
    let text: String

    var body: some View {
        Text(text)
            .font(.system(size: 11.5, weight: .medium))
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(Capsule().fill(Theme.field).shadow(radius: 3, y: 1))
            .overlay(Capsule().strokeBorder(Theme.edge, lineWidth: 1))
            .allowsHitTesting(false)
    }
}

/// Zen's layouts as small pictures of a screen in zen, for Settings ›
/// Appearance: the one in use ringed in the accent, and what it is for under
/// them.
struct ZenPicker: View {
    @Binding var selection: ZenLayout

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 14) {
                ForEach(ZenLayout.allCases) { l in
                    let on = selection == l
                    Button { selection = l } label: {
                        VStack(spacing: 6) {
                            ZenSketch(layout: l)
                                .frame(width: 132, height: 84)
                                .overlay(RoundedRectangle(cornerRadius: 9, style: .continuous)
                                    .strokeBorder(on ? Theme.accent : Theme.edge, lineWidth: on ? 2 : 1))
                            Text(l.title)
                                .font(Theme.font(12, on ? .bold : .regular))
                                .foregroundStyle(on ? Theme.ink : Theme.muted)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(l.blurb)
                    .accessibilityLabel(l.title)
                    .accessibilityHint(l.blurb)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            .frame(maxWidth: .infinity)
            Text(selection.blurb)
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// A screen in a zen layout, drawn small in the palette in use: the ground,
/// the terminal in its frame with a few lines of text, the title over it,
/// and Keep Watch's rail of marks, one of them red.
private struct ZenSketch: View {
    let layout: ZenLayout

    var body: some View {
        let term = Color(nsColor: Theme.terminalBackground)
        let ink = Color(nsColor: Theme.terminalForeground)
        Canvas { ctx, size in
            let w = size.width, h = size.height
            ctx.fill(Path(CGRect(origin: .zero, size: size)), with: .color(layout == .edge ? term : Theme.ground))
            // Lines of text, ragged as an agent's are, in a box.
            func text(in r: CGRect) {
                let widths: [CGFloat] = [0.62, 0.4, 0.86, 0.7, 0.28, 0.78, 0.52, 0.66, 0.35, 0.8, 0.46]
                var y = r.minY + 6
                for (i, f) in widths.enumerated() where y + 2 <= r.maxY - 5 {
                    let line = CGRect(x: r.minX + 6, y: y, width: (r.width - 12) * f, height: 2)
                    ctx.fill(Path(roundedRect: line, cornerRadius: 1), with: .color(ink.opacity(i % 4 == 3 ? 0.25 : 0.5)))
                    y += 5.5
                }
            }
            func frame(_ r: CGRect) {
                let p = Path(roundedRect: r, cornerRadius: 4)
                ctx.fill(p, with: .color(term))
                ctx.stroke(p, with: .color(Theme.agent(slot: 1)), lineWidth: 1.2)
                text(in: r)
            }
            // The session's card as a header bubble over the terminal's
            // top-left corner: its mark and name, edged in the agent's
            // colour as the selected card is.
            func bubble(at x: CGFloat, _ y: CGFloat) {
                let r = CGRect(x: x, y: y, width: 30, height: 12)
                let p = Path(roundedRect: r, cornerRadius: 4)
                ctx.fill(p, with: .color(Theme.selection))
                ctx.stroke(p, with: .color(Theme.agent(slot: 1)), lineWidth: 0.9)
                ctx.fill(Path(ellipseIn: CGRect(x: r.minX + 3, y: r.minY + 3, width: 3, height: 3)), with: .color(Theme.agent(slot: 1)))
                ctx.fill(Path(roundedRect: CGRect(x: r.minX + 8, y: r.minY + 3.5, width: 15, height: 1.8), cornerRadius: 0.9),
                         with: .color(Theme.ink.opacity(0.7)))
                ctx.fill(Path(roundedRect: CGRect(x: r.minX + 8, y: r.minY + 7, width: 10, height: 1.5), cornerRadius: 0.75),
                         with: .color(Theme.muted.opacity(0.7)))
            }
            switch layout {
            case .centred:
                let cw = w * 0.5
                let f = CGRect(x: (w - cw) / 2, y: 7, width: cw, height: h - 14)
                frame(f)
                bubble(at: f.minX + 3, f.minY + 3)
            case .edge:
                let f = CGRect(x: 3, y: 3, width: w - 6, height: h - 6)
                frame(f)
                bubble(at: f.minX + 3, f.minY + 3)
            case .watch:
                ctx.fill(Path(CGRect(x: 0, y: 0, width: 34, height: h)), with: .color(Theme.sidebar))
                // The others, a line each: a mark, one of them red, and a name.
                let marks: [Color] = [Theme.needs, Theme.agent(slot: 4), Look.doneColor, Theme.muted, Theme.agent(slot: 3), Theme.agent(slot: 2)]
                for (i, c) in marks.enumerated() {
                    let y = 9 + CGFloat(i) * 7
                    ctx.fill(Path(ellipseIn: CGRect(x: 6, y: y, width: 3, height: 3)), with: .color(c))
                    ctx.fill(Path(roundedRect: CGRect(x: 12, y: y + 0.6, width: i == 0 ? 16 : 12, height: 1.8), cornerRadius: 0.9),
                             with: .color(i == 0 ? Theme.needs.opacity(0.8) : Theme.ink.opacity(0.45)))
                }
                let f = CGRect(x: 36, y: 4, width: w - 40, height: h - 8)
                frame(f)
                bubble(at: f.minX + 3, f.minY + 3)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
        .accessibilityHidden(true)
    }
}

/// View › Enter Zen Mode and View › Zen Layout. KeyRouter catches Fn+Z,
/// which the item names in its help: AppKit takes fn out of an app's own
/// menu keys (Roots' principles §4.8).
@MainActor
struct ZenMenus: View {
    let store: Store
    let f: MenuState.Facts

    var body: some View {
        Button(f.zen ? "Exit Zen Mode" : "Enter Zen Mode") { store.toggleZen() }
            .help("Fn+Z")
        Menu("Zen Layout") {
            ForEach(ZenLayout.allCases) { l in
                Toggle(l.title, isOn: Binding(get: { f.zenLayout == l }, set: { if $0 { store.zen.layout = l } }))
            }
        }
        Button("Your Zen…") { store.zen.historyShown = true }
            .help("Each zen you’ve had, and the way back to any of them")
    }
}
