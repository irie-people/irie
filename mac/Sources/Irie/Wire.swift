import Foundation

// The core's socket API (DESIGN §2.4, internal/ipc/proto.go): NDJSON, one
// object per line. Field names match the Go structs' JSON tags.

/// One row of the session list.
struct Session: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var agent: String?
    var state: String
    var reason: String?
    var detail: String?
    var tail: [String]?
    var warnings: [String]?
    var exitCode: Int?
    var created: Date
    var cwd: String?
    var repo: String?
    var worktree: String?
    var branch: String?
    var copied: [String]?
    var profile: String?
    var vendorId: String?
    var pane: String?
    var shown: Bool?
    var subagents: [Subagent]?

    enum CodingKeys: String, CodingKey {
        case id, name, agent, state, reason, detail, tail, warnings, created, cwd, repo, worktree, branch, copied, profile, pane, shown, subagents
        case exitCode = "exit_code"
        case vendorId = "vendor_id"
    }
}

/// A running subagent of a session: a helper.
struct Subagent: Codable, Equatable, Identifiable {
    var agent: String
    var type: String?
    var started: Date
    var detail: String?
    var needsYou: Bool?

    var id: String { agent }

    enum CodingKeys: String, CodingKey {
        case agent, type, started, detail
        case needsYou = "needs_you"
    }
}

/// One streamed message after subscribe. Which fields are set depends on t.
struct Frame: Decodable {
    var t: String
    var sessions: [Session]?
    var session: Session?
    var id: String?
}

struct HelloArgs: Encodable {
    var client = "mac"
    var api = 1
}

struct HelloResult: Decodable {
    var version: String
    var api: Int
    var features: [String]
}

struct SubscribeArgs: Encodable {
    var view = "app"
}

struct AttachInfo: Decodable, Equatable {
    var tmux: String
    var socket: String
    var target: String
}

struct SpawnArgs: Encodable {
    var task = ""
    var repo: String?
    var inPlace: Bool?
    var profile: String?

    enum CodingKeys: String, CodingKey {
        case task, repo, profile
        case inPlace = "in_place"
    }
}

struct SpawnResult: Decodable {
    var session: Session
}

struct ShowArgs: Encodable {
    var session: String
    var focus: Bool
}

struct Empty: Codable {}

struct WireError: Decodable, Error, LocalizedError {
    var code: String
    var msg: String
    var errorDescription: String? { msg }
}

struct Request<A: Encodable>: Encodable {
    var id: Int
    var op: String
    var args: A?
}

struct Reply<R: Decodable>: Decodable {
    var id: Int?
    var ok: Bool
    var result: R?
    var error: WireError?
}

enum Wire {
    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            guard let date = parseTime(s) else {
                throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "bad time \(s)"))
            }
            return date
        }
        return d
    }()

    static let encoder = JSONEncoder()

    /// Parses Go's RFC 3339 times, whose fractions run to nanoseconds; the
    /// system formatter takes at most milliseconds.
    static func parseTime(_ s: String) -> Date? {
        var text = s
        if let dot = text.firstIndex(of: "."),
           let end = text[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
            let digits = text[text.index(after: dot)..<end]
            text.replaceSubrange(dot..<end, with: "." + String(digits.prefix(3)).padding(toLength: 3, withPad: "0", startingAt: 0))
        }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: text) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: text)
    }
}
