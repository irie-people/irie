#if canImport(SwiftTerm)
import AppKit
import MetalKit
import SwiftTerm
import SwiftUI

/// SwiftTerm's terminal view running a tmux client of the main session. The
/// core keeps the slot zoomed while the app is open, so this shows the shown
/// agent and nothing else.
struct TerminalHost: NSViewRepresentable {
    let info: AttachInfo
    let focusToken: Int
    let fontSize: CGFloat
    let store: Store
    /// It shows, so a focus request is its; not while a split stands in
    /// for it (TeamSplit.swift).
    var active = true

    func makeCoordinator() -> Coordinator { Coordinator(info: info) }

    func makeNSView(context: Context) -> IrieTerminalView {
        let view = IrieTerminalView(frame: .zero)
        view.processDelegate = context.coordinator
        view.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        view.nativeBackgroundColor = Theme.terminalBackground
        view.nativeForegroundColor = Theme.terminalForeground
        view.selectedTextBackgroundColor = Theme.terminalSelection
        store.terminalView = view
        view.takesKeysOnStart = store.showTerminal
        context.coordinator.view = view
        context.coordinator.focusToken = focusToken
        context.coordinator.attach()
        return view
    }

    func updateNSView(_ view: IrieTerminalView, context: Context) {
        context.coordinator.info = info // used by the next attach
        if view.font.pointSize != fontSize {
            view.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        }
        if context.coordinator.focusToken != focusToken {
            context.coordinator.focusToken = focusToken
            if active { view.window?.makeFirstResponder(view) }
        }
        // Back from a split: the keyboard the split's terminal had, which
        // went with it, comes here.
        if active, !context.coordinator.active, let w = view.window, w.firstResponder === w {
            w.makeFirstResponder(view)
        }
        context.coordinator.active = active
    }

    final class Coordinator: NSObject, LocalProcessTerminalViewDelegate {
        var info: AttachInfo
        var focusToken = 0
        var active = true
        weak var view: IrieTerminalView?

        init(info: AttachInfo) { self.info = info }

        func attach() {
            var env = Terminal.getEnvironmentVariables(termName: "xterm-256color")
            env.removeAll { $0.hasPrefix("TMUX=") || $0.hasPrefix("TMUX_PANE=") }
            view?.startProcess(executable: info.tmux, args: ["-S", info.socket, "attach-session", "-t", info.target],
                               environment: env, execName: "tmux")
        }

        func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {}
        func setTerminalTitle(source: LocalProcessTerminalView, title: String) {}
        func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}

        /// tmux detached or its server went away: attach again shortly.
        func processTerminated(source: TerminalView, exitCode: Int32?) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) { [weak self] in self?.attach() }
        }
    }
}

/// Draws with Metal once it is in a window. SwiftTerm compiles its shaders
/// from Shaders.metal in the app's resources at run time, so no Xcode is
/// needed; without Metal it keeps drawing with Core Graphics.
final class IrieTerminalView: LocalProcessTerminalView {
    private var triedMetal = false
    /// Whether it takes the keyboard once it is in a window; not while the
    /// conversation shows over it.
    var takesKeysOnStart = true

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window, !triedMetal else { return }
        triedMetal = true
        try? setUseMetal(true)
        if takesKeysOnStart {
            window.makeFirstResponder(self) // type to the agent straight away
        }
    }

    /// SwiftTerm's Metal view covers the terminal and takes no keyboard
    /// focus, so clicks on it count as clicks on the terminal.
    override func hitTest(_ point: NSPoint) -> NSView? {
        let hit = super.hitTest(point)
        return hit is MTKView ? self : hit
    }

    // A drag selects in the terminal itself, as in the Ghostty terminal
    // (TerminalSelection.swift): SwiftTerm's mouse reporting is off while
    // the button is down, so it selects and tmux hears nothing. A click
    // still goes to tmux, and to the program in the terminal if it asks.

    /// The press that waits to be known as a click or a drag.
    private var pressed: NSEvent?
    /// The press is selecting, with mouse reporting off until it ends.
    private var selecting = false
    /// A finished selection holds with mouse reporting still off: SwiftTerm
    /// drops its selection at any output while it reports the mouse, and
    /// tmux draws all the time. The next click, key or wheel lets it go,
    /// with reporting on again first, so each goes on as before; this
    /// watches for the key and the wheel, which SwiftTerm keeps to itself.
    private var holding: Any?

    deinit {
        if let holding { NSEvent.removeMonitor(holding) }
    }

    /// With tmux's mouse mode on, SwiftTerm hands a click to tmux and never
    /// takes the keyboard, so a press claims it here. A second or third
    /// click selects a word or a line at once, and a Shift-click extends
    /// the selection at once; a plain press waits.
    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        pressed = nil
        if selecting { finishSelecting(event) } // its button-up went astray
        letGo()
        if event.clickCount > 1 || event.modifierFlags.contains(.shift) {
            selecting = true
            allowMouseReporting = false
            super.mouseDown(with: event)
        } else {
            pressed = event
        }
    }

    override func mouseDragged(with event: NSEvent) {
        if let down = pressed, TerminalSelection.isDrag(from: down, to: event) {
            pressed = nil
            selecting = true
            allowMouseReporting = false
            super.mouseDown(with: down)
            // SwiftTerm starts a drag's selection where its first move is,
            // so the first move given it is where the button went down.
            if let start = NSEvent.mouseEvent(with: .leftMouseDragged, location: down.locationInWindow,
                                              modifierFlags: down.modifierFlags, timestamp: event.timestamp,
                                              windowNumber: event.windowNumber, context: nil, eventNumber: 0,
                                              clickCount: 1, pressure: 1) {
                super.mouseDragged(with: start)
            }
        }
        if selecting { super.mouseDragged(with: event) }
    }

    override func mouseUp(with event: NSEvent) {
        if let down = pressed {
            // A click: tmux has it where the button went down, and the
            // program in the terminal too, if it asks. The selection goes,
            // as the Ghostty terminal's does.
            pressed = nil
            selection.active = false
            super.mouseDown(with: down)
            super.mouseUp(with: event)
        } else if selecting {
            finishSelecting(event)
        }
    }

    /// Ends a selection where the button came up: the selection is copied
    /// as Settings says, and holds until the next click, key or wheel.
    private func finishSelecting(_ event: NSEvent) {
        super.mouseUp(with: event)
        selecting = false
        copySelection(asked: false)
        if selection.active { hold() } else { allowMouseReporting = true }
    }

    private func hold() {
        guard holding == nil else { return }
        holding = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .scrollWheel]) { [weak self] event in
            guard let self, event.window === self.window else { return event }
            let mine = event.type == .keyDown
                ? self.window?.firstResponder === self && !event.modifierFlags.contains(.command) // ⌘C keeps it
                : self.bounds.contains(self.convert(event.locationInWindow, from: nil))
            if mine { self.letGo() }
            return event
        }
    }

    /// Turns mouse reporting back on, as tmux wants it, once nothing holds.
    private func letGo() {
        if let holding { NSEvent.removeMonitor(holding) }
        holding = nil
        if !selecting { allowMouseReporting = true }
    }

    /// A right-click or a Control-click opens the terminal's own menu
    /// (TerminalSelection.menu), whose Select All holds as a drag does.
    override func menu(for event: NSEvent) -> NSMenu? {
        let menu = TerminalSelection.menu(for: event, target: self, canCopy: selection.active)
        menu?.items.last?.action = #selector(selectAllAndHold(_:))
        return menu
    }

    @objc private func selectAllAndHold(_ sender: Any?) {
        allowMouseReporting = false
        selectAll(sender)
        hold()
    }

    override func rightMouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        super.rightMouseDown(with: event)
    }

    /// ⌘C, and Copy in the terminal's menu.
    override func copy(_ sender: Any) {
        copySelection(asked: true)
    }

    /// Copies SwiftTerm's selection, and says so: when asked, or when a drag
    /// or a click made it and Settings keeps copying on. With no selection,
    /// or only blanks, the pasteboard keeps what it has.
    private func copySelection(asked: Bool) {
        guard asked || TerminalSelection.copies, selection.active else { return }
        let text = selection.getSelectedText()
        guard TerminalSelection.worthCopying(text) else { return }
        super.copy(self)
        TerminalSelection.copied(text)
    }
}

/// One terminal of the split (TeamSplit.swift): a tmux client of a session
/// that is not the main one, in its own process, ended when it goes.
struct SplitTerminal: NSViewRepresentable {
    let tmux: String
    let socket: String
    let target: String
    let fontSize: CGFloat
    let focusToken: Int
    let followsFocus: Bool
    let takesKeysOnStart: Bool

    func makeCoordinator() -> Coordinator { Coordinator(tmux: tmux, socket: socket, target: target) }

    func makeNSView(context: Context) -> IrieTerminalView {
        let view = IrieTerminalView(frame: .zero)
        view.processDelegate = context.coordinator
        view.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        view.nativeBackgroundColor = Theme.terminalBackground
        view.nativeForegroundColor = Theme.terminalForeground
        view.selectedTextBackgroundColor = Theme.terminalSelection
        view.takesKeysOnStart = takesKeysOnStart
        context.coordinator.view = view
        context.coordinator.focusToken = focusToken
        context.coordinator.attach()
        return view
    }

    func updateNSView(_ view: IrieTerminalView, context: Context) {
        if view.font.pointSize != fontSize {
            view.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        }
        if context.coordinator.focusToken != focusToken {
            context.coordinator.focusToken = focusToken
            if followsFocus { view.window?.makeFirstResponder(view) }
        }
    }

    static func dismantleNSView(_ view: IrieTerminalView, coordinator: Coordinator) {
        coordinator.gone = true
        view.terminate()
    }

    final class Coordinator: NSObject, LocalProcessTerminalViewDelegate {
        let tmux: String
        let socket: String
        let target: String
        var focusToken = 0
        var gone = false
        private var tries = 0
        weak var view: IrieTerminalView?

        init(tmux: String, socket: String, target: String) {
            self.tmux = tmux
            self.socket = socket
            self.target = target
        }

        func attach() {
            guard !gone, let view else { return }
            var env = Terminal.getEnvironmentVariables(termName: "xterm-256color")
            env.removeAll { $0.hasPrefix("TMUX=") || $0.hasPrefix("TMUX_PANE=") }
            view.startProcess(executable: tmux, args: ["-S", socket, "attach-session", "-t", target], environment: env, execName: "tmux")
        }

        func sizeChanged(source: LocalProcessTerminalView, newCols: Int, newRows: Int) {}
        func setTerminalTitle(source: LocalProcessTerminalView, title: String) {}
        func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}

        /// tmux detached, or its session is not there yet, as the view
        /// session in the moment the split opens: attach again soon.
        func processTerminated(source: TerminalView, exitCode: Int32?) {
            guard !gone else { return }
            tries += 1
            DispatchQueue.main.asyncAfter(deadline: .now() + (tries < 10 ? 0.2 : 1)) { [weak self] in self?.attach() }
        }
    }
}
#endif
