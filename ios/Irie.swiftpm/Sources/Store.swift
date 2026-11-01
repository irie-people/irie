import SwiftUI
import UIKit

// The app's state: the machines this phone is paired with, what each core
// said last, and the ops a person sends. Like every Irie client it draws
// what the core says, sends what is typed to `input`, and shows the core's
// words as they are (docs/CORE.md). This is the native form of the web
// app's app.js.

struct SessionRef: Hashable {
    var mid: String
    var sid: String
}

struct Toast: Identifiable {
    let id = UUID()
    var text: String
    var error = false
    var actionLabel: String?
    var action: (() -> Void)?
    var seconds: Double = 4.5
}

enum ConnStatus: Equatable {
    case connecting, connected, offline, unpaired

    var words: String {
        switch self {
        case .connected: return "Connected"
        case .connecting: return "Connecting…"
        case .unpaired: return "Not paired"
        case .offline: return "Offline"
        }
    }

    var color: Color {
        switch self {
        case .connected: return Theme.done
        case .connecting: return Theme.paused
        case .unpaired: return Theme.needs
        case .offline: return Theme.muted
        }
    }
}

/// Where a task from the task bar runs on one machine.
struct TaskPrefs: Codable, Equatable {
    var repo = ""
    var profile = ""
}

/// A question the core asks before an uncertain undo or redo.
struct ChoicePrompt: Identifiable {
    let id = UUID()
    var conn: Conn
    var op: String
    var choice: Choice
}

struct SessionPrompt: Identifiable {
    let id = UUID()
    var conn: Conn
    var session: Session
}

enum Sheet: Identifiable {
    case machines
    case link(PairLink)
    case again(Machine)

    var id: String {
        switch self {
        case .machines: return "machines"
        case .link(let l): return "link:" + l.base.absoluteString + l.code
        case .again(let m): return "again:" + m.id
        }
    }
}

enum Haptic {
    static func success() { UINotificationFeedbackGenerator().notificationOccurred(.success) }
    static func error() { UINotificationFeedbackGenerator().notificationOccurred(.error) }
    static func tap() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
    static func pick() { UISelectionFeedbackGenerator().selectionChanged() }
}

// MARK: - A machine

/// A paired machine and what its core said last.
@Observable @MainActor
final class Conn {
    var m: Machine
    let mid: String
    let gateway: Gateway
    let token: String
    var status: ConnStatus = .connecting
    var sessions: [String: Session] = [:]
    var header: Header?
    var closed: [ClosedSession] = []
    var loaded = false
    var features: Swift.Set<String>?
    var version: String?
    var info: MachineInfo?
    var seen: Date?
    var unpairedWords = ""
    @ObservationIgnored private var stream: FrameStream?
    @ObservationIgnored weak var app: AppModel?

    init(_ m: Machine, token: String, app: AppModel) {
        self.m = m
        mid = m.id
        self.token = token
        self.app = app
        gateway = Gateway(base: m.url, pin: m.pin)
    }

    func start() {
        status = .connecting
        let s = FrameStream(gateway: gateway, token: token, args: [:])
        s.onFrame = { [weak self] f in self?.frame(f) }
        s.onPhase = { [weak self] phase in
            guard let self, self.status != .unpaired else { return }
            switch phase {
            case .connected:
                self.status = .connected
                self.seen = Date()
                Task { await self.hello() }
            case .connecting: self.status = .connecting
            case .offline: self.status = .offline
            }
        }
        s.onUnpaired = { [weak self] msg in
            guard let self else { return }
            self.app?.unpaired(self, msg)
        }
        stream = s
        s.start()
        Task { await fetchInfo() }
    }

    func stop() {
        stream?.stop()
        stream = nil
    }

    func kick() {
        guard status != .unpaired else { return }
        if let stream { stream.kick() } else { start() }
        if info == nil { Task { await fetchInfo() } }
    }

    private func frame(_ f: Frame) {
        switch f.t {
        case "snapshot":
            sessions = Dictionary((f.sessions ?? []).map { ($0.id, $0) }, uniquingKeysWith: { _, b in b })
            if let h = f.header { header = h }
            loaded = true
        case "upsert":
            if let s = f.session { sessions[s.id] = s }
        case "remove":
            if let id = f.id { sessions[id] = nil }
        case "header":
            header = f.header
        case "closed":
            closed = f.closed ?? []
        default:
            return
        }
        seen = Date()
    }

    func hello() async {
        do {
            let r = try await call("hello", ["client": "irie-phone", "api": 1], as: HelloResult.self)
            features = (r.features?.isEmpty ?? true) ? nil : Swift.Set(r.features ?? [])
            version = r.version
        } catch {
            // An older core still takes the ops; offer them all.
        }
    }

    func fetchInfo() async {
        guard let i = try? await gateway.info() else { return }
        info = i
        if let name = i.name, !name.isEmpty, name != m.name {
            m.name = name
            app?.renamed(self)
        }
    }

    /// Whether its core serves an op, by hello's features.
    func can(_ op: String) -> Bool { features?.contains(op) ?? true }

    func call<R: Decodable>(_ op: String, _ args: [String: Any] = [:], timeout: TimeInterval = 15, as: R.Type) async throws -> R {
        try await gateway.call(op, args, token: token, timeout: timeout, as: R.self)
    }

    /// Its rows in the core's order, the parked ones after the rest.
    var rows: (live: [Session], parked: [Session]) {
        let all = sessions.values.sorted { a, b in
            let oa = a.order ?? Int.max, ob = b.order ?? Int.max
            if oa != ob { return oa < ob }
            let ca = a.created ?? .distantPast, cb = b.created ?? .distantPast
            if ca != cb { return ca < cb }
            return a.id < b.id
        }
        return (all.filter { !$0.isParked }, all.filter(\.isParked))
    }

    var redOnDone: Bool { header?.redOnDone ?? false }
}

// MARK: - The app

@Observable @MainActor
final class AppModel {
    var machines: [Machine]
    var conns: [Conn] = []
    var path: [SessionRef] = []
    var toasts: [Toast] = []
    var filter = false
    var parkedOpen: [String: Bool] = Keep.value("irie.parkedOpen", default: [:])
    var closedOpen: [String: Bool] = [:]
    var taskMachine: String = UserDefaults.standard.string(forKey: "irie.taskMachine") ?? ""
    var taskPrefs: [String: TaskPrefs] = Keep.value("irie.taskPrefs", default: [:])
    var draft: String = UserDefaults.standard.string(forKey: "irie.draft") ?? ""
    var taskBusy = false
    var sheet: Sheet?
    var choice: ChoicePrompt?
    var renaming: SessionPrompt?
    var renameText = ""
    var closing: SessionPrompt?
    @ObservationIgnored var backgroundAt: Date?
    @ObservationIgnored weak var conversation: ConversationModel?

    init() {
        machines = Keep.loadMachines()
        for m in machines { startConn(m) }
    }

    private func startConn(_ m: Machine) {
        guard let token = Keep.token(for: m.id) else { return }
        let c = Conn(m, token: token, app: self)
        conns.append(c)
        c.start()
    }

    func conn(_ mid: String) -> Conn? { conns.first { $0.mid == mid } }

    func session(_ ref: SessionRef) -> Session? { conn(ref.mid)?.sessions[ref.sid] }

    func renamed(_ c: Conn) {
        if let i = machines.firstIndex(where: { $0.id == c.mid }) {
            machines[i].name = c.m.name
            Keep.save(machines)
        }
    }

    // MARK: Toasts

    func toast(_ text: String, error: Bool = false, action: String? = nil, seconds: Double = 4.5, run: (() -> Void)? = nil) {
        let t = Toast(text: text, error: error, actionLabel: action, action: run, seconds: seconds)
        withAnimation(.spring(duration: 0.32)) {
            toasts.append(t)
            if toasts.count > 3 { toasts.removeFirst(toasts.count - 3) }
        }
        let id = t.id
        Task {
            try? await Task.sleep(nanoseconds: UInt64(seconds * 1_000_000_000))
            self.dismiss(id)
        }
    }

    func dismiss(_ id: UUID) {
        withAnimation(.easeOut(duration: 0.2)) { toasts.removeAll { $0.id == id } }
    }

    // MARK: Ops

    func fail(_ c: Conn, _ error: Error) {
        let e = error as? APIError ?? APIError(code: "error", message: error.localizedDescription)
        Haptic.error()
        if e.unpaired { unpaired(c, e.message) } else { toast(e.message, error: true) }
    }

    /// An op on a session, then a toast with Undo, or one that says it's done.
    @discardableResult
    func op(_ c: Conn, _ name: String, _ args: [String: Any], undo: String? = nil, done: String? = nil) async -> Bool {
        do {
            _ = try await c.call(name, args, as: Ignored.self)
            Haptic.success()
            if let undo {
                toast(undo, action: "Undo") { [weak self] in Task { await self?.undoLast(c) } }
            } else if let done {
                toast(done)
            }
            return true
        } catch {
            fail(c, error)
            return false
        }
    }

    func undoLast(_ c: Conn) async {
        do {
            afterHistory(c, "undo", try await c.call("undo", [:], as: UndoResult.self))
        } catch {
            fail(c, error)
        }
    }

    /// What an undo or redo did; a question the core asks goes to a dialog.
    func afterHistory(_ c: Conn, _ op: String, _ r: UndoResult) {
        if r.did == "asked", let ch = r.choice {
            choice = ChoicePrompt(conn: c, op: op, choice: ch)
            return
        }
        let word = ["undone": "Undone", "redone": "Redone", "settled": "It was already so", "skipped": "Skipped", "cancelled": "Nothing changed"][r.did] ?? "Done"
        if let label = r.label, !label.isEmpty { toast("\(word): \(Self.step(label))") } else { toast(word) }
    }

    /// A step's label as a person reads it, as the Mac app's UndoText has
    /// it: the core's `park "api-fix"` reads Park “api-fix”.
    static func step(_ label: String) -> String {
        func upper(_ s: Substring) -> String { s.prefix(1).uppercased() + s.dropFirst() }
        guard let sp = label.firstIndex(of: " ") else { return upper(label[...]) }
        let verb = upper(label[..<sp])
        let rest = label[label.index(after: sp)...]
        if rest.count >= 2, rest.hasPrefix("\""), rest.hasSuffix("\"") {
            let name = String(rest.dropFirst().dropLast())
                .replacingOccurrences(of: "\\\"", with: "\"")
                .replacingOccurrences(of: "\\\\", with: "\\")
            return "\(verb) “\(name)”"
        }
        return "\(verb) \(rest)"
    }

    func answer(_ p: ChoicePrompt, _ key: String) async {
        do {
            afterHistory(p.conn, p.op, try await p.conn.call(p.op, ["choice_id": p.choice.id, "choice": key], as: UndoResult.self))
        } catch {
            fail(p.conn, error)
        }
    }

    func close(_ c: Conn, _ s: Session) async {
        do {
            _ = try await c.call("close", ["session": s.id], as: Ignored.self)
            Haptic.success()
            path.removeAll { $0 == SessionRef(mid: c.mid, sid: s.id) }
            toast("Closed “\(s.name)”", action: "Undo") { [weak self] in Task { await self?.undoLast(c) } }
        } catch {
            fail(c, error)
        }
    }

    func reopen(_ c: Conn, _ id: String) async {
        do {
            let r = try await c.call("reopen", ["session": id], as: ReopenResult.self)
            Haptic.success()
            toast("Reopened “\(r.session?.name ?? "the session")”")
        } catch {
            fail(c, error)
        }
    }

    func rename(_ p: SessionPrompt, to name: String) async {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty, n != p.session.name else { return }
        do {
            _ = try await p.conn.call("rename", ["session": p.session.id, "name": n], as: Ignored.self)
            Haptic.success()
        } catch {
            fail(p.conn, error)
        }
    }

    func setParked(_ c: Conn, _ s: Session, _ on: Bool) async {
        await op(c, on ? "park" : "unpark", ["session": s.id], undo: "\(on ? "Parked" : "Unparked") “\(s.name)”")
    }

    func open(_ c: Conn, _ id: String) {
        let ref = SessionRef(mid: c.mid, sid: id)
        if path.last != ref { path = [ref] }
    }

    // MARK: The task bar

    /// The machine a task runs on: the one picked, else one that answers.
    var taskConn: Conn? {
        let all = conns.filter { $0.status != .unpaired }
        return all.first { $0.mid == taskMachine } ?? all.first { $0.status == .connected } ?? all.first
    }

    func prefs(_ c: Conn) -> TaskPrefs { taskPrefs[c.mid] ?? TaskPrefs() }

    func setPrefs(_ c: Conn, _ p: TaskPrefs) {
        taskPrefs[c.mid] = p
        Keep.set("irie.taskPrefs", taskPrefs)
    }

    func pickMachine(_ id: String) {
        taskMachine = id
        UserDefaults.standard.set(id, forKey: "irie.taskMachine")
    }

    func saveDraft() {
        UserDefaults.standard.set(draft, forKey: "irie.draft")
    }

    func submitTask() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !taskBusy else { return }
        guard let c = taskConn else {
            toast("No machine to run it on.", error: true)
            return
        }
        let p = prefs(c)
        var args: [String: Any] = ["text": text]
        if !p.repo.isEmpty { args["repo"] = p.repo }
        if !p.profile.isEmpty { args["profile"] = p.profile }
        taskBusy = true
        defer { taskBusy = false }
        do {
            // A task in a repo may make a worktree first, so this can take a while.
            let r = try await c.call("input", args, timeout: 120, as: InputResult.self)
            if draft.trimmingCharacters(in: .whitespacesAndNewlines) == text {
                draft = ""
                saveDraft()
            }
            Haptic.success()
            if r.did == "asked", let ch = r.choice, let op = r.op {
                choice = ChoicePrompt(conn: c, op: op, choice: ch)
                return
            }
            if r.did == "spawned", let s = r.session {
                c.sessions[s.id] = s
                if r.show == true {
                    open(c, s.id)
                } else {
                    let said = (r.text ?? "").isEmpty ? "Started “\(s.name)”" : r.text!
                    toast(said, action: "Open") { [weak self] in self?.open(c, s.id) }
                }
            } else if let s = r.session, r.show == true {
                open(c, s.id)
            } else if let t = r.text, !t.isEmpty {
                toast(t, seconds: r.did == "answered" ? 9 : 4.5)
            }
        } catch {
            fail(c, error)
        }
    }

    // MARK: Pairing

    /// Keeps a machine paired with its token, replacing an older pairing of it.
    @discardableResult
    func remember(url: URL, pin: String, result r: PairResult) -> Machine {
        let m = Machine(id: r.machine.id, name: (r.machine.name ?? "").isEmpty ? Gateway.host(url) : r.machine.name!, url: url, pin: pin)
        for old in conns where old.mid == m.id || old.m.url == url {
            old.stop()
            Keep.removeToken(for: old.mid)
        }
        conns.removeAll { $0.mid == m.id || $0.m.url == url }
        machines.removeAll { $0.id == m.id || $0.url == url }
        machines.append(m)
        Keep.setToken(r.token, for: m.id)
        Keep.save(machines)
        startConn(m)
        Haptic.success()
        return m
    }

    func unpaired(_ c: Conn, _ msg: String) {
        guard c.status != .unpaired else { return }
        c.status = .unpaired
        c.unpairedWords = msg
        c.stop()
        if conversation?.conn === c { conversation?.stop() }
        Haptic.error()
        toast("\(c.m.name) no longer knows this phone.", error: true, action: "Pair again", seconds: 8) { [weak self] in
            self?.sheet = .again(c.m)
        }
    }

    func forget(_ c: Conn) async {
        do {
            try await c.gateway.forget(token: c.token)
        } catch {
            if let e = error as? APIError, !e.unpaired { toast("Forgot \(c.m.name) here. \(e.message)", error: true) }
        }
        c.stop()
        Keep.removeToken(for: c.mid)
        conns.removeAll { $0 === c }
        machines.removeAll { $0.id == c.mid }
        Keep.save(machines)
        path.removeAll { $0.mid == c.mid }
        if machines.isEmpty { sheet = nil }
    }

    // MARK: Coming back

    /// iOS ends streams in the background, often without a word: a stream
    /// that went quiet while the app was away is taken for dead.
    func kickAll() {
        for c in conns { c.kick() }
        conversation?.kick()
    }

    func wentAway() {
        backgroundAt = Date()
    }

    func cameBack() {
        guard let at = backgroundAt else { return }
        backgroundAt = nil
        if Date().timeIntervalSince(at) > 2.5 { kickAll() }
    }
}
