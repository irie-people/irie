import AppKit
import Observation
import SwiftUI

// Zen (Fn+Z, View › Enter Zen Mode): the session on show alone, full
// screen, in a layout of zen's own, apart from View › Layout's. Zen hides
// the sidebar, the title bar's toolbar, the right sidebar, the panes and
// the strip under the terminal. It moves and closes nothing, so leaving it
// gives the window back as it was. The terminal stays the same view all
// along, so its tmux client never restarts, and every agent's window takes
// zen's size once (PaneSizes.swift).
//
// The three layouts answer three ways of working with one agent:
//
// - Centred, the first: a column of easy reading width in the middle of the
//   screen. A full screen's terminal is far wider than a line of text should
//   be, and an agent's words wrap at the terminal's width; VS Code's zen
//   centres its editor too (PROVENANCE.md).
// - Edge to Edge: the terminal across the whole screen, for the widest
//   diffs, logs and tables, and Claude Code's diff panel, which docks from
//   144 columns.
// - Keep Watch: the other sessions as marks down the left edge, red when one
//   needs you, so an agent waiting on a question is never lost behind the
//   one you watch. They stand where the sidebar was.
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
            return "A column of easy reading width in the middle of the screen, the session’s name above it. For reading an agent’s work and writing to it."
        case .edge:
            return "The terminal across the whole screen, with nothing round it. The most room, for wide diffs, logs and tables."
        case .watch:
            return "Your other sessions as marks down the left edge, red when one needs you. Stay on one without losing the rest; a click shows another."
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

    /// The widths Centred offers, in columns of text, and the one it starts
    /// with.
    static let widths = [80, 100, 120]
    static let standardColumns = 100
    /// Keep Watch's rail, and the room over the terminal for the title.
    static let railWidth: CGFloat = 40
    static let titleRoom: CGFloat = 40

    /// Zen is on.
    fileprivate(set) var on = false
    var layout = ZenLayout(rawValue: UserDefaults.standard.string(forKey: ZenModel.layoutKey) ?? "") ?? .centred {
        didSet { if layout != oldValue, !fixture { UserDefaults.standard.set(layout.rawValue, forKey: Self.layoutKey) } }
    }
    /// How many columns of text wide Centred's column is.
    var columns = ZenModel.savedColumns() {
        didSet { if columns != oldValue, !fixture { UserDefaults.standard.set(columns, forKey: Self.columnsKey) } }
    }
    /// Zen takes the window full screen, onto a desktop of its own; off, it
    /// stays in the window.
    var fullScreen = UserDefaults.standard.object(forKey: ZenModel.screenKey) as? Bool ?? true {
        didSet { if fullScreen != oldValue, !fixture { UserDefaults.standard.set(fullScreen, forKey: Self.screenKey) } }
    }
    /// Zen's own text size, which ⌘+ and ⌘- change while zen is on; nil
    /// keeps the window's.
    var textSize = ZenModel.savedText() {
        didSet {
            guard textSize != oldValue, !fixture else { return }
            if let t = textSize { UserDefaults.standard.set(t, forKey: Self.textKey) } else { UserDefaults.standard.removeObject(forKey: Self.textKey) }
        }
    }
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
        useZenText(false)
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
    /// for, such as the right sidebar or the panes: zen ends, and the part
    /// shows as asked. Whether zen was on. What an agent asks for, such as a
    /// card of `irie ctl open`, waits behind zen instead and shows once zen
    /// ends.
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

/// Keep Watch's rail: every session the sidebar draws, in its order, as its
/// card's mark in its own colours, the one on show ringed in its frame's
/// colour. A click shows one, still in zen. Over them, a count of those that
/// need you goes to the first, as the title bar's does.
struct ZenRail: View {
    @Environment(Store.self) private var store

    var body: some View {
        let shown = store.selection
        let needs = store.counts.needs
        VStack(spacing: 4) {
            if needs.count > 0 {
                Button {
                    if let id = needs.first { store.show(id, focus: true) }
                } label: {
                    HStack(spacing: 2) {
                        StateMark(look: .needs, size: 9)
                        Text("\(needs.count)").font(Theme.font(10, .bold)).monospacedDigit()
                    }
                    .foregroundStyle(Theme.needs)
                    .padding(.horizontal, 4)
                    .padding(.vertical, 1.5)
                    .background(Capsule().fill(Theme.needs.opacity(0.12)))
                    .overlay(Capsule().strokeBorder(Theme.needs.opacity(0.4), lineWidth: 1))
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .help(needs.count == 1 ? "1 needs you" : "\(needs.count) need you")
                .accessibilityLabel(needs.count == 1 ? "1 needs you" : "\(needs.count) need you")
                .padding(.top, 8)
            }
            ScrollView(.vertical, showsIndicators: false) {
                VStack(spacing: 2) {
                    ForEach(store.drawnOrder) { s in
                        ZenRailMark(mark: .init(s), shown: s.id == shown) { store.show(s.id, focus: true) }
                            .equatable()
                    }
                }
                .padding(.vertical, 6)
                .frame(maxWidth: .infinity)
            }
        }
        .frame(width: ZenModel.railWidth)
        .frame(maxHeight: .infinity, alignment: .top)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.sidebar))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Sessions")
    }
}

/// One session on the rail. Its inputs are plain values, so a row's news
/// redraws only the marks it changes.
private struct ZenRailMark: View, Equatable {
    struct Mark: Equatable {
        let name: String
        let look: Look
        let label: String
        let tint: Color?
        let edge: Color

        @MainActor
        init(_ s: Session) {
            name = s.name
            look = Look(s)
            label = Look.label(s)
            tint = Look.agentTint(s)
            edge = Look.edge(s)
        }
    }

    let mark: Mark
    let shown: Bool
    let action: () -> Void

    static func == (a: Self, b: Self) -> Bool { a.mark == b.mark && a.shown == b.shown }

    var body: some View {
        let box = RoundedRectangle(cornerRadius: 8, style: .continuous)
        Button(action: action) {
            StateMark(look: mark.look, size: 13, tint: mark.tint)
                .frame(width: 30, height: 26)
                .background {
                    if shown {
                        box.fill(Theme.selection)
                            .overlay(box.strokeBorder(mark.edge, lineWidth: 1.5))
                    }
                }
                .opacity(mark.look.quiet && !shown ? 0.6 : 1)
                .contentShape(box)
        }
        .buttonStyle(.plain)
        .help("\(mark.name) · \(mark.label)")
        .accessibilityLabel("\(mark.name), \(mark.label)")
        .accessibilityAddTraits(shown ? .isSelected : [])
    }
}

/// The session's name and state over its terminal in Centred and Keep
/// Watch, as the title bar has them outside zen, and quieter than what they
/// head (Roots' principles §2.9).
struct ZenTitle: View {
    let session: Session?

    var body: some View {
        if let s = session {
            HStack(spacing: 6) {
                StateMark(look: Look(s), size: 11, tint: Look.agentTint(s))
                Text(s.name)
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(Theme.ink)
                Text(TerminalArea.subtitle(s))
                    .font(.system(size: 11))
                    .foregroundStyle(Look.agentInk(s) ?? Theme.muted)
            }
            .lineLimit(1)
            .accessibilityElement(children: .combine)
        }
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
            // The name and state: a mark and two short lines.
            func title(x: CGFloat, centred: Bool) {
                let total: CGFloat = 4 + 3 + 18 + 3 + 26
                let x0 = centred ? (w - total) / 2 : x
                ctx.fill(Path(ellipseIn: CGRect(x: x0, y: 6, width: 4, height: 4)), with: .color(Theme.agent(slot: 1)))
                ctx.fill(Path(roundedRect: CGRect(x: x0 + 7, y: 7, width: 18, height: 2), cornerRadius: 1), with: .color(Theme.ink.opacity(0.7)))
                ctx.fill(Path(roundedRect: CGRect(x: x0 + 28, y: 7, width: 26, height: 2), cornerRadius: 1), with: .color(Theme.muted.opacity(0.7)))
            }
            switch layout {
            case .centred:
                let cw = w * 0.5
                title(x: 0, centred: true)
                frame(CGRect(x: (w - cw) / 2, y: 15, width: cw, height: h - 22))
            case .edge:
                text(in: CGRect(x: 2, y: 2, width: w - 4, height: h - 4))
            case .watch:
                let rail = CGRect(x: 5, y: 5, width: 12, height: h - 10)
                ctx.fill(Path(roundedRect: rail, cornerRadius: 4), with: .color(Theme.sidebar))
                ctx.stroke(Path(roundedRect: rail, cornerRadius: 4), with: .color(Theme.edge), lineWidth: 0.8)
                let marks: [Color] = [Theme.agent(slot: 1), Theme.needs, Theme.agent(slot: 4), Look.doneColor, Theme.muted]
                for (i, c) in marks.enumerated() {
                    let y = rail.minY + 6 + CGFloat(i) * 8
                    if i == 0 {
                        ctx.stroke(Path(roundedRect: CGRect(x: rail.minX + 1.5, y: y - 2.5, width: rail.width - 3, height: 8), cornerRadius: 2.5),
                                   with: .color(Theme.agent(slot: 1)), lineWidth: 0.8)
                    }
                    ctx.fill(Path(ellipseIn: CGRect(x: rail.midX - 1.5, y: y, width: 3, height: 3)), with: .color(c))
                }
                title(x: 22, centred: false)
                frame(CGRect(x: 22, y: 15, width: w - 27, height: h - 20))
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
    }
}
