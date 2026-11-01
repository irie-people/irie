import AppKit
import Darwin
import SwiftUI

// A first run, from the app (DESIGN §2.7, docs/MAC.md): the app starts Irie
// itself, says what this computer still lacks with a button that fixes it,
// and walks a newcomer through a first task.

// MARK: The ready op

/// What this computer has of what Irie needs, as its agents see it: the
/// core's answer to `ready` (internal/ready).
struct ReadyReport: Codable, Equatable {
    var items: [ReadyItem]

    /// What keeps agents from starting.
    var blocking: [ReadyItem] { items.filter { $0.blocks == true && $0.status == "missing" } }
}

struct ReadyItem: Codable, Equatable, Identifiable {
    var id: String
    var name: String
    var status: String
    var version: String?
    var purpose: String
    var problem: String?
    var blocks: Bool?
    var fix: ReadyFix?

    enum CodingKeys: String, CodingKey {
        case id, name, status, version, problem, blocks, fix
        case purpose = "for"
    }

    var ok: Bool { status == "ok" }

    /// What fixes it, as a button says it.
    var fixLabel: String {
        switch id {
        case "signin": return "Sign In…"
        case "claude" where status == "missing" && problem == "not installed": return "Install…"
        default: return "Fix…"
        }
    }
}

struct ReadyFix: Codable, Equatable {
    var say: String
    var show: String?
    var link: String?
}

struct ToolArgs: Encodable {
    var name: String
}

// MARK: Starting Irie

/// Finds irie and starts Irie with it, for an app opened from the Finder,
/// the Dock or Spotlight, where no terminal ran `irie up`.
enum IrieLauncher {
    /// Where Irie keeps its own programs, as the installer and
    /// tools/live.sh put irie: $XDG_DATA_HOME/irie/bin.
    static var ownBin: String {
        let env = ProcessInfo.processInfo.environment
        var data = (NSHomeDirectory() as NSString).appendingPathComponent(".local/share")
        if let x = env["XDG_DATA_HOME"], x.hasPrefix("/") { data = x }
        return (data as NSString).appendingPathComponent("irie/bin")
    }

    /// The irie to start Irie with: Irie's own, then one a terminal would
    /// run, then the app's own copy (tools/release.sh), which goes to Irie's
    /// own folder first, so agents started from it keep a path that lasts
    /// when the app moves. nil when there is none.
    static func find() -> String? {
        let fm = FileManager.default
        let home = NSHomeDirectory()
        let own = (ownBin as NSString).appendingPathComponent("irie")
        let places = [own, home + "/.local/bin/irie", "/opt/homebrew/bin/irie", "/usr/local/bin/irie", home + "/go/bin/irie"]
        if let found = places.first(where: { fm.isExecutableFile(atPath: $0) }) { return found }
        let bundled = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/irie").path
        guard fm.isExecutableFile(atPath: bundled) else { return nil }
        do {
            try fm.createDirectory(atPath: ownBin, withIntermediateDirectories: true)
            let tmp = own + ".new"
            try? fm.removeItem(atPath: tmp)
            try fm.copyItem(atPath: bundled, toPath: tmp)
            // The app was opened, so its copy may run: a download's
            // quarantine stays with the app.
            removexattr(tmp, "com.apple.quarantine", 0)
            chmod(tmp, 0o755)
            guard rename(tmp, own) == 0 else { return bundled }
            return own
        } catch {
            return bundled
        }
    }

    /// Runs `irie up --login-env`, which starts the server with the login
    /// shell's environment, so agents find what a terminal would. Returns
    /// nil once Irie is up, else what irie said.
    static func up(_ bin: String) async -> String? {
        await Task.detached(priority: .userInitiated) { () -> String? in
            let p = Process()
            p.executableURL = URL(fileURLWithPath: bin)
            p.arguments = ["up", "--login-env"]
            p.standardInput = FileHandle.nullDevice
            p.standardOutput = FileHandle.nullDevice
            // A file, not a pipe: the tmux server outlives irie, and must
            // not hold the app's end open.
            let errPath = (NSTemporaryDirectory() as NSString).appendingPathComponent("irie-up-\(getpid()).txt")
            FileManager.default.createFile(atPath: errPath, contents: nil)
            guard let err = FileHandle(forWritingAtPath: errPath) else { return "Couldn’t start irie." }
            defer { try? FileManager.default.removeItem(atPath: errPath) }
            p.standardError = err
            do {
                try p.run()
            } catch {
                try? err.close()
                return error.localizedDescription
            }
            p.waitUntilExit()
            try? err.close()
            if p.terminationStatus == 0 { return nil }
            var said = ((try? String(contentsOfFile: errPath, encoding: .utf8)) ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if said.hasPrefix("irie: ") { said.removeFirst(6) }
            return said.isEmpty ? "irie up stopped with status \(p.terminationStatus)." : said
        }.value
    }

    /// Opens Terminal on `irie setup`, for what Irie needs before it can
    /// start at all, such as tmux: Terminal opens a .command file at once.
    static func openSetup(_ bin: String) {
        let dir = (NSTemporaryDirectory() as NSString).appendingPathComponent("irie-setup-\(getuid())")
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let path = (dir as NSString).appendingPathComponent("Irie Setup.command")
        let quoted = "'" + bin.replacingOccurrences(of: "'", with: "'\\''") + "'"
        let script = "#!/bin/sh\nclear\n\(quoted) setup\necho\necho 'Irie opens by itself once it can. You can close this window.'\n"
        try? script.write(toFile: path, atomically: true, encoding: .utf8)
        chmod(path, 0o755)
        NSWorkspace.shared.open(URL(fileURLWithPath: path))
    }

    /// The installer's command, for a computer with no irie at all.
    static let installCommand = "curl -fsSL https://irie-people.github.io/irie/install.sh | sh"
}

// MARK: The first task

/// The first task the welcome offers: it asks before it works, so a
/// newcomer sees its card start, work, turn red and finish in about a
/// minute, in a folder of its own, on Claude's quickest model.
enum FirstTask {
    static let text = "+haiku Write a haiku about my city. Ask me which city first."
}

/// The walk through a first task: which of its steps its session has
/// reached, by the states it has been in. An agent that asks with Claude
/// Code's question tool turns its card red; one that asks in plain words
/// ends its turn, so its card shows done until it is answered.
struct Coach: Equatable {
    var session: String?
    var started = false
    /// Its card was red: it asked with the question tool.
    var asked = false
    /// It worked again after its card was red or its turn ended: answered.
    var answered = false
    /// Its turn ended after it was answered.
    var done = false
    var last: String?

    static func over(_ state: String?) -> Bool { state == "done" || state == "idle" }
}

extension Store {
    /// Starts Irie when no terminal did: once a launch, before the app has
    /// ever reached a core. A core that goes away later is the
    /// supervisor's to bring back, or the user's: the offline view has a
    /// button.
    func startIrie() async {
        guard !starting else { return }
        guard let bin = IrieLauncher.find() else {
            startProblem = .notInstalled
            return
        }
        starting = true
        startProblem = nil
        let said = await IrieLauncher.up(bin)
        starting = false
        if let said { startProblem = .failed(said, bin: bin) }
    }

    /// Asks the core what this computer still lacks, again every few
    /// seconds while something blocks, so a sign-in shows at once.
    func checkReady() {
        guard canReady, !checkingReady else { return }
        checkingReady = true
        Task {
            defer { checkingReady = false }
            while true {
                guard let r = try? await client.call("ready", Optional<Empty>.none, timeout: 30, as: ReadyReport.self) else { return }
                if r != readiness { readiness = r }
                if r.blocking.isEmpty { return }
                try? await Task.sleep(nanoseconds: 4_000_000_000)
                guard case .online = link else { return }
            }
        }
    }

    /// Runs `irie setup` in a session of its own, whose terminal asks
    /// before each fix: install Claude Code, sign in.
    func runSetup() {
        Task {
            do {
                let r = try await client.call("tool", ToolArgs(name: "ready"), timeout: 30, as: SpawnResult.self)
                if !showTerminal { showTerminal = true }
                show(r.session.id, focus: true)
                checkReady()
            } catch {
                say("Couldn’t start the setup: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Puts the first task in the task bar, ready for Return, and walks the
    /// next task started through its steps.
    func tryFirstTask() {
        UserDefaults.standard.set(FirstTask.text, forKey: "taskDraft")
        coach = Coach()
        focusTaskBar += 1
    }

    /// Follows the coached session's state.
    func noteCoach(_ s: Session) {
        guard var c = coach, let id = c.session, id == s.id else { return }
        c.started = true
        if s.state == "needs_you" { c.asked = true }
        if s.state == "working", c.last == "needs_you" || Coach.over(c.last) { c.answered = true }
        if Coach.over(s.state), c.answered || c.asked { c.done = true }
        c.last = s.state
        if c != coach { coach = c }
    }

    func endCoach() { coach = nil }
}

/// Why the app could not start Irie.
enum StartProblem: Equatable {
    case notInstalled
    case failed(String, bin: String)
}

// MARK: Views

/// What an empty Irie says: its mark and word, what it does, what this
/// computer still lacks, and a first task to try.
struct Welcome: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        ScrollView {
            WelcomeContent()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .onAppear { store.checkReady() }
    }
}

/// The welcome without its scroll view, which ImageRenderer leaves out
/// (OnboardingSnapshot.swift).
struct WelcomeContent: View {
    @EnvironmentObject private var store: Store

    var body: some View {
            VStack(spacing: 12) {
                SunMark().frame(width: 56, height: 56)
                Text("Everything irie.").font(Theme.font(18, .bold)).foregroundStyle(Theme.ink)
                Text("Type a task below and an agent starts on it, in a folder of its own. Start as many as you like: each gets a card here, and turns red when it needs you.")
                    .font(.system(size: 12.5)).foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                if let r = store.readiness {
                    ReadyList(report: r)
                }
                let blocked = !(store.readiness?.blocking.isEmpty ?? true)
                if let c = store.coach, c.session == nil {
                    Text("It’s in the task bar below: press Return to start it.")
                        .font(.system(size: 12.5, weight: .medium)).foregroundStyle(Theme.accentInk)
                        .multilineTextAlignment(.center)
                } else {
                    Button("Try a First Task") { store.tryFirstTask() }
                        .buttonStyle(SoftButton(prominent: !blocked))
                    Text("It writes a haiku about your city, on Claude’s quickest model, and asks you which city first.")
                        .font(.system(size: 11.5)).foregroundStyle(Theme.muted)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity)
    }
}

/// Each need with its mark, and a button on the first one that blocks.
struct ReadyList: View {
    @EnvironmentObject private var store: Store
    let report: ReadyReport

    var body: some View {
        let first = report.blocking.first
        VStack(alignment: .leading, spacing: 5) {
            ForEach(report.items) { it in
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Text(it.ok ? "✓" : it.blocks == true && it.status == "missing" ? "✕" : "·")
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(it.ok ? Theme.done : it.blocks == true ? Theme.needs : Theme.muted)
                        .frame(width: 12)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(it.ok && it.version != nil ? "\(it.name) \(it.version!)" : it.name)
                            .font(.system(size: 12, weight: .medium)).foregroundStyle(Theme.ink)
                        Text(it.problem ?? it.purpose)
                            .font(.system(size: 11)).foregroundStyle(it.ok ? Theme.muted : Theme.needs)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 4)
                    if it.id == first?.id {
                        Button(it.fixLabel) { store.runSetup() }
                            .buttonStyle(SoftButton(prominent: true))
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
    }
}

/// Above the cards while something keeps agents from starting: what, and
/// the button that fixes it.
struct ReadyBanner: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        if let it = store.readiness?.blocking.first, !store.sessions.isEmpty {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(sentence(it.problem ?? "\(it.name) is missing"))
                        .font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.needs)
                    Text("Agents can’t start until it is.")
                        .font(.system(size: 11)).foregroundStyle(Theme.muted)
                }
                Spacer(minLength: 6)
                Button(it.fixLabel) { store.runSetup() }
                    .buttonStyle(SoftButton(prominent: true))
            }
            .padding(10)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.needsTint))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.needs.opacity(0.45), lineWidth: 1))
            .padding(.horizontal, 10)
            .padding(.top, 8)
            .accessibilityElement(children: .combine)
        }
    }

    private func sentence(_ s: String) -> String {
        guard let f = s.first else { return s }
        let t = f.uppercased() + s.dropFirst()
        return t.hasSuffix(".") ? t : t + "."
    }
}

/// The steps of a first task, above the cards, ticked as its card moves on:
/// it started, it needs you, it is done. Its × ends it.
struct CoachCard: View {
    @EnvironmentObject private var store: Store

    var body: some View {
        if let c = store.coach, let id = c.session, let s = store.sessions.first(where: { $0.id == id }) {
            VStack(alignment: .leading, spacing: 7) {
                HStack {
                    Text("Your first task").font(.system(size: 12.5, weight: .bold)).foregroundStyle(Theme.ink)
                    Spacer()
                    Button { store.endCoach() } label: {
                        Image(systemName: "xmark").font(.system(size: 10, weight: .bold))
                    }
                    .buttonStyle(.plain).foregroundStyle(Theme.muted)
                    .help("Hide the steps; the task runs on")
                    .accessibilityLabel("Hide the steps")
                }
                if let stuck = stuck(s) {
                    step(.now, stuck)
                } else {
                    step(c.started ? .done : .now, c.started ? "It started. Every task gets a card like this one, and its mark shows how it’s doing." : "It’s starting…")
                    if s.state == "needs_you" {
                        step(.now, "Its card is red: it needs you. It asks which city: answer it on the right, then press Return.")
                    } else if c.answered || c.done {
                        step(.done, "It asked you something, and you answered.")
                    } else if Coach.over(s.state) {
                        step(.now, "Its turn is over, and it’s your move: if it asked which city, answer it on the right, then press Return.")
                    } else {
                        step(.later, "When its card turns red, it needs you: here it asks which city.")
                    }
                    step(c.done ? .now : .later, c.done ? "Done. ✓ means it’s ready for you to check: read its haiku. Then close it with ⌘W, or park it with Ⓟ; ⌘Z brings either back." : "When it’s done, its card shows ✓.")
                }
                if c.done {
                    Button("Got It") { store.endCoach() }
                        .buttonStyle(SoftButton(prominent: true))
                }
            }
            .padding(12)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.card))
            .overlay(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).strokeBorder(Theme.accent.opacity(0.5), lineWidth: 1))
            .padding(.horizontal, 10)
            .padding(.top, 8)
        }
    }

    private enum When { case done, now, later }

    private func step(_ w: When, _ text: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Text(w == .done ? "✓" : w == .now ? "●" : "○")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(w == .done ? Theme.done : w == .now ? Theme.accent : Theme.muted)
                .frame(width: 12)
            Text(text)
                .font(.system(size: 11.5, weight: w == .now ? .medium : .regular))
                .foregroundStyle(w == .later ? Theme.muted : Theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// What to do when the task can't go on by itself.
    private func stuck(_ s: Session) -> String? {
        switch s.state {
        case "stalled": return "It’s waiting at the start, usually for a sign-in or to trust its folder. Answer it in its terminal: View > Terminal (⌥⌘T)."
        case "failed": return "It couldn’t start: \(s.reason ?? "see its terminal"). Try the task again once that is fixed."
        default: return nil
        }
    }
}

/// What the sidebar shows while no core answers: Irie starting, why it
/// can't, or how to get it.
struct Offline: View {
    @EnvironmentObject private var store: Store
    let why: String
    let socket: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            switch (store.starting, store.startProblem) {
            case (true, _):
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Starting Irie…").font(Theme.font(13.5, .bold)).foregroundStyle(Theme.ink)
                }
                Text("Its agents run on in the background, even when this window is closed.")
                    .font(.system(size: 12)).foregroundStyle(Theme.muted)
            case let (false, .failed(said, bin)?):
                Text("Irie couldn’t start").font(Theme.font(13.5, .bold)).foregroundStyle(Theme.ink)
                Text((try? AttributedString(markdown: said)) ?? AttributedString(said)).font(.system(size: 12)).foregroundStyle(Theme.needs)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                HStack {
                    Button("Set Up Irie…") { IrieLauncher.openSetup(bin) }
                        .buttonStyle(SoftButton(prominent: true))
                    Button("Try Again") { Task { await store.startIrie() } }
                        .buttonStyle(SoftButton())
                }
                Text("Set Up opens Terminal on `irie setup`, which asks before it installs anything.")
                    .font(.system(size: 11)).foregroundStyle(Theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            case (false, .notInstalled?):
                Text("Irie isn’t installed").font(Theme.font(13.5, .bold)).foregroundStyle(Theme.ink)
                Text("Paste this in Terminal to install it:")
                    .font(.system(size: 12)).foregroundStyle(Theme.muted)
                Text(IrieLauncher.installCommand)
                    .font(.system(size: 11, design: .monospaced)).foregroundStyle(Theme.ink)
                    .textSelection(.enabled)
                    .padding(8)
                    .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.field))
                Button("Copy") { store.copy(IrieLauncher.installCommand, what: "install command") }
                    .buttonStyle(SoftButton(prominent: true))
            case (false, nil):
                Text(why).font(Theme.font(13.5, .bold)).foregroundStyle(Theme.ink)
                Button("Start Irie") { Task { await store.startIrie() } }
                    .buttonStyle(SoftButton(prominent: true))
                Text("Or type `irie up` in a terminal. The app connects by itself once it’s running.")
                    .font(.system(size: 12)).foregroundStyle(Theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text(Age.tilde(socket)).font(.system(size: 10.5, design: .monospaced)).foregroundStyle(Theme.muted)
                .lineLimit(1).truncationMode(.middle)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}
