import AppKit
import Foundation
import SwiftUI

/// The app's state: the core's rows, kept live by one subscription, and the
/// requests the window makes.
@MainActor
final class Store: ObservableObject {
    enum Link: Equatable {
        case connecting
        case online(version: String)
        case offline(String)
    }

    @Published private(set) var link: Link = .connecting
    @Published private(set) var sessions: [Session] = []
    @Published private(set) var attach: AttachInfo?
    @Published var selection: String?
    /// How the sidebar groups the sessions, and the groups folded away. Both
    /// are kept when the user changes them (choose(_:), fold(_:)).
    @Published var mode: SidebarMode = SidebarMode(saved: UserDefaults.standard.string(forKey: "sidebarMode"))
    @Published var folded: Set<String> = Set(UserDefaults.standard.stringArray(forKey: "foldedGroups") ?? [])
    /// Parked is folded until it is opened, so a parked card goes out of
    /// sight at once; the app keeps it open once opened (fold(_:)).
    @Published var parkedOpen = UserDefaults.standard.bool(forKey: "parkedOpen")
    /// The repos the @ words know and the agents the + words name, with what
    /// each runs: what the task bar's menu and the new-task sheet offer.
    @Published private(set) var repoNames: [String] = []
    @Published private(set) var agents: [Agent] = []
    /// Whether the core takes a model for one task.
    @Published private(set) var canPickModel = false
    /// Whether the core reads and edits its config for the Settings window.
    @Published private(set) var canConfigure = false
    /// Whether the core parks sessions (Controls.swift).
    @Published private(set) var canPark = false
    /// Cards parked or unparked from under the pointer, each with whether
    /// it is drawn parked: it stays where it was until the pointer leaves
    /// it, so a second click on the same spot takes it back (Controls.swift).
    @Published var lingering: [String: Bool] = [:]
    /// The card under the pointer.
    var hovered: String?
    /// Whether the core keeps a history to undo, and what undo and redo
    /// would take next (Undo.swift).
    @Published private(set) var canUndo = false
    @Published private(set) var undoLabel: String?
    @Published private(set) var redoLabel: String?
    /// Where a task runs when its text names no @repo: a repo's name or a
    /// folder's path, or nil for a new scratch folder. Kept between runs.
    @Published private(set) var taskRepo: String? = UserDefaults.standard.string(forKey: "taskRepo")
    /// ui.red_on_done, from the core: done sessions are drawn red. Look keeps
    /// it for the marks; this draws the views again when it changes.
    @Published private(set) var redOnDone = false
    /// ui.helper_style, from the core: a helper beside its session is drawn
    /// as cards ("browser") or as a terminal ("terminal").
    @Published private(set) var helperStyle = "browser"
    /// The helper shown beside each session's conversation, as sub:<id>, by
    /// session (ConversationView, HelperPane.swift).
    @Published private(set) var openHelpers: [String: String] = [:]
    /// spawn.close_when_done, from the core: a finished session closes
    /// itself, so the title bar keeps no To verify count, and the review dock
    /// lists what closed. It is in history too (`irie ctl history`, ⇧⌘T).
    @Published private(set) var closeWhenDone = false
    /// Whether the new-task sheet is open (⌘N).
    @Published var newTaskSheet = false
    @Published var notice: Notice?
    @Published var spawning = false
    /// The session whose name its card has open to edit, and the name as
    /// typed so far.
    @Published var renaming: String?
    @Published var renameText = ""
    /// The card a drag holds, and where it would land (Controls.swift).
    @Published var dragging: String?
    @Published var dropMark: DropMark?
    /// Bumped when the terminal should take keyboard focus.
    @Published var focusTerminal = 0
    /// Bumped to move the keyboard to the session list or the find field.
    @Published var focusSidebar = 0
    @Published var focusFind = 0
    /// The sidebar's filter, which the keys that walk the sidebar follow.
    @Published var filter = ""
    @Published private(set) var fontSize: CGFloat = Store.savedFontSize()
    /// The terminal view, to tell whether it has the keyboard.
    weak var terminalView: NSView?
    /// Whether the rows are `--snapshot`'s, which draws the terminal's frame
    /// round an empty terminal, as no core attaches one.
    private(set) var isFixture = false
    /// The right side shows the live terminal instead of the conversation
    /// (View > Terminal, ⌥⌘T); remembered across launches.
    @Published var showTerminal = UserDefaults.standard.bool(forKey: "showTerminal") {
        didSet {
            UserDefaults.standard.set(showTerminal, forKey: "showTerminal")
            if showTerminal { focusTerminal += 1 } else { focusMessage += 1 }
            keepSplit()
        }
    }
    /// The session the core shows beside its teammates, as last asked
    /// (TeamSplit.swift); nil while the slot shows the picked session.
    @Published private(set) var splitShown: String?
    /// Bumped when the conversation's message box should take the keyboard.
    @Published var focusMessage = 0
    /// Whether the message box has the keyboard.
    var messageHasKeys = false
    /// The Claude Code sessions outside Irie, the one selected, the one a
    /// move asks about, and the one moving (OutsideSection.swift).
    @Published var outside: [OutsideSession] = []
    @Published var outsideSelection: String?
    @Published var confirmingMove: OutsideSession?
    @Published var moving: String?
    /// The closed sessions reopen can bring back, the last closed first
    /// (Closing.swift).
    @Published var closed: [ClosedSession] = []
    var outsideProblem = ""
    /// The session the core is to show next, and whether a request is out.
    private var wanted: (id: String, focus: Bool)?
    private var showing = false

    static let defaultFontSize: CGFloat = 13

    struct Notice: Equatable, Identifiable {
        let id = UUID()
        var text: String
        var isError: Bool
        /// It offers Undo, which takes back the step it reports.
        var undo = false
    }

    let client: CoreClient
    /// The core's config, as the Settings window shows and edits it.
    let settings: SettingsModel
    private var started = false

    init(socketPath: String = CorePaths.socket()) {
        client = CoreClient(socketPath: socketPath)
        settings = SettingsModel(client: client)
        settings.changed = { [weak self] in self?.loadChoices() }
    }

    var socketPath: String { client.socketPath }

    // MARK: Connection

    func start() {
        guard !started else { return }
        started = true
        if let seconds = FreezeDog.restartedAfter {
            say("Irie stopped answering for \(seconds) s, so the freeze dog restarted it. Every agent kept running.",
                error: false, seconds: 10)
        }
        Task { await run() }
    }

    /// Connects, subscribes and stays subscribed; on any failure it waits and
    /// tries again, so starting Irie later just works.
    private func run() async {
        while true {
            do {
                let hello = try await client.call("hello", HelloArgs(), timeout: 2, as: HelloResult.self)
                guard hello.features.contains("attach-info") else {
                    link = .offline("This Irie is too old for the app. Run the new build's `irie up` to update it.")
                    try await Task.sleep(nanoseconds: 3_000_000_000)
                    continue
                }
                attach = try await client.call("attach-info", Optional<Empty>.none, timeout: 2, as: AttachInfo.self)
                link = .online(version: hello.version)
                // A core that restarted asks again.
                splitShown = nil
                canPickModel = hello.features.contains("spawn-model")
                canConfigure = hello.features.contains("config")
                canPark = hello.features.contains("park")
                canUndo = hello.features.contains("undo")
                if hello.features.contains("theme") { sendTheme(Theme.palette.id) }
                loadChoices()
                // The store lives as long as the app.
                try await client.subscribe { frame in
                    Task { @MainActor in self.apply(frame) }
                }
                link = .offline("The core went away.")
            } catch {
                link = .offline(Self.describe(error))
            }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
        }
    }

    /// Tells the core the palette in use: the agents' colours on Irie's
    /// status line follow it, so they match the cards.
    func sendTheme(_ id: String) {
        Task { _ = try? await client.call("theme", ThemeArgs(theme: id), timeout: 2, as: Empty.self) }
    }

    private static func describe(_ error: Error) -> String {
        if case let CoreError.system(call, code) = error, call == "connect", code == ENOENT || code == ECONNREFUSED {
            return "Irie isn’t running."
        }
        return error.localizedDescription
    }

    private func apply(_ f: Frame) {
        if let h = f.header, (h.redOnDone ?? false) != Look.redOnDone {
            Look.redOnDone = h.redOnDone ?? false
            redOnDone = Look.redOnDone
        }
        if let h = f.header, (h.closeWhenDone ?? false) != closeWhenDone {
            closeWhenDone = h.closeWhenDone ?? false
        }
        if let s = f.header?.helperStyle, s != helperStyle {
            helperStyle = s
        }
        switch f.t {
        case "snapshot":
            sessions = (f.sessions ?? []).sorted(by: Self.before)
        case "upsert":
            guard let s = f.session else { return }
            if let i = sessions.firstIndex(where: { $0.id == s.id }) {
                let moved = sessions[i].order != s.order
                sessions[i] = s
                if moved { sessions.sort(by: Self.before) }
            } else {
                sessions.append(s)
                sessions.sort(by: Self.before)
            }
        case "remove":
            sessions.removeAll { $0.id == f.id }
            if let id = f.id { lingering[id] = nil }
        case "outside":
            applyOutside(f)
            return
        case "closed":
            closed = f.closed ?? []
            return
        case "history":
            undoLabel = f.undo.flatMap { $0.isEmpty ? nil : $0 }
            redoLabel = f.redo.flatMap { $0.isEmpty ? nil : $0 }
            return
        default:
            return
        }
        if selection == nil, let shown = sessions.first(where: { $0.shown == true }) {
            selection = shown.id
        }
        if let sel = selection, !sessions.contains(where: { $0.id == sel }) {
            selection = nil
        }
        keepSplit()
    }

    /// The picked session while it shows beside its teammates: the terminal
    /// shows, and it has teammates the app can reach.
    var splitSession: Session? {
        guard showTerminal, outsideSelection == nil, let id = selection, attach?.teams != nil || isFixture,
              let s = sessions.first(where: { $0.id == id }), !(s.team ?? []).isEmpty else { return nil }
        return s
    }

    /// Asks the core again for the picked session when it starts or stops
    /// showing beside its teammates.
    private func keepSplit() {
        guard !isFixture else { return }
        let want = splitSession?.id
        guard want != splitShown else { return }
        if let id = selection, outsideSelection == nil {
            show(id)
        } else {
            splitShown = nil
        }
    }

    /// Asks the core for the repos the @ words know and the agents the +
    /// words name. A core that answers neither leaves the menus as they are.
    func loadChoices() {
        Task {
            if let r = try? await client.call("complete", CompleteArgs(text: "@"), timeout: 5, as: CompleteResult.self) {
                repoNames = r.items.map { String($0.dropFirst()) }
            }
            if let r = try? await client.call("complete", CompleteArgs(text: "+"), timeout: 5, as: CompleteResult.self) {
                agents = r.items.enumerated().map { i, item in
                    Agent(name: String(item.dropFirst()), note: r.notes.flatMap { i < $0.count ? $0[i] : nil } ?? "")
                }
            }
        }
    }

    /// Where the task bar's tasks run: a repo's name or a folder's path, or
    /// nil for a new scratch folder.
    func setTaskRepo(_ r: String?) {
        taskRepo = r
        UserDefaults.standard.set(r, forKey: "taskRepo")
    }

    /// Asks for a folder for the task bar's tasks: a git repo gets a worktree,
    /// any other folder runs in place.
    func chooseTaskFolder() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Use This Folder"
        panel.message = "Tasks will run in this folder: a git repo gets its own worktree."
        if panel.runModal() == .OK, let url = panel.url {
            setTaskRepo(url.path)
        }
    }

    /// Fills the store with sessions no core sent, for `--snapshot`.
    func loadFixture(_ rows: [Session], selection: String?) {
        isFixture = true
        sessions = rows.sorted(by: Self.before)
        self.selection = selection
        link = .online(version: "snapshot")
        repoNames = ["bigup", "irie", "notes"]
        agents = [Agent(name: "auto", note: "routes each task to the cheapest profile that does it well"),
                  Agent(name: "claude", note: "claude-opus-5-5 · max")]
        canPickModel = true
        canConfigure = true
        canPark = true
        canUndo = true
        taskRepo = "irie"
    }

    /// The list's order, which the core keeps and the user sets: a new
    /// session goes last, Promote puts one first, and a drag puts one where
    /// it is dropped. A core without it sends no order: the order they
    /// started.
    static func before(_ a: Session, _ b: Session) -> Bool {
        if let x = a.order, let y = b.order, x != y { return x < y }
        return a.created != b.created ? a.created < b.created : a.id < b.id
    }

    /// Changes the rows at once, ahead of the core's frames, which then
    /// confirm or correct them (Controls.swift).
    func preview(_ change: (inout [Session]) -> Void) {
        change(&sessions)
        sessions.sort(by: Self.before)
    }

    // MARK: Keyboard

    /// The sidebar's groups, without the rows the filter hides. In the List
    /// view, one list; in the Repo view, a group per repo in the order its
    /// first session started, then the sessions in no repo. Then the Parked
    /// group. Within a group the sessions keep the list's one order, which
    /// only the user changes, so a state change never moves a card (DESIGN
    /// §4.1).
    var drawnGroups: [SessionGroup] {
        let f = filter.trimmingCharacters(in: .whitespaces).lowercased()
        let rows = f.isEmpty ? sessions : sessions.filter { $0.name.lowercased().contains(f) }
        let parked = rows.filter(drawnParked)
        var out: [SessionGroup] = []
        switch mode {
        case .list:
            let active = rows.filter { !drawnParked($0) }
            if !active.isEmpty {
                out.append(SessionGroup(id: SessionGroup.list, title: "Sessions", path: nil, sessions: active))
            }
        case .repo:
            var order: [String] = [], byRepo: [String: [Session]] = [:], none: [Session] = []
            for s in rows where !drawnParked(s) {
                guard let r = s.repo, !r.isEmpty else {
                    none.append(s)
                    continue
                }
                if byRepo[r] == nil { order.append(r) }
                byRepo[r, default: []].append(s)
            }
            let names = SessionGroup.names(order)
            out = order.map { SessionGroup(id: "repo:" + $0, title: names[$0] ?? $0, path: $0, sessions: byRepo[$0] ?? []) }
            if !none.isEmpty {
                out.append(SessionGroup(id: "repo:", title: "Not in a repo", path: nil, sessions: none))
            }
        }
        if !parked.isEmpty {
            out.append(SessionGroup(id: SessionGroup.parked, title: "Parked", path: nil, sessions: parked))
        }
        return out
    }

    /// Whether a session is drawn in Parked: whether it is parked, or for a
    /// card that lingers under the pointer, whether it was.
    func drawnParked(_ s: Session) -> Bool { lingering[s.id] ?? s.isParked }

    /// The id of the group a session is drawn in: Parked while it is drawn
    /// parked, else the list or its repo's group.
    func groupID(of s: Session) -> String {
        if drawnParked(s) { return SessionGroup.parked }
        switch mode {
        case .list: return SessionGroup.list
        case .repo: return "repo:" + (s.repo ?? "")
        }
    }

    /// The cards in the order the sidebar draws them, without those in folded
    /// groups or hidden by the filter. The keys that walk the sidebar follow
    /// it.
    var drawnOrder: [Session] { drawnGroups.filter(isOpen).flatMap(\.sessions) }

    /// A group is open unless folded, and Parked only once opened. Every
    /// group is open while the filter has text, so what it finds shows.
    func isOpen(_ g: SessionGroup) -> Bool {
        if !filter.trimmingCharacters(in: .whitespaces).isEmpty { return true }
        return g.id == SessionGroup.parked ? parkedOpen : !folded.contains(g.id)
    }

    func fold(_ g: SessionGroup) {
        if g.id == SessionGroup.parked {
            parkedOpen.toggle()
            UserDefaults.standard.set(parkedOpen, forKey: "parkedOpen")
            return
        }
        if folded.contains(g.id) { folded.remove(g.id) } else { folded.insert(g.id) }
        UserDefaults.standard.set(Array(folded).sorted(), forKey: "foldedGroups")
    }

    func choose(_ m: SidebarMode) {
        mode = m
        UserDefaults.standard.set(m.rawValue, forKey: "sidebarMode")
    }

    /// Shows the next (+1) or previous (-1) row of the drawn order, wrapping
    /// at the ends unless told not to. The keyboard stays where it is. From a
    /// card the sidebar doesn't draw, such as the shown one in a folded group
    /// or one the filter hides, it goes on from that card's place in the list.
    func step(_ delta: Int, wrap: Bool = true) {
        let all = drawnGroups.flatMap(\.sessions)
        let drawn = Set(drawnOrder.map(\.id))
        guard !drawn.isEmpty else { return }
        var i = all.firstIndex { $0.id == selection } ?? (delta > 0 ? -1 : all.count)
        for _ in 0..<all.count {
            i += delta
            if wrap {
                i = (i + all.count) % all.count
            } else if !all.indices.contains(i) {
                return
            }
            let s = all[i]
            if drawn.contains(s.id) {
                if outsideSelection != nil || s.id != selection { show(s.id) }
                return
            }
        }
    }

    /// Shows the nth row (from 1) of the drawn order.
    func showRow(_ n: Int) {
        let order = drawnOrder
        guard n >= 1, n <= order.count else { return }
        show(order[n - 1].id)
    }

    /// Moves the keyboard between the session list and the terminal.
    func toggleFocus() {
        if !showTerminal {
            if messageHasKeys { focusSidebar += 1 } else { focusMessage += 1 }
        } else if let t = terminalView, t.window?.firstResponder === t {
            focusSidebar += 1
        } else {
            focusTerminal += 1
        }
    }

    func setFontSize(_ size: CGFloat) {
        fontSize = min(max(size, 9), 28)
        UserDefaults.standard.set(Double(fontSize), forKey: "terminalFontSize")
    }

    private static func savedFontSize() -> CGFloat {
        let v = UserDefaults.standard.double(forKey: "terminalFontSize")
        return v >= 9 && v <= 28 ? CGFloat(v) : defaultFontSize
    }

    // MARK: Requests

    /// Shows a helper beside its session's conversation. The terminal never
    /// splits, so a click on a helper while it shows brings the
    /// conversation back.
    func openHelper(_ agent: String, in session: String) {
        if selection != session || outsideSelection != nil { show(session) }
        openHelpers[session] = agent
        if showTerminal { showTerminal = false }
    }

    /// Shows a session's terminal beside its teammates'.
    func showTeam(_ session: String) {
        if !showTerminal { showTerminal = true }
        show(session, focus: true)
    }

    /// Puts a session's conversation back on its own.
    func closeHelper(in session: String) {
        if let a = openHelpers[session] { lastHelpers[session] = a }
        openHelpers[session] = nil
    }

    /// The helper each session showed last, and the newest its conversation
    /// names, which View > Show Helpers opens when none runs.
    private var lastHelpers: [String: String] = [:]
    private var newestHelpers: [String: String] = [:]

    /// Notes the newest helper a session's conversation names.
    func noteHelper(_ agent: String, in session: String) {
        newestHelpers[session] = agent
    }

    /// Whether the selected session shows a helper beside its conversation.
    var helperShown: Bool {
        guard let id = selection, outsideSelection == nil, !showTerminal else { return false }
        return openHelpers[id] != nil
    }

    /// View > Show Helpers: opens the selected session's helper beside its
    /// conversation (the one shown last, else the newest running, else the
    /// newest), or puts the conversation back on its own.
    func toggleHelpers() {
        guard let id = selection, outsideSelection == nil, let s = sessions.first(where: { $0.id == id }) else { return }
        if helperShown {
            closeHelper(in: id)
            return
        }
        if let a = openHelpers[id] ?? lastHelpers[id] ?? s.subagents?.last?.agent ?? newestHelpers[id] {
            openHelper(a, in: id)
        } else {
            say("\(s.name) has no helpers yet. They show here once it starts one.", error: false)
        }
    }

    /// Swaps the session into the slot, which the terminal shows. Requests
    /// that come faster than the core answers, such as a key held down or
    /// pressed twice, collapse into the last one: the core would take them
    /// in no set order and leave the terminal on a session the list doesn't
    /// highlight.
    func show(_ id: String, focus: Bool = false) {
        // Going to another session renames the one open to edit, as clicking
        // elsewhere does in Finder.
        if renaming != nil && renaming != id { endRename(commit: true) }
        selection = id
        outsideSelection = nil
        wanted = (id, focus)
        guard !showing else { return }
        showing = true
        Task {
            while let next = wanted {
                wanted = nil
                do {
                    let split = splitSession?.id == next.id
                    splitShown = split ? next.id : nil
                    _ = try await client.call("show", ShowArgs(session: next.id, focus: next.focus, split: split), as: Empty.self)
                    // The conversation's message box, or the terminal when it
                    // shows; a name open to edit, after a double click's first
                    // click, keeps the keyboard.
                    if next.focus, wanted == nil, renaming != next.id { if showTerminal { focusTerminal += 1 } else { focusMessage += 1 } }
                } catch where wanted == nil {
                    say("Couldn’t show it: \(error.localizedDescription)", error: true)
                } catch {}
            }
            showing = false
        }
    }

    /// Starts a task typed in the task bar. With no @repo in its text it
    /// runs where the bar's menu says. Returns whether it started.
    func spawn(_ input: String) async -> Bool {
        await spawn(input, repo: taskRepo, worktree: true, agent: nil, model: nil)
    }

    /// Starts a task from the bar or the new-task sheet. The @repo and
    /// +agent words in its text win over the menus' repo and agent.
    func spawn(_ input: String, repo: String?, worktree: Bool, agent: String?, model: String?) async -> Bool {
        var args: SpawnArgs
        do {
            args = try TaskParser.parse(input, home: NSHomeDirectory())
        } catch {
            say(error.localizedDescription, error: true)
            return false
        }
        if args.repo == nil, let repo {
            args.repo = repo
            args.inPlace = worktree ? nil : true
        }
        if args.profile == nil { args.profile = agent }
        if canPickModel { args.model = model }
        return await run(args)
    }

    private func run(_ args: SpawnArgs) async -> Bool {
        spawning = true
        defer { spawning = false }
        do {
            // A task in a repo may make a worktree and check out submodules first.
            let r = try await client.call("spawn", args, timeout: 600, as: SpawnResult.self)
            say("Started \(r.session.name).", error: false)
            // It starts behind the session on show, so a task typed mid-work
            // never pulls the view or the keyboard away. With nothing on
            // show, it comes straight up, ready to type to.
            if selection == nil { show(r.session.id, focus: true) }
            // A repo it ran in is now among the recent ones.
            loadChoices()
            return true
        } catch {
            say(error.localizedDescription, error: true)
            return false
        }
    }

    /// Opens the session's name to edit, in its card.
    func beginRename(_ id: String) {
        guard let s = sessions.first(where: { $0.id == id }) else { return }
        if renaming != id {
            endRename(commit: true)
            renameText = s.name
        }
        selection = id
        outsideSelection = nil
        renaming = id
    }

    /// Closes the name's editor and, with commit, renames the session to
    /// what was typed. The card shows the new name at once; the core's
    /// answer, which numbers a name another session has, follows. The name
    /// is the core's, so it outlasts the app, the core and installs.
    func endRename(commit: Bool) {
        guard let id = renaming else { return }
        renaming = nil
        guard commit, let i = sessions.firstIndex(where: { $0.id == id }) else { return }
        let old = sessions[i].name
        let name = renameText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, name != old else { return }
        sessions[i].name = name
        Task {
            do {
                let r = try await client.call("rename", RenameArgs(session: id, name: name), as: RenameResult.self)
                if let j = sessions.firstIndex(where: { $0.id == id }) { sessions[j].name = r.session.name }
                say("Renamed \(old) to \(r.session.name).", error: false)
            } catch {
                if let j = sessions.firstIndex(where: { $0.id == id }), sessions[j].name == name { sessions[j].name = old }
                say("Couldn’t rename \(old): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Shows a notice above the task bar and has VoiceOver read it. A
    /// confirmation goes after 3 s; an error stays until the next notice, or
    /// until the user edits or submits (WCAG 2.2.1).
    func say(_ text: String, error: Bool, seconds: UInt64 = 3, undo: Bool = false) {
        notice = Notice(text: text, isError: error, undo: undo && canUndo)
        AccessibilityNotification.Announcement(text).post()
        guard !error else { return }
        let id = notice?.id
        Task {
            try? await Task.sleep(nanoseconds: seconds * 1_000_000_000)
            if notice?.id == id { notice = nil }
        }
    }

    func dismissNotice() { notice = nil }

    // MARK: Desktop actions

    func copy(_ text: String, what: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        say("Copied the \(what).", error: false)
    }

    func reveal(_ path: String) {
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }

    /// Opens the slot in a Ghostty window: the terminal for builds without
    /// SwiftTerm, and a second view for builds with it.
    func openInGhostty() {
        guard let a = attach else { return }
        guard NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.mitchellh.ghostty") != nil else {
            say("Ghostty isn’t installed. In any terminal, run: \(a.tmux) -S \(a.socket) attach-session -t \(a.target)", error: true)
            return
        }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        p.arguments = ["-na", "Ghostty", "--args", "--title=Irie", "-e", a.tmux, "-S", a.socket, "attach-session", "-t", a.target]
        do {
            try p.run()
        } catch {
            say("Couldn’t open Ghostty: \(error.localizedDescription)", error: true)
        }
    }
}

/// How the sidebar groups the sessions: one list, or a group per repo.
/// Either way the parked ones come last, in the Parked group, folded until
/// it is opened.
enum SidebarMode: String, CaseIterable {
    case list, repo

    /// The mode the app kept. The State view of earlier builds, which moved
    /// a card whenever its state changed, is the List view now.
    init(saved: String?) {
        self = saved == "state" ? .list : SidebarMode(rawValue: saved ?? "") ?? .repo
    }

    var title: String { self == .list ? "List" : "Repo" }
}

/// One group of the sidebar: the list, a repo, the sessions in no repo, or
/// the parked ones.
struct SessionGroup: Identifiable, Equatable {
    let id: String
    let title: String
    /// The repo's main checkout, for a repo's group.
    let path: String?
    let sessions: [Session]

    /// The List view's one group, which has no header, and the Parked group.
    static let list = "list"
    static let parked = "parked"

    /// A name per repo: its folder's name, with the folder above it when two
    /// repos share a name.
    static func names(_ paths: [String]) -> [String: String] {
        let base = { (p: String) in (p as NSString).lastPathComponent }
        var count: [String: Int] = [:]
        for p in paths { count[base(p), default: 0] += 1 }
        var out: [String: String] = [:]
        for p in paths {
            let b = base(p)
            let parent = ((p as NSString).deletingLastPathComponent as NSString).lastPathComponent
            out[p] = count[b, default: 0] > 1 && !parent.isEmpty ? "\(b) · \(parent)" : b
        }
        return out
    }
}

/// An agent the + words name: a profile, auto routing or a local model, and
/// what it runs and costs.
struct Agent: Identifiable, Equatable {
    let name: String
    let note: String

    var id: String { name }
    /// Whether it takes a model for one task: a Claude Code profile does;
    /// routing and local models pick their own.
    var takesModel: Bool { name != "auto" && !name.hasPrefix("ollama:") }
}

extension String {
    /// A repo's name or a folder's path as the menus show it: a path by its
    /// last folder.
    var repoLabel: String { hasPrefix("/") || hasPrefix("~") ? (self as NSString).lastPathComponent : self }
}
