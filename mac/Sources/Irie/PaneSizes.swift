import AppKit

// Each session's terminal size. A session's panes show beside its terminal
// or not, as it last had them (Rolodex.swift), so its terminal is narrower
// than another's whose panes don't show. Every agent window takes the size
// the core gives it, which was the slot's for all of them, the shown one's:
// so showing a session of the other kind would resize every agent. Instead
// the app tells the core the size it shows each session's terminal at, in
// cells (the sizes op, internal/core/sizes.go), and the core keeps each
// agent's window at its own: showing a session resizes no other agent, and
// the core gives the slot the size of the one coming in before it comes.

/// The sizes op's arguments: every session's size, and those of the
/// sessions whose differs, by id. A zero size sends every agent window back
/// to the slot's size.
struct SizesArgs: Encodable, Equatable {
    var size: CellSize
    var sessions: [String: CellSize]?

    static let off = SizesArgs(size: CellSize(w: 0, h: 0))
}

/// A terminal's size in cells.
struct CellSize: Codable, Equatable {
    var w: Int
    var h: Int
}

extension Store {
    /// Tells the core the sizes soon, once the layout has settled, when
    /// they changed since it last heard them.
    func sizesSoon() {
        // A preview leaves that to the installed app (Preview.swift).
        guard canSizes, !isFixture, !Preview.isOn else { return }
        // Sent from here on, so the core keeps each window at its own size.
        sizing?.cancel()
        sizing = Task { [weak self] in
            // A slide, a fold or a drag under way: again a little later.
            for _ in 0..<8 {
                try? await Task.sleep(nanoseconds: 300_000_000)
                guard let self, !Task.isCancelled else { return }
                guard let args = self.sizesNow() else { continue }
                guard args != self.sizesSent else { return }
                self.sizesSent = args
                do {
                    _ = try await self.client.call("sizes", args, timeout: 3, as: Empty.self)
                } catch {
                    if self.sizesSent == args { self.sizesSent = nil }
                }
                return
            }
        }
    }

    /// Whether the core keeps each agent's window at its session's size, as
    /// last told.
    var sizesKept: Bool { (sizesSent?.size.w ?? 0) > 0 }

    /// Going to a session whose panes show otherwise than the one on show's:
    /// the terminal keeps its size until the core has swapped the session
    /// in, which gives the slot that session's size first, so its agent
    /// comes in at its own size and the one going home leaves at its own
    /// (ResizeHold.holdForSwap).
    func holdsForSwap(to id: String) -> Bool {
        guard sizesKept, !id.contains("/"), id != selection, outsideSelection == nil else { return false }
        let r = rolodex
        return (r.shows[id] ?? PaneShow()) != PaneShow(open: r.open, folded: r.open && r.folded)
    }

    /// The sizes the terminal shows each session at, with its panes hidden,
    /// folded or beside it; none while the layout moves. While the terminal
    /// is popped out, or drawn by an engine whose cells the app can't
    /// count (SwiftTerm's), the panes size no agent: every agent window
    /// takes the slot's size, as before.
    func sizesNow() -> SizesArgs? {
        #if canImport(GhosttyKit)
        let r = rolodex
        guard poppedOut == nil, attach != nil, let t = terminalView as? GhosttyTerminalView else { return SizesArgs.off }
        guard ResizeHold.shared.held == nil, !r.sliding, !r.folding, !sideSliding,
              let main = r.mainProbe, main.window != nil, t.window != nil, let now = t.grid else { return nil }
        // What the terminal frame takes of the room beside the panes, as it
        // is laid out now; the terminal has the rest.
        let inset = main.bounds.width - t.bounds.width
        func size(_ kept: CGFloat) -> CellSize? {
            t.grid(width: r.lastRoom - kept - inset).map { CellSize(w: $0.columns, h: $0.rows) }
        }
        // The layout shows the session on show as it has its panes, and the
        // cells counted here are Ghostty's.
        guard let shown = size(r.room(open: r.open, folded: r.folded)), shown == CellSize(w: now.columns, h: now.rows),
              let hidden = size(0), let strip = size(RolodexModel.stripWidth), let beside = size(r.lastFit) else { return nil }
        var by: [String: CellSize] = [:]
        for (id, s) in r.shows where s.open && !id.contains("/") {
            let z = s.folded ? strip : beside
            if z != hidden { by[id] = z }
        }
        return SizesArgs(size: hidden, sessions: by.isEmpty ? nil : by)
        #else
        return SizesArgs.off
        #endif
    }
}
