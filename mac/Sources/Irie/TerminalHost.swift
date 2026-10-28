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
