import SwiftUI
import UIKit

// One session's conversation (DESIGN §4.7), from the core's `conversation`
// frames on a subscription of its own, patched by item id; what its
// terminal shows while it waits on you (`screen`), and the keys and
// messages sent to it (`keys`, `send`). The web app's js/conv.js.

/// One drawn piece of a conversation: a message, a reply, a call, a run of
/// calls, a helper, a note or a time.
struct Block: Identifiable, Equatable {
    enum Kind: Equatable {
        case user(pending: Bool)
        case text
        case note
        case time(Date)
        case group
        case tool
        case agent(running: Bool, live: String)
        case needItem
    }

    var id: String
    var kind: Kind
    var items: [Item]
}

@Observable @MainActor
final class ConversationModel {
    enum Mode: Equatable { case gone, needs, ended, early, frozen, compose }

    let conn: Conn
    let sid: String
    var items: [Item] = []
    var needs: Item?
    var pending: [Item] = []
    var more = false
    var transcript: String?
    var loaded = false
    var problem = ""
    var expanded: Swift.Set<String> = []
    var screen: ScreenResult?
    var screenProblem = ""
    var showTerm = false
    var showKeys = false
    var busyKey: String?
    var draft = ""
    var sending = false
    var sendProblem = ""
    /// Bumped to scroll to the newest item.
    var scrollRequest = 0
    /// Bumped when the whole conversation comes again, as after a reconnect.
    var resets = 0
    var lastName = ""
    @ObservationIgnored private var index: [String: Int] = [:]
    @ObservationIgnored private var stream: FrameStream?
    @ObservationIgnored private var pollTask: Task<Void, Never>?
    @ObservationIgnored private var running = false
    @ObservationIgnored private var lastMode: Mode?

    init(conn: Conn, sid: String) {
        self.conn = conn
        self.sid = sid
        lastName = conn.sessions[sid]?.name ?? ""
    }

    var row: Session? { conn.sessions[sid] }

    func start() {
        guard !running else { return }
        running = true
        conn.app?.conversation = self
        guard conn.status != .unpaired else {
            problem = "\(conn.m.name) no longer knows this phone."
            return
        }
        let s = FrameStream(gateway: conn.gateway, token: conn.token, args: ["session": sid, "conversation": true])
        s.onFrame = { [weak self] f in self?.frame(f) }
        s.onUnpaired = { [weak self] msg in
            guard let self else { return }
            self.problem = msg.isEmpty ? "\(self.conn.m.name) no longer knows this phone." : msg
            self.conn.app?.unpaired(self.conn, msg)
        }
        s.onRefused = { [weak self] e in
            self?.problem = e.message
            return e.code == "not_found" || e.code == "bad_args"
        }
        stream = s
        s.start()
        modeChanged()
    }

    func stop() {
        running = false
        stream?.stop()
        stream = nil
        pollTask?.cancel()
        pollTask = nil
        if conn.app?.conversation === self { conn.app?.conversation = nil }
    }

    func kick() {
        stream?.kick()
        if watching { poll() }
    }

    // MARK: Frames

    private func frame(_ f: Frame) {
        switch f.t {
        case "conversation":
            if let c = f.conversation { apply(c) }
        case "upsert":
            if let s = f.session, s.id == sid { conn.sessions[sid] = s }
        case "snapshot":
            if let s = f.sessions?.first(where: { $0.id == sid }) { conn.sessions[sid] = s }
        default:
            break
        }
    }

    private func apply(_ u: Conversation) {
        if let s = u.session, !s.isEmpty, s != sid { return }
        let fresh = u.items ?? []
        if u.reset == true {
            items = fresh
            index = [:]
            for (i, it) in fresh.enumerated() { index[it.id] = i }
            more = u.more ?? false
            resets += 1
        } else {
            for it in fresh {
                if let i = index[it.id] {
                    if items[i] != it { items[i] = it }
                } else {
                    index[it.id] = items.count
                    items.append(it)
                }
            }
        }
        needs = u.needs
        pending = u.pending ?? []
        transcript = (u.transcript ?? "").isEmpty ? nil : u.transcript
        loaded = true
        problem = ""
        modeChanged()
    }

    // MARK: What it shows

    var mode: Mode {
        guard let s = row else { return .gone }
        if needs != nil || s.state == "needs_you" { return .needs }
        if s.state == "exited" || s.state == "failed" { return .ended }
        if s.state == "starting" || s.state == "stalled" { return .early }
        if s.state == "frozen" || s.frozen != nil { return .frozen }
        return .compose
    }

    /// The conversation's blocks, as the web app gathers them.
    var blocks: [Block] {
        var out: [Block] = []
        var run: [Item] = []
        var prev: Date?
        let s = row
        func flush() {
            if run.count >= 2 {
                out.append(Block(id: "g:" + run[0].id, kind: .group, items: run))
            } else {
                for it in run { out.append(Block(id: it.id, kind: .tool, items: [it])) }
            }
            run = []
        }
        for it in items {
            let t = it.time.real
            var gap = false
            if let t { gap = prev.map { t.timeIntervalSince($0) >= 30 * 60 } ?? true }
            let groupable = it.kind == "tool" && !(it.group ?? "").isEmpty && it.state == "ok"
            if !groupable || gap || (!run.isEmpty && run[0].group != it.group) { flush() }
            if gap, let t { out.append(Block(id: "t:" + it.id, kind: .time(t), items: [])) }
            if let t { prev = t }
            if groupable {
                run.append(it)
                continue
            }
            switch it.kind {
            case "user": out.append(Block(id: it.id, kind: .user(pending: false), items: [it]))
            case "text": out.append(Block(id: it.id, kind: .text, items: [it]))
            case "note": out.append(Block(id: it.id, kind: .note, items: [it]))
            case "agent":
                let subs = s?.subagents ?? []
                let running = it.state == "running" || (it.agent.map { a in subs.contains { $0.agent == a } } ?? false)
                out.append(Block(id: it.id, kind: .agent(running: running, live: Self.liveDetail(it, subs)), items: [it]))
            case "needs": out.append(Block(id: it.id, kind: .needItem, items: [it]))
            default: out.append(Block(id: it.id, kind: .tool, items: [it]))
            }
        }
        flush()
        for it in pending { out.append(Block(id: "p:" + it.id, kind: .user(pending: true), items: [it])) }
        return out
    }

    /// What a running helper does now: its own row's, or the one of its kind when only one runs.
    private static func liveDetail(_ it: Item, _ subs: [Subagent]) -> String {
        if let a = it.agent, let own = subs.first(where: { $0.agent == a }) { return own.detail ?? "" }
        guard it.state == "running", let type = it.agentType else { return "" }
        let same = subs.filter { $0.type == type }
        return same.count == 1 ? same[0].detail ?? "" : ""
    }

    /// Why there is nothing to show, and whether its terminal can help.
    var empty: (title: String, text: String, terminal: Bool) {
        let s = row
        if !loaded {
            if !problem.isEmpty { return ("Can’t follow this conversation", problem, true) }
            return ("Loading the conversation…", "", false)
        }
        if s?.state == "starting" { return ("Starting…", "Its conversation shows here as soon as it begins.", false) }
        if s?.state == "stalled" { return ("It hasn’t started", (s?.detail).map { $0 + "." } ?? "", true) }
        if transcript == nil {
            return ("This one lives in its terminal", "It started before Irie could follow conversations, so there’s nothing to show here. Its terminal has all of it.", true)
        }
        return ("Nothing here yet", "Messages show here as they come.", false)
    }

    // MARK: Its screen

    /// Whether the agent's screen is on show: the needs card, or its terminal.
    var watching: Bool {
        guard running else { return false }
        if mode == .needs { return true }
        return showTerm && blocks.isEmpty && empty.terminal
    }

    /// Called when the session's state or its needs change.
    func modeChanged() {
        let m = mode
        if m != lastMode {
            lastMode = m
            if m == .needs {
                screen = nil
                screenProblem = ""
                showTerm = false
                showKeys = false
            }
        }
        if watching {
            if pollTask == nil { poll() }
        } else {
            pollTask?.cancel()
            pollTask = nil
        }
    }

    func toggleTerminal() {
        showTerm.toggle()
        if showTerm { poll() } else { modeChanged() }
    }

    /// Reads the agent's screen now and every 1.5 s while it is on show.
    func poll() {
        pollTask?.cancel()
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self, self.watching else { break }
                if UIApplication.shared.applicationState == .active { await self.readScreen() }
                try? await Task.sleep(nanoseconds: 1_500_000_000)
            }
        }
    }

    func readScreen() async {
        do {
            let r = try await conn.call("screen", ["session": sid], timeout: 5, as: ScreenResult.self)
            if r != screen { screen = r }
            screenProblem = ""
        } catch {
            let e = error as? APIError ?? APIError(code: "error", message: error.localizedDescription)
            if e.unpaired {
                conn.app?.unpaired(conn, e.message)
                return
            }
            screenProblem = e.code == "unknown_op" ? "This machine’s Irie can’t read the screen yet: use the keys." : e.message
        }
    }

    func press(_ keys: [String]) async {
        guard busyKey == nil else { return }
        busyKey = keys.first
        Haptic.pick()
        do {
            _ = try await conn.call("keys", ["session": sid, "keys": keys], timeout: 8, as: Ignored.self)
        } catch {
            conn.app?.fail(conn, error)
        }
        busyKey = nil
        await readScreen()
    }

    // MARK: Sending

    func send() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !sending else { return }
        sending = true
        sendProblem = ""
        defer { sending = false }
        do {
            _ = try await conn.call("send", ["session": sid, "text": text], timeout: 20, as: Ignored.self)
            Haptic.success()
            if draft.trimmingCharacters(in: .whitespacesAndNewlines) == text { draft = "" }
        } catch {
            let e = error as? APIError ?? APIError(code: "error", message: error.localizedDescription)
            Haptic.error()
            if e.unpaired { conn.app?.unpaired(conn, e.message) } else { sendProblem = e.message }
        }
        scrollRequest += 1
    }
}
