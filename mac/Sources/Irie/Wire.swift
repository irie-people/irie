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
    /// The tool call the detail is about, as "Bash · go test ./...".
    var command: String?
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
    /// Its agent-team teammates, each a terminal of its own on the team
    /// server, in the order they started (TeamSplit.swift).
    var team: [Teammate]?
    var tokens: Tokens?
    /// Its place in the list, which the user sets; whether it is parked;
    /// when it was frozen, and whether its agent still runs on to its next
    /// tool call; 1 while it is promoted (Controls.swift).
    var order: Int?
    var parked: Bool?
    var frozen: Date?
    var freezing: Bool?
    var priority: Int?
    /// What its jot left that has not landed, by the jot dog's last look,
    /// while its turn is over (DESIGN §4.8); nil when it is through.
    var loose: Loose?
    /// What the trod bot did for it in this night's shift or the last
    /// (DESIGN §4.10); nil when it did nothing.
    var trod: TrodRow?
    /// Which of the agents' colours it takes while it works, from 1 to
    /// Theme.agentSlots, the same for as long as it lives; the core picks
    /// it so that cards side by side differ (internal/core, hue.go).
    var hue: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, agent, state, reason, detail, command, tail, warnings, created, cwd, repo, worktree, branch, copied, profile, pane, shown, subagents, team, tokens
        case order, parked, frozen, freezing, priority, loose, trod, hue
        case exitCode = "exit_code"
        case vendorId = "vendor_id"
    }
}

/// How often the trod bot sent a session on in one shift, when it last
/// did, and why: question, error, limit or loose.
struct TrodRow: Codable, Equatable {
    var sent: Int
    var last: Date
    var reason: String
}

/// What a jot left that has not landed: files not committed, and its own
/// commits not yet in its repo's land branch (DESIGN §4.8). The core says
/// it in words in the session's detail too.
struct Loose: Codable, Equatable {
    var dirty: Int?
    var ahead: Int?
    var land: String?
}

/// What a session's model calls have used, each reply counted once.
struct Tokens: Codable, Equatable {
    var input: Int64
    var output: Int64
    var cacheRead: Int64
    var cacheWrite: Int64

    var total: Int64 { input + output + cacheRead + cacheWrite }

    /// 48.2k tokens: 1.2k in, 3.4k out, 40k cache read, 3.6k cache write
    var breakdown: String {
        "\(Self.short(total)) tokens: \(Self.short(input)) in, \(Self.short(output)) out, "
            + "\(Self.short(cacheRead)) cache read, \(Self.short(cacheWrite)) cache write"
    }

    static func short(_ n: Int64) -> String {
        switch n {
        case ..<1000: return "\(n)"
        case ..<1_000_000: return String(format: n < 10000 ? "%.1fk" : "%.0fk", Double(n) / 1000)
        default: return String(format: n < 10_000_000 ? "%.1fM" : "%.0fM", Double(n) / 1_000_000)
        }
    }

    enum CodingKeys: String, CodingKey {
        case input, output
        case cacheRead = "cache_read"
        case cacheWrite = "cache_write"
    }
}

/// A running subagent of a session: a helper.
struct Subagent: Codable, Equatable, Identifiable {
    var agent: String
    var type: String?
    var started: Date
    var detail: String?
    var command: String?
    var needsYou: Bool?

    var id: String { agent }

    enum CodingKeys: String, CodingKey {
        case agent, type, started, detail, command
        case needsYou = "needs_you"
    }
}

/// One of a session's agent-team teammates: a Claude Code session of its
/// own, in a terminal on the team server, where its tmux session is
/// `session`.
struct Teammate: Codable, Equatable, Identifiable {
    var session: String
    var name: String
    var color: String?
    var type: String?
    var title: String?
    var working: Bool?
    var dead: Bool?
    var exitCode: Int?

    var id: String { session }

    enum CodingKeys: String, CodingKey {
        case session, name, color, type, title, working, dead
        case exitCode = "exit_code"
    }
}

/// One streamed message after subscribe. Which fields are set depends on t.
struct Frame: Decodable {
    var t: String
    var sessions: [Session]?
    var session: Session?
    var id: String?
    /// An outside frame: the sessions outside Irie, and why the list may be old.
    var outside: [OutsideSession]?
    var problem: String?
    /// A closed frame: the sessions reopen can bring back, the last closed first.
    var closed: [ClosedSession]?
    /// A snapshot's or a header frame's header.
    var header: Header?
    /// A history frame: what undo and redo would take next, empty when
    /// nothing (Undo.swift).
    var undo: String?
    var redo: String?
}

/// What a snapshot says besides its rows.
struct Header: Decodable {
    /// ui.red_on_done: a done session is drawn red, not green.
    var redOnDone: Bool?
    /// spawn.close_when_done: finished sessions close themselves, so there
    /// is no To verify count.
    var closeWhenDone: Bool?
    /// ui.helper_style: a helper beside its session is drawn as cards
    /// ("browser") or as a terminal ("terminal").
    var helperStyle: String?
    /// The trod bot, while it is within its hours (DESIGN §4.10).
    var trod: TrodShift?

    enum CodingKeys: String, CodingKey {
        case redOnDone = "red_on_done"
        case closeWhenDone = "close_when_done"
        case helperStyle = "helper_style"
        case trod
    }
}

/// The trod bot within its hours: until when, whether you are away so it
/// steps in, whether it keeps the Mac awake, and whether the plan's use
/// stops it sending anything on.
struct TrodShift: Decodable, Equatable {
    var until: Date?
    var away: Bool?
    var awake: Bool?
    var capped: Bool?
    /// You started it by hand, so it works while you watch.
    var hand: Bool?
}

/// Start a run of the trod bot by hand, or stop one.
struct TrodArgs: Encodable {
    var start: Bool?
    var stop: Bool?
}

/// The trod bot's report, as far as the app reads it: the run you started
/// by hand last.
struct TrodResult: Decodable {
    struct Hand: Decodable {
        var on: Bool
        var why: String?
    }
    var hand: Hand?
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

/// The palette in use, which the agents' colours on Irie's status line
/// follow, so they match the cards.
struct ThemeArgs: Encodable {
    var theme: String
}

struct AttachInfo: Decodable, Equatable {
    var tmux: String
    var socket: String
    var target: String
    /// The session of the same server that shows the session the app
    /// splits, and the team server's socket; nil from a core before teams.
    var view: String?
    var teams: String?
}

/// What was typed in the task bar, for the core to read (DESIGN §4.2,
/// docs/CORE.md): the text as typed, the session selected for a : command
/// to act on, and where and on what a task runs when its words don't say.
/// The app keeps no rules of its own for the words.
struct InputArgs: Encodable {
    var text: String
    var session: String?
    /// The selected session outside Irie, which :move brings in.
    var outside: String?
    var repo: String?
    var inPlace: Bool?
    var profile: String?
    /// A model for a task instead of its profile's; only a core whose hello
    /// lists spawn-model takes it.
    var model: String?

    enum CodingKeys: String, CodingKey {
        case text, session, outside, repo, profile, model
        case inPlace = "in_place"
    }
}

/// What the core made of it: spawned, answered, ran or asked, and what to
/// say, as it is.
struct InputResult: Decodable {
    var did: String
    var op: String?
    var text: String
    var session: Session?
    /// Show the session with the keyboard: a tool session waits on its user.
    var show: Bool?
    /// The question to answer when did is asked, through op (undo or redo).
    var choice: UndoChoice?
}

/// An @ or + word to complete: "@" lists the repos, "+" the agents.
struct CompleteArgs: Encodable {
    var text: String
}

struct CompleteResult: Decodable {
    var items: [String]
    /// What each item runs and costs, for the + words.
    var notes: [String]?
}

struct ShowArgs: Encodable {
    var session: String
    var focus: Bool
    /// Beside its teammates (TeamSplit.swift).
    var split = false
}

struct RenameArgs: Encodable {
    var session: String
    var name: String
}

/// The session with the name the core gave it, numbered when another
/// session has it.
struct RenameResult: Decodable {
    var session: Session
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
