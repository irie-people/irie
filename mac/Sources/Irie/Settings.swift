import AppKit
import SwiftUI

// The Settings window (Irie > Settings…, ⌘,): every preference in one place.
// The app's own, its palette, light or dark, the terminal's text size and
// what the right side shows, live in its defaults. The rest are the core's
// config (DESIGN §5.5): the `config` op reads it, and writes each change
// into config.toml in place, comments and layout kept, and the core runs on
// it at once. A change the core refuses is put back, and the window says
// why.

// MARK: Wire

struct ConfigArgs: Encodable {
    var set: [ConfigSet] = []
}

/// A key and its new value; null puts the key back to its default.
struct ConfigSet: Encodable {
    var key: String
    var value: SettingValue
}

struct ConfigResult: Decodable {
    var path: String
    var config: CoreConfig
    /// Why config.toml would not load as it stands, when it would not.
    var problem: String?
}

/// A setting's value, as JSON.
enum SettingValue: Encodable, Equatable {
    case string(String), bool(Bool), int(Int), number(Double), strings([String]), table([String: String]), null

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case let .string(v): try c.encode(v)
        case let .bool(v): try c.encode(v)
        case let .int(v): try c.encode(v)
        case let .number(v): try c.encode(v)
        case let .strings(v): try c.encode(v)
        case let .table(v): try c.encode(v)
        case .null: try c.encodeNil()
        }
    }
}

/// A type a setting takes.
protocol SettingType: Equatable {
    var setting: SettingValue { get }
}

extension Bool: SettingType { var setting: SettingValue { .bool(self) } }
extension String: SettingType { var setting: SettingValue { .string(self) } }
extension Int: SettingType { var setting: SettingValue { .int(self) } }
extension Double: SettingType { var setting: SettingValue { .number(self) } }
extension Array: SettingType where Element == String { var setting: SettingValue { .strings(self) } }
extension Dictionary: SettingType where Key == String, Value == String { var setting: SettingValue { .table(self) } }

/// The core's config: the keys the window shows, by the file's names.
struct CoreConfig: Decodable, Equatable {
    var ui: UI
    var spawn: Spawn
    var profiles: [String: Profile]
    var local: Local
    var route: Route
    var repos: Repos
    var worktree: Worktree
    var usage: Usage
    var dog: Dog
    /// The trod bot; nil from a core from before it.
    var trod: Trod?
    /// The arch agents; nil from a core from before them.
    var arch: Arch?

    struct UI: Decodable, Equatable {
        var redOnDone: Bool
        /// How a helper beside its session is drawn: browser or terminal.
        var helperStyle: String
        /// How long the app may leave the freeze dog unanswered before the
        /// dog restarts it, "0s" for never (FreezeDog.swift); nil from a
        /// core from before the dog.
        var freezeDog: String?
        enum CodingKeys: String, CodingKey { case redOnDone = "red_on_done", helperStyle = "helper_style", freezeDog = "freeze_dog" }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            redOnDone = try c.decode(Bool.self, forKey: .redOnDone)
            // A core from before helper styles has none: the cards.
            helperStyle = try c.decodeIfPresent(String.self, forKey: .helperStyle) ?? "browser"
            freezeDog = try c.decodeIfPresent(String.self, forKey: .freezeDog)
        }
    }

    struct Spawn: Decodable, Equatable {
        var defaultProfile: String
        var closeWhenDone: Bool
        var closeDelay: String
        /// Agent teams' teammates: panes beside their session, or off.
        var teammates: String
        /// How long after a jot one that reads as more for it goes to its
        /// agent; "0s" never. Nil from a core that does not.
        var followUpWindow: String?
        /// How you like agents to work, a line each, which every session is
        /// told as it starts. Nil from a core from before them.
        var preferences: [String]?
        enum CodingKeys: String, CodingKey {
            case defaultProfile = "default_profile", closeWhenDone = "close_when_done", closeDelay = "close_delay", teammates
            case followUpWindow = "follow_up_window", preferences
        }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            defaultProfile = try c.decode(String.self, forKey: .defaultProfile)
            closeWhenDone = try c.decode(Bool.self, forKey: .closeWhenDone)
            closeDelay = try c.decode(String.self, forKey: .closeDelay)
            // A core from before agent teams starts none.
            teammates = try c.decodeIfPresent(String.self, forKey: .teammates) ?? "off"
            followUpWindow = try c.decodeIfPresent(String.self, forKey: .followUpWindow)
            preferences = try c.decodeIfPresent([String].self, forKey: .preferences)
        }
    }

    struct Profile: Decodable, Equatable {
        var harness: String
        var command: String
        var model: String
        var effort: String
        var permissionMode: String
        var local: LocalModel?
        enum CodingKeys: String, CodingKey {
            case harness, command, model, effort, local
            case permissionMode = "permission_mode"
        }
    }

    struct LocalModel: Decodable, Equatable {
        var runtime: String
    }

    struct Local: Decodable, Equatable {
        var url: String
        var port: Int
        var context: Int
        var kvCache: String
        var memoryGib: Int
        enum CodingKeys: String, CodingKey {
            case url, port, context
            case kvCache = "kv_cache", memoryGib = "memory_gib"
        }
    }

    struct Route: Decodable, Equatable {
        var profiles: [String]
        var minAccuracy: Double
        var failover: Bool
        var delegate: Bool
        var accuracy: [String: [String: Double]]
        var cost: [String: Double]
        enum CodingKeys: String, CodingKey {
            case profiles, failover, delegate, accuracy, cost
            case minAccuracy = "min_accuracy"
        }
    }

    struct Repos: Decodable, Equatable {
        var roots: [String]
        var aliases: [String: String]
    }

    struct Worktree: Decodable, Equatable {
        var base: String
        var branchPrefix: String
        var submodules: Bool
        enum CodingKeys: String, CodingKey {
            case base, submodules
            case branchPrefix = "branch_prefix"
        }
    }

    struct Usage: Decodable, Equatable {
        var statusLine: Bool
        enum CodingKeys: String, CodingKey { case statusLine = "status_line" }
    }

    struct Dog: Decodable, Equatable {
        var watch: Bool
        var nudges: Int
    }

    /// The trod bot (DESIGN §4.10). Durations are as the core writes them,
    /// such as "15m"; times of day as "23:00".
    struct Trod: Decodable, Equatable {
        var on: Bool
        var start: String
        var end: String
        var away: String
        var questions: Bool
        var nudges: Int
        var retry: String
        var within: String
        var maxWorking: Int
        var maxPlanUse: Double
        var keepAwake: Bool
        var rules: [String]
        var guidelines: [String]
        enum CodingKeys: String, CodingKey {
            case on, start, end, away, questions, nudges, retry, within, rules, guidelines
            case maxWorking = "max_working", maxPlanUse = "max_plan_use", keepAwake = "keep_awake"
        }
    }

    /// Irie's arch agents (DESIGN §4.11): helpers who go with every session.
    struct Arch: Decodable, Equatable {
        var mikael: Mikael
        /// Gabriel; nil from a core from before him.
        var gabriel: Gabriel?

        /// Mikael, the first: he stands with you against evil and for the good
        /// you want to see in the world.
        struct Mikael: Decodable, Equatable {
            var on: Bool
            var door: Bool
            var good: [String]
        }

        /// Gabriel, who carries your focus to the machine: while you are at
        /// your computer and it is busy, what you work on comes first.
        /// Durations are as the core writes them, such as "5m".
        struct Gabriel: Decodable, Equatable {
            var on: Bool
            var outside: Bool
            var away: String
            var word: String
            var heavy: Double
            var spare: [String]
        }
    }

    /// The profiles by name, the default's first among equals: auto's own
    /// four in their order, then the rest.
    var profileNames: [String] {
        let builtIn = ["claude", "sonnet", "haiku", "local"].filter { profiles[$0] != nil }
        return builtIn + profiles.keys.filter { !builtIn.contains($0) }.sorted()
    }
}

/// A dotted config key, its parts quoted where TOML needs it, such as
/// profiles."my agent".model.
func configKey(_ parts: String...) -> String {
    parts.map { p in
        p.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "_" || $0 == "-") } && !p.isEmpty
            ? p : "\"" + p.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") + "\""
    }.joined(separator: ".")
}

// MARK: Model

/// The core's config as the window shows it. A change shows at once; the
/// core then writes it to config.toml and runs on it, and its answer is
/// what the window shows from then on. Changes made faster than the core
/// answers go in order, a batch at a time, as `show` does.
@MainActor
final class SettingsModel: ObservableObject {
    @Published private(set) var config: CoreConfig?
    /// The config file, and why it would not load as it stands.
    @Published private(set) var path = ""
    @Published private(set) var problem: String?
    /// Why the core refused the last change it refused.
    @Published var error: String?
    /// Called once the core has taken a change: the agents' notes in the
    /// task bar's menu follow their profiles.
    var changed: @MainActor () -> Void = {}

    private let client: CoreClient
    private var pending: [ConfigSet] = []
    private var sending = false

    init(client: CoreClient) {
        self.client = client
    }

    /// Reads the config in effect.
    func load() async {
        do {
            take(try await client.call("config", ConfigArgs(), timeout: 5, as: ConfigResult.self))
        } catch {
            self.error = "Couldn’t read the settings: \(error.localizedDescription)"
        }
    }

    /// A binding to one setting: it shows value and sends a change to key,
    /// showing it at once through apply.
    func binding<V: SettingType>(_ value: V, _ key: String, _ apply: @escaping (inout CoreConfig, V) -> Void) -> Binding<V> {
        Binding(get: { value }, set: { v in
            guard v != value, var c = self.config else { return }
            apply(&c, v)
            self.config = c
            self.send(ConfigSet(key: key, value: v.setting))
        })
    }

    func bind<V: SettingType>(_ c: CoreConfig, _ path: WritableKeyPath<CoreConfig, V>, _ key: String) -> Binding<V> {
        binding(c[keyPath: path], key) { $0[keyPath: path] = $1 }
    }

    func bind<V: SettingType>(profile name: String, _ p: CoreConfig.Profile,
                              _ path: WritableKeyPath<CoreConfig.Profile, V>, _ field: String) -> Binding<V> {
        binding(p[keyPath: path], configKey("profiles", name, field)) { $0.profiles[name]?[keyPath: path] = $1 }
    }

    /// Puts a key back to its default.
    func reset(_ key: String) {
        send(ConfigSet(key: key, value: .null))
    }

    private func send(_ set: ConfigSet) {
        pending.append(set)
        guard !sending else { return }
        sending = true
        Task {
            while !pending.isEmpty {
                let batch = pending
                pending = []
                do {
                    let r = try await client.call("config", ConfigArgs(set: batch), timeout: 10, as: ConfigResult.self)
                    // A change made since shows as it was made, until its own answer.
                    if pending.isEmpty { take(r) }
                    error = nil
                    changed()
                } catch {
                    self.error = error.localizedDescription
                    if pending.isEmpty { await load() }
                }
            }
            sending = false
        }
    }

    private func take(_ r: ConfigResult) {
        config = r.config
        path = r.path
        problem = r.problem
    }

    /// Fills the model with a config no core sent, for `--snapshot`.
    func loadFixture(_ r: ConfigResult) {
        take(r)
    }
}

/// Light, dark, or as the system is: the app's own choice, kept in its
/// defaults and set on the whole app.
enum AppearanceChoice: String, CaseIterable, Identifiable {
    case system, light, dark

    static let defaultsKey = "appearance"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .system: return "Match System"
        case .light: return "Light"
        case .dark: return "Dark"
        }
    }

    /// Sets the saved choice on the app.
    static func apply() {
        switch AppearanceChoice(rawValue: UserDefaults.standard.string(forKey: defaultsKey) ?? "") ?? .system {
        case .system: NSApp.appearance = nil
        case .light: NSApp.appearance = NSAppearance(named: .aqua)
        case .dark: NSApp.appearance = NSAppearance(named: .darkAqua)
        }
    }
}

// MARK: Window

struct SettingsView: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: SettingsModel

    enum Tab: String, CaseIterable, Identifiable {
        case general, appearance, agents, arch, trod, local, repos

        var id: String { rawValue }

        var title: String {
            switch self {
            case .general: return "General"
            case .appearance: return "Appearance"
            case .agents: return "Agents"
            case .arch: return "Arch Agents"
            case .trod: return "Trod Bot"
            case .local: return "Local Models"
            case .repos: return "Repos"
            }
        }

        var icon: String {
            switch self {
            case .general: return "gearshape"
            case .appearance: return "paintpalette"
            case .agents: return "person.2"
            case .arch: return "shield.lefthalf.filled"
            case .trod: return "moon.stars"
            case .local: return "desktopcomputer"
            case .repos: return "folder"
            }
        }
    }

    static let size = CGSize(width: 600, height: 560)

    /// What the core's settings hang on: a core to ask, and one that knows
    /// the config op.
    private struct Reach: Equatable {
        var link: Store.Link
        var canConfigure: Bool
    }

    var body: some View {
        TabView {
            ForEach(Tab.allCases) { t in
                Self.page(t, model: model)
                    .tabItem { Label(t.title, systemImage: t.icon) }
            }
        }
        .frame(width: Self.size.width, height: Self.size.height)
        .tint(Theme.accent)
        .task(id: Reach(link: store.link, canConfigure: store.canConfigure)) {
            if case .online = store.link, store.canConfigure { await model.load() }
        }
    }

    /// One tab's page, which `--snapshot` also draws alone.
    @ViewBuilder
    static func page(_ t: Tab, model: SettingsModel) -> some View {
        switch t {
        case .general: GeneralSettings(model: model)
        case .appearance: AppearanceSettings(model: model)
        case .agents: AgentSettings(model: model)
        case .arch: ArchSettings(model: model)
        case .trod: TrodSettings(model: model)
        case .local: LocalSettings(model: model)
        case .repos: RepoSettings(model: model)
        }
    }
}

/// A tab: what the core refused, or why config.toml would not load, then
/// the tab's sections.
private struct SettingsForm<Content: View>: View {
    @ObservedObject var model: SettingsModel
    @ViewBuilder var content: Content

    var body: some View {
        Form {
            if let e = model.error {
                Section {
                    HStack(alignment: .firstTextBaseline) {
                        Label(e, systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(Theme.needs)
                            .textSelection(.enabled)
                        Spacer()
                        Button("Dismiss") { model.error = nil }
                    }
                }
            }
            if let p = model.problem {
                Section {
                    Label("config.toml doesn’t load as it stands, so Irie runs on what it loaded last. \(p)",
                          systemImage: "exclamationmark.triangle")
                        .foregroundStyle(Theme.needs)
                        .textSelection(.enabled)
                }
            }
            content
        }
        .formStyle(.grouped)
    }
}

/// The core's settings once it has said what they are, or why they can't
/// show.
private struct WithConfig<Content: View>: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: SettingsModel
    @ViewBuilder var content: (CoreConfig) -> Content

    var body: some View {
        switch store.link {
        case .online:
            if !store.canConfigure {
                ContentUnavailableView("This Irie is too old for Settings", systemImage: "gearshape.arrow.triangle.2.circlepath",
                                       description: Text("Install a newer build to change these here, or edit config.toml."))
            } else if let c = model.config {
                content(c)
            } else if let e = model.error {
                ContentUnavailableView {
                    Label(e, systemImage: "exclamationmark.triangle")
                } actions: {
                    Button("Try Again") { Task { await model.load() } }
                }
            } else {
                ProgressView()
            }
        case .connecting:
            ProgressView()
        case let .offline(why):
            ContentUnavailableView("Irie isn’t answering", systemImage: "bolt.horizontal.circle",
                                   description: Text("\(why) These settings are the core’s, so they show once it runs."))
        }
    }
}

// MARK: General

private struct GeneralSettings: View {
    @ObservedObject var model: SettingsModel

    /// How long a finished session waits before it closes.
    private static let delays = ["10s", "30s", "1m", "2m", "5m", "10m"]
    /// How long after a jot one that reads as more for it goes to its agent.
    private static let windows = ["0s", "1m", "2m", "5m", "10m"]
    /// How long Irie may stop answering before the freeze dog restarts it.
    private static let freezes = ["5s", "10s", "20s", "30s", "1m"]
    private static let defaultFreeze = "10s"

    var body: some View {
        WithConfig(model: model) { c in
            SettingsForm(model: model) {
                Section {
                    Toggle("Close sessions once they finish", isOn: model.bind(c, \.spawn.closeWhenDone, "spawn.close_when_done"))
                    Picker("After they have been quiet for", selection: model.bind(c, \.spawn.closeDelay, "spawn.close_delay")) {
                        ForEach(Self.delays + (Self.delays.contains(c.spawn.closeDelay) ? [] : [c.spawn.closeDelay]), id: \.self) { d in
                            Text(Self.spoken(d)).tag(d)
                        }
                    }
                    .disabled(!c.spawn.closeWhenDone)
                } header: {
                    Text("Sessions")
                } footer: {
                    footnote("Sessions that report back or ask you something stay open, and ⇧⌘T reopens the last one closed.")
                }
                Section {
                    let teams = model.bind(c, \.spawn.teammates, "spawn.teammates")
                    Toggle("Let Claude Code start agent teams", isOn: Binding(get: { teams.wrappedValue == "panes" },
                                                                             set: { teams.wrappedValue = $0 ? "panes" : "off" }))
                } header: {
                    Text("Agent teams")
                } footer: {
                    footnote("Each teammate runs in a terminal of its own, which the terminal view shows beside its session. Teammates are Claude Code sessions too, so a team uses more tokens. Sessions started from now on follow this.")
                }
                if let window = c.spawn.followUpWindow {
                    Section {
                        Picker("Add a jot to the one just sent", selection: model.binding(window, "spawn.follow_up_window") { $0.spawn.followUpWindow = $1 }) {
                            ForEach(Self.windows + (Self.windows.contains(window) ? [] : [window]), id: \.self) { w in
                                Text(w == "0s" ? "Never" : "Within " + Self.spoken(w)).tag(w)
                            }
                        }
                    } header: {
                        Text("Jots")
                    } footer: {
                        footnote("A jot that starts with “also”, “oh and”, “it”, “PS” or the like, or is only a link, a path or an error message, goes to the agent of the jot sent just before, in the same place, instead of starting one. Until it goes in, New task above the bar starts it apart.")
                    }
                }
                Section {
                    Toggle("Watch each jot until its work lands", isOn: model.bind(c, \.dog.watch, "dog.watch"))
                    LabeledContent("Send a jot back to finish") {
                        Stepper(value: model.bind(c, \.dog.nudges, "dog.nudges"), in: 0...5) {
                            Text(c.dog.nudges == 0 ? "Never" : c.dog.nudges == 1 ? "Once" : "Up to \(c.dog.nudges) times")
                        }
                    }
                    .disabled(!c.dog.watch)
                } header: {
                    Text("Jot dog")
                } footer: {
                    footnote("When a jot’s turn ends with work uncommitted or not yet in its branch, its card says what is left, and the dog sends it back between two of your prompts.")
                }
                if let wait = c.ui.freezeDog {
                    Section {
                        let dog = model.binding(wait, "ui.freeze_dog") { $0.ui.freezeDog = $1 }
                        Toggle("Restart Irie when it stops answering", isOn: Binding(get: { wait != "0s" },
                                                                                set: { dog.wrappedValue = $0 ? Self.defaultFreeze : "0s" }))
                        // While the dog is off, the picker shows what turning it on picks.
                        Picker("Once it has not answered for", selection: Binding(get: { wait == "0s" ? Self.defaultFreeze : wait },
                                                                                set: { dog.wrappedValue = $0 })) {
                            ForEach(Self.freezes + (Self.freezes.contains(wait) || wait == "0s" ? [] : [wait]), id: \.self) { d in
                                Text(Self.spoken(d)).tag(d)
                            }
                        }
                        .disabled(wait == "0s")
                    } header: {
                        Text("Freeze dog")
                    } footer: {
                        footnote("Irie is force quit and opened again as it was, and your agents keep running. A report of where it was stuck goes in the freezes folder beside core.log.")
                    }
                }
                Section {
                    Toggle("Read cost and plan use from Claude Code’s status line", isOn: model.bind(c, \.usage.statusLine, "usage.status_line"))
                } header: {
                    Text("Usage")
                } footer: {
                    footnote("Irie becomes the status line of the sessions it starts from now on, and runs yours inside it.")
                }
                ConfigFileSection(path: model.path)
            }
        }
    }

    /// 30s as "30 seconds", 2m as "2 minutes".
    static func spoken(_ d: String) -> String {
        guard let n = Int(d.dropLast()) else { return d }
        switch d.last {
        case "s": return n == 1 ? "1 second" : "\(n) seconds"
        case "m": return n == 1 ? "1 minute" : "\(n) minutes"
        case "h": return n == 1 ? "1 hour" : "\(n) hours"
        default: return d
        }
    }
}

/// Where the settings are kept, to open or find.
private struct ConfigFileSection: View {
    let path: String

    private var exists: Bool { FileManager.default.fileExists(atPath: path) }

    var body: some View {
        Section {
            LabeledContent("Config file") {
                Text((path as NSString).abbreviatingWithTildeInPath)
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
            }
            HStack {
                Spacer()
                Button("Show in Finder") {
                    NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: exists ? path : (path as NSString).deletingLastPathComponent)])
                }
                Button("Open") { open() }
                    .disabled(!exists)
            }
        } footer: {
            footnote(exists
                 ? "Changes here are written to this file as you make them, with your comments and layout kept. A hand edit applies when Irie next starts, or with your next change here."
                 : "There is no file yet: Irie runs on its defaults, and your first change here makes it.")
        }
    }

    /// Opens the file in its app, or in TextEdit when no app takes .toml.
    private func open() {
        let url = URL(fileURLWithPath: path)
        if !NSWorkspace.shared.open(url) {
            NSWorkspace.shared.open([url], withApplicationAt: URL(fileURLWithPath: "/System/Applications/TextEdit.app"),
                                    configuration: NSWorkspace.OpenConfiguration())
        }
    }
}

// MARK: Appearance

private struct AppearanceSettings: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: SettingsModel
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id
    @AppStorage(AppearanceChoice.defaultsKey) private var appearance = AppearanceChoice.system.rawValue
    @AppStorage(TerminalEngine.defaultsKey) private var engine = ""

    var body: some View {
        SettingsForm(model: model) {
            Section {
                PalettePicker(selection: $theme)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 4)
                Picker("Appearance", selection: $appearance) {
                    ForEach(AppearanceChoice.allCases) { a in Text(a.title).tag(a.rawValue) }
                }
                .pickerStyle(.segmented)
                .onChange(of: appearance) { AppearanceChoice.apply() }
            } header: {
                Text("Theme")
            } footer: {
                footnote("Each palette has a light and a dark set; Irie’s status line in the terminal follows it too.")
            }
            Section {
                Picker("On the right", selection: $store.showTerminal) {
                    Text("Conversation").tag(false)
                    Text("Terminal").tag(true)
                }
                .pickerStyle(.segmented)
                LabeledContent("Terminal text") {
                    Stepper(value: Binding(get: { Int(store.fontSize) }, set: { store.setFontSize(CGFloat($0)) }), in: 9...28) {
                        Text("\(Int(store.fontSize)) pt").monospacedDigit()
                    }
                }
                if TerminalEngine.built.count > 1 {
                    Picker("Terminal drawn by", selection: Binding(get: { TerminalEngine.named(engine).rawValue }, set: { engine = $0 })) {
                        ForEach(TerminalEngine.built) { e in Text(e.title).tag(e.rawValue) }
                    }
                    .pickerStyle(.segmented)
                }
            } header: {
                Text("Window")
            } footer: {
                footnote(TerminalEngine.built.count > 1
                    ? "View > Terminal (⌥⌘T) and ⌘+ and ⌘- change these too. Ghostty draws the terminals with your own Ghostty config, and its split keys move between a session and its teammates."
                    : "View > Terminal (⌥⌘T) and ⌘+ and ⌘- change these too.")
            }
            Section {
                switch (store.link, model.config) {
                case let (.online, c?):
                    Picker("Helpers look like", selection: model.bind(c, \.ui.helperStyle, "ui.helper_style")) {
                        Text("Browser").tag("browser")
                        Text("Terminal").tag("terminal")
                    }
                    .pickerStyle(.segmented)
                default:
                    Picker("Helpers look like", selection: .constant(store.helperStyle)) {
                        Text("Browser").tag("browser")
                        Text("Terminal").tag("terminal")
                    }
                    .pickerStyle(.segmented)
                    .disabled(true)
                }
            } header: {
                Text("Helpers")
            } footer: {
                footnote("A helper is a subagent its session started. Click one, or press ⇧⌘H, to see its work beside the conversation: as cards and plain words, or as a terminal. ⌘[ and ⌘] step through them.")
            }
            Section {
                switch (store.link, model.config) {
                case let (.online, c?):
                    Toggle("Draw finished sessions in red", isOn: model.bind(c, \.ui.redOnDone, "ui.red_on_done"))
                default:
                    Toggle("Draw finished sessions in red", isOn: .constant(store.redOnDone))
                        .disabled(true)
                }
            } footer: {
                footnote("Red is otherwise kept for what needs you; done is green.")
            }
        }
    }
}

/// The palettes as small pictures of the sidebar, in the mode the window
/// is drawn in.
private struct PalettePicker: View {
    @Binding var selection: String
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        HStack(spacing: 14) {
            ForEach(Palette.all) { p in
                let on = selection == p.id
                Button { selection = p.id } label: {
                    VStack(spacing: 6) {
                        swatch(scheme == .dark ? p.dark : p.light, on: on)
                        Text(p.name)
                            .font(Theme.font(12, on ? .bold : .regular))
                            .foregroundStyle(on ? Theme.ink : Theme.muted)
                    }
                }
                .buttonStyle(.plain)
                .accessibilityLabel(p.name)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
    }

    /// The sidebar's ground with the sun and waves, a selected card edged
    /// in the accent, and a quiet card.
    private func swatch(_ s: Swatches, on: Bool) -> some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(Color(hex: s.sidebar))
            .frame(width: 104, height: 68)
            .overlay(alignment: .topLeading) {
                VStack(alignment: .leading, spacing: 5) {
                    HStack(spacing: 4) {
                        Circle().fill(Color(hex: s.sun)).frame(width: 9, height: 9)
                        Capsule().fill(Color(hex: s.waves)).frame(width: 20, height: 4)
                    }
                    RoundedRectangle(cornerRadius: 4, style: .continuous)
                        .fill(Color(hex: s.selection))
                        .overlay(RoundedRectangle(cornerRadius: 4, style: .continuous).strokeBorder(Color(hex: s.accent), lineWidth: 1.5))
                        .frame(height: 13)
                    RoundedRectangle(cornerRadius: 4, style: .continuous)
                        .fill(Color(hex: s.card))
                        .overlay(RoundedRectangle(cornerRadius: 4, style: .continuous).strokeBorder(Color(hex: s.edge), lineWidth: 1))
                        .frame(height: 13)
                }
                .padding(9)
            }
            .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(on ? Theme.accent : Theme.edge, lineWidth: on ? 2.5 : 1))
    }
}

// MARK: Agents

private struct AgentSettings: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: SettingsModel
    /// The profile open to edit.
    @State private var editing = "claude"

    var body: some View {
        WithConfig(model: model) { c in
            SettingsForm(model: model) {
                Section {
                    Picker("New tasks run on", selection: model.bind(c, \.spawn.defaultProfile, "spawn.default_profile")) {
                        Text("auto · the cheapest agent that does each task well").tag("auto")
                        ForEach(c.profileNames, id: \.self) { name in Text(label(name, c)).tag(name) }
                    }
                } footer: {
                    footnote("A +agent word in a task’s text runs that task on another, such as +sonnet.")
                }
                if let prefs = c.spawn.preferences {
                    Section {
                        LineList(lines: prefs, prompt: "Add a preference, such as “Ask before you push.”") {
                            model.binding(prefs, "spawn.preferences") { $0.spawn.preferences = $1 }.wrappedValue = $0
                        }
                    } header: {
                        ListHeader(title: "Your preferences", reset: { model.reset("spawn.preferences") })
                    } footer: {
                        footnote("Every Claude Code session is told these as it starts, so agents work the way you like, such as how short a text to you should be.")
                    }
                }
                Section {
                    Picker("Agent", selection: $editing) {
                        ForEach(c.profileNames, id: \.self) { name in Text(name).tag(name) }
                    }
                    if let p = c.profiles[editing] {
                        ProfileEditor(model: model, name: editing, profile: p)
                    }
                } header: {
                    Text("Each agent")
                }
                RouteSection(model: model, config: c)
            }
            .onAppear { if c.profiles[editing] == nil, let first = c.profileNames.first { editing = first } }
        }
    }

    /// A profile with what it runs, as the + list says.
    private func label(_ name: String, _ c: CoreConfig) -> String {
        if let note = store.agents.first(where: { $0.name == name })?.note, !note.isEmpty {
            return "\(name) · \(note)"
        }
        guard let p = c.profiles[name] else { return name }
        return ([name] + [p.model, p.effort].filter { !$0.isEmpty }).joined(separator: " · ")
    }
}

/// One profile's model, effort and permissions. A Claude Code profile takes
/// them; a local one picks its model from the local server, and a generic
/// one is set in the file.
private struct ProfileEditor: View {
    @ObservedObject var model: SettingsModel
    let name: String
    let profile: CoreConfig.Profile

    /// Claude Code's models by their full names.
    private static let models = [("Fable 5.1", "claude-fable-5-1"), ("Opus 5.5", "claude-opus-5-5"),
                                 ("Sonnet 5.5", "claude-sonnet-5-5"), ("Haiku 5.5", "claude-haiku-5-5")]
    /// `claude --effort`'s levels.
    private static let efforts = ["low", "medium", "high", "xhigh", "max"]
    /// `claude --permission-mode`'s modes.
    private static let modes = [("Auto", "auto"), ("Accept edits", "acceptEdits"), ("Plan only", "plan"),
                                ("Don’t ask", "dontAsk"), ("Bypass permissions", "bypassPermissions")]

    var body: some View {
        if profile.harness != "claude-code" {
            LabeledContent("Runs", value: profile.command)
            Text("An agent on the generic harness is set up in config.toml, with its own flags in args.")
                .font(.callout)
                .foregroundStyle(.secondary)
        } else if profile.local != nil {
            LabeledContent("Runs") { Text("Claude Code on a local model").foregroundStyle(.secondary) }
            CommitField(title: "Model", prompt: "the one irie setup local chose",
                        value: model.bind(profile: name, profile, \.model, "model"))
        } else {
            Picker("Model", selection: model.bind(profile: name, profile, \.model, "model")) {
                Text("Claude Code’s default").tag("")
                ForEach(Self.models, id: \.1) { m in Text(m.0).tag(m.1) }
                if !profile.model.isEmpty, !Self.models.contains(where: { $0.1 == profile.model }) {
                    Text(profile.model).tag(profile.model)
                }
            }
            Picker("Effort", selection: model.bind(profile: name, profile, \.effort, "effort")) {
                Text("Claude Code’s default").tag("")
                ForEach(Self.efforts + (Self.efforts.contains(profile.effort) || profile.effort.isEmpty ? [] : [profile.effort]), id: \.self) { e in
                    Text(e == "xhigh" ? "Extra high" : e.prefix(1).uppercased() + e.dropFirst()).tag(e)
                }
            }
            Picker("Permissions", selection: model.bind(profile: name, profile, \.permissionMode, "permission_mode")) {
                Text("Claude Code’s default").tag("")
                ForEach(Self.modes, id: \.1) { m in Text(m.0).tag(m.1) }
                if !profile.permissionMode.isEmpty, !Self.modes.contains(where: { $0.1 == profile.permissionMode }) {
                    Text(profile.permissionMode).tag(profile.permissionMode)
                }
            }
        }
    }
}

/// How auto picks an agent (DESIGN §6.4).
private struct RouteSection: View {
    @ObservedObject var model: SettingsModel
    let config: CoreConfig
    /// The floor while its slider is dragged; sent when it is let go.
    @State private var floor: Double?

    var body: some View {
        let c = config
        Section {
            Toggle("Carry on with a cheaper agent at a usage limit", isOn: model.bind(c, \.route.failover, "route.failover"))
            Toggle("Let agents hand subtasks to cheaper ones", isOn: model.bind(c, \.route.delegate, "route.delegate"))
            LabeledContent("Accuracy floor") {
                HStack {
                    Slider(value: Binding(get: { floor ?? c.route.minAccuracy }, set: { floor = $0 }), in: 0.5...1) { editing in
                        guard !editing, let f = floor else { return }
                        model.bind(c, \.route.minAccuracy, "route.min_accuracy").wrappedValue = (f * 100).rounded() / 100
                        floor = nil
                    }
                    .frame(width: 200)
                    Text("\(Int(((floor ?? c.route.minAccuracy) * 100).rounded()))%")
                        .monospacedDigit()
                        .frame(width: 40, alignment: .trailing)
                }
            }
        } header: {
            Text("Auto")
        } footer: {
            footnote("Auto runs each task on the agent with the lowest cost per success among those at least this accurate for the task’s size. At 90% medium work stays on the strongest.")
        }
        Section {
            ForEach(routable, id: \.self) { name in
                Toggle(isOn: picks(name)) {
                    HStack(spacing: 6) {
                        Text(name)
                        Text(runs(name)).foregroundStyle(.secondary)
                    }
                }
                .disabled(c.route.profiles == [name])
            }
        } header: {
            Text("Auto picks from")
        }
    }

    /// What a profile runs: its model, or a local one.
    private func runs(_ name: String) -> String {
        guard let p = config.profiles[name] else { return "" }
        return p.local != nil ? "a local model" : p.model.isEmpty ? "Claude Code’s default model" : p.model
    }

    /// The profiles auto can weigh: those with a starting accuracy and cost.
    private var routable: [String] {
        config.profileNames.filter { config.route.accuracy[$0] != nil && config.route.cost[$0] != nil }
    }

    private func picks(_ name: String) -> Binding<Bool> {
        let list = model.bind(config, \.route.profiles, "route.profiles")
        return Binding(get: { config.route.profiles.contains(name) }, set: { on in
            list.wrappedValue = on ? config.route.profiles + [name] : config.route.profiles.filter { $0 != name }
        })
    }
}

// MARK: Local models

private struct LocalSettings: View {
    @ObservedObject var model: SettingsModel
    /// Whether the window shows a server you run, before its address is in.
    @State private var external: Bool?

    /// Context lengths in tokens; 64k is Ollama's advice for coding agents.
    private static let contexts = [16384, 32768, 65536, 131072, 262144]
    private static let caches = [("f16, full size", "f16"), ("q8_0, about half the memory", "q8_0"), ("q4_0, about a quarter", "q4_0")]

    var body: some View {
        WithConfig(model: model) { c in
            let ext = external ?? !c.local.url.isEmpty
            SettingsForm(model: model) {
                Section {
                    Picker("Ollama server", selection: Binding(get: { ext }, set: { on in
                        external = on
                        if !on, !c.local.url.isEmpty { model.reset("local.url") }
                    })) {
                        Text("Irie runs its own").tag(false)
                        Text("One you run").tag(true)
                    }
                    .pickerStyle(.radioGroup)
                    if ext {
                        CommitField(title: "Address", prompt: "http://127.0.0.1:11434", value: model.bind(c, \.local.url, "local.url"))
                    } else {
                        CommitField(title: "Port", prompt: "11435", value: Binding(
                            get: { String(c.local.port) },
                            set: { if let n = Int($0) { model.bind(c, \.local.port, "local.port").wrappedValue = n } }))
                    }
                } header: {
                    Text("Server")
                } footer: {
                    footnote("+local and +ollama:<model> run Claude Code on a model of this server. Irie’s own listens on 127.0.0.1 only.")
                }
                Section {
                    Picker("Context", selection: model.bind(c, \.local.context, "local.context")) {
                        ForEach(Self.contexts + (Self.contexts.contains(c.local.context) ? [] : [c.local.context]), id: \.self) { n in
                            Text(n % 1024 == 0 ? "\(n / 1024)k tokens" : "\(n) tokens").tag(n)
                        }
                    }
                    Picker("Context cache", selection: model.bind(c, \.local.kvCache, "local.kv_cache")) {
                        ForEach(Self.caches, id: \.1) { m in Text(m.0).tag(m.1) }
                    }
                    LabeledContent("Memory for models") {
                        Stepper(value: model.bind(c, \.local.memoryGib, "local.memory_gib"), in: 0...1024, step: 2) {
                            Text(c.local.memoryGib == 0 ? "Two thirds of this Mac’s" : "\(c.local.memoryGib) GB")
                        }
                    }
                } header: {
                    Text("Irie’s own server")
                } footer: {
                    footnote("These apply when Irie’s own server next starts. Below 64k tokens a coding agent loses its instructions.")
                }
                .disabled(ext)
            }
        }
    }
}

// MARK: Repos

private struct RepoSettings: View {
    @ObservedObject var model: SettingsModel

    var body: some View {
        WithConfig(model: model) { c in
            SettingsForm(model: model) {
                Section {
                    ForEach(c.repos.roots, id: \.self) { root in
                        row(icon: "folder", root, nil) {
                            model.bind(c, \.repos.roots, "repos.roots").wrappedValue = c.repos.roots.filter { $0 != root }
                        }
                    }
                    Button("Add a Folder…") {
                        if let path = chooseFolder("Irie looks for git repos up to two levels under this folder."), !c.repos.roots.contains(path) {
                            model.bind(c, \.repos.roots, "repos.roots").wrappedValue = c.repos.roots + [path]
                        }
                    }
                } header: {
                    Text("Where @repo looks")
                } footer: {
                    footnote("Every git repo up to two levels under these folders answers to @ and its folder’s name.")
                }
                Section {
                    ForEach(c.repos.aliases.keys.sorted(), id: \.self) { name in
                        row(icon: "at", name, c.repos.aliases[name]) {
                            var a = c.repos.aliases
                            a[name] = nil
                            model.bind(c, \.repos.aliases, "repos.aliases").wrappedValue = a
                        }
                    }
                    Button("Add a Repo…") {
                        if let path = chooseFolder("@ and this folder’s name will find this repo.") {
                            var a = c.repos.aliases
                            a[Self.freeName(for: path, in: a)] = path
                            model.bind(c, \.repos.aliases, "repos.aliases").wrappedValue = a
                        }
                    }
                } header: {
                    Text("Names")
                } footer: {
                    footnote("A name here wins over a repo of that name under the folders above.")
                }
                Section {
                    CommitField(title: "Branch names start with", prompt: "irie/", value: model.bind(c, \.worktree.branchPrefix, "worktree.branch_prefix"))
                    CommitField(title: "New branches start from", prompt: "HEAD", value: model.bind(c, \.worktree.base, "worktree.base"))
                    Toggle("Check out submodules", isOn: model.bind(c, \.worktree.submodules, "worktree.submodules"))
                } header: {
                    Text("Worktrees")
                } footer: {
                    footnote("A task in a repo works in a worktree of its own, on a new branch, so tasks never trip over each other.")
                }
            }
        }
    }

    private func row(icon: String, _ title: String, _ detail: String?, remove: @escaping () -> Void) -> some View {
        HStack {
            Image(systemName: icon).foregroundStyle(Theme.muted)
            Text(title).fontWeight(detail == nil ? .regular : .semibold)
            if let detail { Text(detail).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle) }
            Spacer()
            Button(action: remove) { Image(systemName: "minus.circle") }
                .buttonStyle(.borderless)
                .help("Remove")
                .accessibilityLabel("Remove \(title)")
        }
    }

    /// A folder, as a ~/ path when it is under the home folder.
    private func chooseFolder(_ message: String) -> String? {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Add"
        panel.message = message
        guard panel.runModal() == .OK, let url = panel.url else { return nil }
        return (url.path as NSString).abbreviatingWithTildeInPath
    }

    /// The folder's name as a repo's name, numbered when taken: letters,
    /// digits, '.', '_' and '-', starting with a letter or digit.
    static func freeName(for path: String, in taken: [String: String]) -> String {
        var base = String((path as NSString).lastPathComponent.map { $0.isASCII && ($0.isLetter || $0.isNumber || ".-_".contains($0)) ? $0 : "-" })
        while let f = base.first, !(f.isLetter || f.isNumber) { base.removeFirst() }
        if base.isEmpty { base = "repo" }
        var name = base, n = 1
        while taken[name] != nil {
            n += 1
            name = "\(base)-\(n)"
        }
        return name
    }
}

// MARK: Arch agents

/// Irie's arch agents (DESIGN §4.11): helpers who go with every session.
/// Mikael, the first, stands with you against evil and for the good you
/// want to see in the world, which you write here in your own words.
private struct ArchSettings: View {
    @ObservedObject var model: SettingsModel

    var body: some View {
        WithConfig(model: model) { c in
            if let a = c.arch {
                form(a)
            } else {
                ContentUnavailableView("This Irie has no arch agents yet", systemImage: "shield.lefthalf.filled",
                                       description: Text("Install a newer build to have Mikael with your sessions."))
            }
        }
    }

    /// A binding to one of Mikael's settings.
    private func bind<V: SettingType>(_ m: CoreConfig.Arch.Mikael, _ path: WritableKeyPath<CoreConfig.Arch.Mikael, V>, _ key: String) -> Binding<V> {
        model.binding(m[keyPath: path], "arch.mikael." + key) { $0.arch?.mikael[keyPath: path] = $1 }
    }

    /// A binding to one of Gabriel's settings.
    private func bindG<V: SettingType>(_ g: CoreConfig.Arch.Gabriel, _ path: WritableKeyPath<CoreConfig.Arch.Gabriel, V>, _ key: String) -> Binding<V> {
        model.binding(g[keyPath: path], "arch.gabriel." + key) { $0.arch?.gabriel?[keyPath: path] = $1 }
    }

    private static let aways = ["2m", "5m", "10m", "15m"]

    private func form(_ a: CoreConfig.Arch) -> some View {
        let m = a.mikael
        return SettingsForm(model: model) {
            Section {
                Toggle("Mikael goes with every session", isOn: bind(m, \.on, "on"))
                Toggle("He weighs calls that can’t be taken back", isOn: bind(m, \.door, "door"))
                    .disabled(!m.on)
            } header: {
                Text("Mikael")
            } footer: {
                footnote("He stands with you against evil, and for the good you want to see, in every session. Each agent hears his word as it starts, and can ask him whether something is right, a scam or a trap. Before a call that could do harm that can’t be taken back, such as sending something off your Mac, deleting, or running code from the web, a small model weighs it in his name and stops one that would do evil, telling the agent why. Start a task with +mikael to talk with him yourself.")
            }
            Section {
                LineList(lines: m.good, prompt: "Add the good you want to see, such as “Give credit where it’s due.”") {
                    bind(m, \.good, "good").wrappedValue = $0
                }
            } header: {
                ListHeader(title: "The good you want to see in the world", reset: { model.reset("arch.mikael.good") })
            } footer: {
                footnote("In your own words. Every agent Mikael goes with hears them, and he helps it toward them.")
            }
            .disabled(!m.on)
            if let g = a.gabriel {
                Section {
                    Toggle("Gabriel gives your focus the machine first", isOn: bindG(g, \.on, "on"))
                    Toggle("Slow your heavy apps outside Irie too", isOn: bindG(g, \.outside, "outside"))
                        .disabled(!g.on)
                    Picker("You’re away after", selection: bindG(g, \.away, "away")) {
                        ForEach(Self.aways.contains(g.away) ? Self.aways : Self.aways + [g.away], id: \.self) { d in
                            Text(d == "0s" ? "Never" : GeneralSettings.spoken(d)).tag(d)
                        }
                    }
                    .disabled(!g.on)
                } header: {
                    Text("Gabriel")
                } footer: {
                    footnote("While you’re at your computer and it’s busy, what you’re working on comes first: the session you’re looking at or messaged in the last \(GeneralSettings.spoken(g.word)), and the app in front. Every other session, and your other heavy apps, step back into macOS’s background state until there’s room again or you step away. Nothing is held back while you’re away. `irie ctl gabriel` says what he holds back.")
                }
                Section {
                    LineList(lines: g.spare, prompt: "Add an app never to slow, such as “Xcode”") {
                        bindG(g, \.spare, "spare").wrappedValue = $0
                    }
                } header: {
                    ListHeader(title: "Apps Gabriel never slows", reset: { model.reset("arch.gabriel.spare") })
                } footer: {
                    footnote("Calls, music, video and recording, which must never stutter. An app that plays sound or video, or holds a call, is spared too.")
                }
                .disabled(!g.on || !g.outside)
            }
        }
    }
}

// MARK: Trod bot

/// The trod bot (DESIGN §4.10): when it works, how far it goes, how much of
/// the plan it may use, and the rules and guidelines every agent it keeps
/// going is given. Setting it up is writing those.
private struct TrodSettings: View {
    @EnvironmentObject private var store: Store
    @ObservedObject var model: SettingsModel

    private static let aways = ["0s", "5m", "15m", "30m", "1h"]
    private static let retries = ["5m", "10m", "15m", "30m", "1h"]
    private static let withins = ["12h", "24h", "48h", "0s"]

    var body: some View {
        WithConfig(model: model) { c in
            if let t = c.trod {
                form(t)
            } else {
                ContentUnavailableView("This Irie has no trod bot yet", systemImage: "moon.stars",
                                       description: Text("Install a newer build to keep sessions going at night."))
            }
        }
    }

    /// A binding to one of the bot's settings.
    private func bind<V: SettingType>(_ t: CoreConfig.Trod, _ path: WritableKeyPath<CoreConfig.Trod, V>, _ key: String) -> Binding<V> {
        model.binding(t[keyPath: path], "trod." + key) { $0.trod?[keyPath: path] = $1 }
    }

    private func form(_ t: CoreConfig.Trod) -> some View {
        SettingsForm(model: model) {
            Section {
                Toggle("Keep sessions going while you’re away at night", isOn: bind(t, \.on, "on"))
                DatePicker("From", selection: clock(bind(t, \.start, "start")), displayedComponents: .hourAndMinute)
                DatePicker("Until", selection: clock(bind(t, \.end, "end")), displayedComponents: .hourAndMinute)
                Picker("Once you’ve been away for", selection: bind(t, \.away, "away")) {
                    ForEach(Self.choices(Self.aways, t.away), id: \.self) { d in
                        Text(d == "0s" ? "No wait" : GeneralSettings.spoken(d)).tag(d)
                    }
                }
                LabeledContent("Or now, while you watch") {
                    Button(store.trod?.hand == true ? "Stop" : "Start Now") { store.setTrod(store.trod?.hand != true) }
                        .disabled(!store.canTrod)
                }
            } header: {
                Text("Trod bot")
            } footer: {
                footnote("While your Mac has had no keyboard or mouse for that long, it tells an agent that asks you something to decide for itself, and sends on a turn an API error stopped and a task that isn’t through. It never answers in your name or approves a permission. A session you park or freeze is left alone. Started now, it works while you watch, until no session has work left or your plan is as full as below. Its boot print marks each card it sends on.\(t.start == t.end ? " The same time for both is around the clock." : "")")
            }
            .disabled(false)
            Section {
                Toggle("Tell agents that ask you something to decide", isOn: bind(t, \.questions, "questions"))
                LabeledContent("Send one session on") {
                    Stepper(value: bind(t, \.nudges, "nudges"), in: 0...30) {
                        Text(t.nudges == 0 ? "Never" : t.nudges == 1 ? "Once a night" : "Up to \(t.nudges) times a night")
                    }
                }
                Picker("Before it tries again, wait", selection: bind(t, \.retry, "retry")) {
                    ForEach(Self.choices(Self.retries, t.retry), id: \.self) { d in Text(GeneralSettings.spoken(d)).tag(d) }
                }
                Picker("Keep going sessions that stopped", selection: bind(t, \.within, "within")) {
                    ForEach(Self.choices(Self.withins, t.within), id: \.self) { d in
                        Text(d == "0s" ? "However long ago" : "In the last " + GeneralSettings.spoken(d)).tag(d)
                    }
                }
                LabeledContent("Sessions working at once") {
                    Stepper(value: bind(t, \.maxWorking, "max_working"), in: 0...20) {
                        Text(t.maxWorking == 0 ? "No limit" : "Up to \(t.maxWorking)")
                    }
                }
                Toggle("Keep the Mac awake while sessions have work left", isOn: bind(t, \.keepAwake, "keep_awake"))
            } header: {
                Text("How far it goes")
            } footer: {
                footnote("The display may still sleep, and a MacBook with its lid closed sleeps anyway.")
            }
            .disabled(!t.on)
            Section {
                LabeledContent("Stop at") {
                    Stepper(value: bind(t, \.maxPlanUse, "max_plan_use"), in: 5...100, step: 5) {
                        Text("\(Int(t.maxPlanUse))% of your plan").monospacedDigit()
                    }
                }
            } header: {
                Text("Your plan")
            } footer: {
                footnote("Once your plan’s five hours or its week are this full, it sends nothing on. Once the week is, it also lets the Mac sleep and freezes the sessions that would still use it, so nothing goes over your weekly allotment. It thaws them if the week resets before morning.")
            }
            .disabled(!t.on)
            Section {
                LineList(lines: t.rules, prompt: "Add a rule, such as “Never touch production.”") {
                    bind(t, \.rules, "rules").wrappedValue = $0
                }
            } header: {
                ListHeader(title: "Rules", reset: { model.reset("trod.rules") })
            } footer: {
                footnote("Every agent the trod bot keeps going must follow these. Each message also tells it to leave you only what needs you, such as a login or a permission prompt.")
            }
            Section {
                LineList(lines: t.guidelines, prompt: "Add a guideline, such as “Prefer the smallest change.”") {
                    bind(t, \.guidelines, "guidelines").wrappedValue = $0
                }
            } header: {
                ListHeader(title: "Guidelines", reset: { model.reset("trod.guidelines") })
            } footer: {
                footnote("What those agents weigh when they decide while you’re away.")
            }
        }
    }

    /// A list's choices, with the value in effect when it is not one of them.
    private static func choices(_ list: [String], _ value: String) -> [String] {
        list.contains(value) ? list : list + [value]
    }

    /// A time of day such as "23:00" as a date today, for a picker; set, it
    /// writes the time back the same way.
    private func clock(_ value: Binding<String>) -> Binding<Date> {
        Binding(get: {
            let parts = value.wrappedValue.split(separator: ":").compactMap { Int($0) }
            let cal = Calendar.current
            return cal.date(bySettingHour: parts.first ?? 0, minute: parts.count > 1 ? parts[1] : 0, second: 0, of: Date()) ?? Date()
        }, set: { d in
            let c = Calendar.current.dateComponents([.hour, .minute], from: d)
            value.wrappedValue = String(format: "%02d:%02d", c.hour ?? 0, c.minute ?? 0)
        })
    }
}

/// A list's header, with a way back to the built-in list.
private struct ListHeader: View {
    let title: String
    let reset: () -> Void

    var body: some View {
        HStack {
            Text(title)
            Spacer()
            Button("Restore Defaults", action: reset)
                .buttonStyle(.borderless)
                .foregroundStyle(Theme.accent)
                .font(.callout)
        }
    }
}

/// A list of one-line settings, such as the trod bot's rules: each row
/// edits one in place and can remove it, and a last row adds one.
private struct LineList: View {
    let lines: [String]
    let prompt: String
    let change: ([String]) -> Void
    @State private var adding = ""

    var body: some View {
        ForEach(Array(lines.enumerated()), id: \.offset) { i, line in
            HStack(alignment: .firstTextBaseline) {
                Text("\(i + 1).")
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                LineField(value: line) { new in
                    var l = lines
                    if new.isEmpty { l.remove(at: i) } else { l[i] = new }
                    change(l)
                }
                Button {
                    var l = lines
                    l.remove(at: i)
                    change(l)
                } label: {
                    Image(systemName: "minus.circle")
                }
                .buttonStyle(.borderless)
                .help("Remove")
                .accessibilityLabel("Remove \(line)")
            }
        }
        HStack(alignment: .firstTextBaseline) {
            Image(systemName: "plus.circle").foregroundStyle(Theme.muted)
            TextField("", text: $adding, prompt: Text(prompt), axis: .vertical)
                .labelsHidden()
                .onSubmit(add)
        }
    }

    private func add() {
        let t = LineField.oneLine(adding)
        guard !t.isEmpty else { return }
        change(lines + [t])
        adding = ""
    }
}

/// One line of a LineList: it wraps to show a long line whole, and is sent
/// on Return or when it loses the keyboard, as one line.
private struct LineField: View {
    let value: String
    let commit: (String) -> Void
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        TextField("", text: $text, axis: .vertical)
            .labelsHidden()
            // A line may be 400 characters, which wraps to five lines at
            // the window's width.
            .lineLimit(1...6)
            .focused($focused)
            .onAppear { text = value }
            .onChange(of: value) { text = value }
            .onSubmit(send)
            .onChange(of: focused) { if !focused { send() } }
    }

    private func send() {
        let t = Self.oneLine(text)
        if t != value { commit(t) }
    }

    /// The text on one line, as each rule must be.
    static func oneLine(_ s: String) -> String {
        s.components(separatedBy: .newlines).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }.joined(separator: " ")
    }
}

// MARK: Controls

/// A section's footnote: quiet, and lined up with the rows above it.
private func footnote(_ text: String) -> some View {
    Text(text)
        .font(.callout)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
}

/// A text setting, sent when Return is pressed or the field loses the
/// keyboard rather than at each key.
private struct CommitField: View {
    let title: String
    let prompt: String
    @Binding var value: String
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        TextField(title, text: $text, prompt: Text(prompt))
            .focused($focused)
            .onAppear { text = value }
            .onChange(of: value) { text = value }
            .onSubmit(commit)
            .onChange(of: focused) { if !focused { commit() } }
    }

    private func commit() {
        let t = text.trimmingCharacters(in: .whitespaces)
        if t != value { value = t }
    }
}
