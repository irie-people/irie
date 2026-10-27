import Foundation
import SwiftUI

// Outside Irie (DESIGN §4.6): the Claude Code sessions that run in other
// terminals or in the background, after Irie's own cards. Each row is one
// line, in the order they started, so a state change never moves or resizes
// one. A click or the arrow keys select a row; its details, Move into Irie
// and Copy Session ID go in the details panel. An outside session cannot be
// shown, so the keys that show a session skip it, and Return says why.

/// A Claude Code session that runs outside Irie, as the core lists it:
/// `outside` in an outside frame.
struct OutsideSession: Codable, Identifiable, Equatable {
    var vendorId: String?
    var shortId: String?
    var name: String?
    var kind: String
    var status: String?
    var waitingFor: String?
    var state: String?
    var pid: Int?
    var cwd: String
    var started: Date?
    var movable: Bool?
    var why: String?

    /// Its key in the list, as the core's OutsideSession.Key.
    var id: String {
        if let v = vendorId, !v.isEmpty { return v }
        if let s = shortId, !s.isEmpty { return "id:" + s }
        if let p = pid, p != 0 { return "pid:\(p)" }
        return "\(cwd)@\(Int64(((started?.timeIntervalSince1970 ?? 0) * 1000).rounded()))"
    }

    /// The name its row shows.
    var label: String {
        if let n = name, !n.isEmpty { return n }
        let base = (cwd as NSString).lastPathComponent
        return base.isEmpty ? id : base
    }

    /// How it runs.
    var whereItRuns: String {
        if kind == "background" { return shortId.map { "in the background as \($0)" } ?? "in the background" }
        return pid.map { "in a terminal, pid \($0)" } ?? "in a terminal"
    }

    /// What it is doing, in words.
    var doing: String {
        switch status ?? "" {
        case "busy": return "Busy"
        case "idle": return "Idle"
        case "waiting": return waitingFor.map { "Waiting for \($0)" } ?? "Waiting for you"
        case "": return "Not running"
        case let s: return s.prefix(1).uppercased() + s.dropFirst()
        }
    }

    enum CodingKeys: String, CodingKey {
        case name, kind, status, state, pid, cwd, started, movable, why
        case vendorId = "vendor_id"
        case shortId = "id"
        case waitingFor = "waiting_for"
    }
}

struct MoveArgs: Encodable {
    var vendorId: String
    var profile: String?

    enum CodingKeys: String, CodingKey {
        case profile
        case vendorId = "vendor_id"
    }
}

struct MoveResult: Decodable {
    var session: Session
}

extension Look {
    /// An outside session looks like the Irie state it matches.
    init(outside o: OutsideSession) {
        switch o.status ?? "" {
        case "busy": self = .working
        case "idle": self = .idle
        case "waiting": self = .needs
        default: self = .exited
        }
    }
}

/// What a move ends, for the confirmation.
let moveEnds = "Its copy outside Irie ends, and anything it runs in the background stops."

extension Store {
    /// The outside rows the sidebar draws: the order they started, without
    /// those the filter hides.
    var drawnOutside: [OutsideSession] {
        let f = filter.trimmingCharacters(in: .whitespaces).lowercased()
        return f.isEmpty ? outside : outside.filter { $0.label.lowercased().contains(f) }
    }

    var selectedOutside: OutsideSession? {
        guard let k = outsideSelection else { return nil }
        return outside.first { $0.id == k }
    }

    /// Takes an outside frame. A problem listing them shows once, when new.
    func applyOutside(_ f: Frame) {
        outside = f.outside ?? []
        if let k = outsideSelection, !outside.contains(where: { $0.id == k }) {
            outsideSelection = nil
        }
        let problem = f.problem ?? ""
        if problem != outsideProblem {
            outsideProblem = problem
            if !problem.isEmpty { say(problem, error: true) }
        }
    }

    /// The arrow keys: the next (+1) or previous (-1) row of the whole list,
    /// Irie's cards then the outside rows, without wrapping. A card shows as
    /// before; an outside row is only selected.
    func walk(_ delta: Int) {
        let cards = drawnOrder, rows = drawnOutside
        let n = cards.count + rows.count
        guard n > 0 else { return }
        var at: Int?
        if let k = outsideSelection, let j = rows.firstIndex(where: { $0.id == k }) {
            at = cards.count + j
        } else if let i = cards.firstIndex(where: { $0.id == selection }) {
            at = i
        }
        let next = at.map { min(max($0 + delta, 0), n - 1) } ?? (delta > 0 ? 0 : n - 1)
        if next < cards.count {
            if outsideSelection != nil || cards[next].id != selection { show(cards[next].id) }
        } else {
            outsideSelection = rows[next - cards.count].id
        }
    }

    /// Return on an outside row: it cannot be shown, so say why.
    func explainOutside(_ o: OutsideSession) {
        var text = "\(o.label) runs outside Irie, \(o.whereItRuns)."
        if o.movable == true {
            text += " Move into Irie carries it on here."
        } else if let why = o.why {
            text += " It can’t move yet: \(why)."
        }
        say(text, error: false)
    }

    /// Asks before a move, which ends the outside copy.
    func askToMove(_ o: OutsideSession) {
        guard o.movable == true, o.vendorId != nil else {
            say("\(o.label) can’t move yet: \(o.why ?? "it isn’t idle").", error: true)
            return
        }
        confirmingMove = o
    }

    /// Ends the outside copy and carries the conversation on in Irie, then
    /// shows it and gives its terminal the keyboard.
    func move(_ o: OutsideSession) {
        guard moving == nil, let id = o.vendorId else { return }
        moving = o.id
        Task {
            defer { moving = nil }
            do {
                // A move waits its turn behind a spawn, which may make a worktree.
                let r = try await client.call("move", MoveArgs(vendorId: id), timeout: 600, as: MoveResult.self)
                say("Moved \(r.session.name) into Irie.", error: false)
                show(r.session.id, focus: true)
            } catch {
                say(error.localizedDescription, error: true)
            }
        }
    }
}

/// The section after Irie's cards: a header with the count, then a row per
/// session.
struct OutsideSection: View {
    @EnvironmentObject private var store: Store
    let now: Date
    let listFocused: Bool

    var body: some View {
        let rows = store.drawnOutside
        if !rows.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    Text("Outside Irie").font(.system(size: 11, weight: .semibold))
                    Text("\(rows.count)").font(.system(size: 11, weight: .bold, design: .rounded)).monospacedDigit()
                    Rectangle().fill(Theme.edge).frame(height: 1)
                }
                .foregroundStyle(Theme.muted)
                .padding(.top, 10)
                .padding(.horizontal, 4)
                .help("Claude Code sessions that run in other terminals. Select one to move it into Irie.")
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Outside Irie, \(rows.count) Claude Code sessions in other terminals")
                .accessibilityAddTraits(.isHeader)
                ForEach(rows) { o in
                    OutsideRow(outside: o, selected: o.id == store.outsideSelection, listFocused: listFocused, now: now)
                        .id("outside:" + o.id)
                }
            }
        }
    }
}

/// One session outside Irie, always one line: its mark, name and age. While
/// it is selected or hovered, Move into Irie takes the age's fixed slot, off
/// while the session can't move, so the row never grows.
private struct OutsideRow: View {
    @EnvironmentObject private var store: Store
    let outside: OutsideSession
    let selected: Bool
    let listFocused: Bool
    let now: Date
    @State private var hovering = false

    private var look: Look { Look(outside: outside) }
    private var canMove: Bool { outside.movable == true && store.moving == nil }

    var body: some View {
        HStack(spacing: 8) {
            StateMark(look: look)
            Text(outside.label)
                .font(.system(size: 12.5, design: .rounded))
                .foregroundStyle(look.quiet ? Theme.muted : Theme.ink)
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
            ZStack(alignment: .trailing) {
                Text(outside.started.map { Age.short($0, now: now) } ?? "")
                    .font(.system(size: 11)).monospacedDigit()
                    .foregroundStyle(Theme.muted)
                    .opacity(hovering || selected ? 0 : 1)
                Button { store.askToMove(outside) } label: {
                    Image(systemName: "tray.and.arrow.down").frame(width: 20, height: 18)
                }
                .buttonStyle(.borderless)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(Theme.muted)
                .disabled(!canMove)
                .help(outside.movable == true ? "Move into Irie" : "Can’t move yet: \(outside.why ?? "it isn’t idle")")
                .opacity(hovering || selected ? 1 : 0)
                .allowsHitTesting(hovering || selected)
                .accessibilityHidden(true)
            }
            .frame(width: 44, alignment: .trailing)
        }
        .padding(.vertical, 5)
        .padding(.horizontal, Theme.inset)
        .background(shape.fill(fill))
        .overlay(shape.strokeBorder(edge, lineWidth: selected ? 1.5 : 1))
        .contentShape(shape)
        .onHover { hovering = $0 }
        .onTapGesture {
            store.outsideSelection = outside.id
            store.focusSidebar += 1
        }
        .contextMenu { OutsideActions(outside: outside) }
        .help([outside.label, outside.doing, Age.tilde(outside.cwd)].joined(separator: "\n"))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { store.outsideSelection = outside.id }
        .accessibilityActions {
            Button("Move into Irie") { store.askToMove(outside) }
            if let v = outside.vendorId {
                Button("Copy the session ID") { store.copy(v, what: "session ID") }
            }
            Button("Show the folder in Finder") { store.reveal(outside.cwd) }
        }
    }

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: Theme.radius, style: .continuous) }

    private var fill: Color {
        if selected { return listFocused ? Theme.selection : Theme.selection.opacity(0.45) }
        return hovering ? Theme.hover : Theme.card
    }

    private var edge: Color {
        if selected { return listFocused ? Theme.accent : Theme.accent.opacity(0.5) }
        if hovering { return Theme.accent.opacity(0.45) }
        return look == .needs ? Theme.needs.opacity(0.6) : Theme.edge
    }

    private var spoken: String {
        [outside.label + ", outside Irie", outside.doing, outside.whereItRuns, outside.started.map { "started " + Age.spoken($0, now: now) }]
            .compactMap { $0 }.joined(separator: ", ")
    }
}

/// The outside row's right-click menu.
private struct OutsideActions: View {
    @EnvironmentObject private var store: Store
    let outside: OutsideSession

    var body: some View {
        Button(outside.movable == true ? "Move into Irie…" : "Move into Irie (\(outside.doing.lowercased()))") {
            store.askToMove(outside)
        }
        .disabled(outside.movable != true || store.moving != nil)
        Divider()
        if let v = outside.vendorId {
            Button("Copy Session ID") { store.copy(v, what: "session ID") }
        }
        Button("Show Folder in Finder") { store.reveal(outside.cwd) }
        Button("Copy Folder Path") { store.copy(outside.cwd, what: "folder path") }
    }
}

/// The selected outside session in the details panel: what it is doing,
/// where it runs, Move into Irie, off with the reason while it can't move,
/// and Copy Session ID.
struct OutsideDetails: View {
    @EnvironmentObject private var store: Store
    let outside: OutsideSession

    var body: some View {
        let look = Look(outside: outside)
        WholeLines(spacing: 4) {
            HStack(spacing: 6) {
                StateMark(look: look)
                Text(outside.doing)
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundStyle(look.loud ? Theme.needs : Theme.ink)
                    .lineLimit(1)
                Spacer(minLength: 4)
                Button(store.moving == outside.id ? "Moving…" : "Move into Irie") { store.askToMove(outside) }
                    .disabled(outside.movable != true || store.moving != nil)
                if let v = outside.vendorId {
                    Button("Copy Session ID") { store.copy(v, what: "session ID") }
                }
                DetailsFold()
            }
            .controlSize(.small)
            line(outside.label, color: Theme.ink)
            line("Outside Irie, " + outside.whereItRuns)
            line(Age.tilde(outside.cwd), mono: true)
            if outside.movable != true {
                // The button is off; this says why.
                line("Can’t move yet: " + (outside.why ?? "it isn’t idle"), color: look == .needs ? Theme.needs : Theme.sunInk)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private func line(_ text: String, mono: Bool = false, color: Color = Theme.muted) -> some View {
        Text(text)
            .font(.system(size: mono ? 10.5 : 11.5, design: mono ? .monospaced : .default))
            .foregroundStyle(color)
            .lineLimit(1)
            .truncationMode(.middle)
            .help(text)
    }
}

/// Asks before a move, with what it ends.
struct OutsideMoveConfirmation: ViewModifier {
    @EnvironmentObject private var store: Store

    func body(content: Content) -> some View {
        content.confirmationDialog(
            "Move \(store.confirmingMove?.label ?? "it") into Irie?",
            isPresented: Binding(get: { store.confirmingMove != nil }, set: { if !$0 { store.confirmingMove = nil } }),
            titleVisibility: .visible,
            presenting: store.confirmingMove
        ) { o in
            Button("Move into Irie") { store.move(o) }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text(moveEnds)
        }
    }
}
