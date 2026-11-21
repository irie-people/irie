import Foundation
import Observation

/// A change to the notepad that the core has not answered yet. The app keeps
/// it in its defaults until the core has, so a change made while the core is
/// away, or just before the app stops, goes in once the core is back.
struct PendingNote: Codable, Equatable {
    var uid = UUID()
    var args: NotepadArgs
}

/// The notepad as the app has it (DESIGN §4.20): the person's notes for
/// later, which the core keeps (NotesPanel.swift draws them).
///
/// What the panel draws is the core's notepad with the app's changes not yet
/// answered laid over it, so a change shows at once and typing never waits.
/// Each change goes to the core in order, one at a time, and is kept in the
/// defaults until the core answers. Each is the same however often it is
/// sent (ipc.NotepadArgs), so one the app is unsure of simply goes again.
/// The app also keeps a copy of the core's notepad, so the notes show while
/// the core is away, as an install restarts it.
@MainActor
@Observable
final class NotesModel {
    /// The notes as the panel draws them.
    private(set) var pad = Notepad()
    /// Whether the sidebar shows the notepad: the core serves it, or the app
    /// has notes from before.
    private(set) var shown = false
    /// The notes on their way to the task bar.
    private(set) var sending: Set<String> = []

    @ObservationIgnored weak var store: Store?
    /// The core's notepad, the newest the app heard of.
    @ObservationIgnored private var base = Notepad()
    @ObservationIgnored private var heard = false
    @ObservationIgnored private var pending: [PendingNote] = []
    /// The change on its way, which typing never changes.
    @ObservationIgnored private var inFlight: UUID?
    @ObservationIgnored private var flushing = false
    @ObservationIgnored private var wait: Task<Void, Never>?
    /// The core said hello and serves the notepad.
    @ObservationIgnored private var served = false
    /// The next frame starts a subscription, and is taken whatever its rev:
    /// a restarted core's file may have gone back to a copy.
    @ObservationIgnored private var fresh = true
    @ObservationIgnored private var fixture = false
    /// Where notes deleted here were, so Undo shows them back in place
    /// before the core says.
    @ObservationIgnored private var places: [String: (prev: String?, at: Int)] = [:]
    /// What a note said before an edit emptied it: deleted empty, it keeps
    /// those words in Recently Deleted, as the core keeps them.
    @ObservationIgnored private var emptied: [String: String] = [:]
    @ObservationIgnored private var copying: Task<Void, Never>?

    static let copyKey = "notepadCopy"
    static let pendingKey = "notepadPending"
    /// The most of the copy kept in the defaults.
    private static let copyMost = 2 << 20

    init() {
        let d = UserDefaults.standard
        if let b = d.data(forKey: Self.copyKey), let n = try? JSONDecoder().decode(Notepad.self, from: b) {
            base = n
            heard = true
        }
        // A preview starts from a copy of the app's defaults: what the app
        // had not sent then is the app's to send, not the preview's
        // (Preview.swift).
        if !Preview.isOn, let b = d.data(forKey: Self.pendingKey),
           let p = try? JSONDecoder().decode([PendingNote].self, from: b) {
            pending = p
        }
        shown = heard || !pending.isEmpty
        redraw()
    }

    // MARK: The core

    /// The core said hello: whether it serves the notepad. Its notepad comes
    /// with the subscription, after the first snapshot.
    func connected(serves: Bool) {
        guard !fixture else { return }
        served = serves
        fresh = true
        if serves { shown = true }
    }

    /// Takes the notepad from a frame. Past a subscription's first frame, an
    /// older one than the app has is left: a reply may have brought a newer
    /// one first.
    func take(frame n: Notepad) {
        guard !fixture else { return }
        if !fresh, heard, (n.rev ?? 0) < (base.rev ?? 0) { return }
        fresh = false
        accept(n)
        flush()
    }

    private func accept(_ n: Notepad) {
        base = n
        heard = true
        shown = true
        keepCopy()
        redraw()
    }

    /// Keeps a copy of the core's notepad in the defaults a second after it
    /// last changed, so the notes show while the core is away; typing never
    /// waits on it. The core's file is what keeps them.
    private func keepCopy() {
        guard !fixture else { return }
        copying?.cancel()
        copying = Task { [weak self] in
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            guard !Task.isCancelled, let self else { return }
            if let b = try? JSONEncoder().encode(self.base), b.count <= Self.copyMost {
                UserDefaults.standard.set(b, forKey: Self.copyKey)
            }
        }
    }

    /// Fixture notes, for --snapshot: nothing is saved or sent.
    func loadFixture(_ notes: [Note], vault: NotepadVault? = nil) {
        fixture = true
        served = true
        shown = true
        pending = []
        base = Notepad()
        base.notes = notes
        base.vault = vault
        base.deleted = [Note(id: "gone", text: "Try the Asahi build again", deleted: Date())]
        redraw()
    }

    // MARK: Changes

    /// Adds a note with text, after the note after, or last; returns its id.
    @discardableResult
    func add(_ text: String, after: String? = nil) -> String {
        let id = UUID().uuidString
        queue(NotepadArgs(action: "add", id: id, text: text, after: after))
        return id
    }

    /// Sets a note's text as it is typed. The last change not yet on its way,
    /// when it is this note's edit or add, takes the new text instead of a
    /// change a key, and it goes once typing rests.
    func edit(_ id: String, _ text: String) {
        // A note gone from the list, as one just deleted whose field ends
        // its editing, takes no edit: it would bring the note back.
        guard let n = note(id), n.text != text else { return }
        if text.isBlank, !n.text.isBlank {
            emptied[id] = n.text
        } else if !text.isBlank {
            emptied[id] = nil
        }
        if let i = pending.indices.last, pending[i].uid != inFlight, pending[i].args.id == id,
           pending[i].args.action == "edit" || pending[i].args.action == "add" {
            pending[i].args.text = text
            keep()
            redraw()
            flush(after: 0.4)
            return
        }
        queue(NotepadArgs(action: "edit", id: id, text: text), after: 0.4)
    }

    func check(_ id: String, _ done: Bool) {
        queue(NotepadArgs(action: "check", id: id, done: done))
    }

    /// Puts a note just before another, or last.
    func move(_ id: String, before: String?) {
        queue(NotepadArgs(action: "move", id: id, before: before))
    }

    /// Moves notes to Recently Deleted, where the core keeps them 30 days,
    /// and returns what they said: a line never written in keeps nothing,
    /// and one emptied keeps the words it had.
    @discardableResult
    func delete(_ ids: [String]) -> [String] {
        guard !ids.isEmpty else { return [] }
        var kept: [String] = []
        for id in ids {
            guard let i = pad.notes.firstIndex(where: { $0.id == id }) else { continue }
            places[id] = (i > 0 ? pad.notes[i - 1].id : nil, i)
            let words = pad.notes[i].text.isBlank ? emptied[id] : pad.notes[i].text
            if let words, !words.isBlank { kept.append(words) }
        }
        queue(NotepadArgs(action: "delete", ids: ids))
        return kept
    }

    /// Brings notes back from Recently Deleted, to where they were.
    func restore(_ ids: [String]) {
        guard !ids.isEmpty else { return }
        queue(NotepadArgs(action: "restore", ids: ids))
    }

    func note(_ id: String) -> Note? { pad.notes.first { $0.id == id } }

    /// The notes can change: the core's file is not a newer version's.
    var canChange: Bool { fixture || !(pad.readOnly ?? false) }

    private func queue(_ args: NotepadArgs, after delay: Double = 0) {
        guard !(base.readOnly ?? false) || fixture else {
            store?.say(base.problem ?? "These notes can’t change now.", error: true)
            return
        }
        pending.append(PendingNote(args: args))
        keep()
        redraw()
        flush(after: delay)
    }

    /// Keeps the changes not yet answered in the defaults, at once: they
    /// live there until the core has them.
    private func keep() {
        guard !fixture else { return }
        let d = UserDefaults.standard
        if pending.isEmpty {
            d.removeObject(forKey: Self.pendingKey)
        } else if let b = try? JSONEncoder().encode(pending) {
            d.set(b, forKey: Self.pendingKey)
        }
    }

    // MARK: Sending

    /// Sends the changes in order, one at a time, after delay seconds. A
    /// change the core refuses as wrong is dropped, and said; one it could
    /// not take, as while it is away, waits and goes again.
    func flush(after delay: Double = 0) {
        guard !fixture else { return }
        wait?.cancel()
        wait = nil
        if delay > 0 {
            wait = Task { [weak self] in
                try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
                guard !Task.isCancelled else { return }
                self?.flush()
            }
            return
        }
        // While the core's file is a newer version's, the changes wait,
        // kept, for a core that can take them.
        guard served, !(base.readOnly ?? false), !flushing, let head = pending.first, let store else { return }
        flushing = true
        inFlight = head.uid
        Task { [weak self] in
            do {
                let r = try await store.client.call("notepad", head.args, timeout: 10, as: Notepad.self)
                guard let self else { return }
                self.answered(head)
                if !self.heard || (r.rev ?? 0) >= (self.base.rev ?? 0) {
                    self.accept(r)
                } else {
                    self.redraw()
                }
                self.flush()
            } catch let e as WireError where e.code == "bad_args" || e.code == "not_found" {
                guard let self else { return }
                self.answered(head)
                self.redraw()
                if e.code == "bad_args" { store.say(e.msg, error: true) }
                self.flush()
            } catch where (error as? WireError)?.code == "busy" {
                // The file is read only: the frame says so, the panel shows
                // why, and the changes wait for a core that can take them.
                guard let self else { return }
                self.flushing = false
                self.inFlight = nil
            } catch {
                // Away, busy or unable to write: it waits, kept.
                guard let self else { return }
                self.flushing = false
                self.inFlight = nil
                self.flush(after: 5)
            }
        }
    }

    private func answered(_ p: PendingNote) {
        flushing = false
        inFlight = nil
        pending.removeAll { $0.uid == p.uid }
        keep()
    }

    /// Sends note id to the task bar, as if typed there and sent. Its own
    /// changes go first, so the core has the note it ticks off.
    func send(_ id: String) {
        guard let store, !fixture, !sending.contains(id), let n = note(id) else { return }
        let text = n.text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        sending.insert(id)
        Task {
            defer { sending.remove(id) }
            await settle()
            _ = await store.sendNote(text, note: id)
        }
    }

    /// Waits, up to five seconds, while changes are still on their way.
    private func settle() async {
        flush()
        for _ in 0..<50 where !pending.isEmpty {
            try? await Task.sleep(nanoseconds: 100_000_000)
            if !flushing { flush() }
        }
    }

    // MARK: Drawing

    /// The core's notepad with the changes not yet answered laid over it.
    private func redraw() {
        var n = base
        for p in pending { apply(p.args, to: &n) }
        if n != pad { pad = n }
    }

    /// What a change does, as the core does it (internal/core/notepad.go),
    /// so it shows before the core answers.
    private func apply(_ a: NotepadArgs, to n: inout Notepad) {
        let now = Date()
        let ids = (a.ids ?? []) + [a.id].compactMap { $0 }
        func at(_ id: String) -> Int? { n.notes.firstIndex { $0.id == id } }
        switch a.action {
        case "add":
            guard let id = a.id, let text = a.text, at(id) == nil, !n.deleted.contains(where: { $0.id == id }) else { return }
            let i = a.after.flatMap(at).map { $0 + 1 } ?? n.notes.count
            n.notes.insert(Note(id: id, text: text, created: now, changed: now), at: i)
        case "edit":
            guard let id = a.id, let text = a.text else { return }
            if let i = at(id) {
                n.notes[i].text = text
            } else if !text.isBlank, let j = n.deleted.firstIndex(where: { $0.id == id }) {
                var x = n.deleted.remove(at: j)
                x.text = text
                x.deleted = nil
                n.notes.insert(x, at: place(id, in: n.notes))
            } else if !text.isBlank {
                n.notes.append(Note(id: id, text: text, created: now, changed: now))
            }
        case "check":
            if let id = a.id, let i = at(id) { n.notes[i].done = a.done ?? false }
        case "move":
            guard let id = a.id, let i = at(id) else { return }
            let x = n.notes.remove(at: i)
            let to = a.before.flatMap(at) ?? n.notes.count
            n.notes.insert(x, at: to)
        case "delete":
            for id in ids {
                guard let i = at(id) else { continue }
                var x = n.notes.remove(at: i)
                if x.text.isBlank {
                    guard let words = emptied[id], !words.isBlank else { continue }
                    x.text = words
                }
                x.deleted = now
                n.deleted.insert(x, at: 0)
            }
        case "restore":
            let want = Set(ids)
            for x in n.deleted where want.contains(x.id) {
                n.deleted.removeAll { $0.id == x.id }
                guard at(x.id) == nil else { continue }
                var y = x
                y.deleted = nil
                n.notes.insert(y, at: place(x.id, in: n.notes))
            }
        default:
            break
        }
    }

    /// Where a note deleted here goes back: first when it was, else after the
    /// note it came after; last when the app never knew.
    private func place(_ id: String, in notes: [Note]) -> Int {
        guard let p = places[id] else { return notes.count }
        guard let prev = p.prev else { return 0 }
        if let i = notes.firstIndex(where: { $0.id == prev }) { return i + 1 }
        return min(p.at, notes.count)
    }
}

/// The task bar's history: the tasks sent from it, newest last, kept across
/// launches, which ↑ walks back through (TaskBar.swift). A note sent as a
/// jot goes in it too, as if typed.
enum TaskHistory {
    static let key = "taskHistory"
    static let kept = 50

    static func load() -> [String] { UserDefaults.standard.stringArray(forKey: key) ?? [] }

    /// Puts input last, once, and tells the bar.
    @discardableResult
    static func remember(_ input: String) -> [String] {
        var h = load()
        h.removeAll { $0 == input }
        h.append(input)
        h = Array(h.suffix(kept))
        UserDefaults.standard.set(h, forKey: key)
        NotificationCenter.default.post(name: .irieTaskHistory, object: nil)
        return h
    }
}

extension Notification.Name {
    /// The task bar's history changed (TaskHistory).
    static let irieTaskHistory = Notification.Name("irieTaskHistory")
}

extension String {
    var isBlank: Bool { trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
}
