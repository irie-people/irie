import AppKit
import SwiftUI

/// `Irie --render-onboarding DIR` draws what a first run shows, offscreen
/// with ImageRenderer, from fixtures rather than a core, in light and dark
/// mode (Onboarding.swift): the welcome, ready and with a sign-in to do;
/// the banner over the cards; the first task's steps; and each way the
/// sidebar says Irie isn't running.
@MainActor
enum OnboardingSnapshot {
    static func run(into dir: String) -> Never {
        NSApplication.shared.setActivationPolicy(.accessory)
        let ready = ReadyReport(items: [
            ReadyItem(id: "tmux", name: "tmux", status: "ok", version: "3.7b", purpose: "keeps your agents running when no window is open", blocks: true),
            ReadyItem(id: "claude", name: "Claude Code", status: "ok", version: "2.1.316", purpose: "the coding agent Irie starts for you", blocks: true),
            ReadyItem(id: "signin", name: "Signed in", status: "ok", purpose: "Claude Code works on your Claude account", blocks: true),
            ReadyItem(id: "git", name: "git", status: "ok", version: "2.47.1", purpose: "gives each task on a project its own worktree"),
        ])
        var signedOut = ready
        signedOut.items[2] = ReadyItem(id: "signin", name: "Signed in", status: "missing", purpose: "lets Claude Code work on your account",
                                       problem: "Claude Code isn’t signed in", blocks: true,
                                       fix: ReadyFix(say: "Sign in to Claude Code in your browser", show: "claude auth login"))
        var shots: [(String, Store, CGFloat?)] = []
        func store(_ edit: (Store) -> Void) -> Store {
            let s = Store(socketPath: "/nonexistent/irie-snapshot.sock")
            edit(s)
            return s
        }
        shots.append(("welcome-ready", store { $0.readiness = ready }, nil))
        shots.append(("welcome-signed-out", store { $0.readiness = signedOut }, nil))
        shots.append(("welcome-task-bar", store { $0.readiness = ready; $0.coach = Coach() }, nil))
        let first = Fixture.sessions[0]
        func coached(_ state: String, _ c: Coach) -> Store {
            store {
                var s = first
                s.state = state
                $0.loadFixture([s], selection: nil)
                var c = c
                c.session = s.id
                $0.coach = c
            }
        }
        shots.append(("coach-working", coached("working", Coach(started: true)), nil))
        shots.append(("coach-needs-you", coached("needs_you", Coach(started: true, asked: true)), nil))
        shots.append(("coach-your-move", coached("done", Coach(started: true, last: "done")), nil))
        shots.append(("coach-done", coached("done", Coach(started: true, asked: true, answered: true, done: true)), nil))
        shots.append(("coach-stalled", coached("stalled", Coach(started: true)), nil))
        shots.append(("banner", store { $0.loadFixture(Fixture.sessions, selection: nil); $0.readiness = signedOut }, nil))
        shots.append(("offline-starting", store { $0.starting = true }, 300))
        shots.append(("offline-failed", store { $0.startProblem = .failed("tmux not found: `irie setup` installs it, or set tmux.path", bin: "/home/alice/.local/share/irie/bin/irie") }, 300))
        shots.append(("offline-not-installed", store { $0.startProblem = .notInstalled }, 300))
        shots.append(("offline", store { _ in }, 300))
        do {
            try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            for (name, s, height) in shots {
                for dark in [false, true] {
                    Theme.use(Palette.standard.id)
                    let file = URL(fileURLWithPath: dir).appendingPathComponent("\(name)-\(dark ? "dark" : "light").png")
                    try render(page(name, s, height: height), dark: dark, to: file)
                    print(file.path)
                }
            }
        } catch {
            FileHandle.standardError.write(Data("render-onboarding: \(error.localizedDescription)\n".utf8))
            exit(1)
        }
        exit(0)
    }

    /// The part of the sidebar each shot is about, at the sidebar's width.
    @ViewBuilder private static func page(_ name: String, _ s: Store, height: CGFloat?) -> some View {
        Group {
            if name.hasPrefix("welcome") {
                WelcomeContent()
            } else if name.hasPrefix("offline") {
                Offline(why: "Irie isn’t running.", socket: "/tmp/irie-501/core.sock").frame(height: height)
            } else {
                VStack(spacing: 0) {
                    ReadyBanner()
                    CoachCard()
                    Spacer(minLength: 12)
                }
            }
        }
        .frame(width: 300)
        .background(Theme.sidebar)
        .environmentObject(s)
    }

    private static func render(_ view: some View, dark: Bool, to url: URL) throws {
        let appearance = NSAppearance(named: dark ? .darkAqua : .aqua)!
        var data: Data?
        appearance.performAsCurrentDrawingAppearance {
            let r = ImageRenderer(content: view.environment(\.colorScheme, dark ? .dark : .light))
            r.scale = 2
            if let cg = r.cgImage {
                data = NSBitmapImageRep(cgImage: cg).representation(using: .png, properties: [:])
            }
        }
        guard let data else { throw CocoaError(.fileWriteUnknown) }
        try data.write(to: url)
    }
}
