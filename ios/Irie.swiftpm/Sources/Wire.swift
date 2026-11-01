import Foundation

// The core's wire types as the gateway relays them (DESIGN §2.4,
// internal/ipc/proto.go, conversation.go, screen.go). They follow the Mac
// app's mac/Sources/Irie/Wire.swift, which this can't share: that file
// needs types the Mac app keeps in its AppKit files, and its Session leaves
// out what a phone shows (model, cost, context, memory, rating). Field
// names match the Go structs' JSON tags; a field this app doesn't draw is
// left out, so a new one in the core never breaks decoding.

/// One row of a machine's session list.
struct Session: Decodable, Identifiable, Equatable {
    var id: String
    var name: String
    var state: String
    var reason: String?
    var detail: String?
    /// The tool call the detail is about, as "Bash · go test ./...".
    var command: String?
    var warnings: [String]?
    var exitCode: Int?
    var created: Date?
    var cwd: String?
    var repo: String?
    var worktree: String?
    var branch: String?
    var profile: String?
    var subagents: [Subagent]?
    var tokens: Tokens?
    var model: String?
    var costUSD: Double?
    var context: Double?
    var memory: Int64?
    /// small, medium or hard, when auto routed it.
    var taskClass: String?
    var routed: Bool?
    var rated: String?
    var order: Int?
    var parked: Bool?
    var priority: Int?
    var frozen: Date?
    var freezing: Bool?
    var loose: Loose?
    /// Which of the agents' colours it takes while it works, from 1; the
    /// core picks it so that cards side by side differ.
    var hue: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, state, reason, detail, command, warnings, created, cwd, repo, worktree, branch, profile, subagents, tokens
        case model, context, memory, routed, rated, order, parked, priority, frozen, freezing, loose, hue
        case exitCode = "exit_code"
        case costUSD = "cost_usd"
        case taskClass = "class"
    }

    var isParked: Bool { parked ?? false }
    var isPromoted: Bool { (priority ?? 0) > 0 }
    var isFrozen: Bool { state == "frozen" || frozen != nil }
}

/// A running helper of a session.
struct Subagent: Decodable, Equatable, Identifiable {
    var agent: String
    var type: String?
    var detail: String?
    var needsYou: Bool?

    var id: String { agent }

    enum CodingKeys: String, CodingKey {
        case agent, type, detail
        case needsYou = "needs_you"
    }
}

/// What a session's model calls have used.
struct Tokens: Decodable, Equatable {
    var input: Int64?
    var output: Int64?
    var cacheRead: Int64?
    var cacheWrite: Int64?

    var total: Int64 { (input ?? 0) + (output ?? 0) + (cacheRead ?? 0) + (cacheWrite ?? 0) }

    enum CodingKeys: String, CodingKey {
        case input, output
        case cacheRead = "cache_read"
        case cacheWrite = "cache_write"
    }
}

/// What a jot left that has not landed (DESIGN §4.8).
struct Loose: Decodable, Equatable {
    var dirty: Int?
    var ahead: Int?
    var land: String?

    /// 2 files not committed · 1 commit not in main
    var words: String {
        func n(_ k: Int, _ w: String) -> String { "\(k) \(w)\(k == 1 ? "" : "s")" }
        var parts: [String] = []
        if let d = dirty, d > 0 { parts.append(n(d, "file") + " not committed") }
        if let a = ahead, a > 0 { parts.append(n(a, "commit") + " not in " + (land.flatMap { $0.isEmpty ? nil : $0 } ?? "its branch")) }
        return parts.joined(separator: " · ")
    }
}

/// A closed session that reopen can bring back.
struct ClosedSession: Decodable, Equatable, Identifiable {
    var id: String
    var name: String
    var closed: Date?
    var auto: Bool?
}

/// One streamed message after subscribe. Which fields are set depends on t.
struct Frame: Decodable {
    var t: String
    var sessions: [Session]?
    var session: Session?
    var id: String?
    var closed: [ClosedSession]?
    var header: Header?
    var undo: String?
    var redo: String?
    var conversation: Conversation?
}

/// What a snapshot says besides its rows.
struct Header: Decodable, Equatable {
    var version: String?
    var redOnDone: Bool?
    var plan: Plan?

    enum CodingKeys: String, CodingKey {
        case version, plan
        case redOnDone = "red_on_done"
    }
}

/// The use of a Claude plan's windows, from the newest status line.
struct Plan: Decodable, Equatable {
    struct Window: Decodable, Equatable {
        var used: Double?
        var resetsAt: Date?

        enum CodingKeys: String, CodingKey {
            case used
            case resetsAt = "resets_at"
        }
    }

    var fiveHour: Window?
    var sevenDay: Window?

    enum CodingKeys: String, CodingKey {
        case fiveHour = "five_hour"
        case sevenDay = "seven_day"
    }
}

/// A session's conversation, or what changed in it.
struct Conversation: Decodable {
    var session: String?
    var transcript: String?
    var reset: Bool?
    var more: Bool?
    var items: [Item]?
    var needs: Item?
    var pending: [Item]?
}

/// One entry of a conversation.
struct Item: Decodable, Equatable, Identifiable {
    var id: String
    var kind: String
    var time: Date?
    var text: String?
    var phrase: String?
    var detail: String?
    var tool: String?
    var state: String?
    var ms: Int64?
    var added: Int?
    var removed: Int?
    var group: String?
    var output: String?
    var agentType: String?
    var agent: String?
    var tools: Int?
    var from: String?
    var options: [String]?
    var queued: Bool?

    enum CodingKeys: String, CodingKey {
        case id, kind, time, text, phrase, detail, tool, state, ms, added, removed, group, output, agent, tools, from, options, queued
        case agentType = "agent_type"
    }
}

/// What a session's terminal shows, and the choices of the prompt on it.
struct ScreenResult: Decodable, Equatable {
    struct Option: Decodable, Equatable, Hashable {
        var key: String
        var label: String
        var selected: Bool?
    }

    var lines: [String]?
    var options: [Option]?
}

/// What an undo or redo did, or the question it asks first.
struct UndoResult: Decodable {
    var label: String?
    var did: String
    var choice: Choice?
}

/// A question the core asks before an uncertain step.
struct Choice: Decodable, Identifiable {
    struct Option: Decodable, Identifiable {
        var key: String
        var label: String
        var detail: String?
        var recommended: Bool?

        var id: String { key }
    }

    var id: String
    var question: String
    var options: [Option]
}

/// What the core made of what the task bar sent.
struct InputResult: Decodable {
    var did: String
    var op: String?
    var text: String?
    var session: Session?
    var show: Bool?
    var choice: Choice?
}

struct CompleteResult: Decodable {
    var items: [String]?
    var notes: [String]?
}

struct HelloResult: Decodable {
    var version: String?
    var features: [String]?
}

struct ReopenResult: Decodable {
    var session: Session?
}

/// GET /api/info: who a gateway is, with no pairing.
struct MachineInfo: Decodable, Equatable {
    var id: String
    var name: String?
    var version: String?
    var api: Int?
    var os: String?
}

/// POST /api/pair: the token, and the machine it pairs with.
struct PairResult: Decodable {
    struct Machine: Decodable {
        var id: String
        var name: String?
    }

    var token: String
    var machine: Machine
}

/// An answer of the gateway's: an op's {ok, result, error}, or an error alone.
struct Envelope<R: Decodable>: Decodable {
    var ok: Bool?
    var result: R?
    var error: WireError?
}

struct WireError: Decodable {
    var code: String?
    var msg: String?
}

/// Any JSON object, for a result this app doesn't read.
struct Ignored: Decodable {}

enum Wire {
    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        // Go's zero time and a time that won't parse are "no time", never
        // a reason to drop the whole frame.
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            return parseTime(s) ?? .distantPast
        }
        return d
    }()

    /// Parses Go's RFC 3339 times, whose fractions run to nanoseconds; the
    /// system formatter takes at most milliseconds.
    static func parseTime(_ s: String) -> Date? {
        var text = s
        if let dot = text.firstIndex(of: "."),
           let end = text[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
            let digits = text[text.index(after: dot)..<end]
            text.replaceSubrange(dot..<end, with: "." + String(digits.prefix(3)).padding(toLength: 3, withPad: "0", startingAt: 0))
        }
        if let d = fractional.date(from: text) { return d }
        return whole.date(from: text)
    }

    private static let fractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static let whole: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()
}

extension Optional where Wrapped == Date {
    /// The time, unless the core had none (Go's zero time).
    var real: Date? {
        guard let d = self, d.timeIntervalSince1970 > 0 else { return nil }
        return d
    }
}
