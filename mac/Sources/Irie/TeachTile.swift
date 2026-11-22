import SwiftUI

/// The Teach tile: a picture of what you want explained, in a frosted tile
/// over the window with the soft edge of the look-ahead at keys
/// (Frosted.swift). A local model makes it, as it makes every materialized
/// card, so it costs no tokens; the core keeps it for the question, so the
/// same question asked again draws at once (docs/proposals/materialized-
/// cards.md §4). Asked while a session is selected, it is made from that
/// session's conversation too, so a question about its work is answered
/// from what happened. Help › Teach Tile (⌃⌘T) or Fn+T shows and hides it;
/// its head moves it, wherever in the window, and × or Esc closes it.
@MainActor
@Observable
final class TeachTile {
    static let width: CGFloat = 420
    /// The least room it keeps from the window's sides and foot, and from
    /// its top, where the toolbar would take a click on its head as a drag
    /// of the window.
    static let margin: CGFloat = 12
    static let top: CGFloat = 52
    /// The least of it kept in the window under its top, so its head can
    /// always be taken hold of.
    static let grip: CGFloat = 120
    /// The room its card leaves at the window's foot, over the phrase bar.
    static let foot: CGFloat = 48
    /// Its head, field and padding: the room in it that is not the card.
    static let chrome: CGFloat = 112

    static let shownKey = "teachTileShown"
    static let placeKey = "teachTilePlace"
    static let recentKey = "teachTileRecent"
    static let mostRecent = 6

    /// Shown, as it was when the app last ran.
    var shown: Bool {
        didSet {
            if shown != oldValue, !fixture { UserDefaults.standard.set(shown, forKey: Self.shownKey) }
            if shown, !oldValue { wantsKeys += 1 }
        }
    }

    /// Where its top left corner sits, as a share of the window's room
    /// across and down, so it keeps its place as the window changes size;
    /// nil until it is moved, while it sits at the top right. Its top, not
    /// its middle: the tile is as tall as its card, which is known only
    /// once drawn, and a place that waited on that would jump.
    private(set) var place: CGPoint? {
        didSet {
            guard !fixture else { return }
            UserDefaults.standard.set(place.map { [$0.x, $0.y] }, forKey: Self.placeKey)
        }
    }

    /// What its field holds.
    var text = ""
    /// The question the card on show answers, and the session it was asked
    /// in, by id and name.
    private(set) var asked: String?
    private(set) var askedIn: (id: String, name: String)?
    /// The card, as the core last gave it.
    private(set) var result: MaterializeResult?
    /// An ask under way, while the card is made.
    private(set) var asking = false
    /// Why there is no card, when the ask itself failed.
    private(set) var problem: String?
    /// The questions asked last, newest first, to ask again with a click.
    private(set) var recent: [String]
    /// Counts up when its field should take the keyboard.
    private(set) var wantsKeys = 0

    @ObservationIgnored private var fixture = false
    @ObservationIgnored private var pending: Task<Void, Never>?

    init() {
        let d = UserDefaults.standard
        shown = d.bool(forKey: Self.shownKey)
        recent = d.stringArray(forKey: Self.recentKey) ?? []
        if let p = d.array(forKey: Self.placeKey) as? [Double], p.count == 2 {
            place = CGPoint(x: p[0], y: p[1])
        }
    }

    func toggle() { shown.toggle() }

    func close() { shown = false }

    /// Asks for the card that explains q, made from the selected session's
    /// conversation when one is selected, and shows it; while a local
    /// model makes it, the tile says so, and draws it once it is made.
    /// Again drops the kept card first, so it is made anew.
    func ask(_ q: String, store: Store, again: Bool = false) {
        let q = q.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else { return }
        shown = true
        text = q
        asked = q
        askedIn = store.selectedRow.map { ($0.id, $0.name) }
        problem = nil
        // Another question's card goes; the same one's stays on show while
        // it is made again.
        if !again { result = nil }
        remember(q)
        guard store.canExplain else {
            result = nil
            problem = "This \(Brand.name)’s core is older than the Teach tile. Install the new build to use it."
            return
        }
        let session = askedIn?.id
        pending?.cancel()
        pending = Task { [weak self] in
            guard let self else { return }
            asking = true
            defer { if self.asked == q { self.asking = false } }
            do {
                if again { _ = try await store.explainCard(q, session: session, forget: true) }
                var r = try await store.explainCard(q, session: session)
                guard asked == q, !Task.isCancelled else { return }
                result = r
                if r.state == "cold" || r.state == "refreshing", r.err == nil || r.began == true {
                    // A remake runs: wait for it, as long as a local model
                    // may take, while the tile says it is being made.
                    r = try await store.explainCard(q, session: session, wait: 240)
                    guard asked == q, !Task.isCancelled else { return }
                    result = r
                }
            } catch {
                guard asked == q else { return }
                problem = error.localizedDescription
            }
        }
    }

    /// The card on show, made again.
    func again(store: Store) {
        guard let q = asked else { return }
        ask(q, store: store, again: true)
    }

    private func remember(_ q: String) {
        let one = q.replacingOccurrences(of: "\n", with: " ")
        recent.removeAll { $0 == one }
        recent.insert(one, at: 0)
        recent = Array(recent.prefix(Self.mostRecent))
        if !fixture { UserDefaults.standard.set(recent, forKey: Self.recentKey) }
    }

    /// Its top left corner in a window's room: where it was put, else at
    /// the top right, below the toolbar.
    func origin(in room: CGSize) -> CGPoint {
        let p = place.map { CGPoint(x: $0.x * room.width, y: $0.y * room.height) }
            ?? CGPoint(x: room.width - Self.width - 28, y: 60)
        return Self.clamp(p, in: room)
    }

    /// The tile put down after a drag by moved, from where it was.
    func moved(by moved: CGSize, in room: CGSize) {
        guard room.width > 0, room.height > 0 else { return }
        let o = origin(in: room)
        let p = Self.clamp(CGPoint(x: o.x + moved.width, y: o.y + moved.height), in: room)
        place = CGPoint(x: p.x / room.width, y: p.y / room.height)
    }

    /// A top left corner that keeps the tile's width in the room, margin in
    /// from its sides, and its top below the toolbar with grip of it in the
    /// room under that; in a room too small for either, as far in as it
    /// goes.
    static func clamp(_ p: CGPoint, in room: CGSize) -> CGPoint {
        CGPoint(x: min(max(p.x, margin), max(room.width - width - margin, margin)),
                y: min(max(p.y, top), max(room.height - grip, top)))
    }

    /// The most room its card may take, below a top at y: the rest of the
    /// window under the tile's head and field, past which the card scrolls.
    static func most(below y: CGFloat, in room: CGSize) -> CGFloat {
        max(room.height - y - foot - chrome, 60)
    }

    /// The tile as the snapshots draw it: shown, at its place, with a card
    /// made, and nothing saved.
    func loadFixture(shown: Bool, asked: String?, in session: (String, String)?, result: MaterializeResult?, recent: [String] = []) {
        fixture = true
        pending?.cancel()
        self.shown = shown
        self.asked = asked
        self.text = asked ?? ""
        self.askedIn = session.map { (id: $0.0, name: $0.1) }
        self.result = result
        self.recent = recent
        self.asking = false
        self.problem = nil
        self.place = nil
    }
}

/// The tile, drawn over the window wherever it was put. Only the tile takes
/// clicks: the rest of the window goes on as if it were not there.
@MainActor
struct TeachTileView: View {
    @Environment(Store.self) private var store
    let tile: TeachTile
    @Environment(\.accessibilityReduceMotion) private var still
    @FocusState private var typing: Bool
    @State private var cardHeight: CGFloat = 0
    @GestureState private var drag = CGSize.zero
    @State private var grabbing = false

    var body: some View {
        GeometryReader { geo in
            let room = geo.size
            ZStack(alignment: .topLeading) {
                if tile.shown {
                    // Where it was put, moved by a drag under way. Its card
                    // keeps the height it had where it was, so it does not
                    // grow and shrink under the pointer.
                    let o = tile.origin(in: room)
                    let at = TeachTile.clamp(CGPoint(x: o.x + drag.width, y: o.y + drag.height), in: room)
                    panel(room: room, most: TeachTile.most(below: o.y, in: room))
                        .offset(x: at.x, y: at.y)
                        .transition(still ? .opacity : .opacity.combined(with: .scale(scale: 0.97, anchor: .topTrailing)))
                }
            }
            .frame(width: room.width, height: room.height, alignment: .topLeading)
            .animation(.easeOut(duration: 0.14), value: tile.shown)
        }
        // A word to explain from a terminal's menu (TerminalSelection).
        .onReceive(NotificationCenter.default.publisher(for: .irieExplain)) { note in
            if let text = note.userInfo?["text"] as? String { tile.ask(text, store: store) }
        }
        .onChange(of: tile.wantsKeys) { typing = true }
    }

    private func panel(room: CGSize, most: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            head(room: room)
            field
            content(most: most)
        }
        .padding(.horizontal, 16)
        .padding(.top, 12)
        .padding(.bottom, 14)
        .frame(width: TeachTile.width, alignment: .leading)
        .frostedPanel()
        .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .onExitCommand { closeAndGiveBackKeys() }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Teach tile")
    }

    /// Its name, who made the card and when, and its two buttons. A drag
    /// on it moves the tile.
    private func head(room: CGSize) -> some View {
        HStack(spacing: 7) {
            Image(systemName: "graduationcap.fill")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.accentInk)
            Text("Teach").font(Theme.font(13, .bold)).foregroundStyle(Theme.ink)
            Text(caption).font(Theme.font(11)).foregroundStyle(Theme.muted).lineLimit(1)
            Spacer(minLength: 4)
            if tile.asked != nil {
                Button { tile.again(store: store) } label: {
                    Image(systemName: "arrow.clockwise").font(.system(size: 11, weight: .semibold))
                }
                .buttonStyle(.borderless)
                .foregroundStyle(Theme.muted)
                .disabled(tile.asking)
                .help("Make the picture again")
                .accessibilityLabel("Make it again")
            }
            Button { closeAndGiveBackKeys() } label: {
                Image(systemName: "xmark").font(.system(size: 11, weight: .bold))
            }
            .buttonStyle(.borderless)
            .foregroundStyle(Theme.muted)
            .help("Close the Teach tile (⌃⌘T, Esc)")
            .accessibilityLabel("Close")
        }
        .frame(minHeight: 20)
        .contentShape(Rectangle())
        .gesture(
            DragGesture(minimumDistance: 2, coordinateSpace: .global)
                .updating($drag) { v, state, _ in state = v.translation }
                .onEnded { v in tile.moved(by: v.translation, in: room) }
        )
        .onHover { inside in
            if inside, !grabbing { NSCursor.openHand.push(); grabbing = true }
            if !inside, grabbing { NSCursor.pop(); grabbing = false }
        }
        .onDisappear { if grabbing { NSCursor.pop(); grabbing = false } }
    }

    /// Who made the card, the session it was made from and when; that it is
    /// being made; and that it costs no tokens.
    private var caption: String {
        var words: [String] = []
        if let s = tile.askedIn { words.append("from \(s.name)") }
        if tile.asking && tile.result?.spec == nil {
            words.append("drawing")
        } else if let r = tile.result, let by = r.by, let made = r.made {
            words.append("\(by), \(Age.short(made, now: Date()))")
            if r.state == "refreshing" { words.append("updating") }
        }
        if tile.asked != nil { words.append("no tokens") }
        return words.joined(separator: " · ")
    }

    private var field: some View {
        @Bindable var tile = tile
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "sparkle.magnifyingglass")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(Theme.muted)
            TextField("What do you want explained?", text: $tile.text, axis: .vertical)
                .textFieldStyle(.plain)
                .font(Theme.font(12.5))
                .lineLimit(1...4)
                .focused($typing)
                .onSubmit { tile.ask(tile.text, store: store) }
            if !tile.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, tile.text != tile.asked || tile.result == nil && !tile.asking {
                Button("Explain") { tile.ask(tile.text, store: store) }
                    .buttonStyle(.borderless)
                    .font(Theme.font(11.5, .semibold))
                    .foregroundStyle(Theme.accentInk)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.field))
        .overlay(RoundedRectangle(cornerRadius: 9, style: .continuous)
            .strokeBorder(typing ? Theme.sunEdge : Theme.edge, lineWidth: typing ? 1.5 : 1))
    }

    /// The card, as tall as it is up to most, past which it scrolls; else
    /// what the tile is doing, or how to begin.
    @ViewBuilder private func content(most: CGFloat) -> some View {
        if let card = tile.result?.spec {
            ScrollView(.vertical, showsIndicators: cardHeight > most) {
                GenCardBody(card: card, spacing: 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .textSelection(.enabled)
                    .background(GeometryReader { g in Color.clear.preference(key: CardHeight.self, value: g.size.height) })
            }
            .frame(height: min(max(cardHeight, 40), most))
            .onPreferenceChange(CardHeight.self) { cardHeight = $0 }
        } else if tile.asking || tile.result?.state == "cold" && tile.result?.err == nil && tile.result?.began == true {
            HStack(spacing: 8) {
                Image(systemName: "sparkles")
                    .foregroundStyle(Theme.accentInk)
                    .symbolEffect(.pulse, isActive: !still)
                Text("A local model is drawing it. It costs no tokens, and is kept, so asking again shows it at once.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        } else if let why = tile.problem ?? tile.result?.err {
            Text("No picture yet: \(why)")
                .font(Theme.font(11.5))
                .foregroundStyle(Theme.muted)
                .lineLimit(4)
                .fixedSize(horizontal: false, vertical: true)
        } else {
            VStack(alignment: .leading, spacing: 8) {
                Text("Ask about anything: a word, a step the agent took, how two parts fit. A local model draws it, so it costs no tokens. In a terminal, select text and choose Explain from its menu.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                if !tile.recent.isEmpty { recents }
            }
        }
    }

    /// The questions asked last, to ask again.
    private var recents: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Asked before").font(Theme.font(10.5, .semibold)).foregroundStyle(Theme.muted)
            ForEach(tile.recent.prefix(4), id: \.self) { q in
                Button { tile.ask(q, store: store) } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "clock.arrow.circlepath").font(.system(size: 10)).foregroundStyle(Theme.muted)
                        Text(q).font(Theme.font(11.5)).foregroundStyle(Theme.ink).lineLimit(1).truncationMode(.tail)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func closeAndGiveBackKeys() {
        let had = typing
        tile.close()
        if had { if store.showTerminal { store.focusTerminal += 1 } else { store.focusMessage += 1 } }
    }
}

private struct CardHeight: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

extension Notification.Name {
    /// Asks the Teach tile to explain what userInfo's "text" holds, as a
    /// terminal's Explain does (TerminalSelection).
    static let irieExplain = Notification.Name("irieExplain")
}
