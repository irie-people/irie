import SwiftUI

// The catalog's pictures (internal/genui): Flow, Diagram, Compare and Code,
// drawn in Irie's own look from the data a model gave, which never holds a
// style, a colour or markup. A Diagram is links alone: Irie lays it out.

/// Steps in order, joined by arrows: across, for up to four that fit,
/// else down, as a line of numbered steps.
struct GenFlowView: View {
    let steps: [GenItem]

    static let most = 8

    var body: some View {
        let shown = Array(steps.prefix(Self.most))
        VStack(alignment: .leading, spacing: 4) {
            if shown.count <= 4 {
                ViewThatFits(in: .horizontal) {
                    across(shown)
                    down(shown)
                }
            } else {
                down(shown)
            }
            GenMore(count: steps.count - Self.most)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Steps")
    }

    private func across(_ shown: [GenItem]) -> some View {
        HStack(alignment: .top, spacing: 4) {
            ForEach(Array(shown.enumerated()), id: \.offset) { i, step in
                if i > 0 {
                    Image(systemName: "arrow.right")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .padding(.top, 12)
                        .accessibilityHidden(true)
                }
                // The number over the title, so the title has the box's
                // whole width to wrap in.
                VStack(alignment: .leading, spacing: 4) {
                    GenStepNumber(n: i + 1)
                    Text(step.title)
                        .font(Theme.font(12, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(3)
                        .fixedSize(horizontal: false, vertical: true)
                    if let d = step.detail, !d.isEmpty {
                        Text(d)
                            .font(Theme.font(11))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(4)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .padding(8)
                .frame(width: 104, alignment: .topLeading)
                .frame(maxHeight: .infinity, alignment: .topLeading)
                .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.card))
                .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
            }
        }
        .fixedSize(horizontal: false, vertical: true)
    }

    private func down(_ shown: [GenItem]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(shown.enumerated()), id: \.offset) { i, step in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    GenStepNumber(n: i + 1)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(step.title).font(Theme.font(12, .semibold)).foregroundStyle(Theme.ink).lineLimit(2)
                        if let d = step.detail, !d.isEmpty {
                            Text(d).font(Theme.font(11.5)).foregroundStyle(Theme.muted).lineLimit(3)
                        }
                    }
                    .padding(.bottom, i == shown.count - 1 ? 0 : 12)
                    Spacer(minLength: 0)
                }
                // The line to the next step, under the numbers.
                .background(alignment: .topLeading) {
                    if i < shown.count - 1 {
                        Rectangle()
                            .fill(Theme.edge)
                            .frame(width: 2)
                            .frame(maxHeight: .infinity)
                            .padding(.leading, GenStepNumber.size / 2 - 1)
                            .padding(.top, GenStepNumber.size + 2)
                            .accessibilityHidden(true)
                    }
                }
            }
        }
    }
}

/// A step's number, in a disc of the accent.
struct GenStepNumber: View {
    let n: Int
    static let size: CGFloat = 18

    var body: some View {
        Text("\(n)")
            .font(.system(size: 10, weight: .bold, design: .rounded))
            .foregroundStyle(Theme.onAccent)
            .frame(width: Self.size, height: Self.size)
            .background(Circle().fill(Theme.accent))
            .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
    }
}

/// Things and the links between them, laid out by GenGraph and drawn as
/// boxes and arrows: down from what leads to what it leads to, and back up
/// beside the boxes, dashed, for a link that closes a loop.
struct GenDiagramView: View {
    let links: [GenLink]

    static let boxHeight: CGFloat = 34
    static let rowGap: CGFloat = 30
    static let boxGap: CGFloat = 10
    /// The lane kept on the right for the links that loop back up, and
    /// their words, when there are any.
    static let loopLane: CGFloat = 100

    var body: some View {
        let g = GenGraph(links)
        let rows = g.layers.count
        let height = CGFloat(rows) * Self.boxHeight + CGFloat(max(rows - 1, 0)) * Self.rowGap
        GeometryReader { geo in
            let w = geo.size.width
            ZStack(alignment: .topLeading) {
                Canvas { ctx, _ in
                    for e in g.edges { draw(e, g, width: w, in: &ctx) }
                }
                ForEach(g.names.indices, id: \.self) { v in
                    let f = Self.frame(of: v, g, width: w)
                    Text(g.names[v])
                        .font(Theme.font(11.5, .semibold))
                        .foregroundStyle(Theme.ink)
                        .multilineTextAlignment(.center)
                        .lineLimit(2)
                        .minimumScaleFactor(0.8)
                        .padding(.horizontal, 6)
                        .frame(width: f.width, height: f.height)
                        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.card))
                        .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.accent.opacity(0.75), lineWidth: 1))
                        .position(x: f.midX, y: f.midY)
                }
            }
        }
        .frame(height: max(height, Self.boxHeight))
        .padding(.vertical, 2)
        .accessibilityElement()
        .accessibilityLabel("Diagram")
        .accessibilityValue(g.edges.map { e in
            "\(g.names[e.from]) to \(g.names[e.to])" + (e.label.isEmpty ? "" : ", \(e.label)")
        }.joined(separator: "; "))
    }

    /// Where thing v's box goes: its layer's row, and its place across it,
    /// left of the loops' lane.
    static func frame(of v: Int, _ g: GenGraph, width: CGFloat) -> CGRect {
        let row = g.layers[g.layer[v]]
        let n = CGFloat(row.count)
        let room = (g.loops ? max(width - loopLane, width / 2) : width) / n
        let w = min(150, room - boxGap)
        let x = room * (CGFloat(g.slot[v]) + 0.5)
        let y = CGFloat(g.layer[v]) * (boxHeight + rowGap)
        return CGRect(x: x - w / 2, y: y, width: w, height: boxHeight)
    }

    private func draw(_ e: GenGraph.Edge, _ g: GenGraph, width: CGFloat, in ctx: inout GraphicsContext) {
        let a = Self.frame(of: e.from, g, width: width), b = Self.frame(of: e.to, g, width: width)
        var path = Path()
        let start, c1, c2, end: CGPoint
        if e.from == e.to {
            // A thing that leads to itself: a small loop on its right.
            start = CGPoint(x: a.maxX, y: a.midY - 7)
            end = CGPoint(x: a.maxX, y: a.midY + 7)
            c1 = CGPoint(x: a.maxX + 24, y: a.midY - 20)
            c2 = CGPoint(x: a.maxX + 24, y: a.midY + 20)
        } else if e.back {
            // Back up a loop: out of the right side and into the right side,
            // bowing out past both.
            start = CGPoint(x: a.maxX, y: a.midY)
            end = CGPoint(x: b.maxX, y: b.midY)
            let out = min(max(a.maxX, b.maxX) + 30, width - 2)
            c1 = CGPoint(x: out, y: a.midY)
            c2 = CGPoint(x: out, y: b.midY)
        } else {
            start = CGPoint(x: a.midX, y: a.maxY)
            end = CGPoint(x: b.midX, y: b.minY)
            let dy = (end.y - start.y) / 2
            c1 = CGPoint(x: start.x, y: start.y + dy)
            c2 = CGPoint(x: end.x, y: end.y - dy)
        }
        path.move(to: start)
        path.addCurve(to: end, control1: c1, control2: c2)
        let colour = e.back || e.from == e.to ? Theme.muted.opacity(0.8) : Theme.muted
        ctx.stroke(path, with: .color(colour), style: StrokeStyle(lineWidth: 1.3, lineCap: .round, dash: e.back ? [4, 3] : []))
        // The arrow's head, along the curve's last stretch.
        let angle = atan2(end.y - c2.y, end.x - c2.x)
        var head = Path()
        head.move(to: end)
        head.addLine(to: CGPoint(x: end.x - 7 * cos(angle - 0.45), y: end.y - 7 * sin(angle - 0.45)))
        head.addLine(to: CGPoint(x: end.x - 7 * cos(angle + 0.45), y: end.y - 7 * sin(angle + 0.45)))
        head.closeSubpath()
        ctx.fill(head, with: .color(colour))
        guard !e.label.isEmpty else { return }
        // Its words, on a patch of the ground, at the curve's middle.
        let mid = CGPoint(x: 0.125 * start.x + 0.375 * c1.x + 0.375 * c2.x + 0.125 * end.x,
                          y: 0.125 * start.y + 0.375 * c1.y + 0.375 * c2.y + 0.125 * end.y)
        let text = ctx.resolve(Text(e.label).font(Theme.font(10)).foregroundColor(Theme.muted))
        let size = text.measure(in: CGSize(width: e.back || e.from == e.to ? Self.loopLane - 12 : 130, height: 28))
        var at = CGRect(x: mid.x - size.width / 2 - 4, y: mid.y - size.height / 2 - 1, width: size.width + 8, height: size.height + 2)
        if e.back || e.from == e.to {
            // A loop's words go outside it, in the lane, where no box is.
            at.origin.x = mid.x + 3
        }
        at.origin.x = min(max(at.minX, 0), max(width - at.width, 0))
        ctx.fill(Path(roundedRect: at, cornerRadius: 4), with: .color(Theme.field))
        ctx.draw(text, in: at.insetBy(dx: 4, dy: 1))
    }
}

/// A Diagram's links laid out in layers, top to bottom: each thing a layer
/// below the lowest thing that leads to it, so links run down, but those
/// that close a loop, which run back up. The things of a layer are put in
/// the order that crosses fewest links, under the middle of what leads to
/// them. Things of the same name are one thing.
struct GenGraph: Equatable {
    struct Edge: Equatable {
        var from: Int
        var to: Int
        var label: String
        var back: Bool
    }

    private(set) var names: [String] = []
    private(set) var edges: [Edge] = []
    /// The things of each layer, left to right.
    private(set) var layers: [[Int]] = []
    /// Each thing's layer, and its place in it.
    private(set) var layer: [Int] = []
    private(set) var slot: [Int] = []
    /// Whether a link runs back up a loop, or from a thing to itself.
    var loops: Bool { edges.contains { $0.back || $0.from == $0.to } }

    init(_ links: [GenLink]) {
        var index: [String: Int] = [:]
        func id(_ name: String) -> Int {
            if let i = index[name] { return i }
            index[name] = names.count
            names.append(name)
            return names.count - 1
        }
        for l in links {
            let a = l.from.trimmingCharacters(in: .whitespacesAndNewlines)
            let b = l.to.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !a.isEmpty, !b.isEmpty else { continue }
            edges.append(Edge(from: id(a), to: id(b), label: (l.label ?? "").trimmingCharacters(in: .whitespacesAndNewlines), back: false))
        }
        let n = names.count
        var out = Array(repeating: [Int](), count: n)
        for (k, e) in edges.enumerated() { out[e.from].append(k) }

        // A link that closes a loop: walking from each thing in the order
        // they came, a link back to a thing still on the walk.
        var mark = Array(repeating: 0, count: n) // 0 not yet, 1 on the walk, 2 done
        func walk(_ v: Int) {
            mark[v] = 1
            for k in out[v] {
                let w = edges[k].to
                if mark[w] == 1 {
                    edges[k].back = true
                } else if mark[w] == 0 {
                    walk(w)
                }
            }
            mark[v] = 2
        }
        for v in 0..<n where mark[v] == 0 { walk(v) }

        // The layers, by the links that run down, which hold no loop now.
        layer = Array(repeating: 0, count: n)
        var ins = Array(repeating: 0, count: n)
        for e in edges where !e.back { ins[e.to] += 1 }
        var queue = (0..<n).filter { ins[$0] == 0 }
        var head = 0
        while head < queue.count {
            let v = queue[head]
            head += 1
            for k in out[v] where !edges[k].back {
                let w = edges[k].to
                layer[w] = max(layer[w], layer[v] + 1)
                ins[w] -= 1
                if ins[w] == 0 { queue.append(w) }
            }
        }
        layers = Array(repeating: [], count: (layer.max() ?? -1) + 1)
        for v in 0..<n { layers[layer[v]].append(v) }
        slot = Array(repeating: 0, count: n)
        number()

        // Fewer crossings: each thing under the middle of the things it is
        // linked to in the row above, then over those in the row below,
        // twice over.
        for pass in 0..<4 {
            let down = pass % 2 == 0
            let rows = down ? Array(layers.indices.dropFirst()) : Array(layers.indices.dropLast().reversed())
            for r in rows {
                let near = down ? r - 1 : r + 1
                var at: [Int: Double] = [:]
                for v in layers[r] {
                    var linked: [Double] = []
                    for e in edges {
                        if e.from == v, layer[e.to] == near { linked.append(across(e.to)) }
                        if e.to == v, layer[e.from] == near { linked.append(across(e.from)) }
                    }
                    at[v] = linked.isEmpty ? across(v) : linked.reduce(0, +) / Double(linked.count)
                }
                let was = slot
                var row = layers[r]
                row.sort { (at[$0] ?? 0, was[$0]) < (at[$1] ?? 0, was[$1]) }
                layers[r] = row
                number()
            }
        }
    }

    /// Each thing's place in its layer, from the layers' order.
    private mutating func number() {
        for row in layers { for (j, v) in row.enumerated() { slot[v] = j } }
    }

    /// How far across its row thing v sits, from 0 to 1.
    private func across(_ v: Int) -> Double {
        (Double(slot[v]) + 0.5) / Double(max(layers[layer[v]].count, 1))
    }
}

/// Two to four things side by side, each with its points.
struct GenCompareView: View {
    let sides: [GenSide]

    /// Each side's mark, in the palette on show; never the red of what
    /// needs you, which is Irie's alone.
    static var marks: [Color] { [Theme.accent, Theme.done, Theme.paused, Theme.frozen] }

    var body: some View {
        let shown = Array(sides.prefix(4))
        // Four go two by two; up to three fit in one row.
        let perRow = shown.count == 4 ? 2 : max(shown.count, 1)
        Grid(alignment: .topLeading, horizontalSpacing: 8, verticalSpacing: 8) {
            ForEach(Array(stride(from: 0, to: shown.count, by: perRow)), id: \.self) { start in
                GridRow {
                    ForEach(start..<min(start + perRow, shown.count), id: \.self) { i in
                        side(shown[i], mark: Self.marks[i % Self.marks.count])
                    }
                }
            }
        }
    }

    private func side(_ s: GenSide, mark: Color) -> some View {
        let points = s.points ?? []
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                RoundedRectangle(cornerRadius: 1.5).fill(mark).frame(width: 3, height: 13)
                Text(s.title).font(Theme.font(12, .bold)).foregroundStyle(Theme.ink).lineLimit(2)
            }
            ForEach(Array(points.prefix(6).enumerated()), id: \.offset) { _, p in
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Circle().fill(mark).frame(width: 4, height: 4).accessibilityHidden(true)
                    Text(p.text).font(Theme.font(11.5)).foregroundStyle(Theme.ink).lineLimit(3)
                }
            }
            GenMore(count: points.count - 6)
        }
        .padding(9)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.card))
        .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// A short example, in a monospaced block, with its language over it. A
/// long line scrolls rather than wraps, as code reads.
struct GenCodeView: View {
    let text: String
    let language: String?

    static let most = 14

    var body: some View {
        let lines = text.replacingOccurrences(of: "\t", with: "    ")
            .split(separator: "\n", omittingEmptySubsequences: false)
        let trimmed = lines.last?.isEmpty == true ? Array(lines.dropLast()) : Array(lines)
        VStack(alignment: .leading, spacing: 4) {
            if let language, !language.isEmpty {
                Text(language).font(Theme.font(10, .semibold)).foregroundStyle(Theme.muted)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                Text(trimmed.prefix(Self.most).joined(separator: "\n"))
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: true, vertical: false)
                    .textSelection(.enabled)
            }
            GenMore(count: trimmed.count - Self.most, what: "lines")
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.panel))
        .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
    }
}

/// "and 3 more", under a part cut short.
struct GenMore: View {
    let count: Int
    var what = ""

    var body: some View {
        if count > 0 {
            Text(what.isEmpty ? "and \(count) more" : "and \(count) more \(what)")
                .font(Theme.font(10.5))
                .foregroundStyle(Theme.muted)
        }
    }
}
