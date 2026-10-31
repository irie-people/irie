import Dispatch
import Foundation

/// How often the window takes in what the core sends (DESIGN §2.1,
/// principle 7). Every change to the store draws the window's views again,
/// and with many agents at work the core has news many times a second. On
/// a busy machine each drawing takes longer, and while the main thread
/// draws, a click or a key waits behind it. So the frames are gathered off
/// the main thread and go in a batch at a time (Batcher): at once after a
/// quiet spell, then at most every `interval`, which is longer while the
/// machine is busy and while another app is in front. What the user does
/// is never held: the views answer a click at once, and only the news from
/// the core waits for its turn.
final class Pace: @unchecked Sendable {
    static let shared = Pace()

    private let lock = NSLock()
    private var busy = false
    private var active = true

    /// The least time between two batches.
    var interval: TimeInterval {
        lock.lock()
        defer { lock.unlock() }
        if !active { return 0.5 }
        return busy ? 0.25 : 0.1
    }

    /// The core counts the machine busy (Store, the header's busy).
    func set(busy: Bool) {
        lock.lock()
        self.busy = busy
        lock.unlock()
    }

    /// The app is in front.
    func set(active: Bool) {
        lock.lock()
        self.active = active
        lock.unlock()
    }
}

/// Gathers what one stream from the core sends, on the thread that reads
/// it, and hands it to the main thread in batches, in order (Pace). Every
/// change made in one batch is drawn once.
final class Batcher<Item>: @unchecked Sendable {
    private let lock = NSLock()
    private var pending: [Item] = []
    private var due = false
    private var last = DispatchTime(uptimeNanoseconds: 0)
    private let deliver: @MainActor ([Item]) -> Void
    private let pace: Pace

    init(pace: Pace = .shared, _ deliver: @escaping @MainActor ([Item]) -> Void) {
        self.pace = pace
        self.deliver = deliver
    }

    /// Takes one item, from any thread.
    func add(_ item: Item) {
        lock.lock()
        pending.append(item)
        let schedule = !due
        due = true
        let next = last + pace.interval
        lock.unlock()
        guard schedule else { return }
        // The main queue runs in every mode of the main run loop, so a batch
        // goes in while a menu is open too; the store holds it then
        // (Store.take(_:)).
        DispatchQueue.main.asyncAfter(deadline: max(.now(), next)) { [self] in
            MainActor.assumeIsolated { flush() }
        }
    }

    @MainActor private func flush() {
        lock.lock()
        let batch = pending
        pending = []
        due = false
        last = .now()
        lock.unlock()
        if !batch.isEmpty { deliver(batch) }
    }
}
