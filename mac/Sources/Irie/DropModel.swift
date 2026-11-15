import CoreGraphics
import Foundation

// Where what a drag holds lands in the session list (DESIGN §4.1; Drops.swift
// draws it and performs it). The list is a column of rows, cards and
// headers, in sections: Pinned, then the List view's cards with your groups
// among them, or a group per repo, then Parked, then each paired machine's
// group. A drop goes between two rows, or inside a group by its header.
// The pointer's height picks the row: over its top half the drop goes just
// before the row, over its bottom half just after it, so the spacing
// between rows, a card's helpers and the room under the last card all take
// a drop; past a header's top edge the drop goes inside the group, first
// when the group is open, where the eye is, and last when it is folded.
//
// Between a group's last card and the row after it there is more than one
// place to land: last in the group, after the group in the one around it,
// and so on out to the top of the list. The row the pointer is on decides:
// the group's own card for inside it, the row after for outside; and the
// pointer's travel to the side since the drag began picks among them, a
// step every 24 points, left a group further out and right a group further
// in, as a file dragged sideways in Finder's list view picks the folder it
// lands in. Under the list's last card the drop goes to the top of the
// list, last, or, moved right, last in the groups that end there.
//
// This is the pure part, so it is tested on its own (mac/test.sh): the
// rows, their frames and the pointer go in, and where the hold would land
// comes out.
enum DropModel {
    /// The sections, by the sidebar's group ids (SessionGroup).
    static let list = "list"
    static let pinned = "pinned"
    static let parked = "parked"
    /// The strip under the List view's last card.
    static let end = "end"

    /// How far the pointer travels sideways before its first step, and how
    /// far for each step after.
    static let deadZone: CGFloat = 12
    static let step: CGFloat = 24
    /// A header's top edge, which drops before the group rather than inside
    /// it: this share of its height, and 6 points at least.
    static let edge: CGFloat = 0.3
    static let edgeMin: CGFloat = 6
    /// A line drawn beside a row, outside a gap, sits this far from it.
    static let beside: CGFloat = 3
    /// Groups go this deep at most (ipc.MaxGroupDepth).
    static let maxDepth = 8

    /// A row of the list as the sidebar draws it.
    struct Row: Equatable {
        enum Kind: Equatable {
            case card, header, end
        }

        var id: String
        var kind: Kind
        /// Pinned, the List view's cards (list), Parked, a repo's group or
        /// a machine's, by the sidebar's ids.
        var section: String
        /// The groups of yours around it, the outermost first; a header of
        /// an open group counts its own group last, as what follows it is
        /// inside.
        var chain: [String] = []
        /// A header's group of yours; nil for Pinned, Parked, a repo's or
        /// a machine's header, and for a card.
        var group: String? = nil
        /// A header's group is open.
        var open = false
        /// The sessions it stands for, in the list's order: a card's own,
        /// a header's group's, every one within; none for the strip.
        var sessions: [String] = []
        /// A paired machine's card, which never moves here.
        var peer = false

        /// How many groups are around it.
        var depth: Int { chain.count }
        /// How many groups are around what comes just before it: for an
        /// open group's header, its own group is not one of them.
        var outerDepth: Int { kind == .header && open ? chain.count - 1 : chain.count }
    }

    /// What the drag holds.
    enum Hold: Equatable {
        /// This Mac's cards, by id: whether the dragged one is pinned or
        /// parked, the group it is in, and its repo's section id.
        case cards([String], pinned: Bool, parked: Bool, group: String?, repo: String)
        /// A group of yours, by its header.
        case group(String)
    }

    /// Your groups: each one's group, "" at the top of the list.
    struct Tree: Equatable {
        var parent: [String: String] = [:]

        /// Group id and the groups around it, the outermost first; none
        /// for nil or a group that is gone.
        func path(_ id: String?) -> [String] {
            var out: [String] = []
            var at = id
            while let g = at, !g.isEmpty, let p = parent[g], !out.contains(g) {
                out.insert(g, at: 0)
                at = p
            }
            return out
        }

        /// How many groups are around and including id: 0 for nil.
        func level(_ id: String?) -> Int { path(id).count }

        /// Whether group id is group anc or inside it, at any depth.
        func within(_ id: String?, _ anc: String) -> Bool { path(id).contains(anc) }

        /// How many groups deep id goes, itself included: 1 for a group
        /// with no group inside.
        func height(_ id: String) -> Int {
            1 + (parent.filter { $0.value == id && $0.key != id }.keys.map(height).max() ?? 0)
        }
    }

    /// What the core and the view allow.
    struct Rules: Equatable {
        var canGroup = true
        var canPin = true
        var canPark = true
        /// The List view, with your groups in it; else the Repo view.
        var listView = true
    }

    /// Where the hold would land.
    struct Landing: Equatable {
        enum Place: Equatable {
            /// In the List view: in group into, nil at the top of the list,
            /// just before what holds session before, else just after what
            /// holds after, else last in into.
            case list(into: String?, before: String?, after: String?)
            /// Among the pinned cards, just before this one, or last.
            case pinned(before: String?)
            case parked(before: String?)
            /// Among its repo's cards, in the Repo view.
            case repo(before: String?)
        }

        var place: Place
        /// The line that shows the drop's place: where it starts, as far in
        /// as the group it lands in; nil when a folded group's header lights
        /// alone.
        var line: CGPoint?
        /// The header lit, by its sidebar id, for a drop inside its group.
        var lit: String?
        /// Whether the hold is there already, so the drop changes nothing.
        var same = false
    }

    /// How far the depth steps for travel points to the right since the
    /// drag began: one group further in per step right, further out per
    /// step left, after a dead zone.
    static func steps(_ travel: CGFloat) -> Int {
        let t = abs(travel)
        guard t > deadZone else { return 0 }
        let n = Int((t - deadZone) / step) + 1
        return travel > 0 ? n : -n
    }

    /// The rows that go with the hold, by id, and the sessions among them.
    static func moving(_ rows: [Row], _ hold: Hold) -> (rows: Set<String>, sessions: Set<String>) {
        switch hold {
        case let .cards(ids, _, _, _, _):
            let set = Set(ids)
            return (Set(rows.filter { $0.kind == .card && set.contains($0.id) }.map(\.id)), set)
        case let .group(g):
            var ids = Set<String>(), ss = Set<String>()
            for r in rows {
                switch r.kind {
                case .card where r.chain.contains(g):
                    ids.insert(r.id)
                    ss.insert(r.id)
                case .header where r.group == g || r.chain.contains(g):
                    ids.insert(r.id)
                    ss.formUnion(r.sessions)
                default:
                    continue
                }
            }
            return (ids, ss)
        }
    }

    /// How far from the rows a drop still lands: past this, nowhere.
    static let reach: CGFloat = 40

    /// The row under the pointer, or the nearest to it up or down, within
    /// reach.
    static func nearest(_ rows: [Row], _ frames: [String: CGRect], _ p: CGPoint) -> (Int, CGRect)? {
        var best: (Int, CGRect)?
        var dist = reach
        for (i, r) in rows.enumerated() {
            guard let f = frames[r.id] else { continue }
            let d = p.y < f.minY ? f.minY - p.y : p.y > f.maxY ? p.y - f.maxY : 0
            if d < dist {
                dist = d
                best = (i, f)
            }
        }
        return best
    }

    /// Where the hold lands with the pointer at p in the list's frame,
    /// having travelled travel points to the right since the drag began,
    /// over the rows whose frames are known; nil where nothing it holds can
    /// land. Lines start inset from the list's edge, and a step of indent
    /// further in per group.
    static func landing(at p: CGPoint, travel: CGFloat, rows: [Row], frames: [String: CGRect], hold: Hold,
                        tree: Tree, rules: Rules, inset: CGFloat, indent: CGFloat) -> Landing?
    {
        if case let .cards(ids, _, _, _, _) = hold, ids.isEmpty { return nil }
        let (movingRows, movingSessions) = moving(rows, hold)
        guard let (i, f) = nearest(rows, frames, p) else { return nil }
        let row = rows[i]

        // The group the hold is in now, whether it is pinned or parked, and
        // its repo's section.
        let holdGroup: String?, holdPinned: Bool, holdParked: Bool, holdRepo: String?
        switch hold {
        case let .cards(_, pinned, parked, group, repo):
            (holdGroup, holdPinned, holdParked, holdRepo) = (group, pinned, parked, repo)
        case let .group(g):
            (holdGroup, holdPinned, holdParked, holdRepo) = (tree.parent[g].flatMap { $0.isEmpty ? nil : $0 }, false, false, nil)
        }

        // The rows before and after a place, passing over what moves and
        // the strip, which is no row.
        func prev(_ i: Int) -> Int? {
            var j = i - 1
            while j >= 0, movingRows.contains(rows[j].id) || rows[j].kind == .end { j -= 1 }
            return j >= 0 ? j : nil
        }
        func next(_ i: Int) -> Int? {
            var j = i + 1
            while j < rows.count, movingRows.contains(rows[j].id) || rows[j].kind == .end { j += 1 }
            return j < rows.count ? j : nil
        }
        // The rows that move, when they are one run of the list.
        let movingIndices = rows.indices.filter { movingRows.contains(rows[$0].id) }
        var run: ClosedRange<Int>?
        if let lo = movingIndices.first, let hi = movingIndices.last, hi - lo + 1 == movingIndices.count {
            run = lo ... hi
        }
        // Whether the hold sits between rows a and b already.
        func sits(between a: Int?, _ b: Int?) -> Bool {
            guard let run else { return false }
            return prev(run.lowerBound) == a && next(run.upperBound) == b
        }
        // A row's session for the core to place by: a card's own, or the
        // first of a header's group that stays.
        func anchor(_ i: Int?) -> String? {
            guard let i else { return nil }
            let r = rows[i]
            return r.kind == .card ? r.id : r.sessions.first { !movingSessions.contains($0) }
        }
        func x(_ depth: Int) -> CGFloat { inset + indent * CGFloat(depth) }
        // Whether the hold can go in group into, nil for the top of the list.
        func fits(_ into: String?) -> Bool {
            switch hold {
            case .cards:
                return rules.canGroup || into == holdGroup
            case let .group(g):
                guard rules.canGroup, into != g, !tree.within(into, g) else { return false }
                return tree.level(into) + tree.height(g) <= maxDepth
            }
        }

        // Inside the group header i heads: first in it while it is open,
        // else last.
        func inside(_ i: Int) -> Landing? {
            let h = rows[i]
            let first = h.open
            let before = first ? anchor(i) : nil
            // Whether the hold is first in it already, just under the
            // header, or last, with nothing of the group's after it.
            var already = false
            if let run {
                if first {
                    already = prev(run.lowerBound) == i
                } else if let n = next(run.upperBound) {
                    let r = rows[n]
                    already = h.group.map { !r.chain.contains($0) } ?? (r.section != h.section)
                } else {
                    already = true
                }
            }
            let line = first ? CGPoint(x: x(h.depth), y: frames[h.id].map { $0.maxY + beside } ?? p.y) : nil
            if let g = h.group {
                guard rules.listView, fits(g) else { return nil }
                return Landing(place: .list(into: g, before: before, after: nil), line: line, lit: h.id,
                               same: already && holdGroup == g && !holdPinned && !holdParked)
            }
            guard case .cards = hold else { return nil }
            switch h.section {
            case pinned:
                guard rules.canPin else { return nil }
                return Landing(place: .pinned(before: before), line: line, lit: h.id, same: already && holdPinned)
            case parked:
                guard rules.canPark else { return nil }
                return Landing(place: .parked(before: before), line: line, lit: h.id, same: already && holdParked)
            default:
                guard h.section.hasPrefix("repo:"), h.section == holdRepo else { return nil }
                return Landing(place: .repo(before: before), line: line, lit: h.id,
                               same: already && !holdPinned && !holdParked)
            }
        }

        // The two rows the drop goes between, and which decides the place:
        // the row above (deep: as far in as it), the row below (shallow),
        // the strip (the top of the list), or the hold's own place.
        enum Side { case deep, shallow, top, own }
        let a: Int?, b: Int?, side: Side
        if movingRows.contains(row.id) {
            var s = i, e = i
            while s > 0, movingRows.contains(rows[s - 1].id) { s -= 1 }
            while e + 1 < rows.count, movingRows.contains(rows[e + 1].id) { e += 1 }
            (a, b, side) = (prev(s), next(e), .own)
        } else {
            switch row.kind {
            case .card:
                if p.y < f.midY { (a, b, side) = (prev(i), i, .shallow) } else { (a, b, side) = (i, next(i), .deep) }
            case .header:
                guard p.y < f.minY + max(edgeMin, f.height * edge) else { return inside(i) }
                // Above the first header there is nothing: inside it, then.
                if row.group == nil, prev(i) == nil { return inside(i) }
                (a, b, side) = (prev(i), i, .shallow)
            case .end:
                (a, b, side) = (prev(i), next(i), .top)
            }
        }
        let ra = a.map { rows[$0] }, rb = b.map { rows[$0] }

        // The section the drop goes in: the row's that decides, or, before
        // Pinned's, Parked's or a repo's header, the section that ends
        // above it.
        let section: String?
        switch side {
        case .deep:
            section = ra?.section
        case .shallow:
            section = rb?.kind == .header && rb?.group == nil ? ra?.section : rb?.section
        case .top:
            section = list
        case .own:
            switch hold {
            case .cards: section = holdPinned ? pinned : holdParked ? parked : rules.listView ? list : holdRepo
            case .group: section = list
            }
        }
        guard let section else { return nil }

        // The card below, of the section and this Mac's, to land before.
        func beforeCard(in s: String) -> String? {
            rb.flatMap { $0.kind == .card && $0.section == s && !$0.peer ? $0.id : nil }
        }
        // Where the line goes: between the two rows, over the hold's own
        // place, or along the strip.
        func lineY() -> CGFloat {
            if side == .own, let run, let fr = frames[rows[run.lowerBound].id] { return fr.minY - beside }
            if side == .top, let fe = frames[end] { return fe.midY }
            let fa = ra.flatMap { frames[$0.id] }, fb = rb.flatMap { frames[$0.id] }
            if let fa, let fb { return (fa.maxY + fb.minY) / 2 }
            if let fa { return fa.maxY + beside }
            if let fb { return fb.minY - beside }
            return p.y
        }

        switch section {
        case pinned:
            guard case .cards = hold, rules.canPin else { return nil }
            return Landing(place: .pinned(before: beforeCard(in: pinned)), line: CGPoint(x: x(0), y: lineY()),
                           same: holdPinned && sits(between: a, b))
        case parked:
            guard case .cards = hold, rules.canPark else { return nil }
            return Landing(place: .parked(before: beforeCard(in: parked)), line: CGPoint(x: x(0), y: lineY()),
                           same: holdParked && sits(between: a, b))
        case list:
            guard rules.listView else { return nil }
            let lo = rb?.section == list ? rb?.outerDepth ?? 0 : 0
            let hi = ra?.section == list ? ra?.depth ?? 0 : 0
            guard lo <= hi else { return nil }
            var d: Int
            switch side {
            case .deep: d = hi
            case .shallow: d = lo
            case .top: d = 0
            case .own: d = tree.level(holdGroup)
            }
            d = min(max(d + steps(travel), lo), hi)
            let into: String? = d == 0 ? nil : ra?.chain[d - 1]
            guard fits(into) else { return nil }
            let before = rb?.section == list ? anchor(b) : nil
            let after = ra?.section == list ? anchor(a) : nil
            return Landing(place: .list(into: into, before: before, after: after), line: CGPoint(x: x(d), y: lineY()),
                           same: into == holdGroup && !holdPinned && !holdParked && sits(between: a, b))
        default:
            guard case .cards = hold, section.hasPrefix("repo:"), section == holdRepo else { return nil }
            return Landing(place: .repo(before: beforeCard(in: section)), line: CGPoint(x: x(0), y: lineY()),
                           same: !holdPinned && !holdParked && sits(between: a, b))
        }
    }
}
