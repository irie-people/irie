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
    /// While its own turn is over (done or idle) and its helpers or
    /// teammates still work: how many of each it waits on (Look.waiting).
    var subs: Subs?
    var tokens: Tokens?
    /// Its place in the list, which the user sets; whether it is parked;
    /// when it was frozen, and whether its agent still runs on to its next
    /// tool call; 1 while it is boosted (Controls.swift).
    var order: Int?
    var parked: Bool?
    /// Whether it is pinned, in the Pinned group at the top (Tabs.swift).
    var pinned: Bool?
    /// The space it is in; nil or "" for the first (Spaces.swift).
    var space: String?
    /// The group of yours it is in; nil or "" for none (Groups.swift).
    var group: String?
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
    /// The paired machine it runs on, when that is another one (DESIGN
    /// §2.8): its id is then that machine's name, a slash and its own id
    /// there, which the core passes on.
    var host: String?
    /// How often its prompt cache was written again today and this week,
    /// and what that cost; its words, for the card, come only once today's
    /// cost enough to matter (DESIGN §6.3).
    var rebuilds: Rebuilds?
    /// What waking it costs now that its prompt cache has run out, which
    /// Resume says (DESIGN §6.3); nil while its cache is warm.
    var cold: Cold?
    /// What its agent lock names that a check asks OSV.dev about, and the
    /// card's words while a known advisory affects one (DESIGN §4.21).
    var lock: LockRow?

    enum CodingKeys: String, CodingKey {
        case id, name, agent, state, reason, detail, command, tail, warnings, created, cwd, repo, worktree, branch, copied, profile, pane, shown, subagents, team, subs, tokens
        case order, parked, pinned, space, group, frozen, freezing, priority, loose, trod, hue, tutor, host, rebuilds, cold, lock
        case exitCode = "exit_code"
        case vendorId = "vendor_id"
    }
}

/// A session's prompt-cache rebuilds, today and this week, at list price
/// (DESIGN §6.3). The core writes the card's words, "$4.10 today: woke cold 3
/// times", once today's cost usage.rebuild_card.
struct Rebuilds: Codable, Equatable {
    var today: Int
    var todayUSD: Double
    var week: Int
    var weekUSD: Double
    var cause: String?
    var words: String?

    enum CodingKeys: String, CodingKey {
        case today, week, cause, words
        case todayUSD = "today_usd"
        case weekUSD = "week_usd"
    }

    /// Today's and the week's, for the details panel: "Cache rebuilt 3×
    /// today, $4.10 · 9× this week, $12.30".
    var summary: String? {
        guard week > 0 else { return nil }
        let w = String(format: "%d× this week, $%.2f", week, weekUSD)
        guard today > 0 else { return "Cache rebuilt " + w }
        return String(format: "Cache rebuilt %d× today, $%.2f · ", today, todayUSD) + w
    }
}

/// What waking a session costs once its prompt cache has run out: its next
/// turn writes its whole context into the cache again. The core says the
/// price in a few words, "~$2.40 to wake".
struct Cold: Codable, Equatable {
    var tokens: Int64
    var usd: Double
    var words: String
}

/// What a row says of its session's agent lock (DESIGN §4.21): the
/// harness and package versions it names, each with what the last check
/// found, and the card's words while a known advisory affects one: "Claude
/// Code 2.1.250: 1 advisory, fixed in 2.1.251".
struct LockRow: Codable, Equatable {
    var packages: [LockPackage]?
    var advisories: Int?
    var words: String?

    /// The versions it names, for the details panel: "Locked: Claude Code
    /// 2.1.250, MCP server github 0.6.2", and whether OSV.dev was asked.
    var summary: String? {
        guard let packages, !packages.isEmpty else { return nil }
        let names = packages.map { "\($0.part ?? $0.name) \($0.version)" }.joined(separator: ", ")
        let asked = packages.allSatisfy { $0.checked == true } ? "" : " · not checked yet"
        return "Locked: " + names + asked
    }
}

/// A package at one version, as OSV.dev names it, with what the last check
/// found for it.
struct LockPackage: Codable, Equatable {
    var ecosystem: String
    var name: String
    var version: String
    var part: String?
    var checked: Bool?
    var advisories: Int?
    var fixed: String?
}

/// How often the trod bot sent a session on in one shift, when it last
/// did, and why: question, error, limit or loose.
struct TrodRow: Codable, Equatable {
    var sent: Int
    var last: Date
    var reason: String
}

/// What a session whose own turn is over still waits on: its running
/// helpers and its teammates at work. The session's detail says what it
/// waits on in words: its work in the background, as its Stop named it, or
/// else "Waiting on 3 teammates".
struct Subs: Codable, Equatable {
    var helpers: Int?
    var teammates: Int?
}

/// What a jot left that has not landed: files not committed, and its own
/// commits not yet in its repo's land branch; or a claim its own record
/// contradicts, "tests" when it says the tests pass while its last test run
/// failed (DESIGN §4.8). The core says it in words in the session's detail
/// too.
struct Loose: Codable, Equatable {
    var dirty: Int?
    var ahead: Int?
    var land: String?
    var unproven: String?
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
    /// A notepad frame: the notes for later (Notes.swift).
    var notepad: Notepad?
    /// A card frame: show a card of this kind beside the session id
    /// (Rolodex.swift, `irie ctl card`); for a code card, the file to open
    /// on it and the line and column to go to (`irie ctl open`).
    var kind: String?
    var path: String?
    var line: Int?
    var col: Int?
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
    /// ui.terminal_links: where a link clicked in a terminal opens,
    /// "rolodex" (the session's panes) or "browser" (the Mac's own apps).
    var terminalLinks: String?
    /// The trod bot, while it is within its hours (DESIGN §4.10).
    var trod: TrodShift?
    /// The VPN, while vpn.on (DESIGN §4.18).
    var vpn: VPNMark?
    /// Gabriel's boost while one is on, however it was started (DESIGN
    /// §4.11), from a core whose hello lists boost-always.
    var boost: BoostMark?
    /// How busy the machine is, read every 5 s (DESIGN §2.4).
    var load: MachineLoad?
    /// The spaces, in their order, the first first (Spaces.swift).
    var spaces: [SpaceInfo]?
    /// Your groups, in the order they were made (Groups.swift).
    var groups: [GroupInfo]?
    /// Your zen, whether an app shows it, and what it offers now, from a
    /// core that keeps your zen (Zen.swift).
    var zen: ZenState?

    enum CodingKeys: String, CodingKey {
        case redOnDone = "red_on_done"
        case closeWhenDone = "close_when_done"
        case helperStyle = "helper_style"
        case terminalLinks = "terminal_links"
        case trod, vpn, boost, load, spaces, groups, zen
    }
}

/// Your zen's settings, as the core keeps them (docs/proposals/zen.md §3).
struct ZenProfile: Codable, Equatable {
    var layout: String
    var columns: Int
    /// Zen's own text size in points; 0 keeps the window's.
    var textSize: Int
    var fullScreen: Bool
    /// Zen asks the agent to be gentle.
    var gentle: Bool
    /// Minutes at the computer before zen offers a rest; 0 for none.
    var breaks: Int
    /// Late at night, zen says once that you can rest.
    var night: Bool

    enum CodingKeys: String, CodingKey {
        case layout, columns, gentle, breaks, night
        case textSize = "text_size"
        case fullScreen = "full_screen"
    }
}

/// Zen as the header gives it: your zen, or a step's change on trial, the
/// newest version, and the one thing zen offers now.
struct ZenState: Decodable, Equatable {
    var profile: ZenProfile
    var head: String?
    var on: Bool?
    var trying: String?
    var offer: ZenOffer?
}

/// The one thing zen offers at a time: a step of its path, a rest or a word
/// at night. It stays until it is answered.
struct ZenOffer: Decodable, Equatable, Identifiable {
    var id: String
    /// learn, try, keep, rest or night.
    var kind: String
    var text: String
    var yes: String
    var no: String?
    /// What the first button also opens: "history".
    var show: String?
    /// A page the text names.
    var link: String?
}

/// One version of your zen.
struct ZenVersion: Decodable, Equatable, Identifiable {
    /// "sha512:" and the hex of the SHA-512 of its canonical bytes.
    var name: String
    var parent: String?
    var created: Date
    var why: String
    /// The name of its settings alone, which versions with the same
    /// settings share.
    var state: String
    var zen: ZenProfile

    var id: String { name }
}

/// Part of your zen to change: the keys set.
struct ZenChange: Encodable, Equatable {
    var layout: String?
    var columns: Int?
    var textSize: Int?
    var fullScreen: Bool?
    var gentle: Bool?
    var breaks: Int?
    var night: Bool?

    var isEmpty: Bool { self == ZenChange() }

    /// The keys other sets, on top of these.
    mutating func merge(_ other: ZenChange) {
        layout = other.layout ?? layout
        columns = other.columns ?? columns
        textSize = other.textSize ?? textSize
        fullScreen = other.fullScreen ?? fullScreen
        gentle = other.gentle ?? gentle
        breaks = other.breaks ?? breaks
        night = other.night ?? night
    }

    enum CodingKeys: String, CodingKey {
        case layout, columns, gentle, breaks, night
        case textSize = "text_size"
        case fullScreen = "full_screen"
    }
}

/// The zen op's one ask at a time.
struct ZenArgs: Encodable {
    var on: Bool?
    var session: String?
    var windowText: Int?
    var set: ZenChange?
    var why: String?
    var offer: String?
    var answer: String?
    var log: Int?
    var restore: String?

    enum CodingKeys: String, CodingKey {
        case on, session, set, why, offer, answer, log, restore
        case windowText = "window_text"
    }
}

struct ZenResult: Decodable {
    var zen: ZenState?
    var version: ZenVersion?
    var versions: [ZenVersion]?
}

/// Gabriel's boost: when one started by hand ends, and whether it is
/// always on while you're at the computer (arch.gabriel.always).
struct BoostMark: Decodable, Equatable {
    var until: Date?
    var always: Bool?
}

/// How busy the core's machine is: its CPUs, the work waiting to run over
/// the last minute and the kernel's memory pressure (1, 2 or 4).
struct MachineLoad: Decodable, Equatable {
    var cpus: Int
    var runQueue: Double
    var pressure: Int?

    /// Busy as Gabriel counts it: the work waiting leaves less than a CPU
    /// free, or memory is under pressure (DESIGN §4.11).
    var busy: Bool { cpus > 0 && (runQueue >= Double(cpus - 1) || (pressure ?? 0) >= 2) }

    enum CodingKeys: String, CodingKey {
        case cpus, pressure
        case runQueue = "run_queue"
    }
}

/// The VPN as the core sees it, whichever vpn.provider names: its state,
/// where it goes and the state in one line. Never an address or an
/// account's number.
struct VPNMark: Decodable, Equatable {
    /// Which VPN, as vpn.provider names it, such as mullvad or tailscale;
    /// nil from a core from before there were others than Mullvad.
    var kind: String?
    /// Its name, such as Cisco Secure Client.
    var title: String?
    /// What its words do beyond on and off: place, lockdown, time.
    var can: [String]?
    var state: String?
    var city: String?
    var country: String?
    var host: String?
    var lockdown: Bool?
    /// Off, and lockdown mode blocks the network.
    var blocked: Bool?
    var text: String
    /// Why the core can't read the VPN, such as its command missing.
    var problem: String?

    /// Whether its words do what (place, lockdown or time): a core from
    /// before other VPNs drives only Mullvad, which does them all.
    func can(_ what: String) -> Bool {
        guard kind != nil else { return true }
        return can?.contains(what) ?? false
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
    /// What he found or holds, a line each, such as a cache to clear.
    var items: [ArchItem]?
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
/// they came, and says done once it worked. One with ask is yours to say
/// OK to first: the app shows ask, such as the commands a fix runs, and
/// only on your OK calls op, with args and "ok": true (argsOK).
struct ArchAction: Decodable, Equatable {
    var label: String
    var op: String
    var args: JSONValue?
    var done: String?
    var ask: String?

    /// The args once you said OK to what ask shows.
    var argsOK: JSONValue {
        var o: [String: JSONValue] = [:]
        if case let .object(v) = args { o = v }
        o["ok"] = .bool(true)
        return .object(o)
    }
}

/// One thing an arch agent found or holds, as a line under his status: how
/// it stands (ok, note or warn), his advice, and what you can do about it.
struct ArchItem: Decodable, Equatable {
    var text: String
    var detail: String?
    var level: String?
    var actions: [ArchAction]?
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
    /// A theme of yours: its colours by role, as "#RRGGBB".
    var light: [String: String]?
    var dark: [String: String]?
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
    /// The space on show, where a task starts (Spaces.swift).
    var space: String?
    /// The note of the notepad the text came from: the core ticks it off
    /// once the input goes through (Notes.swift).
    var note: String?
    /// Send to Top: a task goes first in Pinned, boosted (DESIGN §4.1).
    /// Only a core whose hello lists spawn-top takes it.
    var top: Bool?

    enum CodingKeys: String, CodingKey {
        case text, session, outside, repo, profile, model, apart, tutor, space, note, top
        case inPlace = "in_place"
    }
}

/// One note of the notepad (DESIGN §4.20): a line of a checklist, kept for
/// later (Notes.swift).
struct Note: Codable, Identifiable, Equatable {
    var id: String
    var text: String
    var done: Bool?
    var created: Date?
    var changed: Date?
    /// When it last went to the task bar, and the session it started or
    /// went to.
    var sent: Date?
    var session: String?
    /// When a note in Recently Deleted was deleted.
    var deleted: Date?

    var isDone: Bool { done ?? false }
}

/// The notes in their order, and Recently Deleted, the last deleted first.
/// problem says what went wrong with the file and what the core did; while
/// readOnly the core only shows them. rev counts the file's changes, so the
/// app keeps the newest of a reply and a frame.
struct Notepad: Codable, Equatable {
    var notes: [Note] = []
    var deleted: [Note] = []
    var problem: String?
    var readOnly: Bool?
    var path: String?
    var rev: UInt64?
    /// The note the notepad is kept as in the Obsidian vault too, while it
    /// is (Vault.swift).
    var vault: NotepadVault?

    enum CodingKeys: String, CodingKey {
        case notes, deleted, problem, path, rev, vault
        case readOnly = "read_only"
    }

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        notes = try c.decodeIfPresent([Note].self, forKey: .notes) ?? []
        deleted = try c.decodeIfPresent([Note].self, forKey: .deleted) ?? []
        problem = try c.decodeIfPresent(String.self, forKey: .problem)
        readOnly = try c.decodeIfPresent(Bool.self, forKey: .readOnly)
        path = try c.decodeIfPresent(String.self, forKey: .path)
        rev = try c.decodeIfPresent(UInt64.self, forKey: .rev)
        vault = try? c.decodeIfPresent(NotepadVault.self, forKey: .vault)
    }
}

/// A change to the notepad. Each is the same however often it is sent, so
/// the app sends again what it is not sure went through: add, edit, check,
/// move, delete or restore; none lists.
struct NotepadArgs: Codable, Equatable {
    var action: String
    var id: String?
    var ids: [String]?
    var text: String?
    var done: Bool?
    var after: String?
    var before: String?

    enum CodingKeys: String, CodingKey {
        case id, ids, text, done, after, before
        case action = "do"
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
    /// What the strip under the terminal shows, ui.bar: "cheer" or "tips";
    /// nil from a core from before it (DESIGN §2.10).
    var bar: String?
    /// The tips it shows one at a time when it is tips.
    var tips: [String]?
    /// What the session's project's own docs teach, doc by doc, each in its
    /// order, for the Teach tile; nil from a core from before them.
    var topics: [LearnTopic]?
    /// The explanations the library keeps for the session's project, the
    /// newest first.
    var saved: [SavedExplanation]?
}

/// An explanation the library keeps (internal/explained).
struct SavedExplanation: Decodable, Hashable {
    var question: String
    var topic: String?
    var file: String
    var versions: Int
    var kept: Bool?
    var made: Date?
}

/// One thing a project's docs teach: an item of a list that leads with its
/// name in bold, the doc it is in, and the heading it sits under.
struct LearnTopic: Decodable, Hashable {
    var title: String
    var file: String
    var heading: String?
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

/// The variorum op (internal/core/variorum.go), which only a core whose
/// hello lists variorum serves: the Writing plugin's tab asks where things
/// stand, keeps one of Variorum's setups, lists the notes shared with
/// Irie, reads one, or shows one in Variorum (VariorumTab.swift). The core
/// calls Variorum's door; the app never does.
struct VariorumArgs: Codable, Equatable {
    /// status (the default), connect, forget, notes, read or show.
    var action: String?
    /// read: whose note, when note is empty.
    var session: String?
    var note: String?
    /// show: the paragraph whose takes to show.
    var block: String?
    /// show: note (the default) or takes.
    var target: String?
    /// connect: the setup as copied in Variorum, which the app never shows
    /// or keeps.
    var setup: String?
}

struct VariorumResult: Codable, Equatable {
    /// ready; setup, while a setup is missing; needs, while Variorum is
    /// missing or a kept setup is broken; or off.
    var state: String
    /// Where things stand, in words.
    var text: String
    /// Claude Code's setup is kept, for the writing sessions.
    var sessions: Bool?
    /// Any MCP client's setup is kept, which the tab reads with.
    var tab: Bool?
    /// Variorum.app is in an Applications folder.
    var installed: Bool?
    /// The action failed: a sentence to show as it is.
    var error: String?
    var notes: [VariorumNoteRef]?
    var note: VariorumNote?
    /// show: shown, or offered while Variorum offers it to the writer.
    var shown: String?
}

/// A note shared with Irie, for the tab's menu of notes.
struct VariorumNoteRef: Codable, Equatable, Identifiable {
    var id: String
    var title: String
    /// When it was last changed, in RFC 3339, kept as text so that a time
    /// that doesn't parse costs only itself.
    var edited: String?
}

/// A note as Variorum holds it, as far as the core read it.
struct VariorumNote: Codable, Equatable, Identifiable {
    var id: String
    var title: String
    var blocks: [VariorumBlock]
    /// It goes on past the blocks read.
    var more: Bool?
    /// When the core read it.
    var read: Date
}

/// A paragraph, with its takes and its comments.
struct VariorumBlock: Codable, Equatable, Identifiable {
    var id: String
    var name: String?
    /// The take the note shows.
    var shown: String?
    var takes: [VariorumTake]
    var comments: [VariorumComment]?
}

struct VariorumTake: Codable, Equatable, Identifiable {
    var id: String
    var label: String?
    var text: String
    var chosen: Bool?
}

struct VariorumComment: Codable, Equatable, Identifiable {
    var id: String
    var text: String
}

// Go writes an empty list as null; a note without paragraphs, or a
// paragraph without takes, still reads.
extension VariorumNote {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        blocks = try c.decodeIfPresent([VariorumBlock].self, forKey: .blocks) ?? []
        more = try c.decodeIfPresent(Bool.self, forKey: .more)
        read = try c.decode(Date.self, forKey: .read)
    }
}

extension VariorumBlock {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name)
        shown = try c.decodeIfPresent(String.self, forKey: .shown)
        takes = try c.decodeIfPresent([VariorumTake].self, forKey: .takes) ?? []
        comments = try c.decodeIfPresent([VariorumComment].self, forKey: .comments)
    }
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

/// An ask for a materialized card (docs/proposals/materialized-cards.md,
/// internal/ipc/materialize.go): a session's own card, where it stands,
/// which the core makes from its conversation; or, with explain, a card
/// that explains what the person asked, as the Teach tile shows it, from
/// that session's conversation when one is given.
struct MaterializeArgs: Encodable {
    var session: String?
    /// How long the core waits for a remake under way, in nanoseconds, as
    /// Go writes a time.Duration; nil answers at once.
    var wait: Int64? = nil
    var explain: String? = nil
    /// Drops the kept card, so the next ask makes it again.
    var forget: Bool? = nil
    /// A turn of the Teach tile's chat: the newest card made again with
    /// this done; the topic of the project's docs it explains; and keep,
    /// which marks the explanation as one that helped (internal/explained).
    var refine: String? = nil
    var topic: String? = nil
    var keep: Bool? = nil
}

/// A materialized card as it stands. `state` is cold, fresh, stale or
/// refreshing; `spec` the card, none while cold, or when it is from a
/// catalog this app cannot read; `err` why the last remake failed.
struct MaterializeResult: Decodable, Equatable {
    var id: String
    var state: String
    var spec: GenCard?
    var made: Date?
    var by: String?
    var err: String?
    var began: Bool?
    /// For an explanation the library keeps: its Markdown file, its
    /// versions, and whether it was kept.
    var saved: String?
    var versions: Int?
    var kept: Bool?

    private enum CodingKeys: String, CodingKey { case id, state, spec, made, by, err, began, saved, versions, kept }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        state = try c.decode(String.self, forKey: .state)
        // A card this app cannot read is no card, not a failed answer.
        spec = try? c.decodeIfPresent(GenCard.self, forKey: .spec)
        made = try c.decodeIfPresent(Date.self, forKey: .made)
        by = try c.decodeIfPresent(String.self, forKey: .by)
        err = try c.decodeIfPresent(String.self, forKey: .err)
        began = try c.decodeIfPresent(Bool.self, forKey: .began)
        saved = try c.decodeIfPresent(String.self, forKey: .saved)
        versions = try c.decodeIfPresent(Int.self, forKey: .versions)
        kept = try c.decodeIfPresent(Bool.self, forKey: .kept)
    }
}

/// A card of Irie's catalog (internal/genui): a title over its parts, which
/// the app draws in its own look. A part of a kind it does not know is
/// skipped.
struct GenCard: Decodable, Equatable {
    var title: String
    var body: [GenPart]
}

struct GenPart: Decodable, Equatable {
    var type: String
    var text: String?
    var style: String?
    var items: [GenItem]?
    var facts: [GenFact]?
    var columns: [GenCell]?
    var rows: [[GenCell]]?
    var label: String?
    var value: GenNumber?
    var total: GenNumber?
    /// A Diagram's links, a Compare's sides, and the language of Code.
    var links: [GenLink]?
    var sides: [GenSide]?
    var language: String?
}

/// One line of a Diagram: from one thing to another, by their names.
struct GenLink: Decodable, Equatable {
    var from: String
    var to: String
    var label: String?
}

/// One side of a Compare: what it is, and its points.
struct GenSide: Decodable, Equatable {
    var title: String
    var points: [GenCell]?
}

struct GenItem: Decodable, Equatable {
    var title: String
    var detail: String?
    var status: String?
}

struct GenFact: Decodable, Equatable {
    var label: String
    var value: GenCell
}

/// A table's cell or a fact's value: words, though a model may write a
/// number or true for one, which reads as it was written.
struct GenCell: Decodable, Hashable {
    var text: String

    init(_ text: String) { self.text = text }

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() {
            text = ""
        } else if let s = try? c.decode(String.self) {
            text = s
        } else if let n = try? c.decode(Double.self) {
            text = GenNumber.words(n)
        } else if let b = try? c.decode(Bool.self) {
            text = b ? "true" : "false"
        } else {
            text = ""
        }
    }
}

/// A progress figure, which a model may write as "3" as well as 3.
struct GenNumber: Decodable, Hashable {
    var value: Double

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if let n = try? c.decode(Double.self) {
            value = n
        } else if let s = try? c.decode(String.self), let n = Double(s.trimmingCharacters(in: .whitespaces)) {
            value = n
        } else {
            value = 0
        }
    }

    /// A figure as briefly as it reads: 3, 2.5.
    static func words(_ n: Double) -> String {
        n == n.rounded() && abs(n) < 1e15 ? String(Int64(n)) : String(n)
    }
}
