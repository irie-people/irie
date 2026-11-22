import SwiftUI

/// Where a session stands, over its message box: a card a local model made
/// from the session's conversation, which the core keeps and makes again
/// only when the session has moved on (docs/proposals/materialized-cards.md).
/// It shows while materialize.sessions is on, and folds to its one line.
struct StandsCard: View {
    @Environment(Store.self) private var store
    let session: Session
    @AppStorage("standsCardOpen") private var open = true

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let r = store.sessionCards[session.id] {
                header(r)
                if open { MaterializedCardView(result: r) }
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, store.sessionCards[session.id] == nil ? 0 : 8)
        .frame(maxWidth: 780, alignment: .leading)
        .frame(maxWidth: .infinity)
        .background(Theme.ground)
        // Asked when the session shows and when its state changes, then
        // every two minutes while it shows: the core makes the card again
        // only when it is out of date, so most asks cost nothing.
        .task(id: "\(session.id)|\(session.state)") {
            while !Task.isCancelled {
                guard await store.refreshCard(session.id) else { return }
                try? await Task.sleep(for: .seconds(120))
            }
        }
    }

    private func header(_ r: MaterializeResult) -> some View {
        Button { open.toggle() } label: {
            HStack(spacing: 6) {
                Image(systemName: open ? "chevron.down" : "chevron.right")
                    .font(.system(size: 9, weight: .semibold))
                    .frame(width: 10)
                Text("Where it stands")
                    .font(Theme.font(11.5, .semibold))
                    .foregroundStyle(Theme.ink)
                Text(caption(r))
                    .font(Theme.font(11))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                Spacer(minLength: 0)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .foregroundStyle(Theme.muted)
        .help(open ? "Fold the card" : "Show the card")
    }

    /// Who made it and when, and whether a new one is being made.
    private func caption(_ r: MaterializeResult) -> String {
        var words: [String] = []
        if let by = r.by, let made = r.made {
            words.append("made by \(by), \(Age.short(made, now: Date()))")
        }
        switch r.state {
        case "refreshing": words.append("updating")
        case "stale": words.append("out of date")
        case "cold" where r.err == nil: words.append("being made")
        default: break
        }
        return words.joined(separator: " · ")
    }
}

/// A materialized card, drawn from Irie's catalog in Irie's look, in a quiet
/// frame of its own. Every string is drawn as plain words: a card cannot
/// style itself into one of Irie's own prompts.
struct MaterializedCardView: View {
    let result: MaterializeResult

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            if let card = result.spec {
                GenCardBody(card: card)
            } else if let err = result.err {
                Text("No card yet: \(err)")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(3)
            } else {
                Text("A local model is making it.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .textSelection(.enabled)
    }
}

/// A card's title over its parts, at most eight of them, as the session's
/// card and the Teach tile draw it. Wide, the parts go in two columns, each
/// to the one that is shorter so far, in order, so the first, the picture a
/// card that explains begins with, is at the top left.
struct GenCardBody: View {
    let card: GenCard
    var spacing: CGFloat = 7
    var wide = false

    var body: some View {
        let parts = Array(card.body.prefix(8))
        VStack(alignment: .leading, spacing: spacing) {
            Text(card.title)
                .font(Theme.font(12.5, .bold))
                .foregroundStyle(Theme.ink)
            if wide, parts.count > 1 {
                let (left, right) = Self.columns(parts)
                HStack(alignment: .top, spacing: 20) {
                    column(left)
                    column(right)
                }
            } else {
                column(parts)
            }
        }
    }

    private func column(_ parts: [GenPart]) -> some View {
        VStack(alignment: .leading, spacing: spacing) {
            ForEach(Array(parts.enumerated()), id: \.offset) { _, part in
                GenPartView(part: part)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }

    /// The parts shared between two columns: each, in order, to the one
    /// that is shorter so far, by about how tall it draws.
    static func columns(_ parts: [GenPart]) -> ([GenPart], [GenPart]) {
        var left: [GenPart] = [], right: [GenPart] = []
        var l: CGFloat = 0, r: CGFloat = 0
        for p in parts {
            if l <= r {
                left.append(p)
                l += height(p)
            } else {
                right.append(p)
                r += height(p)
            }
        }
        return (left, right)
    }

    /// About how tall a part draws, in points: enough to share parts out.
    static func height(_ p: GenPart) -> CGFloat {
        switch p.type {
        case "Flow":
            let n = min(p.items?.count ?? 0, GenFlowView.most)
            return n <= 4 ? 130 : CGFloat(n) * 44
        case "Diagram":
            let rows = GenGraph(p.links ?? []).layers.count
            return CGFloat(rows) * (GenDiagramView.boxHeight + GenDiagramView.rowGap)
        case "Compare":
            let sides = p.sides ?? []
            let most = sides.map { min($0.points?.count ?? 0, 6) }.max() ?? 0
            return (40 + CGFloat(most) * 20) * (sides.count == 4 ? 2 : 1)
        case "Code":
            return 30 + CGFloat(min((p.text ?? "").split(separator: "\n").count, GenCodeView.most)) * 15
        case "List": return 8 + CGFloat(min(p.items?.count ?? 0, 6)) * 22
        case "Facts": return CGFloat(min(p.facts?.count ?? 0, 8)) * 19
        case "Table": return 20 + CGFloat(min(p.rows?.count ?? 0, 6)) * 19
        case "Text": return 18 * CGFloat((p.text ?? "").count / 70 + 1)
        default: return 20
        }
    }
}

/// One part of a card. A kind the app does not know draws nothing.
struct GenPartView: View {
    let part: GenPart

    var body: some View {
        switch part.type {
        case "Text": text
        case "List": list
        case "Facts": facts
        case "Table": table
        case "Progress": progress
        case "Divider": Divider().overlay(Theme.edge)
        // The pictures (GenPictures.swift).
        case "Flow": GenFlowView(steps: part.items ?? [])
        case "Diagram": GenDiagramView(links: part.links ?? [])
        case "Compare": GenCompareView(sides: part.sides ?? [])
        case "Code": GenCodeView(text: part.text ?? "", language: part.language)
        default: EmptyView()
        }
    }

    @ViewBuilder private var text: some View {
        let words = part.text ?? ""
        switch part.style {
        case "title":
            Text(words).font(Theme.font(12, .semibold)).foregroundStyle(Theme.ink).lineLimit(2)
        case "caption":
            Text(words).font(Theme.font(10.5)).foregroundStyle(Theme.muted).lineLimit(2)
        case "mono":
            Text(words).font(.system(size: 10.5, design: .monospaced)).foregroundStyle(Theme.ink).lineLimit(3)
        default:
            Text(words).font(Theme.font(12)).foregroundStyle(Theme.ink).lineLimit(4)
        }
    }

    private var list: some View {
        let items = part.items ?? []
        return VStack(alignment: .leading, spacing: 4) {
            ForEach(Array(items.prefix(6).enumerated()), id: \.offset) { _, it in
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    mark(it.status)
                    (Text(it.title).foregroundColor(Theme.ink)
                        + Text(it.detail.map { " — \($0)" } ?? "").foregroundColor(Theme.muted))
                        .font(Theme.font(12))
                        .lineLimit(2)
                }
            }
            more(items.count - 6)
        }
    }

    /// A list item's status mark, in the palette's own colours; never the
    /// red of what needs you, which is Irie's alone.
    private func mark(_ status: String?) -> some View {
        let (symbol, colour): (String, Color) = switch status {
        case "ok": ("checkmark.circle.fill", Theme.done)
        case "warn": ("exclamationmark.triangle.fill", Theme.sunInk)
        case "note": ("info.circle", Theme.accentInk)
        default: ("circle.fill", Theme.muted)
        }
        return Image(systemName: symbol)
            .font(.system(size: status == nil || status == "none" ? 5 : 10))
            .foregroundStyle(colour)
            .frame(width: 12)
            .accessibilityLabel(status ?? "")
    }

    private var facts: some View {
        let facts = part.facts ?? []
        return Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 3) {
            ForEach(Array(facts.prefix(8).enumerated()), id: \.offset) { _, f in
                GridRow {
                    Text(f.label).foregroundStyle(Theme.muted)
                    Text(f.value.text).foregroundStyle(Theme.ink).lineLimit(2)
                }
                .font(Theme.font(12))
            }
        }
    }

    private var table: some View {
        let rows = part.rows ?? []
        return VStack(alignment: .leading, spacing: 3) {
            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 3) {
                if let columns = part.columns, !columns.isEmpty {
                    GridRow {
                        ForEach(Array(columns.enumerated()), id: \.offset) { _, c in
                            Text(c.text).font(Theme.font(11, .semibold)).foregroundStyle(Theme.muted)
                        }
                    }
                }
                ForEach(Array(rows.prefix(6).enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, c in
                            Text(c.text).font(Theme.font(12)).foregroundStyle(Theme.ink).lineLimit(1)
                        }
                    }
                }
            }
            more(rows.count - 6)
        }
    }

    private var progress: some View {
        let total = part.total?.value ?? 0
        let done = total > 0 ? min(max((part.value?.value ?? 0) / total, 0), 1) : 0
        let words = "\(GenNumber.words(part.value?.value ?? 0)) of \(GenNumber.words(total))"
        return VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(part.label ?? "").foregroundStyle(Theme.ink)
                Spacer(minLength: 8)
                Text(words)
                    .foregroundStyle(Theme.muted)
                    .monospacedDigit()
            }
            .font(Theme.font(12))
            // Drawn from shapes, not ProgressView: an AppKit control, which
            // the snapshots' ImageRenderer cannot draw.
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.edge)
                    Capsule().fill(Theme.accent).frame(width: g.size.width * done)
                }
            }
            .frame(height: 5)
            .accessibilityElement()
            .accessibilityLabel(part.label ?? "Progress")
            .accessibilityValue(words)
        }
    }

    /// "and 3 more", under a list or table cut short in the card.
    @ViewBuilder private func more(_ n: Int) -> some View {
        if n > 0 {
            Text("and \(n) more").font(Theme.font(10.5)).foregroundStyle(Theme.muted)
        }
    }
}
