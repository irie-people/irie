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
    /// The cards picked with ⌘ or ⇧ to act on together (Tabs.swift); empty
    /// while one card is simply selected.
    var picked: Set<String> = []
    /// The card picked last, where a ⇧-click's range starts.
    @ObservationIgnored var pickAnchor: String?
    /// The card under the pointer, which a middle click closes.
    @ObservationIgnored var hovered: String?
    /// Helpers and teammates hidden from their cards, by session: each
    /// keeps working (Tabs.swift).
    var hiddenSubs: [String: Set<String>] = [:]
    /// The spaces, as the core's header lists them, and the one on show,
    /// by its row key: "" for the first (Spaces.swift).
    var spaces: [SpaceInfo] = []
    var space = UserDefaults.standard.string(forKey: "space") ?? ""
    /// The space whose name is open to edit.
    var renamingSpace: String?
    /// Your groups, as the core's header lists them, and the one whose name
    /// is open to edit (Groups.swift).
    var groups: [GroupInfo] = []
    var renamingGroup: String?
    /// The card shown last in each space, which showing it again shows.
    @ObservationIgnored var lastShown: [String: String] = [:]
    /// The repos the @ words know and the agents the + words name, with what
    /// each runs: what the task bar's menu and the new-task sheet offer.
    private(set) var repoNames: [String] = []
    private(set) var agents: [Agent] = []
    /// Whether the core takes a model for one task.
    private(set) var canPickModel = false
    /// Whether the core sends a task to the top, first in Pinned and
    /// boosted (spawn-top): the task bar's Send to Top.
    private(set) var canSendToTop = false
    /// The core takes a tutor mode for one task, and serves the Help menu
    /// (docs/proposals/learning.md).
    private(set) var canTutor = false
    private(set) var canLearn = false
    /// The core makes materialized cards (docs/proposals/materialized-cards.md).
    private(set) var canMaterialize = false
    /// Each session's card of where it stands, as the core last gave it.
    var sessionCards: [String: MaterializeResult] = [:]
    /// The core runs the right sidebar's Dock (op dock, Dock.swift).
    private(set) var canDock = false
    /// The Dock controls the user stopped, by session, which the Dock then
    /// leaves stopped until the user starts them again.
    @ObservationIgnored var dockStopped: [String: Set<String>] = [:]
    /// The Learn window's page (Learn.swift).
    var learnTab = LearnTab.resources
    /// Whether the core reads and edits its config for the Settings window.
    private(set) var canConfigure = false
    /// Whether the core sends an agent on with "continue" (op continue), for
    /// a double ↓ on its empty prompt (ContinueKey.swift).
    private(set) var canContinue = false
    /// Whether the core parks sessions (Controls.swift).
    private(set) var canPark = false
    /// Whether the core pins sessions (Tabs.swift).
    private(set) var canPin = false
    /// Whether the core keeps your groups (Groups.swift).
    private(set) var canGroup = false
    /// Whether the core has the trod bot (DESIGN §4.10).
    private(set) var canTrod = false
    /// The core gives a boost of Gabriel's (DESIGN §4.11).
    private(set) var canBoost = false
    /// The core boosts always too (arch.gabriel.always), and its header
    /// says how Gabriel boosts.
    private(set) var canBoostAlways = false
    /// The core lists its arch agents (op arch).
    private(set) var canArch = false
    /// The core serves the plugins op (DESIGN §4.17).
    private(set) var canPlugins = false
    /// The core serves the helpers op: your helpers, which the right
    /// sidebar's Helpers tab shows (YourHelpers.swift).
    private(set) var canHelpers = false
    /// Your helpers as the core last said they stand; empty from a core
    /// from before them.
    var ownHelpers: [HelperInfo] = []
    /// The core serves the sizes op: it keeps each agent's window at the
    /// size its session's terminal shows at (PaneSizes.swift); what it last
    /// heard, and the send on its way.
    @ObservationIgnored var canSizes = false
    @ObservationIgnored var sizesSent: SizesArgs?
    @ObservationIgnored var sizing: Task<Void, Never>?
    /// The core serves the vpn op: its build has the privacy plugin
    /// (DESIGN §4.17), whose VPN items in the Plugins menu show only then.
    private(set) var canVPN = false
    /// The core serves the variorum op: its build has the Writing plugin,
    /// whose Variorum tab shows only then (VariorumTab.swift).
    private(set) var canVariorum = false
    /// The core's hello is in, so the features above are its own.
    private(set) var knowsFeatures = false
    /// The Variorum tab's state, kept while the app runs.
    let variorum = VariorumModel()
    /// The core serves the mail op: its build has the mail plugin
    /// (docs/proposals/mail.md), whose Mail tab and window show only then.
    private(set) var canMail = false
    /// Irie's plugins as the core says they stand, for the Plugins menu
    /// (Plugins.swift); empty from a core from before them.
    var plugins: [PluginInfo] = [] {
        // The Variorum tab shows only while the Writing plugin can answer it.
        didSet { if Self.writing(plugins) != Self.writing(oldValue) { variorum.check() } }
    }
    /// Your bundles (PluginPanes.swift); empty from a core from before them.
    var bundles: [BundleInfo] = []
    /// Reads the plugins once a minute (keepPlugins).
    @ObservationIgnored var pluginsTimer: Task<Void, Never>?
    /// The arch agents, as the core last listed them.
    private(set) var archAgents: [ArchAgent] = []
    /// While a boost is on, when it ends.
    private(set) var boostUntil: Date?
    /// Gabriel boosts always while you're at the computer
    /// (arch.gabriel.always), as the core's header says.
    private(set) var boostAlways = false
    /// Whether the core keeps a history to undo, and what undo and redo
    /// would take next (Undo.swift).
    private(set) var canUndo = false
    private(set) var undoLabel: String?
    private(set) var redoLabel: String?
    /// The core asks before an undo or redo when told to (undo-confirm), so
    /// ⌘Z takes only the step its question named; and that question is on
    /// its way or on show, so another key waits for it.
    private(set) var canConfirmUndo = false
    @ObservationIgnored var undoAsking = false
    /// Where a task runs when its text names no @repo: a repo's name or a
    /// folder's path, or nil for a new scratch folder. Kept between runs.
    private(set) var taskRepo: String? = UserDefaults.standard.string(forKey: "taskRepo")
    /// The task bar's Send to Top, armed: the next task sent goes to the
    /// top, first in Pinned and boosted (DESIGN §4.1), and disarms it
    /// (sendTop). Kept between runs, as an install relaunches the app.
    private(set) var sendToTop = UserDefaults.standard.bool(forKey: "sendToTop")
    /// ui.red_on_done, from the core: done sessions are drawn red. Look keeps
    /// it for the marks; this draws the views again when it changes.
    private(set) var redOnDone = false
    /// ui.helper_style, from the core: a helper beside its session is drawn
    /// as cards ("browser") or as a terminal ("terminal").
    private(set) var helperStyle = "browser"
    /// ui.terminal_links, from the core: where a link clicked in a terminal
    /// opens, "rolodex" (the session's panes) or "browser" (the Mac's own
    /// apps). The core applies it (the link op); Settings shows it while it
    /// can't read the config.
    private(set) var terminalLinks = "rolodex"
    /// Whether the core says where a terminal's link opens (op link).
    private(set) var canLink = false
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
    /// The card a drag holds, with every card picked with it, or the group
    /// of yours it holds by its header, and where the hold would land
    /// (Drops.swift).
    var dragging: String?
    var draggingGroup: String?
    var heldCards: Set<String> = []
    var drop: DropModel.Landing?
    /// Where the pointer was as the drag began, on the screen; the rows'
    /// frames in the list; the spring that opens a folded group under the
    /// pointer; and which drag this is, so an old drag's end lets go of
    /// nothing newer.
    @ObservationIgnored var dragStart: NSPoint?
    @ObservationIgnored var dropFrames: [String: CGRect] = [:]
    @ObservationIgnored var spring: Task<Void, Never>?
    @ObservationIgnored var dragGen = 0
    /// Bumped when the terminal should take keyboard focus, which brings it
    /// over the panes where they overlap (Rolodex.swift).
    var focusTerminal = 0 {
        didSet { rolodex.setRaised(false) }
    }
    /// Bumped to move the keyboard to the session list or the find field.
    var focusSidebar = 0
    var focusFind = 0
    /// The sidebar's filter, which the keys that walk the sidebar follow.
    var filter = ""
    /// The terminal's text size on show: the window's, or zen's own while
    /// zen is on (Zen.swift).
    private(set) var fontSize: CGFloat = Store.savedFontSize()
    /// The window's own text size, kept while zen shows its own.
    private(set) var windowFontSize: CGFloat = Store.savedFontSize()
    /// The terminal view, to tell whether it has the keyboard.
    @ObservationIgnored weak var terminalView: NSView? {
        // The sizes wait for it (PaneSizes.swift).
        didSet { if terminalView !== oldValue { sizesSoon() } }
    }
    /// The split's lead pane, the picked session's own terminal while it
    /// shows beside its teammates (TeamSplit.swift).
    @ObservationIgnored weak var leadView: NSView?
    /// Whether the rows are `--snapshot`'s, which draws the terminal's frame
    /// round an empty terminal, as no core attaches one.
    private(set) var isFixture = false
    /// The right side shows the live terminal instead of the conversation
    /// (View › Show Terminal, ⌥⌘T); remembered across launches.
    var showTerminal = UserDefaults.standard.bool(forKey: "showTerminal") {
        didSet {
            UserDefaults.standard.set(showTerminal, forKey: "showTerminal")
            if showTerminal { focusTerminal += 1 } else { focusMessage += 1 }
            keepSplit()
        }
    }
    /// The layout reversed, the window as in a mirror: the sidebar on the
    /// right, the terminal beside it, and the panes and the right sidebar
    /// on the left (Fn+X Fn+L Fn+R, View › Layout › Reverse; KeyTree.swift).
    /// Text still reads left to right. Remembered across launches; zen
    /// keeps its own layout, so it never reverses.
    var reversed = UserDefaults.standard.bool(forKey: Store.reversedKey) {
        didSet { if reversed != oldValue, !isFixture { UserDefaults.standard.set(reversed, forKey: Store.reversedKey) } }
    }
    static let reversedKey = "layoutReversed"
    /// The window's parts run right to left: the layout is reversed and zen,
    /// which has a layout of its own, is off.
    var mirrored: Bool { reversed && !zen.on }
    /// The right sidebar: whether it is open, its width and its
    /// tab, kept across launches but never by `--snapshot` (RightSidebar.swift).
    var side = SideState.saved() {
        didSet { if side != oldValue, !isFixture { side.save() } }
    }
    /// The Ghostty window the terminal is popped out into, while it is out,
    /// and a pop out on its way (PopOut.swift).
    var poppedOut = PopOut.saved()
    @ObservationIgnored var popping = false
    /// The right sidebar is sliding in or out, over the terminal's edge,
    /// and the terminal keeps no room for it meanwhile (RightSidebar.swift).
    var sideSliding = false
    /// Counts its slides, so that only the last one's end counts.
    @ObservationIgnored var sideSlides = 0
    /// The panes beside the terminal, a rolodex of cards (Rolodex.swift).
    let rolodex = RolodexModel()
    /// The session the core shows beside its teammates, as last asked
    /// (TeamSplit.swift); nil while the slot shows the picked session.
    private(set) var splitShown: String?
    /// Bumped when the conversation's message box should take the keyboard,
    /// which brings the conversation over the panes where they overlap.
    var focusMessage = 0 {
        didSet { rolodex.setRaised(false) }
    }
    /// Whether the message box has the keyboard.
    @ObservationIgnored var messageHasKeys = false
    /// The message box's own field while it has the keyboard. The window
    /// has one field editor for all its fields, so the field it edits tells
    /// the box from the find field and the task bar (ContinueKey.swift).
    @ObservationIgnored weak var messageField: NSTextField?
    /// The Claude Code sessions outside Irie, the one selected, the one a
    /// move asks about, and the one moving (OutsideSection.swift).
    var outside: [OutsideSession] = []
    var outsideSelection: String? {
        didSet { if outsideSelection != oldValue { rolodex.follow(paneSession?.id) } }
    }
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
    /// The session the core's rows last said the slot shows, frame by frame,
    /// and the one this app asked for that they have not shown yet, with
    /// how long its ask still holds once the core has answered. The
    /// selection follows the slot when another hand moves it (followShown),
    /// so the selected card, the frame round the terminal and the lines
    /// between them always name the session the terminal shows (TabLink.swift).
    @ObservationIgnored private var coreShown: String?
    @ObservationIgnored private var awaiting: String?
    @ObservationIgnored private var awaitingUntil: Date?

    static let defaultFontSize: CGFloat = 13

    struct Notice: Equatable, Identifiable {
        let id = UUID()
        var text: String
        var isError: Bool
        /// It offers Undo, which takes back the step it reports.
        var undo = false
        /// It reports notes deleted, and offers Undo, which brings these
        /// back from Recently Deleted (Notes.swift).
        var restore: [String]?
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
    /// The mail plugin's accounts and list, which the Mail tab and window
    /// share (Mail.swift).
    let mail: MailModel
    /// The Yoink plugin's downloads, which its tab draws (Yoink.swift).
    let yoink: YoinkModel
    /// The chat plugin's accounts, drafts and channels, which its tab
    /// draws (Chat.swift).
    let chat: ChatModel
    /// The founding documents, which the Founding tab draws (Founding.swift).
    let founding: FoundingModel
    /// The notepad, the person's notes for later (Notes.swift).
    let notes = NotesModel()
    /// The Obsidian vault, for its cards and the notepad (Vault.swift).
    let vault = VaultModel()
    /// Zen: the session on show alone, full screen (Zen.swift).
    let zen = ZenModel()
    @ObservationIgnored private var started = false

    init(socketPath: String = CorePaths.socket()) {
        client = CoreClient(socketPath: socketPath)
        settings = SettingsModel(client: client)
        mail = MailModel(client: client)
        yoink = YoinkModel(client: client)
        chat = ChatModel(client: client)
        founding = FoundingModel(client: client)
        notes.store = self
        vault.store = self
        settings.changed = { [weak self] in
            self?.loadChoices()
            self?.loadPluginsSoon()
        }
        variorum.store = self
        watchMenu()
    }

    var socketPath: String { client.socketPath }

    // MARK: Connection

    func start() {
        guard !started else { return }
        started = true
        if let seconds = FreezeDog.restartedAfter {
            say("\(Brand.name) stopped answering for \(seconds) s, so ❤️‍🩹 Raffi restarted it. Every agent kept running.",
                error: false, seconds: 10)
        }
        // Behind another app, the window takes in the core's news less
        // often (Pace.swift).
        Pace.shared.set(active: NSApplication.shared.isActive)
        let nc = NotificationCenter.default
        nc.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in Pace.shared.set(active: true) }
        // A part set up in another app's terminal shows once you come back.
        nc.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.loadPluginsSoon() }
        }
        nc.addObserver(forName: NSApplication.didResignActiveNotification, object: nil, queue: .main) { _ in Pace.shared.set(active: false) }
        // A link clicked in a terminal (TerminalLinks.swift).
        nc.addObserver(forName: .irieTerminalLink, object: nil, queue: .main) { [weak self] n in
            guard let link = n.userInfo?["link"] as? String else { return }
            let other = n.userInfo?["other"] as? Bool ?? false
            MainActor.assumeIsolated { self?.openTerminalLink(link, other: other) }
        }
        watchPopOut()
        Task { await run() }
    }

    /// Connects, subscribes and stays subscribed; on any failure it waits and
    /// tries again, so starting Irie later just works.
    private func run() async {
        while true {
            do {
                let hello = try await client.call("hello", HelloArgs(), timeout: 2, as: HelloResult.self)
                guard hello.features.contains("attach-info") else {
                    link = .offline("This \(Brand.name) is too old for the app. Run the new build's `\(Brand.slug) up` to update it.")
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
                canSendToTop = hello.features.contains("spawn-top")
                canTutor = hello.features.contains("spawn-tutor")
                canLearn = hello.features.contains("learn")
                canMaterialize = hello.features.contains("materialize")
                canDock = hello.features.contains("dock")
                rolodex.canShell = hello.features.contains("pane")
                canLink = hello.features.contains("link")
                canConfigure = hello.features.contains("config")
                canContinue = hello.features.contains("continue")
                canPark = hello.features.contains("park")
                canPin = hello.features.contains("pin")
                canGroup = hello.features.contains("group")
                canTrod = hello.features.contains("trod")
                canBoost = hello.features.contains("gabriel")
                canBoostAlways = hello.features.contains("boost-always")
                canVPN = hello.features.contains("vpn")
                canArch = hello.features.contains("arch")
                canUndo = hello.features.contains("undo")
                canConfirmUndo = hello.features.contains("undo-confirm")
                canPlugins = hello.features.contains("plugins")
                canHelpers = hello.features.contains("helpers")
                // A core that restarted, or a new one, hears them again.
                canSizes = hello.features.contains("sizes")
                sizesSent = nil
                sizesSoon()
                notes.connected(serves: hello.features.contains("notepad"))
                vault.served = hello.features.contains("obsidian")
                canVariorum = hello.features.contains("variorum")
                knowsFeatures = true
                keepSideTab()
                variorum.check()
                canMail = hello.features.contains("mail")
                loadPluginsSoon()
                keepPlugins()
                if hello.features.contains("theme") { sendTheme(Palette.chosen) }
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
    /// status line follow it, so they match the cards, and the terminal
    /// sidebar, the slot and the phone app draw in it (Header.Theme). A
    /// theme of yours brings its colours.
    func sendTheme(_ id: String) {
        // A preview leaves that to the installed app (Preview.swift).
        guard !Preview.isOn else { return }
        let p = Palette.named(id)
        let args = ThemeArgs(theme: p.id, light: p.isCustom ? p.light.hexes : nil, dark: p.isCustom ? p.dark.hexes : nil)
        Task { _ = try? await client.call("theme", args, timeout: 2, as: Empty.self) }
    }

    /// The Writing plugin as the Variorum tab cares for it: whether it and
    /// each part is on, and how far each is set up.
    private static func writing(_ all: [PluginInfo]) -> [String] {
        guard let p = all.first(where: { $0.name == "writing" }) else { return [] }
        return ["\(p.on)"] + p.parts.map { "\($0.name) \($0.on) \($0.state)" }
    }

    private static func describe(_ error: Error) -> String {
        if notRunning(error) {
            return "\(Brand.name) isn’t running."
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
        // Cards asked for (`irie ctl card`, `irie ctl open`) show once the
        // rows are in.
        var cards: [(PaneKind, String, CardFile?)] = []
        defer { for (kind, id, file) in cards { showCard(kind, in: id, file: file) } }
        // The session the slot moved to by another hand, if it did: the
        // selection goes to it once the rows are in.
        var follow: String?
        for f in frames {
            if let h = f.header { applyHeader(h) }
            switch f.t {
            case "snapshot":
                rows = (f.sessions ?? []).sorted(by: Self.before)
                touched = true
                noteShown(rows.first { $0.shown == true }?.id, follow: &follow)
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
                if s.shown == true {
                    noteShown(s.id, follow: &follow)
                } else if s.id == coreShown {
                    noteShown(nil, follow: &follow)
                }
            case "remove":
                guard let id = f.id else { continue }
                rows.removeAll { $0.id == id }
                if coach?.session == id { coach = nil }
                touched = true
                if id == coreShown { noteShown(nil, follow: &follow) }
            case "outside":
                applyOutside(f)
            case "closed":
                let c = f.closed ?? []
                if c != closed { closed = c }
            case "card":
                if let id = f.id, let kind = f.kind.flatMap(PaneKind.init(rawValue:)) {
                    cards.append((kind, id, f.path.map { CardFile(path: $0, line: f.line, column: f.col) }))
                }
            case "history":
                let undo = f.undo.flatMap { $0.isEmpty ? nil : $0 }
                let redo = f.redo.flatMap { $0.isEmpty ? nil : $0 }
                if undo != undoLabel { undoLabel = undo }
                if redo != redoLabel { redoLabel = redo }
            case "notepad":
                if let n = f.notepad { notes.take(frame: n) }
            default:
                continue
            }
        }
        guard touched else { return }
        if rows != sessions { sessions = rows }
        if let sel = selection, !sessions.contains(where: { $0.id == sel }) {
            selection = nil
        }
        // With none selected, as at launch or once the selected session
        // closed, the session the slot shows is the selected one too.
        if follow == nil, selection == nil, awaiting == nil, wanted == nil {
            follow = coreShown
        }
        if let id = follow { followShown(id) }
        keepSplit()
    }

    /// Takes in what a frame says the slot shows. A move this app asked
    /// for, and any on the way to it while the keys outran the core, change
    /// nothing; one by another hand, such as `irie ctl show`, the terminal
    /// sidebar or a core that started again, is to be followed. Frames are
    /// read in the order the core sent them, so a move the core made after
    /// this app's last one is the last word.
    private func noteShown(_ id: String?, follow: inout String?) {
        guard id != coreShown else { return }
        coreShown = id
        guard let id else { return }
        // A move the core said it made, but whose rows never came, holds
        // the slot's news back no longer than this.
        if let until = awaitingUntil, Date() > until {
            awaiting = nil
            awaitingUntil = nil
        }
        if id == awaiting {
            awaiting = nil
            awaitingUntil = nil
            return
        }
        guard awaiting == nil, wanted == nil else { return }
        follow = id
    }

    /// Selects the session the slot shows, in its space, without asking
    /// the core for anything: it shows it already. The list brings its card
    /// into sight (SessionList).
    private func followShown(_ id: String) {
        guard id != selection, let s = sessions.first(where: { $0.id == id }) else { return }
        if renaming != nil && renaming != id { endRename(commit: true) }
        if s.peerName == nil, s.spaceKey != space { setSpace(s.spaceKey, showing: false) }
        selection = id
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
        if let s = h.terminalLinks, s != terminalLinks {
            terminalLinks = s
        }
        if h.trod != trod {
            trod = h.trod
        }
        if h.vpn != vpn {
            vpn = h.vpn
            // The VPN's part says how the VPN is now.
            loadPluginsSoon()
        }
        // Gabriel's boost, however it was started: from ctl, Settings or
        // this menu. An older core's header never says, so its boost is
        // the one this app started.
        if canBoostAlways {
            let until = h.boost?.until, always = h.boost?.always ?? false
            if until != boostUntil { boostUntil = until }
            if always != boostAlways { boostAlways = always }
        }
        if let s = h.spaces, s != spaces {
            spaces = s
            // A space removed leaves its cards in the first.
            if !space.isEmpty, !s.dropFirst().contains(where: { $0.id == space }) { setSpace("") }
        }
        // Every header lists every group, so one with none has none.
        if (h.groups ?? []) != groups {
            groups = h.groups ?? []
        }
    }

    /// The picked session while it shows beside its teammates: the terminal
    /// shows, here rather than popped out, and it has teammates the app can
    /// reach.
    var splitSession: Session? {
        guard showTerminal, poppedOut == nil, outsideSelection == nil, attach?.teams != nil || isFixture,
              let s = selectedRow, !(s.team ?? []).isEmpty else { return nil }
        return s
    }

    /// Works out what views read instead of the rows, and sets each only
    /// when it changed: a set is news to every view that read it.
    private func derive() {
        let row = selection.flatMap { id in sessions.first { $0.id == id } }
        if row != selectedRow { selectedRow = row }
        // The panes beside it show as it last had them (Rolodex.swift).
        rolodex.follow(paneSession?.id)
        let c = RowCounts(sessions)
        if c != counts { counts = c }
        if hasRows == sessions.isEmpty { hasRows = !sessions.isEmpty }
    }

    /// Asks the core again for the picked session when it starts or stops
    /// showing beside its teammates.
    func keepSplit() {
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

    /// Arms Send to Top, or disarms it: armed, the next task sent goes to
    /// the top, first in Pinned and boosted.
    func setSendToTop(_ on: Bool) {
        sendToTop = on
        UserDefaults.standard.set(on, forKey: "sendToTop")
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
        // Zen draws only as the snapshot asks, and saves nothing.
        zen.fixture = true
        // Whatever the app popped out, the fixture's terminal is in.
        poppedOut = nil
        sessions = rows.sorted(by: Self.before)
        self.selection = selection
        link = .online(version: "snapshot")
        repoNames = ["bigup", "\(Brand.slug)", "notes"]
        agents = [Agent(name: "auto", note: "routes each task to the cheapest profile that does it well"),
                  Agent(name: "claude", note: "claude-opus-5-5 · max")]
        canPickModel = true
        canSendToTop = true
        canTutor = true
        canLearn = true
        sessionCards = [Fixture.selected: Fixture.standsCard]
        canConfigure = true
        canPark = true
        canGroup = true
        groups = Fixture.groups
        canTrod = true
        canBoost = true
        canBoostAlways = true
        canArch = true
        canVariorum = true
        knowsFeatures = true
        canMail = true
        mail.loadFixture()
        founding.loadFixture()
        archAgents = [
            ArchAgent(name: "mikael", icon: "⚔️🛡️", title: "a helper against evil",
                      about: "Mikael, the first of \(Brand.name)'s arch agents, stands with the user against evil and for the good they want to see in the world.",
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
            ArchAgent(name: "raphael", nickname: "Raffi", icon: "❤️‍🩹", title: "heals \(Brand.name) and your Mac",
                      about: "Raphael keeps \(Brand.name) and the computer it runs on well.",
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
        canConfirmUndo = true
        taskRepo = "\(Brand.slug)"
    }

    /// The list's order, which the core keeps and the user sets: a new
    /// session goes last, and a drag puts one where it is dropped; a boost
    /// moves none. A core without it sends no order: the order they
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
    /// view, one list, with your groups in it; in the Repo view, a group per
    /// repo in the order its first session started, then the sessions in no
    /// repo. Then the Parked
    /// group, then a group for each paired machine's sessions, in the order
    /// the core lists them, so they stand apart from this Mac's (DESIGN
    /// §2.8); parked, one goes into Parked with the rest. Within a group the
    /// sessions keep the list's one order, which only the user changes, so a
    /// state change never moves a card (DESIGN §4.1).
    var drawnGroups: [SessionGroup] {
        let f = filter.trimmingCharacters(in: .whitespaces).lowercased()
        // The space on show, and every paired machine's cards (Spaces.swift).
        let rows = (f.isEmpty ? sessions : sessions.filter { $0.name.lowercased().contains(f) })
            .filter { $0.peerName != nil || $0.spaceKey == space }
        let parked = rows.filter(\.isParked)
        // Pinned heads the list, in either view (Tabs.swift).
        let pinned = rows.filter { $0.isPinned && !$0.isParked && $0.peerName == nil }
        let here = rows.filter { !$0.isParked && !$0.isPinned && $0.peerName == nil }
        var hosts: [String] = [], byHost: [String: [Session]] = [:]
        for s in rows where !s.isParked {
            guard let h = s.peerName else { continue }
            if byHost[h] == nil { hosts.append(h) }
            byHost[h, default: []].append(s)
        }
        var out: [SessionGroup] = []
        switch mode {
        case .list:
            if !pinned.isEmpty {
                out.append(SessionGroup(id: SessionGroup.pinned, title: "Pinned", path: nil, sessions: pinned))
            }
            // Your groups, each where the first of its cards is, and the
            // cards of none between them (Groups.swift).
            out += listGroups(here)
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
            if !pinned.isEmpty {
                out.append(SessionGroup(id: SessionGroup.pinned, title: "Pinned", path: nil, sessions: pinned))
            }
            out += order.map { SessionGroup(id: "repo:" + $0, title: names[$0] ?? $0, path: $0, sessions: byRepo[$0] ?? []) }
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
        if s.isPinned { return SessionGroup.pinned }
        switch mode {
        case .list: return SessionGroup.list
        case .repo: return "repo:" + (s.repo ?? "")
        }
    }

    /// The cards in the order the sidebar draws them, without those in folded
    /// groups, a folded group inside one of yours too, or hidden by the
    /// filter. The keys that walk the sidebar follow it.
    var drawnOrder: [Session] { drawnGroups.filter(isOpen).flatMap { g in g.node.map(shown) ?? g.sessions } }

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

    /// The session a terminal view types to, while it is the picked
    /// session's own: the main terminal, or the split's lead pane. A
    /// session of another machine's has no terminal here (ContinueTarget).
    func continueSession(for view: NSView) -> String? {
        guard let s = selectedRow, s.host == nil, outsideSelection == nil else { return nil }
        if view === terminalView, showTerminal, splitSession == nil { return s.id }
        if view === leadView, splitSession?.id == s.id { return s.id }
        return nil
    }

    /// Whether the field editor edits the message box.
    func isMessageBox(_ editor: NSTextView) -> Bool {
        guard messageHasKeys, let field = messageField else { return false }
        return (editor.delegate as AnyObject?) === field
    }

    /// Sends a session's agent on: the core types "continue" and Enter
    /// into its prompt while the prompt is empty (op continue), for a
    /// double ↓ on it (ContinueKey.swift). When it can't, as when the
    /// prompt has something typed in it, a notice says why.
    func continueAgent(_ id: String) {
        guard canContinue else { return }
        Task {
            do {
                _ = try await client.call("continue", ContinueArgs(session: id), as: Empty.self)
            } catch {
                say(error.localizedDescription, error: true)
            }
        }
    }

    /// Moves the keyboard between the session list and the terminal. In zen,
    /// which hides the list, zen ends and the list takes it (Zen.swift).
    func toggleFocus() {
        if zen.on {
            afterZen { [weak self] in self?.focusSidebar += 1 }
            return
        }
        if !showTerminal {
            if messageHasKeys { focusSidebar += 1 } else { focusMessage += 1 }
        } else if let t = terminalView, t.window?.firstResponder === t {
            focusSidebar += 1
        } else {
            focusTerminal += 1
        }
    }

    /// ⌘+, ⌘- and ⌘0: the text size on show, the window's, or zen's own
    /// while zen is on, which zen keeps apart (Zen.swift).
    func setFontSize(_ size: CGFloat) {
        fontSize = min(max(size, 9), 28)
        if zen.on {
            zen.textSize = Double(fontSize)
        } else {
            keepWindowFontSize(fontSize)
        }
    }

    /// Settings › Appearance › Window: the window's text size, which zen
    /// follows while it has none of its own.
    func setWindowFontSize(_ size: CGFloat) {
        keepWindowFontSize(min(max(size, 9), 28))
        if !zen.on || zen.textSize == nil { fontSize = windowFontSize }
    }

    private func keepWindowFontSize(_ size: CGFloat) {
        if windowFontSize != size { windowFontSize = size }
        UserDefaults.standard.set(Double(size), forKey: "terminalFontSize")
    }

    /// Puts on zen's own text size as zen starts, or the window's as it ends.
    func useZenText(_ on: Bool) {
        let size = on ? zen.textSize.map { CGFloat($0) } ?? windowFontSize : windowFontSize
        if fontSize != size { fontSize = size }
    }

    private static func savedFontSize() -> CGFloat {
        let v = UserDefaults.standard.double(forKey: "terminalFontSize")
        return v >= 9 && v <= 28 ? CGFloat(v) : defaultFontSize
    }

    // MARK: Requests

    /// Shows a helper in its session's Helpers card, in the panes beside the
    /// conversation or the terminal, whichever shows (Rolodex.swift): a
    /// session's helpers are its own, and the right sidebar is the same for
    /// every session. The terminal never splits for a helper, which has no
    /// terminal of its own.
    func openHelper(_ agent: String, in session: String) {
        if selection != session || outsideSelection != nil { show(session) }
        openHelpers[session] = agent
        showCard(.helpers, in: session)
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
    /// names, which View › Helpers opens when none runs.
    @ObservationIgnored private var lastHelpers: [String: String] = [:]
    @ObservationIgnored private var newestHelpers: [String: String] = [:]

    /// Notes the newest helper a session's conversation names.
    func noteHelper(_ agent: String, in session: String) {
        newestHelpers[session] = agent
    }

    /// Whether the panes show the selected session's Helpers card in front.
    var helperShown: Bool {
        guard let id = selection, outsideSelection == nil else { return false }
        let r = rolodex
        return r.open && !r.folded && r.deck(id).frontPane?.kind == .helpers
    }

    /// View › Session Helpers (⇧⌘H) and the toolbar's button: turns the
    /// panes beside the selected session to its Helpers card, on its helper
    /// shown last, else the newest running, else the newest, or hides the
    /// panes when that card is in front.
    func toggleHelpers() {
        guard let id = selection, outsideSelection == nil, let s = sessions.first(where: { $0.id == id }) else { return }
        if helperShown {
            togglePanes()
            return
        }
        if let a = openHelpers[id] ?? lastHelpers[id] ?? s.subagents?.last?.agent ?? newestHelpers[id] {
            openHelper(a, in: id)
        } else {
            showCard(.helpers, in: id)
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
        // A card in another space takes the sidebar there (Spaces.swift).
        if let s = sessions.first(where: { $0.id == id }), s.peerName == nil, s.spaceKey != space { setSpace(s.spaceKey, showing: false) }
        // Its panes show otherwise: the terminal keeps its size until the
        // core has swapped it in (PaneSizes.swift).
        if holdsForSwap(to: id) { ResizeHold.shared.holdForSwap() }
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
                // Until its rows say the slot shows it, the slot's news is
                // older than this ask (noteShown). A session the slot shows
                // already brings none, unless an ask before it is still on
                // its way, which moves the slot first.
                awaiting = next.id == coreShown && awaiting == nil ? nil : next.id
                awaitingUntil = nil
                do {
                    let split = splitSession?.id == next.id
                    let shown = split ? next.id : nil
                    if splitShown != shown { splitShown = shown }
                    _ = try await client.call("show", ShowArgs(session: next.id, focus: next.focus, split: split), as: Empty.self)
                    // The core sent its rows before it answered, on the
                    // stream, which a busy Mac may read a little later.
                    if awaiting == next.id { awaitingUntil = Date().addingTimeInterval(2) }
                    // The conversation's message box, or the terminal when it
                    // shows; a name open to edit, after a double click's first
                    // click, keeps the keyboard.
                    if next.focus, wanted == nil, renaming != next.id { if showTerminal { focusTerminal += 1 } else { focusMessage += 1 } }
                } catch where wanted == nil {
                    if awaiting == next.id { awaiting = nil }
                    // The slot stayed where it was, as a core that is
                    // restarting leaves it: so does the selection, or the
                    // card, its lines and the frame would name a session the
                    // terminal doesn't show.
                    if let id = coreShown { followShown(id) }
                    say("Couldn’t show it: \(error.localizedDescription)", error: true)
                } catch {
                    if awaiting == next.id { awaiting = nil }
                }
            }
            showing = false
            // The session is in: the terminal takes its size.
            ResizeHold.shared.letGoOfSwap()
        }
    }

    /// Sends what was typed in the task bar to the core, which reads it: a
    /// task with its @ and + words, a question such as "hows the parser?",
    /// or a : command (docs/CORE.md). With no @repo in its text a task runs
    /// where the bar's menu says. Returns whether the core took it.
    func input(_ text: String) async -> Bool {
        await input(text, repo: taskRepo, worktree: true, agent: nil, model: nil, tutor: nil)
    }

    /// Sends a jot, what was typed in the task bar or a note sent as one,
    /// as input does, with the bar's Send to Top as it stands (sendTop).
    /// top sends this one to the top, as ⌘Return does, armed or not. The
    /// app's other tasks, the new-task sheet's and its buttons', go as
    /// typed, whatever the bar's toggle says.
    func jot(_ text: String, note: String? = nil, top: Bool = false) async -> Bool {
        let args = inputArgs(text, repo: taskRepo, worktree: true, agent: nil, model: nil, tutor: nil, note: note)
        return await sendTop(args, top: top) != nil
    }

    /// Shows the session a note was sent to, or says it is closed.
    func showNoteSession(_ id: String) {
        if sessions.contains(where: { $0.id == id }) {
            show(id, focus: true)
        } else {
            say("That note’s session is closed. Reopen it from History.", error: false)
        }
    }

    /// Sends a note's text as the task bar would, typed and sent, naming the
    /// note so the core ticks it off once it goes (Notes.swift). The bar's
    /// history takes it too, as if typed there.
    func sendNote(_ text: String, note: String) async -> Bool {
        TaskHistory.remember(text)
        return await jot(text, note: note)
    }

    /// Sends text from the bar or the new-task sheet. The @repo, +agent and
    /// tutor words in it win over the menus' repo, agent and tutor mode.
    func input(_ text: String, repo: String?, worktree: Bool, agent: String?, model: String?, tutor: String?, note: String? = nil) async -> Bool {
        await send(inputArgs(text, repo: repo, worktree: worktree, agent: agent, model: model, tutor: tutor, note: note)) != nil
    }

    /// The input of text, with the menus' repo, agent, model and tutor
    /// mode, the selected card and the space on show.
    private func inputArgs(_ text: String, repo: String?, worktree: Bool, agent: String?, model: String?, tutor: String?, note: String?) -> InputArgs {
        var args = InputArgs(text: text)
        args.note = note
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
        // A task starts in the space on show.
        if !spaces.isEmpty { args.space = space }
        return args
    }

    /// Sends args with Send to Top as it stands, and settles the toggle.
    /// top is a send to the top asked for this once (⌘Return), whatever the
    /// toggle says. Returns what the core says the input did: spawned,
    /// added, answered, ran or asked; nil when it did not go, and its text
    /// came back to the bar to fix. The core reads top only for a task: a
    /// : command or a question goes as it would without it, and a task
    /// sent to the top always starts a session, never added (DESIGN §4.1).
    private func sendTop(_ args: InputArgs, top: Bool) async -> String? {
        var args = args
        let armed = canSendToTop && sendToTop
        if canSendToTop, top || armed { args.top = true }
        // One task each time it is armed, "the jot that sends". It disarms
        // as this one goes, since the bar empties at once and a task typed
        // while this one starts must not ride along, and arms again unless
        // a session started: a : command, a question or a task that did not
        // go leaves it for the next.
        if armed { setSendToTop(false) }
        let did = await send(args)
        if armed, did != "spawned" { setSendToTop(true) }
        return did
    }

    /// What the Help menu shows: the resources, with the shown session's
    /// language first, and the glossary. Nil without a core that serves it.
    func learn() async -> LearnResult? {
        guard canLearn else { return nil }
        return try? await client.call("learn", LearnArgs(session: selection), timeout: 5, as: LearnResult.self)
    }

    /// Asks the core for session id's card of where it stands, and keeps
    /// it; while one is being made, waits for it, up to two minutes. False
    /// when the core gives none, as while materialize.sessions is off.
    @discardableResult
    func refreshCard(_ id: String) async -> Bool {
        guard canMaterialize, !isFixture else { return false }
        guard let r = try? await client.call("materialize", MaterializeArgs(session: id), timeout: 15, as: MaterializeResult.self) else {
            return false
        }
        sessionCards[id] = r
        guard r.state == "cold" || r.state == "refreshing" else { return true }
        let wait = MaterializeArgs(session: id, wait: 120_000_000_000)
        if let done = try? await client.call("materialize", wait, timeout: 150, as: MaterializeResult.self) {
            sessionCards[id] = done
        }
        return true
    }

    /// Takes a task added to the one before back and starts it as a task
    /// of its own (the notice's New task).
    func startApart(_ args: InputArgs) {
        dismissNotice()
        Task { _ = await send(args) }
    }

    /// Sends args to the core and says what came of it. Returns the input's
    /// did, or nil when it did not go.
    private func send(_ args: InputArgs) async -> String? {
        tasksStarting += 1
        defer { tasksStarting -= 1 }
        do {
            // A task in a repo may make a worktree and check out submodules
            // first; one added to a task still starting waits for it.
            let r = try await client.call("input", args, timeout: 600, as: InputResult.self)
            switch r.did {
            case "asked":
                if let c = r.choice, let op = r.op { await answer(op, c) }
            case "answered" where r.op == "markets":
                // :markets: the Markets window says how the map stands.
                NotificationCenter.default.post(name: .irieMarkets, object: nil)
            case _ where r.op == "chat" && chatPane != nil:
                // :chat opens the Chat tab, where the drafts wait to be
                // sent.
                showChat()
            case _ where r.op == "yoink" && yoinkPane != nil:
                // :yoink opens the Yoink tab, where the download shows as it
                // goes; a start says so too.
                showYoink()
                if r.did != "answered" { say(r.text, error: false, seconds: 5) }
            case "answered":
                // :mail opens the Mail tab, which says more than words can.
                if r.op == "mail", mailShown {
                    openSide(.mail)
                } else {
                    say(r.text, error: false, seconds: 8)
                }
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
            return r.did
        } catch {
            say(error.localizedDescription, error: true)
            return nil
        }
    }

    /// Opens the session's name to edit, in its card.
    func beginRename(_ id: String) {
        // The name is edited in its card, which zen hides (Zen.swift).
        if zen.on {
            afterZen { [weak self] in self?.beginRename(id) }
            return
        }
        guard let s = sessions.first(where: { $0.id == id }) else { return }
        if renaming != id {
            endRename(commit: true)
            renameText = s.name
        }
        // Selected as a click selects it: the terminal shows it too, so the
        // frame and the lines never name a session it doesn't show.
        if selection != id || outsideSelection != nil { show(id) }
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

    /// Boosts your focus whenever you're at the computer, or no longer:
    /// arch.gabriel.always, in the config file, shown at once; the core's
    /// header then says so (DESIGN §4.11). Off takes the key out of the
    /// file, as boost off does.
    func setBoostAlways(_ on: Bool) {
        boostAlways = on
        Task {
            do {
                _ = try await client.call("config", ConfigArgs(set: [ConfigSet(key: "arch.gabriel.always", value: on ? .bool(true) : .null)]), timeout: 10, as: ConfigResult.self)
                say(on ? "Boosting your focus whenever you’re at the computer: the rest runs on the efficiency cores, so it finishes later." : "No longer boosting always: Gabriel holds back only while the Mac is busy.", error: false, seconds: 6)
            } catch {
                boostAlways = !on
                say("Couldn’t \(on ? "boost always" : "stop boosting always"): \(error.localizedDescription)", error: true)
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
    func say(_ text: String, error: Bool, seconds: UInt64 = 3, undo: Bool = false, apart: InputArgs? = nil, restore: [String]? = nil) {
        let n = Notice(text: text, isError: error, undo: undo && canUndo, restore: restore, apart: apart)
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
/// parked ones, a paired machine's, or one of your groups.
struct SessionGroup: Identifiable, Equatable {
    let id: String
    let title: String
    /// The repo's main checkout, for a repo's group.
    let path: String?
    let sessions: [Session]
    /// The paired machine whose sessions it holds, for a machine's group.
    var host: String? = nil
    /// Your group's id, for one of your groups (Groups.swift).
    var userGroup: String? = nil
    /// For one of your groups, what it holds as the List view draws it: its
    /// cards and the groups inside it, in the list's one order. Its sessions
    /// are every card within it, its groups' too.
    var node: GroupNode? = nil

    /// Whether it is drawn without a header: the List view's cards of no
    /// group of yours, list, or list:1 and on after your groups.
    var bare: Bool { id == Self.list || id.hasPrefix(Self.list + ":") }

    /// The List view's one group, which has no header, the Parked group,
    /// and the Pinned group at the top.
    static let list = "list"
    static let parked = "parked"
    static let pinned = "pinned"

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

/// The double ↓ on an empty prompt asks the store (ContinueKey.swift).
extension Store: ContinueTarget {}
