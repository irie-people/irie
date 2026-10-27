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

    func makeCoordinator() -> Coordinator { Coordinator(info: info) }

    func makeNSView(context: Context) -> IrieTerminalView {
        let view = IrieTerminalView(frame: .zero)
        view.processDelegate = context.coordinator
        view.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        view.nativeBackgroundColor = Theme.terminalBackground
        view.nativeForegroundColor = Theme.terminalForeground
        view.onKeyboard = { [weak store] has in
            if let store, store.terminalHasKeys != has { store.terminalHasKeys = has }
        }
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
            view.window?.makeFirstResponder(view)
        }
    }

    final class Coordinator: NSObject, LocalProcessTerminalViewDelegate {
        var info: AttachInfo
        var focusToken = 0
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
    private var responder: NSKeyValueObservation?
    /// Told whether the terminal has the keyboard.
    var onKeyboard: ((Bool) -> Void)?
    /// Whether it takes the keyboard once it is in a window; not while the
    /// conversation shows over it.
    var takesKeysOnStart = true

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        // The window's first responder is key-value observable. Report after
        // this turn, since SwiftUI may be updating views when focus moves.
        responder = window?.observe(\.firstResponder) { [weak self] _, _ in
            DispatchQueue.main.async {
                guard let self else { return }
                self.onKeyboard?(self.window?.firstResponder === self)
            }
        }
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

    /// With tmux's mouse mode on, SwiftTerm hands the click to tmux and never
    /// takes the keyboard, so a click claims it here.
    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        super.mouseDown(with: event)
    }

    /// A mouse selection is tmux's, and tmux already put it on the pasteboard
    /// (OSC 52). Copying SwiftTerm's empty selection would wipe that, so ⌘C
    /// copies only a selection SwiftTerm made itself (Shift-drag).
    override func copy(_ sender: Any) {
        guard selection.active else { return }
        super.copy(sender)
    }
}
#endif
