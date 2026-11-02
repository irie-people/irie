import AppKit
import SwiftUI

// The right sidebar (docs/proposals/right-dock.md): tabs about the session
// on show, opened and closed as a whole. Over the terminal it opens as a
// peek, drawn over the terminal's right edge, so the terminal keeps its size.
// Pinned, it sits beside the terminal, which narrows once, and every agent
// follows once, as when the window is resized: the terminal's width is the
// slot's, and every agent window takes the slot's size (DESIGN §2.3). Beside
// the conversation it always sits alongside, which costs nothing, while the
// terminal under the conversation keeps its own width. The sidebar keeps its
// tab and width as the selection changes, and only the user opens it or
// switches its tab: a helper starting only puts a count on a tab.

/// A tab of the right sidebar, in the order the proposal gives them, which
/// also gives each its key: ⌥⌘1 to ⌥⌘5.
enum SideTab: String, CaseIterable, Identifiable {
    case dock, changes, helpers, plan, review

    var id: String { rawValue }

    var title: String {
        switch self {
        case .dock: return "Dock"
        case .changes: return "Changes"
        case .helpers: return "Helpers"
        case .plan: return "Plan"
        case .review: return "Review"
        }
    }

    /// The tabs this build has, in their order. The others come with the
    /// core features they need.
    static let built: [SideTab] = [.dock, .helpers, .review]

    /// Its key with ⌥⌘, by its place among all the tabs, so a key keeps its
    /// tab as more tabs arrive.
    var key: KeyEquivalent {
        KeyEquivalent(Character(String((Self.allCases.firstIndex(of: self) ?? 0) + 1)))
    }
}

/// What the right sidebar shows and how, kept across launches.
struct SideState: Equatable {
    var open = false
    var pinned = false
    var width = SideState.startWidth
    var tab = SideTab.dock

    static let startWidth = 340.0
    static let minWidth = 260.0
    static let maxWidth = 520.0

    static func clamp(_ w: Double) -> Double { min(max(w, minWidth), maxWidth) }

    private static let openKey = "rightSidebarOpen"
    private static let pinnedKey = "rightSidebarPinned"
    private static let widthKey = "rightSidebarWidth"
    private static let tabKey = "rightSidebarTab"

    static func saved() -> SideState {
        let d = UserDefaults.standard
        var s = SideState()
        s.open = d.bool(forKey: openKey)
        s.pinned = d.bool(forKey: pinnedKey)
        let w = d.double(forKey: widthKey)
        if w > 0 { s.width = clamp(w) }
        if let t = d.string(forKey: tabKey).flatMap(SideTab.init(rawValue:)), SideTab.built.contains(t) { s.tab = t }
        return s
    }

    func save() {
        let d = UserDefaults.standard
        d.set(open, forKey: Self.openKey)
        d.set(pinned, forKey: Self.pinnedKey)
        d.set(width, forKey: Self.widthKey)
        d.set(tab.rawValue, forKey: Self.tabKey)
    }
}

extension Store {
    /// View > Show Right Sidebar (⌥⌘B), and the toolbar's button.
    func toggleSide() {
        if side.open { closeSide() } else { openSide(side.tab) }
    }

    /// A tab's own key (⌥⌘1 to ⌥⌘5): shows the sidebar on that tab, or
    /// closes it when that tab already shows.
    func toggleSideTab(_ tab: SideTab) {
        if side.open && side.tab == tab { closeSide() } else { openSide(tab) }
    }

    /// Opens the right sidebar on a tab.
    func openSide(_ tab: SideTab) {
        var s = side
        s.open = true
        s.tab = SideTab.built.contains(tab) ? tab : SideTab.built[0]
        change(s)
    }

    func closeSide() {
        var s = side
        s.open = false
        change(s)
    }

    /// Pinned, the sidebar sits beside the terminal, which narrows; unpinned,
    /// it lies over the terminal's right edge.
    func pinSide(_ on: Bool) {
        var s = side
        s.pinned = on
        change(s, slides: false)
    }

    func setSideWidth(_ width: Double) {
        var s = side
        s.width = SideState.clamp(width)
        change(s, slides: false)
    }

    /// Whether the right sidebar is open and lies over the terminal, which a
    /// click in the terminal then closes (KeyRouter).
    var sidePeeking: Bool { side.open && sidePeekFrame != .zero && showTerminal }

    /// Applies a change. Only a peek slides in and out: anything that changes
    /// the terminal's width happens in one step, since each step of an
    /// animation would resize the shown agent, and every agent at each of
    /// the core's ticks it spanned.
    private func change(_ s: SideState, slides: Bool = true) {
        if slides && !s.pinned && !side.pinned {
            withAnimation(.easeOut(duration: 0.16)) { side = s }
        } else {
            var t = Transaction(animation: nil)
            t.disablesAnimations = true
            withTransaction(t) { side = s }
        }
    }

    /// Everything ready for the user to look at (the Review tab): the
    /// sessions to verify, finished and not parked, in the list's order,
    /// then those that closed on their own in the last day and were not
    /// dropped, oldest first.
    func reviewRows(now: Date, dropped: Set<String>) -> [ReviewRow] {
        var rows: [ReviewRow] = sessions.filter { Look($0) == .done && !$0.isParked }.map { s in
            let detail = s.detail.flatMap { $0.isEmpty ? nil : $0 } ?? Look.label(s)
            return ReviewRow(id: s.id, name: s.name, detail: detail, notThrough: s.loose != nil, closed: nil)
        }
        if closeWhenDone {
            let gone = closed
                .filter { $0.auto == true && now.timeIntervalSince($0.closed) < 24 * 3600 && !dropped.contains($0.id) }
                .reversed()
            rows += gone.map { c in
                ReviewRow(id: c.id, name: c.name, detail: "Closed on its own " + Age.spoken(c.closed, now: now), notThrough: false, closed: c)
            }
        }
        return rows
    }
}

/// One row of the Review tab.
struct ReviewRow: Identifiable, Equatable {
    var id: String
    var name: String
    var detail: String
    /// Its work has not all landed (the jot dog's reading, DESIGN §4.8).
    var notThrough: Bool
    /// Set for a session that closed on its own, which a click reopens.
    var closed: ClosedSession?
}

/// The right sidebar: a row of tabs, the pin and a close button, then the
/// tab that shows.
@MainActor
struct RightSidebar: View {
    @Environment(Store.self) private var store
    /// Drawn over the terminal rather than beside it.
    let peek: Bool
    /// The width a drag of its edge is asking for, while it lasts.
    @Binding var guide: Double?

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: Theme.frameRadius, style: .continuous) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rectangle().fill(Theme.edge).frame(height: 1)
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        }
        .clipShape(shape)
        .background(shape.fill(Theme.panel)
            .shadow(color: .black.opacity(peek ? 0.24 : 0.06), radius: peek ? 14 : 2, x: peek ? -3 : 0, y: peek ? 3 : 0.5))
        .overlay(shape.strokeBorder(Theme.edge, lineWidth: 1))
        .overlay(alignment: .leading) { SideEdge(guide: $guide).offset(x: -5) }
        // Esc closes a peek, which changes no size. A pinned sidebar stays,
        // since closing it would resize every agent: the keyboard goes back.
        .onExitCommand {
            if peek {
                store.closeSide()
            } else if store.showTerminal {
                store.focusTerminal += 1
            } else {
                store.focusMessage += 1
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Right sidebar, \(store.side.tab.title)")
    }

    private var header: some View {
        HStack(spacing: 2) {
            ForEach(SideTab.built) { t in tabButton(t) }
            Spacer(minLength: 4)
            iconButton(store.side.pinned ? "pin.fill" : "pin",
                       help: store.side.pinned ? "Unpin: lie over the terminal, which then keeps its full width"
                           : "Pin: stay beside the terminal, which narrows once, and every agent with it",
                       label: store.side.pinned ? "Unpin the right sidebar" : "Pin the right sidebar",
                       on: store.side.pinned) { store.pinSide(!store.side.pinned) }
            iconButton("xmark", help: "Close the right sidebar (⌥⌘B)", label: "Close the right sidebar") { store.closeSide() }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 7)
    }

    private func tabButton(_ t: SideTab) -> some View {
        let on = store.side.tab == t
        let n = count(t)
        return Button { store.openSide(t) } label: {
            HStack(spacing: 4) {
                Text(t.title)
                    .font(Theme.font(12, on ? .bold : .medium))
                    .lineLimit(1)
                    .fixedSize()
                if n.count > 0 {
                    Text("\(n.count)")
                        .font(Theme.font(10, .bold))
                        .monospacedDigit()
                        .foregroundStyle(n.needsYou ? Theme.onAccent : (on ? Theme.ink : Theme.muted))
                        .padding(.horizontal, 5)
                        .background(Capsule().fill(n.needsYou ? Theme.needs : (on ? Theme.card : Theme.edge)))
                }
            }
            .foregroundStyle(on ? Theme.ink : Theme.muted)
            .padding(.horizontal, 7)
            .padding(.vertical, 4)
            .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(on ? Theme.selection : Color.clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help("\(t.title) (⌥⌘\(String(t.key.character)))")
        .accessibilityLabel(t.title + (n.count > 0 ? ", \(n.count)" : "") + (n.needsYou ? ", needs you" : ""))
        .accessibilityAddTraits(on ? [.isSelected] : [])
    }

    private func iconButton(_ symbol: String, help: String, label: String, on: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 11, weight: .semibold))
                .rotationEffect(.degrees(symbol.hasPrefix("pin") ? 45 : 0))
                .foregroundStyle(on ? Theme.accentInk : Theme.muted)
                .frame(width: 22, height: 22)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(label)
    }

    /// A tab's count, and whether something in it needs the user, which
    /// alone turns it red.
    private func count(_ t: SideTab) -> (count: Int, needsYou: Bool) {
        switch t {
        case .helpers:
            guard store.outsideSelection == nil, let s = store.sessions.first(where: { $0.id == store.selection }) else { return (0, false) }
            let running = s.subagents ?? []
            return (running.count, running.contains { $0.needsYou == true })
        case .review:
            return (store.reviewRows(now: Date(), dropped: ReviewTab.dropped).count, false)
        default:
            return (0, false)
        }
    }

    @ViewBuilder private var content: some View {
        switch store.side.tab {
        case .dock: DockTab()
        case .review: ReviewTab()
        default: HelpersTab()
        }
    }
}

/// The sidebar's leading edge, which a drag moves. A guide line shows where
/// it would go, and the width applies on release: for a pinned sidebar that
/// is one resize of the terminal, and of every agent, not one per frame.
@MainActor
private struct SideEdge: View {
    @Environment(Store.self) private var store
    @Binding var guide: Double?

    var body: some View {
        Color.clear
            .frame(width: 10)
            .contentShape(Rectangle())
            .onHover { inside in
                if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
            }
            .gesture(DragGesture(minimumDistance: 1, coordinateSpace: .global)
                .onChanged { g in guide = SideState.clamp(store.side.width - Double(g.translation.width)) }
                .onEnded { _ in
                    if let g = guide { store.setSideWidth(g) }
                    guide = nil
                })
            .onTapGesture(count: 2) { store.setSideWidth(SideState.startWidth) }
            .help("Drag to make the right sidebar wider or narrower; double-click for its first width")
            .accessibilityHidden(true)
    }
}

/// A quiet page in a tab with nothing to show yet.
struct SideNote: View {
    let title: String
    let text: String

    var body: some View {
        VStack(spacing: 8) {
            Text(title)
                .font(Theme.font(14, .bold))
                .foregroundStyle(Theme.ink)
            Text(text)
                .font(Theme.font(12))
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: 280)
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The Helpers tab: the shown session's helpers, the open one's work under a
/// strip of them all (HelperPane.swift), in either view, so a helper shows
/// beside the terminal as well as the conversation.
@MainActor
struct HelpersTab: View {
    @Environment(Store.self) private var store
    /// The shown session's conversation, which names its helpers.
    @StateObject private var parent = ConversationModel()

    private var selected: Session? {
        store.outsideSelection == nil ? store.sessions.first { $0.id == store.selection } : nil
    }

    var body: some View {
        Group {
            if let s = selected {
                if let agent = store.openHelpers[s.id] ?? s.subagents?.last?.agent ?? HelperRef.list(parent.items, s).last?.agent {
                    HelperPane(session: s, agent: agent, parent: parent, style: store.helperStyle, embedded: true)
                } else {
                    SideNote(title: "No helpers yet",
                             text: "When \(s.name) starts a helper, it shows here: what it was asked, what it does now and what it found.")
                }
            } else {
                SideNote(title: "Pick a session", text: "Its helpers show here.")
            }
        }
        .onAppear { parent.follow(store.selection, client: store.client) }
        .onChange(of: store.selection) { parent.follow(store.selection, client: store.client) }
    }
}

/// The Review tab: everything ready for the user to look at. A row joins at
/// the end and leaves only when the user acts on it, so nothing slides under
/// the pointer.
@MainActor
struct ReviewTab: View {
    @Environment(Store.self) private var store
    /// The order rows first showed in, newline-separated, so a new row joins
    /// at the end.
    @AppStorage("reviewOrder") private var orderRaw = ""
    /// Closed sessions dropped here or from Ready to review, which share it
    /// (ReviewDock.swift).
    @AppStorage("reviewDockSeen") private var seenRaw = ""

    /// The closed sessions dropped from either list.
    static var dropped: Set<String> {
        Set((UserDefaults.standard.string(forKey: "reviewDockSeen") ?? "").split(separator: "\n").map(String.init))
    }

    private var order: [String] { orderRaw.split(separator: "\n").map(String.init) }

    var body: some View {
        TimelineView(.periodic(from: .now, by: 30)) { ctx in
            let rows = arranged(store.reviewRows(now: ctx.date, dropped: Set(seenRaw.split(separator: "\n").map(String.init))))
            Group {
                if rows.isEmpty {
                    SideNote(title: "Nothing to review",
                             text: "Finished work shows here, a row each, until you look at it, park it or drop it.")
                } else {
                    ScrollView {
                        VStack(spacing: 4) {
                            ForEach(rows) { row($0) }
                        }
                        .padding(10)
                    }
                }
            }
            .onAppear { remember(rows.map(\.id)) }
            .onChange(of: rows.map(\.id)) { _, ids in remember(ids) }
        }
    }

    /// The rows in the order they first showed, new ones at the end.
    private func arranged(_ rows: [ReviewRow]) -> [ReviewRow] {
        let place = Dictionary(order.enumerated().map { ($1, $0) }, uniquingKeysWith: { first, _ in first })
        return rows.enumerated().sorted { a, b in
            let pa = place[a.element.id] ?? Int.max, pb = place[b.element.id] ?? Int.max
            return pa != pb ? pa < pb : a.offset < b.offset
        }.map(\.element)
    }

    /// Notes the order of rows not seen before, keeping the list bounded.
    private func remember(_ ids: [String]) {
        let known = Set(order)
        let new = ids.filter { !known.contains($0) }
        guard !new.isEmpty else { return }
        orderRaw = (order + new).suffix(300).joined(separator: "\n")
    }

    private func drop(_ id: String) {
        var lines = seenRaw.split(separator: "\n").map(String.init)
        guard !lines.contains(id) else { return }
        lines.append(id)
        seenRaw = lines.suffix(200).joined(separator: "\n")
    }

    private func row(_ r: ReviewRow) -> some View {
        let session = store.sessions.first { $0.id == r.id }
        let shown = store.selection == r.id && store.outsideSelection == nil
        return HStack(spacing: 8) {
            Button {
                if let c = r.closed {
                    drop(c.id)
                    store.reopen(c)
                } else {
                    store.show(r.id)
                }
            } label: {
                HStack(alignment: .top, spacing: 8) {
                    StateMark(look: .done, size: 12)
                        .padding(.top, 2)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(r.name)
                            .font(Theme.font(12.5, .semibold))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                        Text(r.detail)
                            .font(Theme.font(11.5))
                            .foregroundStyle(r.notThrough ? Theme.accentInk : Theme.muted)
                            .lineLimit(2)
                    }
                    Spacer(minLength: 4)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(r.closed != nil ? "Reopen \(r.name) to review it" : "Show \(r.name)")
            .accessibilityLabel("\(r.name), \(r.detail)")
            .accessibilityHint(r.closed != nil ? "Reopens it" : "Shows it")
            if let c = r.closed {
                Button { drop(c.id) } label: {
                    Image(systemName: "xmark").font(.system(size: 9, weight: .bold)).foregroundStyle(Theme.muted)
                        .frame(width: 18, height: 18)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Drop it from this list (it stays in history)")
                .accessibilityLabel("Drop \(r.name)")
            } else if let s = session, store.canPark {
                Button { store.setParked(s, true) } label: {
                    Image(systemName: "p.circle").font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.muted)
                        .frame(width: 18, height: 18)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Park it: checked, and out of the way")
                .accessibilityLabel("Park \(r.name)")
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(shown ? Theme.selection : Theme.card))
    }
}
