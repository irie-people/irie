import AppKit
import Observation
import SwiftUI

/// While the sidebar's edge or the window's own is dragged, the terminal
/// keeps the size it had, and takes the new one once the drag is let go: one
/// resize of the shown agent, and of every agent the core sizes to the slot
/// (DESIGN §2.3), not one at every step of the drag, as the right sidebar's
/// edge already does. Meanwhile the frame round the terminal follows the
/// drag, the terminal drawn from its top left and cut off where the frame
/// is smaller, on the terminal's own ground where it is larger.
@MainActor
@Observable
final class ResizeHold {
    static let shared = ResizeHold()

    /// The size the terminal keeps while a drag lasts.
    private(set) var held: CGSize?
    /// The room the terminal had at the last layout, which a drag holds it
    /// to as it starts.
    @ObservationIgnored var room: CGSize = .zero
    @ObservationIgnored private var watching: [NSObjectProtocol] = []
    @ObservationIgnored private var poll: Timer?

    /// Follows the window's live resizes and the drags of its sidebar's edge.
    func watch(_ window: NSWindow) {
        guard watching.isEmpty else { return }
        let center = NotificationCenter.default
        watching.append(center.addObserver(forName: NSWindow.willStartLiveResizeNotification, object: window, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.hold() }
        })
        watching.append(center.addObserver(forName: NSWindow.didEndLiveResizeNotification, object: window, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.letGo() }
        })
        // The split view says before each step of a drag of its divider; a
        // step with the button down is the user's drag, and not a toggle
        // or a layout's own move.
        watching.append(center.addObserver(forName: NSSplitView.willResizeSubviewsNotification, object: nil, queue: .main) { [weak self, weak window] note in
            MainActor.assumeIsolated {
                guard let self, NSEvent.pressedMouseButtons & 1 != 0, let split = note.object as? NSSplitView,
                      split.window === window, self.poll == nil, self.held == nil, split === WindowPanes.splitView else { return }
                self.hold()
                self.untilLetGo()
            }
        })
    }

    private func hold() {
        guard held == nil, room.width > 0, room.height > 0 else { return }
        held = room
    }

    /// A divider's drag runs its own event loop, which keeps the button's
    /// release from the app's monitors, so the button is looked at instead,
    /// in every run loop mode, until it is up.
    private func untilLetGo() {
        guard poll == nil else { return }
        let timer = Timer(timeInterval: 0.05, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, NSEvent.pressedMouseButtons & 1 == 0 else { return }
                self.poll?.invalidate()
                self.poll = nil
                // A window still in a live resize lets go at its end.
                if WindowPanes.window?.inLiveResize != true { self.letGo() }
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        poll = timer
    }

    private func letGo() {
        if held != nil { held = nil }
    }
}

/// The terminal at the size a drag holds it to (ResizeHold), or at all the
/// room it has.
struct HeldSize: ViewModifier {
    func body(content: Content) -> some View {
        let held = ResizeHold.shared.held
        content
            .frame(width: held?.width, height: held?.height, alignment: .topLeading)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .clipped()
            .background(GeometryReader { g in
                Color.clear.onChange(of: g.size, initial: true) { ResizeHold.shared.room = g.size }
            })
    }
}
