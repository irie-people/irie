import Darwin
import Foundation
import SwiftUI

// A session's conversation, from the core's `conversation` subscription
// (DESIGN §4.7, internal/ipc/conversation.go): its messages, replies, tool
// calls in plain words, subagents and what it waits on you for.

/// One entry of a conversation.
struct ConversationItem: Codable, Identifiable, Equatable {
    var id: String
    var kind: String // user, text, tool, agent, note, needs
    var time: Date?
    var text: String?
    var phrase: String?
    var detail: String?
    var tool: String?
    var state: String? // running, ok, error, stopped
    var ms: Int64?
    var added: Int?
    var removed: Int?
    var group: String? // read, edit or search
    var output: String?
    var agentType: String?
    var tools: Int?
    var options: [String]?
    var reason: String?
    var queued: Bool?
    var pending: Bool?

    enum CodingKeys: String, CodingKey {
        case id, kind, time, text, phrase, detail, tool, state, ms, added, removed, group, output, tools, options, reason, queued, pending
        case agentType = "agent_type"
    }

    /// The time, unless the core had none (Go's zero time).
    var when: Date? { time.flatMap { $0.timeIntervalSince1970 > 0 ? $0 : nil } }
}

/// A conversation, or what changed in it.
struct ConversationUpdate: Codable {
    var session: String
    var transcript: String?
    var reset: Bool?
    var more: Bool?
    var items: [ConversationItem]?
    var needs: ConversationItem?
    var pending: [ConversationItem]?
}

private struct ConversationFrame: Decodable {
    var t: String
    var conversation: ConversationUpdate?
}

private struct FollowArgs: Encodable {
    var session: String
    var conversation = true
}

struct SendMessageArgs: Encodable {
    var session: String
    var text: String
}

extension CoreClient {
    /// Streams one session's conversation until the task is cancelled or the
    /// connection ends. The read times out every second, so a cancelled
    /// follow stops within a second.
    func followConversation(_ session: String, onUpdate: @escaping @Sendable (ConversationUpdate) -> Void) async throws {
        let path = socketPath
        let reader = Task.detached(priority: .userInitiated) {
            let c = try CoreConnection(path: path, timeout: 1)
            defer { c.close() }
            try c.send(op: "subscribe", args: FollowArgs(session: session))
            var first = true
            while !Task.isCancelled {
                let line: Data
                do {
                    line = try c.readLine()
                } catch let CoreError.system(_, code) where code == EAGAIN || code == EWOULDBLOCK || code == EINTR {
                    continue
                }
                if first {
                    // The subscribe reply, which may be an error.
                    first = false
                    let r = try Wire.decoder.decode(Reply<Empty>.self, from: line)
                    if let e = r.error { throw e }
                    continue
                }
                if let f = try? Wire.decoder.decode(ConversationFrame.self, from: line), f.t == "conversation", let u = f.conversation {
                    onUpdate(u)
                }
            }
        }
        try await withTaskCancellationHandler {
            try await reader.value
        } onCancel: {
            reader.cancel()
        }
    }
}

/// The shown session's conversation, kept live by one subscription that
/// follows the selection.
@MainActor
final class ConversationModel: ObservableObject {
    @Published private(set) var session: String?
    @Published private(set) var items: [ConversationItem] = []
    @Published private(set) var needs: ConversationItem?
    @Published private(set) var pending: [ConversationItem] = []
    @Published private(set) var transcript: String?
    @Published private(set) var more = false
    /// The first frame arrived.
    @Published private(set) var loaded = false
    /// Why the conversation could not be followed, while it cannot.
    @Published private(set) var problem: String?
    @Published var sending = false

    private var index: [String: Int] = [:]
    private var follow: Task<Void, Never>?
    private var client: CoreClient?

    /// Follows a session's conversation, and stops following the last one.
    func follow(_ id: String?, client: CoreClient) {
        self.client = client
        guard id != session || follow == nil && id != nil else { return }
        follow?.cancel()
        follow = nil
        session = id
        items = []
        index = [:]
        needs = nil
        pending = []
        transcript = nil
        more = false
        loaded = false
        problem = nil
        guard let id else { return }
        if let fixture = ConversationFixture.update(for: id) {
            apply(fixture, for: id)
            return
        }
        follow = Task { [weak self] in
            while !Task.isCancelled {
                do {
                    try await client.followConversation(id) { update in
                        Task { @MainActor in self?.apply(update, for: id) }
                    }
                } catch is CancellationError {
                    return
                } catch {
                    if Task.isCancelled { return }
                    await MainActor.run { self?.problem = error.localizedDescription }
                }
                try? await Task.sleep(nanoseconds: 1_500_000_000)
            }
        }
    }

    func apply(_ u: ConversationUpdate, for id: String) {
        guard id == session else { return }
        let new = u.items ?? []
        if u.reset == true {
            items = new
            index = Dictionary(new.enumerated().map { ($1.id, $0) }, uniquingKeysWith: { _, last in last })
            more = u.more ?? false
        } else {
            for it in new {
                if let i = index[it.id] {
                    items[i] = it
                } else {
                    index[it.id] = items.count
                    items.append(it)
                }
            }
        }
        if needs != u.needs { needs = u.needs }
        let p = u.pending ?? []
        if pending != p { pending = p }
        transcript = u.transcript
        loaded = true
        problem = nil
    }

    /// Types a message to the agent, as a person would. Returns why it
    /// could not, or nil once it is on its way.
    func send(_ text: String) async -> String? {
        guard let id = session, let client else { return "No session is shown." }
        sending = true
        defer { sending = false }
        do {
            _ = try await client.call("send", SendMessageArgs(session: id, text: text), as: Empty.self)
            return nil
        } catch {
            return error.localizedDescription
        }
    }
}
