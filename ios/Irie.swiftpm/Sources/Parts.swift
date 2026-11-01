import SwiftUI
import UIKit

// The small pieces every screen uses: buttons, the session actions for a
// menu or a long press, and toasts.

enum Screen {
    /// The height of the screen the app is on, as it is turned now.
    static var height: CGFloat {
        let scene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
        return scene?.screen.bounds.height ?? 800
    }
}

/// A press that shrinks a little, as the web app's cards and buttons do.
struct PressStyle: ButtonStyle {
    var scale: CGFloat = 0.97

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? scale : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

struct PrimaryButton: ButtonStyle {
    var small = false
    var wide = false
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(Theme.rounded(small ? 14.5 : 17, .semibold))
            .foregroundStyle(enabled ? Theme.onPrimary : Theme.muted)
            .padding(.horizontal, small ? 14 : 18)
            .frame(minHeight: small ? 36 : 50)
            .frame(maxWidth: wide ? .infinity : nil)
            .background(enabled ? Theme.accent : Theme.edge, in: Capsule())
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

struct SecondaryButton: ButtonStyle {
    var small = false
    var danger = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(Theme.rounded(small ? 14.5 : 16, .semibold))
            .foregroundStyle(danger ? Theme.needs : Theme.ink)
            .padding(.horizontal, small ? 14 : 18)
            .frame(minHeight: small ? 36 : 44)
            .background(danger ? Color.clear : Theme.card, in: Capsule())
            .overlay(Capsule().strokeBorder(danger ? Color.clear : Theme.edge, lineWidth: 1))
            .scaleEffect(configuration.isPressed ? 0.97 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

/// The round send button of the task bar and the message box.
@MainActor struct SendButton: View {
    var enabled: Bool
    var busy: Bool
    var label: String
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Circle().fill(enabled ? Theme.accent : Theme.edge)
                if busy {
                    ProgressView().controlSize(.small).tint(Theme.onPrimary)
                } else {
                    Image(systemName: "arrow.up").font(.system(size: 17, weight: .bold))
                        .foregroundStyle(enabled ? Theme.onPrimary : Theme.muted)
                }
            }
            .frame(width: 34, height: 34)
            .contentShape(Circle().inset(by: -6))
        }
        .buttonStyle(PressStyle(scale: 0.9))
        .disabled(!enabled || busy)
        .accessibilityLabel(label)
    }
}

/// What can be done to a session, for its menu and its long press.
@MainActor struct SessionActions: View {
    @Environment(AppModel.self) private var app
    let conn: Conn
    let s: Session
    var inSession = false

    var body: some View {
        let look = Look(s)
        let ended = look.ended
        if !inSession {
            Button { app.open(conn, s.id) } label: { Label("Open", systemImage: "arrow.up.forward.app") }
        }
        if conn.can("park") {
            Button {
                Task { await app.setParked(conn, s, !s.isParked) }
            } label: {
                Label(s.isParked ? "Unpark" : "Park", systemImage: s.isParked ? "tray.and.arrow.up" : "tray.and.arrow.down")
            }
        }
        if !ended && conn.can("freeze") {
            if s.isFrozen {
                Button { Task { await app.op(conn, "thaw", ["session": s.id], done: "Thawed “\(s.name)”") } } label: {
                    Label("Thaw", systemImage: "play")
                }
            } else {
                Button { Task { await app.op(conn, "freeze", ["session": s.id], undo: "Froze “\(s.name)”") } } label: {
                    Label("Freeze", systemImage: "snowflake")
                }
            }
        }
        if !ended && conn.can("priority") {
            if s.isPromoted {
                Button { Task { await app.op(conn, "priority", ["session": s.id, "level": 0], done: "Put “\(s.name)” back") } } label: {
                    Label("Put back", systemImage: "arrow.down.to.line")
                }
            } else {
                Button { Task { await app.op(conn, "priority", ["session": s.id, "level": 1], done: "Promoted “\(s.name)”") } } label: {
                    Label("Promote", systemImage: "arrow.up.to.line")
                }
            }
        }
        if conn.can("rate") {
            Menu {
                Button { Task { await app.op(conn, "rate", ["session": s.id, "verdict": "good"], done: "Rated good. Irie’s router learns from it.") } } label: {
                    Label("Good", systemImage: s.rated == "good" ? "checkmark" : "hand.thumbsup")
                }
                Button { Task { await app.op(conn, "rate", ["session": s.id, "verdict": "bad"], done: "Rated bad. Irie’s router learns from it.") } } label: {
                    Label("Bad", systemImage: s.rated == "bad" ? "checkmark" : "hand.thumbsdown")
                }
            } label: {
                Label(s.rated.map { "Rated \($0)" } ?? "Rate", systemImage: "hand.thumbsup")
            }
        }
        if conn.can("rename") {
            Button {
                app.renameText = s.name
                app.renaming = SessionPrompt(conn: conn, session: s)
            } label: { Label("Rename…", systemImage: "pencil") }
        }
        if inSession && !ended && look != .starting && conn.can("keys") {
            Button {
                Task { await app.op(conn, "keys", ["session": s.id, "keys": ["Escape"]], done: "Sent Esc.") }
            } label: { Label("Stop the turn", systemImage: "stop.circle") }
        }
        if conn.can("close") {
            Divider()
            Button(role: .destructive) {
                app.closing = SessionPrompt(conn: conn, session: s)
            } label: { Label("Close", systemImage: "xmark") }
        }
    }
}

/// Toasts float above the bottom bar, the newest last.
@MainActor struct ToastStack: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        VStack(spacing: 8) {
            ForEach(app.toasts) { t in
                HStack(spacing: 4) {
                    Text(t.text)
                        .font(.system(size: 15))
                        .foregroundStyle(t.error ? Theme.toastErrorInk : Theme.toastInk)
                        .padding(.vertical, 6)
                        .fixedSize(horizontal: false, vertical: true)
                    if let label = t.actionLabel, let run = t.action {
                        Button {
                            app.dismiss(t.id)
                            run()
                        } label: {
                            Text(label).font(Theme.rounded(15, .bold))
                                .foregroundStyle(t.error ? Theme.toastErrorInk : Theme.toastAction)
                                .padding(.horizontal, 14).frame(minHeight: 36)
                                .background(.white.opacity(0.14), in: Capsule())
                        }
                        .buttonStyle(PressStyle())
                    }
                }
                .padding(.leading, 16).padding(.trailing, t.actionLabel == nil ? 16 : 6).padding(.vertical, 5)
                .frame(minHeight: 46)
                .background(t.error ? Theme.toastErrorBack : Theme.toastBack, in: RoundedRectangle(cornerRadius: 23, style: .continuous))
                .shadow(color: .black.opacity(0.22), radius: 13, y: 8)
                .onTapGesture { app.dismiss(t.id) }
                .transition(.move(edge: .bottom).combined(with: .opacity))
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isStaticText)
            }
        }
        .padding(.horizontal, 12)
        .frame(maxWidth: 560)
    }
}

/// A dot for a machine's status.
@MainActor struct StatusDot: View {
    var status: ConnStatus

    var body: some View {
        Circle().fill(status.color)
            .frame(width: 8, height: 8)
            .background(Circle().fill(status == .connected ? Theme.done.opacity(0.25) : .clear).frame(width: 14, height: 14))
            .opacity(status == .offline ? 0.8 : 1)
            .accessibilityHidden(true)
    }
}

/// A line of the core's words above what a machine said last.
@MainActor struct Banner<Trailing: View>: View {
    var text: String
    var loud = false
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack(spacing: 10) {
            Text(text)
                .font(.system(size: 14.5, weight: loud ? .semibold : .regular))
                .foregroundStyle(loud ? Theme.needs : Theme.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
            trailing()
        }
        .padding(.leading, 14).padding(.trailing, 10).padding(.vertical, 10)
        .background(loud ? Theme.needsTint : Theme.panel, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(loud ? Theme.needs.opacity(0.4) : Theme.edge, lineWidth: 1))
    }
}
