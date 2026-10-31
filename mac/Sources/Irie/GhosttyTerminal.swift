import AppKit
import SwiftUI
#if canImport(GhosttyKit)
import GhosttyKit
#endif

// The app's terminals drawn by libghostty, Ghostty's own engine
// (docs/proposals/ghostty-terminal.md). A view here is a Ghostty surface
// whose command is a tmux client of one target, as a SwiftTerm view's is,
// so neither tmux nor the core sees a difference: only what draws the
// terminal changes. The owner's Ghostty config sets its font, colours and
// keys, and Irie's ground goes on top, so it matches the frame round it.
// The input handling follows Ghostty's own macOS app
// (SurfaceView_AppKit.swift, MIT; PROVENANCE.md).

/// What draws the app's terminals: Ghostty's engine when this build has it
/// and it starts, else SwiftTerm (View > Terminal Engine, Settings).
enum TerminalEngine: String, CaseIterable, Identifiable {
    case ghostty, swiftterm

    static let defaultsKey = "terminalEngine"
    var id: String { rawValue }
    var title: String { self == .ghostty ? "Ghostty" : "SwiftTerm" }

    /// The engines this build can draw with, the preferred first.
    static var built: [TerminalEngine] {
        #if canImport(GhosttyKit)
        return GhosttyRuntime.shared.app != nil ? [.ghostty, .swiftterm] : [.swiftterm]
        #else
        return [.swiftterm]
        #endif
    }

    /// The engine a saved name picks, or the preferred one.
    static func named(_ name: String?) -> TerminalEngine {
        built.first { $0.rawValue == name } ?? built[0]
    }
}

extension Notification.Name {
    /// A Ghostty split key in one of the split's terminals; the object is
    /// the terminal's view, the user info's "move" a `SplitMove`.
    static let irieSplitMove = Notification.Name("irieSplitMove")
}

/// What a Ghostty split key asks of the split (TeamSplit.swift).
enum SplitMove {
    case focus(next: Bool) // previous or next
    case focusToward(NSRectEdge)
    case resize(NSRectEdge)
    case equalize
    case zoom
}

#if canImport(GhosttyKit)

/// The process's one Ghostty app: its config and the engine's callbacks.
final class GhosttyRuntime {
    static let shared = GhosttyRuntime()

    /// nil when libghostty would not start; the app then draws with
    /// SwiftTerm.
    private(set) var app: ghostty_app_t?
    private var config: ghostty_config_t?

    private init() {
        // Ghostty's resources: its shell integration, with terminfo beside
        // them, which the bundle carries as Ghostty's own app does.
        if let res = Bundle.main.resourceURL?.appendingPathComponent("ghostty"),
           FileManager.default.fileExists(atPath: res.path) {
            setenv("GHOSTTY_RESOURCES_DIR", res.path, 1)
        }
        guard ghostty_init(UInt(CommandLine.argc), CommandLine.unsafeArgv) == GHOSTTY_SUCCESS,
              let config = Self.makeConfig() else { return }
        self.config = config
        var runtime = ghostty_runtime_config_s(
            userdata: nil,
            supports_selection_clipboard: false,
            wakeup_cb: { _ in DispatchQueue.main.async { GhosttyRuntime.shared.tick() } },
            action_cb: { _, target, action in GhosttyRuntime.perform(action, on: target) },
            read_clipboard_cb: { userdata, location, state in GhosttySurfaceBox.readClipboard(userdata, location, state) },
            confirm_read_clipboard_cb: { _, _, _, _ in },
            write_clipboard_cb: { _, location, content, count, confirm in
                GhosttySurfaceBox.writeClipboard(location, content, count, confirm)
            },
            close_surface_cb: { userdata, _ in GhosttySurfaceBox.closed(userdata) })
        guard let app = ghostty_app_new(&runtime, config) else { return }
        self.app = app
        ghostty_app_set_focus(app, NSApp.isActive)
        let center = NotificationCenter.default
        center.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in
            ghostty_app_set_focus(app, true)
        }
        center.addObserver(forName: NSApplication.didResignActiveNotification, object: nil, queue: .main) { _ in
            ghostty_app_set_focus(app, false)
        }
        center.addObserver(forName: NSTextInputContext.keyboardSelectionDidChangeNotification, object: nil, queue: .main) { _ in
            ghostty_app_keyboard_changed(app)
        }
    }

    private func tick() {
        if let app { ghostty_app_tick(app) }
    }

    /// The owner's Ghostty config, then Irie's on top: the terminal's ground
    /// and ink match the frame round it, which has its own padding; a tmux
    /// client is no shell, so no shell integration; nothing asks before a
    /// surface closes; and nothing inside may read the clipboard.
    private static func makeConfig() -> ghostty_config_t? {
        guard let config = ghostty_config_new() else { return nil }
        ghostty_config_load_default_files(config)
        ghostty_config_load_recursive_files(config)
        let mine = FileManager.default.temporaryDirectory.appendingPathComponent("irie-ghostty-\(getpid()).conf")
        let text = """
            background = \(hex(Theme.terminalBackground))
            foreground = \(hex(Theme.terminalForeground))
            window-padding-x = 0
            window-padding-y = 0
            window-padding-balance = false
            shell-integration = none
            confirm-close-surface = false
            clipboard-read = deny

            """
        if (try? text.write(to: mine, atomically: true, encoding: .utf8)) != nil {
            ghostty_config_load_file(config, mine.path)
        }
        ghostty_config_finalize(config)
        try? FileManager.default.removeItem(at: mine)
        return config
    }

    private static func hex(_ color: NSColor) -> String {
        let c = color.usingColorSpace(.sRGB) ?? color
        let v = [c.redComponent, c.greenComponent, c.blueComponent].map { Int(($0 * 255).rounded()) }
        return String(format: "%02X%02X%02X", v[0], v[1], v[2])
    }

    /// The engine's actions. Most are the business of Ghostty's own app and
    /// none of Irie's, and returning false says so. A child that exited
    /// attaches again, the pointer takes the shape the terminal asks, and
    /// the split keys go to the split.
    private static func perform(_ action: ghostty_action_s, on target: ghostty_target_s) -> Bool {
        guard target.tag == GHOSTTY_TARGET_SURFACE, let surface = target.target.surface,
              let box = GhosttySurfaceBox.of(surface) else { return false }
        var move: SplitMove?
        switch action.tag {
        case GHOSTTY_ACTION_SHOW_CHILD_EXITED:
            DispatchQueue.main.async { box.view?.childExited() }
            return true
        case GHOSTTY_ACTION_MOUSE_SHAPE:
            let shape = action.action.mouse_shape
            DispatchQueue.main.async { box.view?.pointer(shape) }
            return true
        case GHOSTTY_ACTION_GOTO_SPLIT:
            switch action.action.goto_split {
            case GHOSTTY_GOTO_SPLIT_PREVIOUS: move = .focus(next: false)
            case GHOSTTY_GOTO_SPLIT_NEXT: move = .focus(next: true)
            case GHOSTTY_GOTO_SPLIT_UP: move = .focusToward(.maxY)
            case GHOSTTY_GOTO_SPLIT_DOWN: move = .focusToward(.minY)
            case GHOSTTY_GOTO_SPLIT_LEFT: move = .focusToward(.minX)
            default: move = .focusToward(.maxX)
            }
        case GHOSTTY_ACTION_RESIZE_SPLIT:
            switch action.action.resize_split.direction {
            case GHOSTTY_RESIZE_SPLIT_UP: move = .resize(.maxY)
            case GHOSTTY_RESIZE_SPLIT_DOWN: move = .resize(.minY)
            case GHOSTTY_RESIZE_SPLIT_LEFT: move = .resize(.minX)
            default: move = .resize(.maxX)
            }
        case GHOSTTY_ACTION_EQUALIZE_SPLITS:
            move = .equalize
        case GHOSTTY_ACTION_TOGGLE_SPLIT_ZOOM:
            move = .zoom
        default:
            return false
        }
        if let move {
            DispatchQueue.main.async {
                guard let view = box.view else { return }
                NotificationCenter.default.post(name: .irieSplitMove, object: view, userInfo: ["move": move])
            }
        }
        return true
    }
}

/// What the engine's callbacks reach for one surface: its handle and its
/// view, kept apart from the view, so a callback never touches the view
/// off the main thread.
final class GhosttySurfaceBox {
    var surface: ghostty_surface_t?
    weak var view: GhosttyTerminalView?

    static func of(_ surface: ghostty_surface_t) -> GhosttySurfaceBox? {
        from(ghostty_surface_userdata(surface))
    }

    static func from(_ userdata: UnsafeMutableRawPointer?) -> GhosttySurfaceBox? {
        userdata.map { Unmanaged<GhosttySurfaceBox>.fromOpaque($0).takeUnretainedValue() }
    }

    /// A paste: the pasteboard's text, if it has any.
    static func readClipboard(_ userdata: UnsafeMutableRawPointer?, _ location: ghostty_clipboard_e,
                              _ state: UnsafeMutableRawPointer?) -> Bool {
        guard location == GHOSTTY_CLIPBOARD_STANDARD, let surface = from(userdata)?.surface,
              let text = NSPasteboard.general.string(forType: .string) else { return false }
        text.withCString { ghostty_surface_complete_clipboard_request(surface, $0, state, false) }
        return true
    }

    /// A copy, as tmux's selections arrive (OSC 52). One that would need
    /// asking first is left out: there is nobody to ask.
    static func writeClipboard(_ location: ghostty_clipboard_e, _ content: UnsafePointer<ghostty_clipboard_content_s>?,
                               _ count: Int, _ confirm: Bool) {
        guard location == GHOSTTY_CLIPBOARD_STANDARD, !confirm, let content else { return }
        for i in 0..<count {
            guard let mime = content[i].mime, String(cString: mime) == "text/plain", let data = content[i].data else { continue }
            let text = String(cString: data)
            DispatchQueue.main.async {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(text, forType: .string)
            }
            return
        }
    }

    static func closed(_ userdata: UnsafeMutableRawPointer?) {
        guard let box = from(userdata) else { return }
        DispatchQueue.main.async { box.view?.childExited() }
    }
}

/// One terminal: a Ghostty surface running a tmux client of `target`.
final class GhosttyTerminalView: NSView, NSTextInputClient {
    struct Attach: Equatable {
        var tmux: String
        var socket: String
        var target: String
    }

    let attach: Attach
    var fontSize: CGFloat {
        didSet { if fontSize != oldValue { applyFontSize() } }
    }
    /// Whether it takes the keyboard once it is in a window.
    var takesKeysOnStart = true
    /// Whether it shows: a terminal under the conversation or under the
    /// split need not draw.
    var shows = true {
        didSet { if let surface, shows != oldValue { ghostty_surface_set_occlusion(surface, shows) } }
    }

    private let box = GhosttySurfaceBox()
    private var surface: ghostty_surface_t? { box.surface }
    private var gone = false
    private var tries = 0
    private var cursor = NSCursor.iBeam
    private var markedText = NSMutableAttributedString()
    /// Text that interpretKeyEvents composed during one keyDown.
    private var keyTextAccumulator: [String]?

    init(attach: Attach, fontSize: CGFloat) {
        self.attach = attach
        self.fontSize = fontSize
        super.init(frame: .zero)
        box.view = self
    }

    required init?(coder: NSCoder) { fatalError("GhosttyTerminalView is made in code") }

    deinit {
        if let s = box.surface { ghostty_surface_free(s) }
    }

    override var acceptsFirstResponder: Bool { true }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window else { return }
        start()
        if takesKeysOnStart { window.makeFirstResponder(self) }
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        if surface == nil { start() } else { sizeDidChange(newSize) }
    }

    /// The shell command a surface runs: the tmux client, a moment after
    /// the surface takes its size. A client that started at the engine's
    /// first size, before its frame's, would resize the window it shows,
    /// and for the main terminal that is the slot, which every agent
    /// follows. On macOS Ghostty runs it as `exec -l <command>` in bash, so
    /// it is one command: env, which starts a plain sh for the pause.
    private static func command(_ a: Attach) -> String {
        let q = { (s: String) in "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'" }
        let client = "sleep 0.1; exec \(q(a.tmux)) -S \(q(a.socket)) attach-session -t \(q(a.target))"
        return "/usr/bin/env -u TMUX -u TMUX_PANE /bin/sh -c \(q(client))"
    }

    /// Makes the surface once the view has a window and a size.
    private func start() {
        guard !gone, surface == nil, let window, bounds.width > 0, bounds.height > 0,
              let app = GhosttyRuntime.shared.app else { return }
        var config = ghostty_surface_config_new()
        config.platform_tag = GHOSTTY_PLATFORM_MACOS
        config.platform = ghostty_platform_u(macos: ghostty_platform_macos_s(nsview: Unmanaged.passUnretained(self).toOpaque()))
        config.userdata = Unmanaged.passUnretained(box).toOpaque()
        config.scale_factor = Double(window.backingScaleFactor)
        config.font_size = Float(fontSize)
        config.context = GHOSTTY_SURFACE_CONTEXT_WINDOW
        box.surface = Self.command(attach).withCString { cmd in
            config.command = cmd
            return ghostty_surface_new(app, &config)
        }
        guard let surface else { return }
        let scale = window.backingScaleFactor
        ghostty_surface_set_content_scale(surface, scale, scale)
        sizeDidChange(bounds.size)
        ghostty_surface_set_color_scheme(surface, GHOSTTY_COLOR_SCHEME_DARK)
        ghostty_surface_set_occlusion(surface, shows)
        ghostty_surface_set_focus(surface, window.firstResponder === self)
        updateTrackingAreas()
    }

    /// The tmux client ended, as when its session goes or the server
    /// restarts: a new surface attaches again shortly, as SwiftTerm's view
    /// does.
    func childExited() {
        guard !gone else { return }
        if let s = box.surface {
            box.surface = nil
            ghostty_surface_free(s)
        }
        tries += 1
        DispatchQueue.main.asyncAfter(deadline: .now() + (tries < 10 ? 0.2 : 1)) { [weak self] in self?.start() }
    }

    /// Ends the surface and its tmux client for good.
    func stop() {
        gone = true
        if let s = box.surface {
            box.surface = nil
            ghostty_surface_free(s)
        }
    }

    /// The terminal's grid, while it has a surface.
    var grid: (columns: Int, rows: Int)? {
        guard let surface else { return nil }
        let s = ghostty_surface_size(surface)
        return (Int(s.columns), Int(s.rows))
    }

    private func sizeDidChange(_ size: CGSize) {
        guard let surface, size.width > 0, size.height > 0 else { return }
        let px = convertToBacking(size)
        ghostty_surface_set_size(surface, UInt32(px.width), UInt32(px.height))
    }

    override func viewDidChangeBackingProperties() {
        super.viewDidChangeBackingProperties()
        if let window {
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            layer?.contentsScale = window.backingScaleFactor
            CATransaction.commit()
        }
        guard let surface, frame.width > 0, frame.height > 0 else { return }
        let fb = convertToBacking(frame)
        ghostty_surface_set_content_scale(surface, fb.width / frame.width, fb.height / frame.height)
        sizeDidChange(frame.size)
    }

    private func applyFontSize() {
        _ = binding("set_font_size:\(Double(fontSize))")
    }

    private func binding(_ action: String) -> Bool {
        guard let surface else { return false }
        let n = action.utf8.count
        return action.withCString { ghostty_surface_binding_action(surface, $0, UInt(n)) }
    }

    // MARK: Focus

    override func becomeFirstResponder() -> Bool {
        let ok = super.becomeFirstResponder()
        if ok, let surface { ghostty_surface_set_focus(surface, true) }
        return ok
    }

    override func resignFirstResponder() -> Bool {
        let ok = super.resignFirstResponder()
        if ok, let surface { ghostty_surface_set_focus(surface, false) }
        return ok
    }

    // MARK: Copy and paste

    /// A mouse selection is tmux's, and tmux has already put it on the
    /// pasteboard (OSC 52). Only a selection Ghostty holds, made with Shift
    /// held, is copied here, so ⌘C never wipes tmux's.
    @objc func copy(_ sender: Any?) {
        guard let surface, ghostty_surface_has_selection(surface) else { return }
        _ = binding("copy_to_clipboard")
    }

    @objc func paste(_ sender: Any?) {
        _ = binding("paste_from_clipboard")
    }

    override func selectAll(_ sender: Any?) {
        _ = binding("select_all")
    }

    // MARK: Pointer

    func pointer(_ shape: ghostty_action_mouse_shape_e) {
        switch shape {
        case GHOSTTY_MOUSE_SHAPE_TEXT: cursor = .iBeam
        case GHOSTTY_MOUSE_SHAPE_POINTER: cursor = .pointingHand
        default: cursor = .arrow
        }
        window?.invalidateCursorRects(for: self)
    }

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: cursor)
    }

    override func updateTrackingAreas() {
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .mouseMoved, .inVisibleRect, .activeAlways],
                                       owner: self, userInfo: nil))
        super.updateTrackingAreas()
    }

    // MARK: Mouse

    private func button(_ state: ghostty_input_mouse_state_e, _ button: ghostty_input_mouse_button_e, _ event: NSEvent) {
        guard let surface else { return }
        _ = ghostty_surface_mouse_button(surface, state, button, Self.mods(event.modifierFlags))
    }

    /// A click also claims the keyboard, as with tmux's mouse mode on the
    /// click itself goes to tmux.
    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        button(GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_LEFT, event)
    }

    override func mouseUp(with event: NSEvent) {
        button(GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_LEFT, event)
        if let surface { ghostty_surface_mouse_pressure(surface, 0, 0) }
    }

    override func rightMouseDown(with event: NSEvent) { button(GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_RIGHT, event) }
    override func rightMouseUp(with event: NSEvent) { button(GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_RIGHT, event) }

    override func otherMouseDown(with event: NSEvent) {
        if event.buttonNumber == 2 { button(GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_MIDDLE, event) }
    }

    override func otherMouseUp(with event: NSEvent) {
        if event.buttonNumber == 2 { button(GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_MIDDLE, event) }
    }

    override func mouseMoved(with event: NSEvent) {
        guard let surface else { return }
        let p = convert(event.locationInWindow, from: nil)
        ghostty_surface_mouse_pos(surface, p.x, frame.height - p.y, Self.mods(event.modifierFlags))
    }

    override func mouseDragged(with event: NSEvent) { mouseMoved(with: event) }
    override func rightMouseDragged(with event: NSEvent) { mouseMoved(with: event) }
    override func otherMouseDragged(with event: NSEvent) { mouseMoved(with: event) }

    override func mouseExited(with event: NSEvent) {
        // Off the view: the engine's way of saying no position.
        guard let surface else { return }
        ghostty_surface_mouse_pos(surface, -1, -1, Self.mods(event.modifierFlags))
    }

    override func scrollWheel(with event: NSEvent) {
        guard let surface else { return }
        var x = event.scrollingDeltaX, y = event.scrollingDeltaY
        let precise = event.hasPreciseScrollingDeltas
        if precise {
            x *= 2
            y *= 2
        }
        // The engine's scroll mods: bit 0 for a precise device, then the
        // momentum phase.
        var mods: Int32 = precise ? 1 : 0
        mods |= Int32(Self.momentum(event.momentumPhase)) << 1
        ghostty_surface_mouse_scroll(surface, x, y, mods)
    }

    private static func momentum(_ phase: NSEvent.Phase) -> UInt8 {
        switch phase {
        case .began: return 1
        case .stationary: return 2
        case .changed: return 3
        case .ended: return 4
        case .cancelled: return 5
        case .mayBegin: return 6
        default: return 0
        }
    }

    // MARK: Keyboard

    override func keyDown(with event: NSEvent) {
        guard let surface else {
            interpretKeyEvents([event])
            return
        }
        // The modifiers that make the text, which Ghostty's config can
        // change, as with Option as Alt. The event keeps its hidden bits,
        // which some dead keys need.
        let translated = Self.flags(ghostty_surface_key_translation_mods(surface, Self.mods(event.modifierFlags)))
        var flags = event.modifierFlags
        for flag in [NSEvent.ModifierFlags.shift, .control, .option, .command] {
            if translated.contains(flag) { flags.insert(flag) } else { flags.remove(flag) }
        }
        // The same event when nothing changes: input methods such as
        // Korean need that.
        let translation: NSEvent
        if flags == event.modifierFlags {
            translation = event
        } else {
            translation = NSEvent.keyEvent(
                with: event.type, location: event.locationInWindow, modifierFlags: flags,
                timestamp: event.timestamp, windowNumber: event.windowNumber, context: nil,
                characters: event.characters(byApplyingModifiers: flags) ?? "",
                charactersIgnoringModifiers: event.charactersIgnoringModifiers ?? "",
                isARepeat: event.isARepeat, keyCode: event.keyCode) ?? event
        }
        let action = event.isARepeat ? GHOSTTY_ACTION_REPEAT : GHOSTTY_ACTION_PRESS
        keyTextAccumulator = []
        defer { keyTextAccumulator = nil }
        let markedBefore = markedText.length > 0
        interpretKeyEvents([translation])
        syncPreedit(clearIfNeeded: markedBefore)
        if let texts = keyTextAccumulator, !texts.isEmpty {
            // A composed character.
            for text in texts { _ = key(action, event, translation: translation, text: text) }
        } else {
            _ = key(action, event, translation: translation, text: Self.characters(translation),
                    composing: markedText.length > 0 || markedBefore)
        }
    }

    override func keyUp(with event: NSEvent) {
        _ = key(GHOSTTY_ACTION_RELEASE, event)
    }

    override func flagsChanged(with event: NSEvent) {
        let mod: UInt32
        switch event.keyCode {
        case 0x39: mod = GHOSTTY_MODS_CAPS.rawValue
        case 0x38, 0x3C: mod = GHOSTTY_MODS_SHIFT.rawValue
        case 0x3B, 0x3E: mod = GHOSTTY_MODS_CTRL.rawValue
        case 0x3A, 0x3D: mod = GHOSTTY_MODS_ALT.rawValue
        case 0x37, 0x36: mod = GHOSTTY_MODS_SUPER.rawValue
        default: return
        }
        if hasMarkedText() { return }
        let raw = event.modifierFlags.rawValue
        var action = GHOSTTY_ACTION_RELEASE
        if Self.mods(event.modifierFlags).rawValue & mod != 0 {
            // A press of this side, or a release of it with the other side
            // still held.
            let side: Bool
            switch event.keyCode {
            case 0x3C: side = raw & UInt(NX_DEVICERSHIFTKEYMASK) != 0
            case 0x3E: side = raw & UInt(NX_DEVICERCTLKEYMASK) != 0
            case 0x3D: side = raw & UInt(NX_DEVICERALTKEYMASK) != 0
            case 0x36: side = raw & UInt(NX_DEVICERCMDKEYMASK) != 0
            default: side = true
            }
            if side { action = GHOSTTY_ACTION_PRESS }
        }
        _ = key(action, event)
    }

    /// ⌘ keys belong to the app's menus first, which keep their shortcuts;
    /// one no menu takes then comes to keyDown and Ghostty's bindings. Some
    /// Control keys, such as Control-Return, reach only this, so they go to
    /// keyDown here.
    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        guard event.type == .keyDown, window?.firstResponder === self,
              !event.modifierFlags.contains(.command), event.modifierFlags.contains(.control) else { return false }
        keyDown(with: event)
        return true
    }

    /// Keys AppKit turns into commands, such as Return or an arrow, reach
    /// the terminal through keyDown, so the command itself does nothing,
    /// and no beep.
    override func doCommand(by selector: Selector) {}

    private func key(_ action: ghostty_input_action_e, _ event: NSEvent, translation: NSEvent? = nil,
                     text: String? = nil, composing: Bool = false) -> Bool {
        guard let surface else { return false }
        var ev = ghostty_input_key_s()
        ev.action = action
        ev.keycode = UInt32(event.keyCode)
        ev.mods = Self.mods(event.modifierFlags)
        // Control and Command never make text; the rest did.
        ev.consumed_mods = Self.mods((translation?.modifierFlags ?? event.modifierFlags).subtracting([.control, .command]))
        ev.unshifted_codepoint = 0
        if event.type == .keyDown || event.type == .keyUp,
           let c = event.characters(byApplyingModifiers: [])?.unicodeScalars.first {
            ev.unshifted_codepoint = c.value
        }
        ev.composing = composing
        // Control characters are Ghostty's to encode, so they go as no text.
        if let text, let first = text.utf8.first, first >= 0x20 {
            return text.withCString { ptr in
                ev.text = ptr
                return ghostty_surface_key(surface, ev)
            }
        }
        return ghostty_surface_key(surface, ev)
    }

    /// The text of a key: a control character's without Control, which
    /// Ghostty encodes itself, and none for a function key.
    private static func characters(_ event: NSEvent) -> String? {
        guard let chars = event.characters else { return nil }
        if chars.count == 1, let scalar = chars.unicodeScalars.first {
            if scalar.value < 0x20 { return event.characters(byApplyingModifiers: event.modifierFlags.subtracting(.control)) }
            if scalar.value >= 0xF700 && scalar.value <= 0xF8FF { return nil }
        }
        return chars
    }

    static func mods(_ flags: NSEvent.ModifierFlags) -> ghostty_input_mods_e {
        var m = GHOSTTY_MODS_NONE.rawValue
        if flags.contains(.shift) { m |= GHOSTTY_MODS_SHIFT.rawValue }
        if flags.contains(.control) { m |= GHOSTTY_MODS_CTRL.rawValue }
        if flags.contains(.option) { m |= GHOSTTY_MODS_ALT.rawValue }
        if flags.contains(.command) { m |= GHOSTTY_MODS_SUPER.rawValue }
        if flags.contains(.capsLock) { m |= GHOSTTY_MODS_CAPS.rawValue }
        let raw = flags.rawValue
        if raw & UInt(NX_DEVICERSHIFTKEYMASK) != 0 { m |= GHOSTTY_MODS_SHIFT_RIGHT.rawValue }
        if raw & UInt(NX_DEVICERCTLKEYMASK) != 0 { m |= GHOSTTY_MODS_CTRL_RIGHT.rawValue }
        if raw & UInt(NX_DEVICERALTKEYMASK) != 0 { m |= GHOSTTY_MODS_ALT_RIGHT.rawValue }
        if raw & UInt(NX_DEVICERCMDKEYMASK) != 0 { m |= GHOSTTY_MODS_SUPER_RIGHT.rawValue }
        return ghostty_input_mods_e(m)
    }

    private static func flags(_ mods: ghostty_input_mods_e) -> NSEvent.ModifierFlags {
        var f = NSEvent.ModifierFlags()
        if mods.rawValue & GHOSTTY_MODS_SHIFT.rawValue != 0 { f.insert(.shift) }
        if mods.rawValue & GHOSTTY_MODS_CTRL.rawValue != 0 { f.insert(.control) }
        if mods.rawValue & GHOSTTY_MODS_ALT.rawValue != 0 { f.insert(.option) }
        if mods.rawValue & GHOSTTY_MODS_SUPER.rawValue != 0 { f.insert(.command) }
        return f
    }

    // MARK: Text input (input methods and dead keys)

    func insertText(_ string: Any, replacementRange: NSRange) {
        guard NSApp.currentEvent != nil, let surface else { return }
        let chars = (string as? NSAttributedString)?.string ?? (string as? String) ?? ""
        unmarkText()
        if var acc = keyTextAccumulator {
            acc.append(chars)
            keyTextAccumulator = acc
            return
        }
        let n = chars.utf8.count
        if n > 0 { chars.withCString { ghostty_surface_text(surface, $0, UInt(n)) } }
    }

    func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        if let s = string as? NSAttributedString {
            markedText = NSMutableAttributedString(attributedString: s)
        } else if let s = string as? String {
            markedText = NSMutableAttributedString(string: s)
        }
        if keyTextAccumulator == nil { syncPreedit() }
    }

    func unmarkText() {
        guard markedText.length > 0 else { return }
        markedText.mutableString.setString("")
        syncPreedit()
    }

    private func syncPreedit(clearIfNeeded: Bool = true) {
        guard let surface else { return }
        if markedText.length > 0 {
            let s = markedText.string
            let n = s.utf8.count
            s.withCString { ghostty_surface_preedit(surface, $0, UInt(n)) }
        } else if clearIfNeeded {
            ghostty_surface_preedit(surface, nil, 0)
        }
    }

    func hasMarkedText() -> Bool { markedText.length > 0 }

    func markedRange() -> NSRange {
        markedText.length > 0 ? NSRange(location: 0, length: markedText.length) : NSRange(location: NSNotFound, length: 0)
    }

    func selectedRange() -> NSRange { NSRange(location: NSNotFound, length: 0) }

    func validAttributesForMarkedText() -> [NSAttributedString.Key] { [] }

    func attributedSubstring(forProposedRange range: NSRange, actualRange: NSRangePointer?) -> NSAttributedString? { nil }

    func characterIndex(for point: NSPoint) -> Int { 0 }

    /// Where an input method's window goes: at the terminal's cursor.
    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        guard let surface else { return .zero }
        var x = 0.0, y = 0.0, w = 0.0, h = 0.0
        ghostty_surface_ime_point(surface, &x, &y, &w, &h)
        let rect = convert(NSRect(x: x, y: frame.height - y, width: range.length == 0 ? 0 : w, height: h), to: nil)
        return window?.convertToScreen(rect) ?? rect
    }
}

/// A Ghostty terminal in SwiftUI: a tmux client of one target.
struct GhosttyTerminal: NSViewRepresentable {
    let attach: GhosttyTerminalView.Attach
    let fontSize: CGFloat
    /// Bumped to give it the keyboard, when it follows the app's focus.
    var focusToken = 0
    var followsFocus = true
    var takesKeysOnStart = true
    /// It shows; a terminal that does not neither draws nor takes the
    /// keyboard, and gives it back when it shows again.
    var shows = true
    /// Hands the view over, as the store keeps the main terminal's.
    var onView: ((NSView) -> Void)? = nil

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> GhosttyTerminalView {
        let view = GhosttyTerminalView(attach: attach, fontSize: fontSize)
        view.takesKeysOnStart = takesKeysOnStart && shows
        view.shows = shows
        context.coordinator.focusToken = focusToken
        context.coordinator.shows = shows
        onView?(view)
        return view
    }

    func updateNSView(_ view: GhosttyTerminalView, context: Context) {
        view.fontSize = fontSize
        view.shows = shows
        let c = context.coordinator
        if c.focusToken != focusToken {
            c.focusToken = focusToken
            if followsFocus && shows { view.window?.makeFirstResponder(view) }
        }
        // Back from under a split: the keyboard the split's terminal had,
        // which went with it, comes here.
        if shows, !c.shows, let w = view.window, w.firstResponder === w {
            w.makeFirstResponder(view)
        }
        c.shows = shows
    }

    static func dismantleNSView(_ view: GhosttyTerminalView, coordinator: Coordinator) {
        view.stop()
    }

    final class Coordinator {
        var focusToken = 0
        var shows = true
    }
}

#endif
