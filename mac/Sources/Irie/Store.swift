import AppKit
import Foundation
import Observation
import SwiftUI

/// The app's state: the core's rows, kept live by one subscription, and the
/// requests the window makes. It is observed property by property
/// (Observation): a view draws again only when a property it read changed,
/// so a session's new detail redraws its card, not the window. A set is
/// news to every view that read the property, even when the value is the
/// same, so the hot paths set only what changed (apply(_:)).
@MainActor
@Observable
final class Store {
    enum Link: Equatable {
        case connecting
        case online(version: String)
        case offline(String)
    }

    private(set) var link: Link = .connecting
    private(set) var sessions: [Session] = [] {
        didSet { derive() }
    }
    private(set) var attach: AttachInfo?
    var selection: String? {
        didSet { if selection != oldValue { derive() } }
    }
    /// What most views want of the rows, kept apart so that a row's change
    /// redraws only the views it changes (derive()): the selected session's
    /// row, the title bar's counts, and whether there is any row. A view
    /// that read every row would draw again on every frame.
    private(set) var selectedRow: Session?
    private(set) var counts = RowCounts()
    private(set) var hasRows = false
    /// How the sidebar groups the sessions, and the groups folded away. Both
    /// are kept when the user changes them (choose(_:), fold(_:)).
    var mode: SidebarMode = SidebarMode(saved: UserDefaults.standard.string(forKey: "sidebarMode"))
    var folded: Set<String> = Set(UserDefaults.standard.stringArray(forKey: "foldedGroups") ?? [])
    /// Parked is folded until it is opened, so a parked card goes out of
    /// sight at once; the app keeps it open once opened (fold(_:)).
    var parkedOpen = UserDefaults.standard.bool(forKey: "parkedOpen")
    /// The repos the @ words know and the agents the + words name, with what
    /// each runs: what the task bar's menu and the new-task sheet offer.
    private(set) var repoNames: [String] = []
    private(set) var agents: [Agent] = []
    /// Whether the core takes a model for one task.
    private(set) var canPickModel = false
    /// The core takes a tutor mode for one task, and serves the Help menu
    /// (docs/proposals/learning.md).
    private(set) var canTutor = false
    private(set) var canLearn = false
    /// The core runs the right sidebar's Dock (op dock, Dock.swift).
    private(set) var canDock = false
    /// The Dock controls the user stopped, by session, which the Dock then
    /// leaves stopped until the user starts them again.
    @ObservationIgnored var dockStopped: [String: Set<String>] = [:]
    /// The Learn window's page (Learn.swift).
    var learnTab = LearnTab.resources
    /// Whether the core reads and edits its config for the Settings window.
    private(set) var canConfigure = false
    /// Whether the core parks sessions (Controls.swift).
    private(set) var canPark = false
    /// Whether the core has the trod bot (DESIGN §4.10).
    private(set) var canTrod = false
    /// The core gives a boost of Gabriel's (DESIGN §4.11).
    private(set) var canBoost = false
    /// The core lists its arch agents (op arch).
    private(set) var canArch = false
    /// The arch agents, as the core last listed them.
    private(set) var archAgents: [ArchAgent] = []
    /// While a boost is on, when it ends.
    private(set) var boostUntil: Date?
    /// Whether the core keeps a history to undo, and what undo and redo
    /// would take next (Undo.swift).
    private(set) var canUndo = false
    private(set) var undoLabel: String?
    private(set) var redoLabel: String?
    /// Where a task runs when its text names no @repo: a repo's name or a
    /// folder's path, or nil for a new scratch folder. Kept between runs.
    private(set) var taskRepo: String? = UserDefaults.standard.string(forKey: "taskRepo")
    /// ui.red_on_done, from the core: done sessions are drawn red. Look keeps
    /// it for the marks; this draws the views again when it changes.
    private(set) var redOnDone = false
    /// ui.helper_style, from the core: a helper beside its session is drawn
    /// as cards ("browser") or as a terminal ("terminal").
    private(set) var helperStyle = "browser"
    /// The helper shown beside each session's conversation, as sub:<id>, by
    /// session (ConversationView, HelperPane.swift).
    private(set) var openHelpers: [String: String] = [:]
    /// spawn.close_when_done, from the core: a finished session closes
    /// itself, so the title bar keeps no To verify count, and the review dock
    /// lists what closed. It is in history too (`irie ctl history`, ⇧⌘T).
    private(set) var closeWhenDone = false
    /// The trod bot, from the core, while it is within its hours; the title
    /// bar shows it (Counts).
    private(set) var trod: TrodShift?
    /// Mullvad VPN, while vpn.on; the title bar shows its mark.
    private(set) var vpn: VPNMark?
    /// Whether the new-task sheet is open (⌘N).
    var newTaskSheet = false
    var notice: Notice?
    /// Every notice shown, oldest first, the newest 100, kept across
    /// launches: the right sidebar's Notices tab lists them, since the one
    /// on show goes by itself (say).
    var notices: [LoggedNotice] = LoggedNotice.saved()
    /// When the Notices tab last showed: the errors since count on its tab.
    var noticesSeen = Date()
    /// Whether the pointer rests on the notice on show, which holds it.
    @ObservationIgnored var noticeHeld = false
    /// Whether a task is starting. The task bar takes the next one
    /// meanwhile, so several may be on their way.
    private(set) var spawning = false
    @ObservationIgnored private var tasksStarting = 0 {
        didSet { spawning = tasksStarting > 0 }
    }
    /// The session whose name its card has open to edit, and the name as
    /// typed so far.
    var renaming: String?
    var renameText = ""
    /// The card a drag holds, and where it would land (Controls.swift).
    var dragging: String?
    var dropMark: DropMark?
    /// Bumped when the terminal should take keyboard focus.
    var focusTerminal = 0
    /// Bumped to move the keyboard to the session list or the find field.
    var focusSidebar = 0
    var focusFind = 0
    /// The sidebar's filter, which the keys that walk the sidebar follow.
    var filter = ""
    private(set) var fontSize: CGFloat = Store.savedFontSize()
    /// The terminal view, to tell whether it has the keyboard.
    @ObservationIgnored weak var terminalView: NSView?
    /// Whether the rows are `--snapshot`'s, which draws the terminal's frame
    /// round an empty terminal, as no core attaches one.
    private(set) var isFixture = false
    /// The right side shows the live terminal instead of the conversation
    /// (View > Terminal, ⌥⌘T); remembered across launches.
    var showTerminal = UserDefaults.standard.bool(forKey: "showTerminal") {
        didSet {
            UserDefaults.standard.set(showTerminal, forKey: "showTerminal")
            if showTerminal { focusTerminal += 1 } else { focusMessage += 1 }
            keepSplit()
        }
    }
    /// The right sidebar: whether it is open, its width and its
    /// tab, kept across launches but never by `--snapshot` (RightSidebar.swift).
    var side = SideState.saved() {
        didSet { if side != oldValue, !isFixture { side.save() } }
    }
    /// The right sidebar is sliding in or out, over the terminal's edge,
    /// and the terminal keeps no room for it meanwhile (RightSidebar.swift).
    var sideSliding = false
    /// Counts its slides, so that only the last one's end counts.
    @ObservationIgnored var sideSlides = 0
    /// The session the core shows beside its teammates, as last asked
    /// (TeamSplit.swift); nil while the slot shows the picked session.
    private(set) var splitShown: String?
    /// Bumped when the conversation's message box should take the keyboard.
    var focusMessage = 0
    /// Whether the message box has the keyboard.
    @ObservationIgnored var messageHasKeys = false
    /// The Claude Code sessions outside Irie, the one selected, the one a
    /// move asks about, and the one moving (OutsideSection.swift).
    var outside: [OutsideSession] = []
    var outsideSelection: String?
    var confirmingMove: OutsideSession?
    var moving: String?
    /// The closed sessions reopen can bring back, the last closed first
    /// (Closing.swift).
    var closed: [ClosedSession] = []
    var outsideProblem = ""
    /// What this computer still lacks of what Irie needs, from the core's
    /// ready op, and whether the core has it (Onboarding.swift).
    var readiness: ReadyReport?
    @ObservationIgnored var canReady = false
    @ObservationIgnored var checkingReady = false
    /// The app starting Irie itself, and why it couldn't (Onboarding.swift).
    /// It tries once a launch, before it has ever reached a core.
    var starting = false
    var startProblem: StartProblem?
    @ObservationIgnored private var everOnline = false
    @ObservationIgnored private var triedStart = false
    /// The walk through a first task, and a bump that gives the task bar
    /// the keyboard (Onboarding.swift).
    var coach: Coach?
    var focusTaskBar = 0
    /// The session the core is to show next, and whether a request is out.
    @ObservationIgnored private var wanted: (id: String, focus: Bool)?
    @ObservationIgnored private var showing = false

    static let defaultFontSize: CGFloat = 13

    struct Notice: Equatable, Identifiable {
        let id = UUID()
        var text: String
        var isError: Bool
        /// It offers Undo, which takes back the step it reports.
        var undo = false
        /// It reports a task added to the one before, and offers New task:
        /// this input, which takes it back and starts it apart.
        var apart: InputArgs?
    }

    let client: CoreClient
    /// The core's config, as the Settings window shows and edits it.
    let settings: SettingsModel
    /// What the menu bar shows, which changes far less often than the
    /// store does (MenuBar.swift).
    let menu = MenuState()
    @ObservationIgnored private var started = false

    init(socketPath: String = CorePaths.socket()) {
        client = CoreClient(socketPath: socketPath)
        settings = SettingsModel(client: client)
        settings.changed = { [weak self] in self?.loadChoices() }
        watchMenu()
    }

    var socketPath: String { client.socketPath }

    // MARK: Connection

    func start() {
        guard !started else { return }
        started = true
        if let seconds = FreezeDog.restartedAfter {
            say("Irie stopped answering for \(seconds) s, so ❤️‍🩹 Raffi restarted it. Every agent kept running.",
                error: false, seconds: 10)
        }
        // Behind another app, the window takes in the core's news less
        // often (Pace.swift).
        Pace.shared.set(active: NSApplication.shared.isActive)
        let nc = NotificationCenter.default
        nc.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in Pace.shared.set(active: true) }
        nc.addObserver(forName: NSApplication.didResignActiveNotification, object: nil, queue: .main) { _ in Pace.shared.set(active: false) }
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
                everOnline = true
                starting = false
                startProblem = nil
                canReady = hello.features.contains("ready")
                checkReady()
                // A core that restarted asks again.
                splitShown = nil
                canPickModel = hello.features.contains("spawn-model")
                canTutor = hello.features.contains("spawn-tutor")
                canLearn = hello.features.contains("learn")
                canDock = hello.features.contains("dock")
                canConfigure = hello.features.contains("config")
                canPark = hello.features.contains("park")
                canTrod = hello.features.contains("trod")
                canBoost = hello.features.contains("gabriel")
                canArch = hello.features.contains("arch")
                canUndo = hello.features.contains("undo")
                if hello.features.contains("theme") { sendTheme(Theme.palette.id) }
                loadChoices()
                // The store lives as long as the app. The frames go in a
                // batch at a time (Pace.swift), and wait while a menu is
                // open (take(_:)).
                let batcher = Batcher<Frame> { [weak self] frames in self?.take(frames) }
                try await client.subscribe { frame in batcher.add(frame) }
                link = .offline("The core went away.")
            } catch {
                link = .offline(Self.describe(error))
                // Opened from the Finder, the Dock or Spotlight, with no
                // Irie running: the app starts it (Onboarding.swift).
                if !everOnline, !triedStart, Self.notRunning(error) {
                    triedStart = true
                    await startIrie()
                    continue
                }
            }
            try? await Task.sleep(nanoseconds: 1_500_000_000)
        }
    }

    /// Tells the core the palette in use: the agents' colours on Irie's
    /// status line follow it, so they match the cards.
    func sendTheme(_ id: String) {
        // A preview leaves that to the installed app (Preview.swift).
        guard !Preview.isOn else { return }
        Task { _ = try? await client.call("theme", ThemeArgs(theme: id), timeout: 2, as: Empty.self) }
    }

    private static func describe(_ error: Error) -> String {
        if notRunning(error) {
            return "Irie isn’t running."
        }
        return error.localizedDescription
    }

    private static func notRunning(_ error: Error) -> Bool {
        if case let CoreError.system(call, code) = error, call == "connect", code == ENOENT || code == ECONNREFUSED {
            return true
        }
        return false
    }

    /// Frames that came while the main run loop tracked a menu or a drag,
    /// in order (take(_:)).
    @ObservationIgnored private var held: [Frame] = []

    /// Takes in a batch of frames (Pace.swift), or holds it while the main
    /// run loop tracks: an open menu (the menu bar's, a card's ⋯ or
    /// right-click menu, the task bar's repo menu), a drag or a live resize.
    /// A change to the store then would draw the views again under the
    /// pointer, and SwiftUI would rebuild the open menu, which loses the
    /// item under the pointer or closes. The held frames go in as soon as
    /// the tracking ends.
    private func take(_ frames: [Frame]) {
        if RunLoop.current.currentMode == .eventTracking {
            let first = held.isEmpty
            held.append(contentsOf: frames)
            if first {
                RunLoop.main.perform(inModes: [.default]) { [weak self] in
                    MainActor.assumeIsolated { self?.release() }
                }
            }
            return
        }
        let all = held + frames
        held = []
        apply(all)
    }

    private func release() {
        guard !held.isEmpty else { return }
        let frames = held
        held = []
        apply(frames)
    }

    /// Applies a batch of frames (take(_:)), in order. Every
    /// view that reads the store draws again whenever one of its published
    /// values is set, even to what it was, so nothing is set unless it
    /// changed: an upsert of a row the app already has, such as one whose
    /// only change is a figure the app doesn't read, or a snapshot of rows
    /// it has, changes nothing (docs/proposals/optimisation.md §2.1).
    private func apply(_ frames: [Frame]) {
        var rows = sessions
        var touched = false
        for f in frames {
            if let h = f.header { applyHeader(h) }
            switch f.t {
            case "snapshot":
                rows = (f.sessions ?? []).sorted(by: Self.before)
                touched = true
            case "upsert":
                guard let s = f.session else { continue }
                if let i = rows.firstIndex(where: { $0.id == s.id }) {
                    guard rows[i] != s else { continue }
                    let moved = rows[i].order != s.order
                    rows[i] = s
                    if moved { rows.sort(by: Self.before) }
                } else {
                    rows.append(s)
                    rows.sort(by: Self.before)
                }
                noteCoach(s)
                touched = true
            case "remove":
                guard let id = f.id else { continue }
                rows.removeAll { $0.id == id }
                if coach?.session == id { coach = nil }
                touched = true
            case "outside":
                applyOutside(f)
            case "closed":
                let c = f.closed ?? []
                if c != closed { closed = c }
            case "history":
                let undo = f.undo.flatMap { $0.isEmpty ? nil : $0 }
                let redo = f.redo.flatMap { $0.isEmpty ? nil : $0 }
                if undo != undoLabel { undoLabel = undo }
                if redo != redoLabel { redoLabel = redo }
            default:
                continue
            }
        }
        guard touched else { return }
        if rows != sessions { sessions = rows }
        if selection == nil, let shown = sessions.first(where: { $0.shown == true }) {
            selection = shown.id
        }
        if let sel = selection, !sessions.contains(where: { $0.id == sel }) {
            selection = nil
        }
        keepSplit()
    }

    /// Takes a header, a snapshot's or a header frame's: each is the whole
    /// header, so one without the bot is one out of its hours.
    private func applyHeader(_ h: Header) {
        // While the machine is busy, the window takes in the core's news
        // less often (Pace.swift).
        Pace.shared.set(busy: h.load?.busy ?? false)
        if (h.redOnDone ?? false) != Look.redOnDone {
            Look.redOnDone = h.redOnDone ?? false
            redOnDone = Look.redOnDone
        }
        if (h.closeWhenDone ?? false) != closeWhenDone {
            closeWhenDone = h.closeWhenDone ?? false
        }
        if let s = h.helperStyle, s != helperStyle {
            helperStyle = s
        }
        if h.trod != trod {
            trod = h.trod
        }
        if h.vpn != vpn {
            vpn = h.vpn
        }
    }

    /// The picked session while it shows beside its teammates: the terminal
    /// shows, and it has teammates the app can reach.
    var splitSession: Session? {
        guard showTerminal, outsideSelection == nil, attach?.teams != nil || isFixture,
              let s = selectedRow, !(s.team ?? []).isEmpty else { return nil }
        return s
    }

    /// Works out what views read instead of the rows, and sets each only
    /// when it changed: a set is news to every view that read it.
    private func derive() {
        let row = selection.flatMap { id in sessions.first { $0.id == id } }
        if row != selectedRow { selectedRow = row }
        let c = RowCounts(sessions)
        if c != counts { counts = c }
        if hasRows == sessions.isEmpty { hasRows = !sessions.isEmpty }
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

    /// The + words that set a tutor mode rather than name an agent
    /// (docs/proposals/learning.md).
    static let tutorWords: Set<String> = ["explain", "teach"]

    /// Asks the core for the repos the @ words know and the agents the +
    /// words name. A core that answers neither leaves the menus as they are.
    func loadChoices() {
        Task {
            if let r = try? await client.call("complete", CompleteArgs(text: "@"), timeout: 5, as: CompleteResult.self) {
                repoNames = r.items.map { String($0.dropFirst()) }
            }
            if let r = try? await client.call("complete", CompleteArgs(text: "+"), timeout: 5, as: CompleteResult.self) {
                // The tutor words are not agents: the sheet has its own menu for them.
                agents = r.items.enumerated().compactMap { i, item in
                    let name = String(item.dropFirst())
                    if Self.tutorWords.contains(name) { return nil }
                    return Agent(name: name, note: r.notes.flatMap { i < $0.count ? $0[i] : nil } ?? "")
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
        canTutor = true
        canLearn = true
        canConfigure = true
        canPark = true
        canTrod = true
        canBoost = true
        canArch = true
        archAgents = [
            ArchAgent(name: "mikael", icon: "⚔️🛡️", title: "a helper against evil",
                      about: "Mikael, the first of Irie's arch agents, stands with the user against evil and for the good they want to see in the world.",
                      on: true, status: "With every Claude Code session, weighing the calls that can’t be taken back.",
                      actions: [ArchAction(label: "Talk with Mikael", op: "spawn")],
                      settings: [ArchSetting(key: "arch.mikael.on", kind: "bool", label: "Mikael goes with every session", value: .bool(true)),
                                 ArchSetting(key: "arch.mikael.good", kind: "lines", label: "The good you want to see in the world",
                                             value: .array([.string("Give credit where it’s due.")]))]),
            ArchAgent(name: "gabriel", icon: "🪽", title: "carries your focus to the machine",
                      about: "Gabriel carries the user's focus to the machine.",
                      on: true, status: "Holding back 3 sessions and 1 app for Safari.",
                      actions: [ArchAction(label: "Boost for 1 hour", op: "gabriel")],
                      settings: [ArchSetting(key: "arch.gabriel.on", kind: "bool", label: "Gabriel gives your focus the machine first", value: .bool(true)),
                                 ArchSetting(key: "arch.gabriel.away", kind: "duration", label: "You’re away after", value: .string("5m"))]),
            ArchAgent(name: "raphael", nickname: "Raffi", icon: "❤️‍🩹", title: "heals Irie and your Mac",
                      about: "Raphael keeps Irie and the computer it runs on well.",
                      on: true, status: "Looked at 14:05: 2 things need care, and 1 is worth a look.",
                      actions: [ArchAction(label: "Look over my Mac now", op: "raphael")],
                      items: [ArchItem(text: "Your disk is nearly full: 9.9 GB free of 995 GB (1%).",
                                       detail: "macOS needs room for updates and swap, and slows down without it. Clear the caches below, and empty the Trash.", level: "warn"),
                              ArchItem(text: "Go’s build cache takes 13.8 GB (~/Library/Caches/go-build).",
                                       detail: "It is safe to clear: Go builds what it needs again, so the next builds and tests take longer.", level: "warn",
                                       actions: [ArchAction(label: "Clear Go’s build cache", op: "raphael",
                                                            ask: "Raffi will run:\n    go clean -cache\n\nFrees about 13.8 GB. Raffi waits until no Go build or test runs, so none of them breaks.")]),
                              ArchItem(text: "com.example.helper is worth a look: it starts ~/.local/bin/helper as you log in.",
                                       detail: "If you know it, mark it as yours and Raffi no longer flags it.\nIts program is signed by no one in particular (an ad-hoc signature).\nNot sure: many good programs show these signs too.", level: "note",
                                       actions: [ArchAction(label: "It’s mine", op: "raphael")])],
                      settings: [ArchSetting(key: "arch.raphael.on", kind: "bool", label: "Raffi restarts what stops answering, and looks over your Mac", value: .bool(true)),
                                 ArchSetting(key: "ui.freeze_dog", kind: "duration", label: "Restart the Mac app when it has not answered for", value: .string("10s"))]),
        ]
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
    /// group, then a group for each paired machine's sessions, in the order
    /// the core lists them, so they stand apart from this Mac's (DESIGN
    /// §2.8); parked, one goes into Parked with the rest. Within a group the
    /// sessions keep the list's one order, which only the user changes, so a
    /// state change never moves a card (DESIGN §4.1).
    var drawnGroups: [SessionGroup] {
        let f = filter.trimmingCharacters(in: .whitespaces).lowercased()
        let rows = f.isEmpty ? sessions : sessions.filter { $0.name.lowercased().contains(f) }
        let parked = rows.filter(\.isParked)
        let here = rows.filter { !$0.isParked && $0.peerName == nil }
        var hosts: [String] = [], byHost: [String: [Session]] = [:]
        for s in rows where !s.isParked {
            guard let h = s.peerName else { continue }
            if byHost[h] == nil { hosts.append(h) }
            byHost[h, default: []].append(s)
        }
        var out: [SessionGroup] = []
        switch mode {
        case .list:
            if !here.isEmpty {
                out.append(SessionGroup(id: SessionGroup.list, title: "Sessions", path: nil, sessions: here))
            }
        case .repo:
            var order: [String] = [], byRepo: [String: [Session]] = [:], none: [Session] = []
            for s in here {
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
        for h in hosts {
            out.append(SessionGroup(id: SessionGroup.hostID(h), title: "On \(h)", path: nil, sessions: byHost[h] ?? [], host: h))
        }
        return out
    }

    /// The id of the group a session is drawn in: Parked while it is
    /// parked, else its machine's group when another machine runs it, else
    /// the list or its repo's group.
    func groupID(of s: Session) -> String {
        if s.isParked { return SessionGroup.parked }
        if let h = s.peerName { return SessionGroup.hostID(h) }
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

    /// Shows a helper in the right sidebar's Helpers tab, beside the
    /// conversation or the terminal, whichever shows. The terminal never
    /// splits for a helper, which has no terminal of its own.
    func openHelper(_ agent: String, in session: String) {
        if selection != session || outsideSelection != nil { show(session) }
        openHelpers[session] = agent
        if !side.open || side.tab != .helpers { openSide(.helpers) }
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
    @ObservationIgnored private var lastHelpers: [String: String] = [:]
    @ObservationIgnored private var newestHelpers: [String: String] = [:]

    /// Notes the newest helper a session's conversation names.
    func noteHelper(_ agent: String, in session: String) {
        newestHelpers[session] = agent
    }

    /// Whether the right sidebar shows the selected session's helpers.
    var helperShown: Bool {
        guard selection != nil, outsideSelection == nil else { return false }
        return side.open && side.tab == .helpers
    }

    /// View > Show Helpers (⇧⌘H): opens the right sidebar's Helpers tab on
    /// the selected session's helper (the one shown last, else the newest
    /// running, else the newest), or closes it.
    func toggleHelpers() {
        guard let id = selection, outsideSelection == nil, let s = sessions.first(where: { $0.id == id }) else { return }
        if helperShown {
            closeSide()
            return
        }
        if let a = openHelpers[id] ?? lastHelpers[id] ?? s.subagents?.last?.agent ?? newestHelpers[id] {
            openHelper(a, in: id)
        } else {
            openSide(.helpers)
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
        // Each set is news to every view that read it, even to the same
        // value, so only what changes is set.
        if selection != id { selection = id }
        if outsideSelection != nil { outsideSelection = nil }
        wanted = (id, focus)
        guard !showing else { return }
        showing = true
        Task {
            while let next = wanted {
                wanted = nil
                do {
                    let split = splitSession?.id == next.id
                    let shown = split ? next.id : nil
                    if splitShown != shown { splitShown = shown }
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

    /// Sends what was typed in the task bar to the core, which reads it: a
    /// task with its @ and + words, a question such as "hows the parser?",
    /// or a : command (docs/CORE.md). With no @repo in its text a task runs
    /// where the bar's menu says. Returns whether the core took it.
    func input(_ text: String) async -> Bool {
        await input(text, repo: taskRepo, worktree: true, agent: nil, model: nil, tutor: nil)
    }

    /// Sends text from the bar or the new-task sheet. The @repo, +agent and
    /// tutor words in it win over the menus' repo, agent and tutor mode.
    func input(_ text: String, repo: String?, worktree: Bool, agent: String?, model: String?, tutor: String?) async -> Bool {
        var args = InputArgs(text: text)
        // A : command acts on the selected card, or :move on the selected
        // row outside Irie.
        if let o = selectedOutside {
            args.outside = o.vendorId
        } else {
            args.session = selection
        }
        if let repo {
            args.repo = repo
            args.inPlace = worktree ? nil : true
        }
        args.profile = agent
        if canPickModel { args.model = model }
        if canTutor { args.tutor = tutor }
        return await send(args)
    }

    /// What the Help menu shows: the resources, with the shown session's
    /// language first, and the glossary. Nil without a core that serves it.
    func learn() async -> LearnResult? {
        guard canLearn else { return nil }
        return try? await client.call("learn", LearnArgs(session: selection), timeout: 5, as: LearnResult.self)
    }

    /// Takes a task added to the one before back and starts it as a task
    /// of its own (the notice's New task).
    func startApart(_ args: InputArgs) {
        dismissNotice()
        Task { _ = await send(args) }
    }

    private func send(_ args: InputArgs) async -> Bool {
        tasksStarting += 1
        defer { tasksStarting -= 1 }
        do {
            // A task in a repo may make a worktree and check out submodules
            // first; one added to a task still starting waits for it.
            let r = try await client.call("input", args, timeout: 600, as: InputResult.self)
            switch r.did {
            case "asked":
                if let c = r.choice, let op = r.op { await answer(op, c) }
            case "answered":
                say(r.text, error: false, seconds: 8)
            case "added":
                // It went to the agent of the task just before, which gets
                // it once the delay is over; until then New task takes it
                // back and starts it apart.
                var apart: InputArgs?
                if let id = r.session?.id {
                    apart = args
                    apart?.apart = id
                }
                let left = max(3, (r.until?.timeIntervalSinceNow ?? 0).rounded(.up))
                say(r.text, error: false, seconds: UInt64(left), apart: apart)
            default:
                say(r.text, error: false, seconds: args.apart == nil ? 3 : 6)
            }
            if let s = r.session {
                if r.did == "spawned" {
                    if coach != nil, coach?.session == nil { coach?.session = s.id }
                    // It starts behind the session on show, so a task typed
                    // mid-work never pulls the view or the keyboard away.
                    // With nothing on show, it comes straight up, ready to
                    // type to.
                    if selection == nil { show(s.id, focus: true) }
                    // A repo it ran in is now among the recent ones.
                    loadChoices()
                } else if r.show == true || r.op == "move" {
                    show(s.id, focus: true)
                }
            }
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

    /// Starts a run of the trod bot by hand, which keeps the sessions
    /// going at once, while you watch, until none has work left or the
    /// plan is as full as it may get; or stops it (DESIGN §4.10).
    func setTrod(_ on: Bool) {
        Task {
            do {
                let r = try await client.call("trod", on ? TrodArgs(start: true) : TrodArgs(stop: true), as: TrodResult.self)
                switch (on, r.hand) {
                case (true, let h?) where h.on:
                    say("The trod bot is keeping your sessions going until none has work left. Watch for its boot print.", error: false, seconds: 6)
                case (true, let h?):
                    say("The trod bot stopped at once: \(h.why ?? "nothing was left to do").", error: false, seconds: 6)
                default:
                    say("The trod bot stopped.", error: false)
                }
            } catch {
                say("Couldn’t \(on ? "start" : "stop") the trod bot: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Reads the arch agents again (op arch).
    func loadArch() async {
        guard canArch, case .online = link else { return }
        if let r = try? await client.call("arch", JSONValue.object([:]), timeout: 5, as: ArchResult.self) {
            archAgents = r.agents
        }
    }

    /// Does what an arch agent offers: calls its op with its args as they
    /// came, says so, and reads the arch agents again. ok says you have
    /// read what an action that asks first does, and said OK to it: only
    /// then does one with ask run.
    func archAction(_ a: ArchAction, ok: Bool = false) {
        if a.ask != nil && !ok { return }
        Task {
            do {
                _ = try await client.call(a.op, ok ? a.argsOK : a.args ?? JSONValue.object([:]), timeout: 20, as: JSONValue.self)
                say(a.done ?? "\(a.label): done.", error: false, seconds: 5)
            } catch {
                say("Couldn’t \(a.label.lowercased()): \(error.localizedDescription)", error: true)
            }
            await loadArch()
        }
    }

    /// Sets one of an arch agent's settings: shown at once, sent to the
    /// config op by its key.
    func setArch(_ agent: String, _ key: String, _ value: JSONValue) {
        if let i = archAgents.firstIndex(where: { $0.name == agent }),
           let j = archAgents[i].settings?.firstIndex(where: { $0.key == key }) {
            archAgents[i].settings?[j].value = value
            if key == "arch.\(agent).on", let on = value.bool { archAgents[i].on = on }
        }
        settings.set(key, value.setting)
        Task {
            try? await Task.sleep(nanoseconds: 500_000_000)
            await loadArch()
        }
    }

    /// Puts one of an arch agent's settings back to its default.
    func resetArch(_ key: String) {
        settings.reset(key)
        Task {
            try? await Task.sleep(nanoseconds: 500_000_000)
            await loadArch()
        }
    }

    /// Whether a boost is on now.
    var boosting: Bool { (boostUntil ?? .distantPast) > Date() }

    /// Starts a boost of Gabriel's, for arch.gabriel.boost: the focus gets
    /// the machine first whether it is busy or not, and idle local models
    /// let their memory go; or stops it (DESIGN §4.11).
    func setBoost(_ on: Bool) {
        Task {
            do {
                let r = try await client.call("gabriel", on ? GabrielArgs(boost: true) : GabrielArgs(stop: true), as: GabrielResult.self)
                boostUntil = r.boostUntil
                if let until = r.boostUntil {
                    var text = "Boosting your focus until \(until.formatted(date: .omitted, time: .shortened)): the rest runs on the efficiency cores."
                    if let freed = r.freed, !freed.isEmpty { text += " Freed \(freed.joined(separator: ", "))." }
                    if r.lowPower == true { text += " Low Power Mode is on: turn it off in System Settings > Battery for full speed." }
                    say(text, error: false, seconds: 6)
                } else {
                    say("The boost is over.", error: false)
                }
            } catch {
                say("Couldn’t \(on ? "start" : "stop") the boost: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Shows a notice at the top of the working area (NoticeToast), never
    /// over the task bar, has VoiceOver read it, and keeps it in the right
    /// sidebar's Notices tab. It goes by itself: a confirmation after
    /// `seconds` (3 s), an error after 8 s at least, and neither while the
    /// pointer rests on it. Its × closes it at once, and an error goes as
    /// the user edits or submits too. The tab keeps each one, so nothing is
    /// lost as it goes (WCAG 2.2.1).
    func say(_ text: String, error: Bool, seconds: UInt64 = 3, undo: Bool = false, apart: InputArgs? = nil) {
        let n = Notice(text: text, isError: error, undo: undo && canUndo, apart: apart)
        notice = n
        noticeHeld = false
        log(n)
        AccessibilityNotification.Announcement(text).post()
        let wait = error ? max(seconds, Self.errorSeconds) : seconds
        Task {
            try? await Task.sleep(nanoseconds: wait * 1_000_000_000)
            // The pointer on it holds it, and it goes a moment after the
            // pointer leaves.
            while notice?.id == n.id, noticeHeld {
                while notice?.id == n.id, noticeHeld { try? await Task.sleep(nanoseconds: 250_000_000) }
                try? await Task.sleep(nanoseconds: 1_500_000_000)
            }
            if notice?.id == n.id { notice = nil }
        }
    }

    /// The least time an error shows, in seconds.
    static let errorSeconds: UInt64 = 8

    func dismissNotice() {
        notice = nil
        noticeHeld = false
    }

    /// Empties the Notices tab.
    func clearNotices() {
        notices = []
        if !isFixture { LoggedNotice.save([]) }
    }

    /// The Notices tab shows, so what it lists has been seen.
    func sawNotices() { noticesSeen = Date() }

    /// The errors since the Notices tab last showed, which its tab counts
    /// in red.
    var unseenErrors: Int { notices.filter { $0.isError && $0.at > noticesSeen }.count }

    private func log(_ n: Notice) {
        notices.append(LoggedNotice(id: n.id, text: n.text, isError: n.isError))
        if notices.count > LoggedNotice.kept { notices.removeFirst(notices.count - LoggedNotice.kept) }
        if !isFixture { LoggedNotice.save(notices) }
        if side.open && side.tab == .notices { sawNotices() }
    }

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

/// The title bar's counts (SidebarView's Counts): for each state it counts,
/// how many sessions are in it and the first of them in list order. A
/// parked session is out of the way, so To verify leaves it out. Working
/// counts the finished turns whose helpers or teammates still work: they
/// are not to verify yet.
struct RowCounts: Equatable {
    struct Tally: Equatable {
        var count = 0
        var first: String?

        mutating func add(_ s: Session) {
            count += 1
            if first == nil { first = s.id }
        }
    }

    var needs = Tally(), working = Tally(), done = Tally(), paused = Tally(), frozen = Tally()

    init() {}

    /// The rows in list order.
    init(_ rows: [Session]) {
        for s in rows {
            switch Look(s) {
            case .failed, .needs, .stalled: needs.add(s)
            case .working, .waiting: working.add(s)
            case .done where !s.isParked: done.add(s)
            case .paused, .pausing: paused.add(s)
            case .frozen: frozen.add(s)
            default: break
            }
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

/// One group of the sidebar: the list, a repo, the sessions in no repo, the
/// parked ones, or a paired machine's.
struct SessionGroup: Identifiable, Equatable {
    let id: String
    let title: String
    /// The repo's main checkout, for a repo's group.
    let path: String?
    let sessions: [Session]
    /// The paired machine whose sessions it holds, for a machine's group.
    var host: String? = nil

    /// The List view's one group, which has no header, and the Parked group.
    static let list = "list"
    static let parked = "parked"

    /// A paired machine's group.
    static func hostID(_ name: String) -> String { "host:" + name }

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
