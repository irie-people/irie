import SwiftUI

/// The Teach tile: a picture of what you want explained, in a frosted tile
/// over the session space with the soft edge of the look-ahead at keys
/// (Frosted.swift). A local model makes it, as it makes every materialized
/// card, so it costs no tokens; the core keeps it for the question, so the
/// same question asked again draws at once (docs/proposals/materialized-
/// cards.md §4). Asked while a session is selected, it is made from that
/// session's conversation too, so a question about its work is answered
/// from what happened. Help › Teach Tile (⌃⌘T) or Fn+T shows and hides it.
/// It sits over the middle of the terminal, in the top of the session
/// space, never over a sidebar, though it may lie over the panes; so its
/// head moves it and its edges resize it, a double-click on its head puts
/// it back, and × or Esc closes it.
@MainActor
@Observable
final class TeachTile {
    /// The least room it keeps from the session space's edges.
    static let margin: CGFloat = 12
    /// The least it can be made, and the share of the session space's
    /// height it takes at most until it is made taller: its top 60 percent.
    static let least = CGSize(width: 360, height: 200)
    static let share: CGFloat = 0.6
    /// Its head, field and padding: the room in it that is not the card.
    static let chrome: CGFloat = 112
    /// How wide a card of pictures has it, and one of words, or none yet,
    /// at most: as wide as the terminal within those.
    static let picturesWidth: CGFloat = 1000
    static let wordsWidth: CGFloat = 680

    static let shownKey = "teachTileShown"
    static let frameKey = "teachTileFrame"
    static let recentKey = "teachTileRecent"
    static let mostRecent = 6

    /// Shown, as it was when the app last ran.
    var shown: Bool {
        didSet {
            if shown != oldValue, !fixture { UserDefaults.standard.set(shown, forKey: Self.shownKey) }
            if shown, !oldValue { wantsKeys += 1 }
        }
    }

    /// Where it was put and how big it was made, as shares of the session
    /// space across and down (x, y, width, height; a height of 0 is as tall
    /// as its card), so it keeps them as the window changes size; nil until
    /// it is moved or resized, while it sits over the middle of the
    /// terminal, at its top.
    private(set) var own: CGRect? {
        didSet {
            guard !fixture else { return }
            UserDefaults.standard.set(own.map { [$0.minX, $0.minY, $0.width, $0.height] }, forKey: Self.frameKey)
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
        if let f = d.array(forKey: Self.frameKey) as? [Double], f.count == 4 {
            own = CGRect(x: f[0], y: f[1], width: f[2], height: f[3])
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

    /// Where the tile goes in area, the session space less any right
    /// sidebar, beside term, the terminal's frame (or the conversation's),
    /// both in one space. Where it was put, kept inside the area (kept);
    /// else over the middle of the terminal, at its top, as wide as the
    /// terminal within the margins but no wider than its card wants, and no
    /// narrower than the least: in a narrow terminal it runs over the panes
    /// beside it, never past the area, so never over a sidebar. Fixed is
    /// whether it was made a height of its own; else its rect's height is
    /// 0, and it is as tall as its card, up to the area's top share.
    static func frame(area: CGRect, term: CGRect, own: CGRect?, wants: CGFloat) -> (rect: CGRect, fixed: Bool) {
        if let o = own {
            let r = CGRect(x: area.minX + o.minX * area.width, y: area.minY + o.minY * area.height,
                           width: o.width * area.width, height: o.height * area.height)
            return (kept(r, fixed: o.height > 0, in: area), o.height > 0)
        }
        let inner = area.insetBy(dx: margin, dy: margin)
        let w = min(max(min(term.width - 2 * margin, wants), least.width), max(inner.width, 0))
        let x = min(max(term.midX - w / 2, inner.minX), max(inner.maxX - w, inner.minX))
        return (CGRect(x: x, y: inner.minY, width: w, height: 0), false)
    }

    /// r kept inside area, margin in from its edges, and no smaller than
    /// the least; one that is not fixed keeps room for the least height.
    static func kept(_ r: CGRect, fixed: Bool, in area: CGRect) -> CGRect {
        let inner = area.insetBy(dx: margin, dy: margin)
        guard inner.width > 0, inner.height > 0 else { return CGRect(x: inner.minX, y: inner.minY, width: max(inner.width, 0), height: 0) }
        var out = r
        out.size.width = min(max(r.width, least.width), inner.width)
        let h = fixed ? min(max(r.height, least.height), inner.height) : min(least.height, inner.height)
        out.origin.x = min(max(r.minX, inner.minX), inner.maxX - out.width)
        out.origin.y = min(max(r.minY, inner.minY), inner.maxY - h)
        out.size.height = fixed ? h : 0
        return out
    }

    /// How wide its card wants it: a card with a picture the widest, one
    /// of words narrower, as is the tile with no card yet.
    static func wants(_ card: GenCard?) -> CGFloat {
        let pictures: Set<String> = ["Flow", "Diagram", "Compare", "Code"]
        return card?.body.contains(where: { pictures.contains($0.type) }) == true ? picturesWidth : wordsWidth
    }

    /// The tile put at r in area, by a move or a stretch; fixed when it was
    /// made a height of its own.
    func place(_ r: CGRect, fixed: Bool, in area: CGRect) {
        guard area.width > 0, area.height > 0 else { return }
        own = CGRect(x: (r.minX - area.minX) / area.width, y: (r.minY - area.minY) / area.height,
                     width: r.width / area.width, height: fixed ? r.height / area.height : 0)
    }

    /// Back over the middle of the terminal, at its top, its own size.
    func recentre() { own = nil }

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
        self.own = nil
    }
}

/// The tile, drawn over the session space where it goes (TeachTile.frame).
/// Only the tile takes clicks: the rest goes on as if it were not there.
@MainActor
struct TeachTileView: View {
    @Environment(Store.self) private var store
    let tile: TeachTile
    /// The terminal's frame (or the conversation's) in the window, and the
    /// right sidebar's width, which lies at the trailing side, or the
    /// leading one when the layout is reversed: the tile never covers it.
    let term: CGRect?
    let side: CGFloat
    let flip: Bool
    @Environment(\.accessibilityReduceMotion) private var still
    @FocusState private var typing: Bool
    @State private var cardHeight: CGFloat = 0
    @State private var drawn: CGFloat = 0
    @GestureState private var change = Change()
    @State private var cursor = false

    /// A move or a stretch under way: the part held, and how far it went.
    struct Change: Equatable {
        var part: Part?
        var by = CGSize.zero
    }

    enum Part: Equatable { case head, left, right, bottom, corner }

    var body: some View {
        GeometryReader { geo in
            let g = geo.frame(in: .global)
            let area = CGRect(x: flip ? side : 0, y: 0, width: max(geo.size.width - side, 0), height: geo.size.height)
            let t = term.map { $0.offsetBy(dx: -g.minX, dy: -g.minY) } ?? area
            ZStack(alignment: .topLeading) {
                if tile.shown, area.width > 0 {
                    let base = TeachTile.frame(area: area, term: t, own: tile.own, wants: TeachTile.wants(tile.result?.spec))
                    let now = changed(base.rect, fixed: base.fixed, by: change, in: area)
                    let most = (now.fixed ? now.rect.height : area.height * TeachTile.share) - TeachTile.chrome
                    panel(width: now.rect.width, height: now.fixed ? now.rect.height : nil, most: max(most, 60))
                        .overlay { handles(base, area: area) }
                        .offset(x: now.rect.minX, y: now.rect.minY)
                        .transition(still ? .opacity : .opacity.combined(with: .scale(scale: 0.98, anchor: .top)))
                }
            }
            .frame(width: geo.size.width, height: geo.size.height, alignment: .topLeading)
            .animation(.easeOut(duration: 0.14), value: tile.shown)
        }
        // A word to explain from a terminal's menu (TerminalSelection).
        .onReceive(NotificationCenter.default.publisher(for: .irieExplain)) { note in
            if let text = note.userInfo?["text"] as? String { tile.ask(text, store: store) }
        }
        .onChange(of: tile.wantsKeys) { typing = true }
    }

    /// base with a move or a stretch applied, kept in the area. A tile as
    /// tall as its card takes the height it was drawn at when its foot is
    /// pulled, and from then has a height of its own.
    private func changed(_ base: CGRect, fixed: Bool, by c: Change, in area: CGRect) -> (rect: CGRect, fixed: Bool) {
        guard let part = c.part else { return (base, fixed) }
        var r = base
        if !fixed { r.size.height = drawn }
        let d = c.by
        var nowFixed = fixed
        switch part {
        case .head:
            r.origin.x += d.width
            r.origin.y += d.height
        case .left:
            r.origin.x += d.width
            r.size.width -= d.width
        case .right:
            r.size.width += d.width
        case .bottom:
            r.size.height += d.height
            nowFixed = true
        case .corner:
            r.size.width += d.width
            r.size.height += d.height
            nowFixed = true
        }
        if part == .left, r.width < TeachTile.least.width {
            // Held at its least, its right edge stays where it was.
            r.origin.x = base.maxX - TeachTile.least.width
        }
        return (TeachTile.kept(r, fixed: nowFixed, in: area), nowFixed)
    }

    /// The parts that move and resize it: its head, its sides, its foot and
    /// its bottom right corner, each with the pointer that says so.
    private func handles(_ base: (rect: CGRect, fixed: Bool), area: CGRect) -> some View {
        func grip(_ part: Part, _ pointer: NSCursor) -> some View {
            Color.clear
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 1, coordinateSpace: .global)
                        .updating($change) { v, s, _ in s = Change(part: part, by: v.translation) }
                        .onEnded { v in
                            let r = changed(base.rect, fixed: base.fixed, by: Change(part: part, by: v.translation), in: area)
                            tile.place(r.rect, fixed: r.fixed, in: area)
                        }
                )
                .onHover { inside in
                    if inside, !cursor { pointer.push(); cursor = true }
                    if !inside, cursor { NSCursor.pop(); cursor = false }
                }
        }
        return ZStack {
            VStack(spacing: 0) {
                // The head: all of its row but its buttons, which lie over it.
                grip(.head, .openHand).frame(height: 34).padding(.trailing, 64)
                Spacer(minLength: 0)
                grip(.bottom, .resizeUpDown).frame(height: 7)
            }
            HStack(spacing: 0) {
                grip(.left, .resizeLeftRight).frame(width: 7)
                Spacer(minLength: 0)
                grip(.right, .resizeLeftRight).frame(width: 7)
            }
            .padding(.vertical, 18)
            VStack {
                Spacer(minLength: 0)
                HStack {
                    Spacer(minLength: 0)
                    grip(.corner, .crosshair).frame(width: 16, height: 16)
                }
            }
        }
        .onDisappear { if cursor { NSCursor.pop(); cursor = false } }
    }

    private func panel(width: CGFloat, height: CGFloat?, most: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            head
            field
            content(most: most, wide: width >= 760)
            if height != nil { Spacer(minLength: 0) }
        }
        .padding(.horizontal, 16)
        .padding(.top, 12)
        .padding(.bottom, 14)
        .frame(width: width, alignment: .leading)
        .frame(height: height, alignment: .top)
        .background(GeometryReader { g in Color.clear.preference(key: TileHeight.self, value: g.size.height) })
        .onPreferenceChange(TileHeight.self) { drawn = $0 }
        .frostedPanel()
        .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .onExitCommand { closeAndGiveBackKeys() }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Teach tile")
    }

    /// Its name, who made the card and when, and its buttons. A drag on it
    /// moves the tile (handles), and a double-click puts it back over the
    /// middle of the terminal.
    private var head: some View {
        HStack(spacing: 7) {
            Image(systemName: "graduationcap.fill")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.accentInk)
            Text("Teach").font(Theme.font(13, .bold)).foregroundStyle(Theme.ink)
            Text(caption).font(Theme.font(11)).foregroundStyle(Theme.muted).lineLimit(1)
            Spacer(minLength: 4)
            if tile.own != nil {
                Button { tile.recentre() } label: {
                    Image(systemName: "align.horizontal.center").font(.system(size: 11, weight: .semibold))
                }
                .buttonStyle(.borderless)
                .foregroundStyle(Theme.muted)
                .help("Put it back over the middle of the terminal (or double-click its head)")
                .accessibilityLabel("Put it back")
            }
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
        .onTapGesture(count: 2) { tile.recentre() }
        .accessibilityAction(named: "Put it back over the terminal") { tile.recentre() }
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
    @ViewBuilder private func content(most: CGFloat, wide: Bool) -> some View {
        if let card = tile.result?.spec {
            ScrollView(.vertical, showsIndicators: cardHeight > most) {
                GenCardBody(card: card, spacing: 10, wide: wide)
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

private struct TileHeight: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
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
