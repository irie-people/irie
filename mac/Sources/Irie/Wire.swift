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
    /// Its tutor mode, explain or teach, while it is in one
    /// (docs/proposals/learning.md).
    var tutor: String?

    enum CodingKeys: String, CodingKey {
        case id, name, agent, state, reason, detail, command, tail, warnings, created, cwd, repo, worktree, branch, copied, profile, pane, shown, subagents, team, tokens
        case order, parked, frozen, freezing, priority, loose, trod, hue, tutor
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
    /// Mullvad VPN, while vpn.on (docs/proposals/extensions.md §4).
    var vpn: VPNMark?

    enum CodingKeys: String, CodingKey {
        case redOnDone = "red_on_done"
        case closeWhenDone = "close_when_done"
        case helperStyle = "helper_style"
        case trod, vpn
    }
}

/// Mullvad VPN as the core sees it: its state, the relay's place and the
/// state in one line. Never an address or the account's number.
struct VPNMark: Decodable, Equatable {
    var state: String?
    var city: String?
    var country: String?
    var host: String?
    var lockdown: Bool?
    /// Off, and lockdown mode blocks the network.
    var blocked: Bool?
    var text: String
    /// Why the core can't read the VPN, such as no mullvad CLI.
    var problem: String?
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

/// Any JSON value: an arch agent's setting as the core has it, or an
/// action's args, which the app passes on as they came.
enum JSONValue: Codable, Equatable {
    case null, bool(Bool), number(Double), string(String), array([JSONValue]), object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() {
            self = .null
        } else if let b = try? c.decode(Bool.self) {
            self = .bool(b)
        } else if let n = try? c.decode(Double.self) {
            self = .number(n)
        } else if let s = try? c.decode(String.self) {
            self = .string(s)
        } else if let a = try? c.decode([JSONValue].self) {
            self = .array(a)
        } else {
            self = .object(try c.decode([String: JSONValue].self))
        }
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case let .bool(v): try c.encode(v)
        case let .number(v): try c.encode(v)
        case let .string(v): try c.encode(v)
        case let .array(v): try c.encode(v)
        case let .object(v): try c.encode(v)
        }
    }

    var bool: Bool? { if case let .bool(v) = self { v } else { nil } }
    var number: Double? { if case let .number(v) = self { v } else { nil } }
    var string: String? { if case let .string(v) = self { v } else { nil } }
    var strings: [String]? { if case let .array(v) = self { v.compactMap(\.string) } else { nil } }

    /// The value as the config op takes it.
    var setting: SettingValue {
        switch self {
        case let .bool(v): .bool(v)
        case let .number(v): .number(v)
        case let .string(v): .string(v)
        case let .array(v): .strings(v.compactMap(\.string))
        default: .null
        }
    }
}

/// Irie's arch agents, as the core lists them (op arch, DESIGN §4.11).
struct ArchResult: Decodable {
    var agents: [ArchAgent]
}

/// One arch agent, as a client draws him.
struct ArchAgent: Decodable, Identifiable, Equatable {
    var name: String
    var nickname: String?
    var icon: String?
    var title: String
    var about: String
    var on: Bool
    var status: String?
    var actions: [ArchAction]?
    var settings: [ArchSetting]?
    var id: String { name }

    /// How he is introduced: his icon, his name (with his nickname first)
    /// and his work.
    var heading: String {
        var n = name.prefix(1).uppercased() + name.dropFirst()
        if let nick = nickname, !nick.isEmpty { n = "\(nick) (\(n))" }
        if let i = icon, !i.isEmpty { n = "\(i) \(n)" }
        return title.isEmpty ? n : "\(n) · \(title)"
    }
}

/// Something you can do with an arch agent: the app calls op with args as
/// they came, and says done once it worked.
struct ArchAction: Decodable, Equatable {
    var label: String
    var op: String
    var args: JSONValue?
    var done: String?
}

/// One of an arch agent's settings, with its value now.
struct ArchSetting: Decodable, Equatable {
    var key: String
    var kind: String
    var label: String
    var help: String?
    var value: JSONValue?
}

/// Start a boost of Gabriel's, or stop it (DESIGN §4.11): for its time, the
/// focus gets the machine first whether it is busy or not.
struct GabrielArgs: Encodable {
    var boost: Bool?
    var stop: Bool?
}

/// Gabriel's report, as far as the app reads it.
struct GabrielResult: Decodable {
    var boostUntil: Date?
    var lowPower: Bool?
    var freed: [String]?
    enum CodingKeys: String, CodingKey {
        case freed
        case boostUntil = "boost_until", lowPower = "low_power"
    }
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
struct InputArgs: Encodable, Equatable {
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
    /// The session a task typed before was added to, as more for its jot:
    /// the core takes it back and starts it on its own (DESIGN §4.2).
    var apart: String?
    /// off, explain or teach for a task, when its words do not say; nil
    /// takes learn.tutor. Only a core whose hello lists spawn-tutor takes it.
    var tutor: String?

    enum CodingKeys: String, CodingKey {
        case text, session, outside, repo, profile, model, apart, tutor
        case inPlace = "in_place"
    }
}

/// What the Help menu shows (docs/proposals/learning.md): the user's own
/// links, then the learning resources, and the glossary.
struct LearnArgs: Encodable {
    var session: String?
}

struct LearnResult: Decodable {
    var resources: [LearnResource]
    var glossary: [LearnTerm]
}

struct LearnResource: Decodable, Identifiable, Hashable {
    var group: String
    var title: String
    var url: String
    var about: String
    var note: String?
    var lang: String?
    /// One of the user's own links (learn.links).
    var yours: Bool?
    /// A tutorial of a language the shown session's project is written in.
    var here: Bool?
    var id: String { title }
}

struct LearnTerm: Decodable, Identifiable, Hashable {
    var term: String
    var means: String
    var id: String { term }
}

/// What the core made of it: spawned, added, answered, ran or asked, and
/// what to say, as it is.
struct InputResult: Decodable {
    var did: String
    var op: String?
    var text: String
    var session: Session?
    /// Show the session with the keyboard: a tool session waits on its user.
    var show: Bool?
    /// The question to answer when did is asked, through op (undo or redo).
    var choice: UndoChoice?
    /// When did is added: when the task is typed to the agent; until then
    /// the same text sent with apart takes it back.
    var until: Date?
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
