import AppKit
import CryptoKit
import Foundation

// The files open in code cards (CodePane.swift). Each file is read once into
// one text, which every card showing it edits, and which is saved as you
// type, a moment after the last key, keeping the file's line endings,
// permissions and links. A file that changes on disk while it is open, as
// when the session's agent edits it, comes in at once while nothing typed
// is unsaved: only its changed lines change, so the caret stays where it
// was, the lines are marked in the gutter, and ⌘Z takes the change back.
// While something typed is unsaved, the card asks which to keep. Typing
// that can't reach its file, as in a conflict, a file gone or a failed save,
// waits in a rescue file beside the app's own until it can, and comes back
// as a conflict when the file next opens, so a quit or a kill loses none.

/// How a file stands on disk: enough to tell another's change from our own
/// save.
struct DiskStamp: Equatable, Sendable {
    var modified: Int64
    var size: Int64
    var inode: UInt64

    /// The file's stamp, its links followed; nil when it is gone or not a
    /// plain file.
    static func of(_ path: String) -> DiskStamp? {
        var st = stat()
        guard stat(path, &st) == 0, st.st_mode & S_IFMT == S_IFREG else { return nil }
        return DiskStamp(modified: Int64(st.st_mtimespec.tv_sec) * 1_000_000_000 + Int64(st.st_mtimespec.tv_nsec),
                         size: Int64(st.st_size), inode: UInt64(st.st_ino))
    }
}

/// A file's text as a code card edits it, and how it stands against the disk.
@MainActor
@Observable
final class CodeDocument {
    enum Phase: Equatable {
        case loading, ready
        /// Gone from disk, or moved: the text stays, to save again.
        case missing
        /// Not text this card can edit: not UTF-8, or binary.
        case notText
        case tooBig
        case failed(String)
    }

    /// The biggest file a card opens, as the core's card op allows.
    static let biggest = 8 << 20
    /// How long after the last key the file is saved.
    static let pause = 0.6

    /// The path as it was asked for, and the file it names, its links
    /// followed, which is the one written.
    let path: String
    let real: String
    private(set) var language: CodeLanguage
    /// The text every card showing the file edits, and its own undo.
    @ObservationIgnored let storage = NSTextStorage()
    @ObservationIgnored let undo = UndoManager()
    @ObservationIgnored private(set) var colorer: SyntaxColorer
    private(set) var phase: Phase = .loading
    /// Something typed is not on disk yet.
    private(set) var dirty = false
    private(set) var saving = false
    private(set) var savedAt: Date?
    /// Why the last save failed, until one works.
    private(set) var problem: String?
    /// The disk changed while something typed was unsaved: the disk's text
    /// waits here until the card says which to keep.
    private(set) var conflict = false
    @ObservationIgnored private var theirs: String?
    /// What came from outside last, as ranges of the text that move with
    /// edits; an empty one is where lines were taken out. Drawn in the
    /// gutter.
    private(set) var marks: [NSRange] = []
    private(set) var outsideAt: Date?
    /// Bumped at every change of the text, typed or not, for views that
    /// draw from it, such as the gutter.
    private(set) var revision = 0
    /// What one indent is in this file (Tab and ⌘] put it in).
    private(set) var indent = "    "

    /// How the text is drawn; set by the cards showing it.
    @ObservationIgnored private(set) var theme: CodeTheme
    @ObservationIgnored private(set) var font: NSFont

    @ObservationIgnored private var stamp: DiskStamp?
    /// The text as last read or saved, with "\n" line ends.
    @ObservationIgnored private var base = ""
    /// The file ends its lines with "\r\n", which the card shows as "\n".
    @ObservationIgnored private var crlf = false
    @ObservationIgnored private var edits = 0
    @ObservationIgnored private var savedEdits = 0
    /// Applying the disk's text: not typing, so nothing to save.
    @ObservationIgnored private var quiet = false
    /// Putting a whole text in, which the colorer then reads afresh.
    @ObservationIgnored private var bulk = false
    /// The text views showing it, which an outside change keeps from
    /// running typing on into it.
    @ObservationIgnored let views = NSHashTable<NSTextView>.weakObjects()
    @ObservationIgnored private var saveTask: Task<Void, Never>?
    @ObservationIgnored private var colouring: Task<Void, Never>?
    @ObservationIgnored private var checking = false
    @ObservationIgnored private var lineStarts: [Int]?
    /// The range edits have left to colour, as the text is now.
    @ObservationIgnored private var toColour: NSRange?
    @ObservationIgnored private lazy var watch = StorageWatch(doc: self)

    init(path: String, theme: CodeTheme, font: NSFont) {
        self.path = path
        real = URL(fileURLWithPath: path).resolvingSymlinksInPath().path
        let language = CodeLanguage.detect(path: path, firstLine: nil)
        self.language = language
        colorer = SyntaxColorer(language: language)
        self.theme = theme
        self.font = font
        undo.levelsOfUndo = 100
        storage.delegate = watch
    }

    var name: String { (path as NSString).lastPathComponent }

    // MARK: Reading

    /// Reads the file, once, off the main thread.
    func load() async {
        let real = real
        let read = await Task.detached(priority: .userInitiated) { Self.read(real) }.value
        switch read {
        case let .text(text, crlf, stamp):
            self.crlf = crlf
            self.stamp = stamp
            base = text
            language = CodeLanguage.detect(path: path, firstLine: text.prefix(200).split(separator: "\n", maxSplits: 1).first.map(String.init))
            colorer = SyntaxColorer(language: language)
            indent = Self.indent(of: text, tabs: language.usesTabs)
            bulk = true
            storage.beginEditing()
            storage.setAttributedString(NSAttributedString(string: text, attributes: baseAttributes))
            storage.endEditing()
            bulk = false
            colorer.load(storage.mutableString)
            lineStarts = nil
            revision += 1
            phase = .ready
            recolourAll()
            restore()
        case .gone: phase = .missing
        case .notText: phase = .notText
        case .tooBig: phase = .tooBig
        case let .failed(why): phase = .failed(why)
        }
    }

    private enum Read: Sendable {
        case text(String, crlf: Bool, stamp: DiskStamp?)
        case gone, notText, tooBig
        case failed(String)
    }

    /// `--snapshot`'s file: its text, with no disk under it.
    func loadFixture(_ text: String) {
        base = text
        language = CodeLanguage.detect(path: path, firstLine: nil)
        colorer = SyntaxColorer(language: language)
        indent = Self.indent(of: text, tabs: language.usesTabs)
        bulk = true
        storage.setAttributedString(NSAttributedString(string: text, attributes: baseAttributes))
        bulk = false
        colorer.load(storage.mutableString)
        lineStarts = nil
        phase = .ready
        recolourAll()
    }

    /// `--snapshot`'s lines from outside, 1-based.
    func markFixture(_ lines: ClosedRange<Int>) {
        let a = lineRange(lines.lowerBound - 1).location
        let b = NSMaxRange(lineRange(lines.upperBound - 1))
        marks = [NSRange(location: a, length: max(0, b - a))]
        outsideAt = Date()
    }

    /// What one indent is in a text: a tab where more lines start with tabs
    /// than with spaces, or its language indents with them and no line says
    /// otherwise; else the step its lines most often go in by, four spaces
    /// with none to go by.
    nonisolated static func indent(of text: String, tabs: Bool) -> String {
        var tabbed = 0, spaced = 0, last = 0
        var steps: [Int: Int] = [:]
        for line in text.split(separator: "\n", maxSplits: 4000).prefix(4000) {
            if line.first == "\t" {
                tabbed += 1
                continue
            }
            let n = line.prefix { $0 == " " }.count
            if n == line.count { continue }
            if n > 0 { spaced += 1 }
            let step = abs(n - last)
            if [2, 3, 4, 8].contains(step) { steps[step, default: 0] += 1 }
            last = n
        }
        if tabbed > spaced || (tabs && spaced == 0) { return "\t" }
        let best = steps.max { $0.value == $1.value ? $0.key > $1.key : $0.value < $1.value }?.key ?? 4
        return String(repeating: " ", count: best)
    }

    nonisolated private static func read(_ path: String) -> Read {
        guard let st = DiskStamp.of(path) else { return FileManager.default.fileExists(atPath: path) ? .notText : .gone }
        guard st.size <= Int64(biggest) else { return .tooBig }
        let data: Data
        do { data = try Data(contentsOf: URL(fileURLWithPath: path)) } catch { return .failed(error.localizedDescription) }
        // A NUL early on means a binary file, whatever it decodes as.
        if data.prefix(8000).contains(0) { return .notText }
        guard let text = String(data: data, encoding: .utf8) else { return .notText }
        let crlf = allCRLF(text)
        return .text(crlf ? text.replacingOccurrences(of: "\r\n", with: "\n") : text, crlf: crlf, stamp: st)
    }

    /// Whether every line ends in "\r\n": a file with any lone "\n" keeps
    /// its line ends as they are, mixed or not.
    nonisolated static func allCRLF(_ text: String) -> Bool {
        var last: UInt8 = 0, any = false
        for b in text.utf8 {
            if b == 10 {
                if last != 13 { return false }
                any = true
            }
            last = b
        }
        return any
    }

    // MARK: Drawing

    private var baseAttributes: [NSAttributedString.Key: Any] {
        [.font: font, .foregroundColor: theme.foreground, .paragraphStyle: paragraph]
    }

    /// Tab stops four spaces apart, as code is read.
    var paragraph: NSParagraphStyle {
        let p = NSMutableParagraphStyle()
        p.tabStops = []
        p.defaultTabInterval = 4 * (" " as NSString).size(withAttributes: [.font: font]).width
        return p
    }

    /// A card's theme or text size changed: the whole text takes them.
    func dress(theme: CodeTheme, font: NSFont) {
        guard theme != self.theme || font != self.font else { return }
        self.theme = theme
        self.font = font
        guard storage.length > 0 else { return }
        storage.beginEditing()
        storage.addAttributes(baseAttributes, range: NSRange(location: 0, length: storage.length))
        storage.endEditing()
        recolourAll()
    }

    /// Colours a range of the text in every card showing it.
    func recolour(_ range: NSRange, in only: NSLayoutManager? = nil) {
        let whole = NSRange(location: 0, length: storage.length)
        let r = NSIntersectionRange(range, whole)
        guard r.length > 0 || r.location < whole.length else { return }
        var runs: [(NSRange, NSColor)] = []
        colorer.tokens(in: r) { t, kind in runs.append((t, theme.color(kind))) }
        for lm in only.map({ [$0] }) ?? storage.layoutManagers {
            lm.removeTemporaryAttribute(.foregroundColor, forCharacterRange: r)
            for (t, c) in runs { lm.addTemporaryAttribute(.foregroundColor, value: c, forCharacterRange: t) }
        }
    }

    /// Colours the whole text, a part at a time, so a big file never holds
    /// up a key.
    func recolourAll(in only: NSLayoutManager? = nil) {
        colouring?.cancel()
        let length = storage.length
        guard length > 0 else { return }
        if length < 120_000 {
            recolour(NSRange(location: 0, length: length), in: only)
            return
        }
        colouring = Task { [weak self] in
            var at = 0
            while at < length {
                guard let self, !Task.isCancelled else { return }
                let n = min(60_000, self.storage.length - at)
                guard n > 0 else { return }
                self.recolour(NSRange(location: at, length: n), in: only)
                at += n
                await Task.yield()
            }
        }
    }

    // MARK: Editing

    /// The storage's characters changed, by typing or from the disk.
    fileprivate func edited(_ range: NSRange, delta: Int) {
        lineStarts = nil
        revision += 1
        guard !bulk else { return }
        let r = colorer.edit(storage.mutableString, editedRange: range, delta: delta)
        shiftMarks(range, delta: delta)
        // Coloured once the storage has settled, outside its editing; a
        // range waiting from an edit before moves with this one.
        if var p = toColour {
            let oldEnd = NSMaxRange(range) - delta
            if p.location >= oldEnd {
                p.location += delta
            } else if NSMaxRange(p) > range.location {
                p.length = max(0, max(NSMaxRange(p) + delta, NSMaxRange(range)) - p.location)
            }
            toColour = NSUnionRange(p, r)
        } else {
            toColour = r
            DispatchQueue.main.async { [weak self] in
                guard let self, let p = self.toColour else { return }
                self.toColour = nil
                self.recolour(p)
            }
        }
        guard !quiet else { return }
        edits += 1
        if !dirty { dirty = true }
        saveSoon()
    }

    /// Keeps the marks on the text they mark as the text around them
    /// changes: one after an edit moves with it, and one an edit reaches
    /// grows or shrinks with it.
    private func shiftMarks(_ range: NSRange, delta: Int) {
        guard !marks.isEmpty, !quiet else { return }
        // Where the edit ended in the text before it.
        let start = range.location, oldEnd = NSMaxRange(range) - delta
        marks = marks.map { m in
            if m.location >= oldEnd { return NSRange(location: m.location + delta, length: m.length) }
            if NSMaxRange(m) <= start { return m }
            let lo = min(m.location, start), hi = max(NSMaxRange(m) + delta, NSMaxRange(range))
            return NSRange(location: lo, length: max(0, hi - lo))
        }
    }

    // MARK: Lines

    /// Where each line starts, in UTF-16 units, kept until the next change.
    var starts: [Int] {
        if let s = lineStarts { return s }
        let text = storage.mutableString
        let n = text.length
        var s = [0]
        s.reserveCapacity(n / 30 + 1)
        var buffer = [unichar](repeating: 0, count: 16_384)
        var at = 0
        while at < n {
            let k = min(buffer.count, n - at)
            buffer.withUnsafeMutableBufferPointer { b in
                text.getCharacters(b.baseAddress!, range: NSRange(location: at, length: k))
                for i in 0..<k where b[i] == 10 { s.append(at + i + 1) }
            }
            at += k
        }
        lineStarts = s
        return s
    }

    var lineCount: Int { starts.count }

    /// The 0-based line a UTF-16 offset is on.
    func lineIndex(_ offset: Int) -> Int? {
        let s = starts
        var lo = 0, hi = s.count - 1
        while lo < hi {
            let mid = (lo + hi + 1) / 2
            if s[mid] <= offset { lo = mid } else { hi = mid - 1 }
        }
        return lo
    }

    /// The range of a 0-based line, its newline left out.
    func lineRange(_ line: Int) -> NSRange {
        let s = starts
        let i = min(max(line, 0), s.count - 1)
        let end = i + 1 < s.count ? s[i + 1] - 1 : storage.length
        return NSRange(location: s[i], length: max(0, end - s[i]))
    }

    // MARK: Saving

    private func saveSoon() {
        saveTask?.cancel()
        saveTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(Self.pause * 1e9))
            guard let self, !Task.isCancelled else { return }
            await self.save()
        }
    }

    /// Saves what is typed now, as a card leaves or the app steps back.
    func saveNow() {
        guard dirty else { return }
        saveTask?.cancel()
        saveTask = Task { [weak self] in await self?.save() }
    }

    /// Saves at once, on the main thread, as the app quits: to the file, or
    /// to its rescue file when it can't go there.
    func saveBeforeQuitting() {
        guard dirty else { return }
        guard !conflict, phase == .ready else {
            rescue(now: true)
            return
        }
        let text = storage.string
        if case .success = Self.write(text, crlf: crlf, to: real, expecting: stamp) {
            base = text
            dirty = false
            rescue(now: true)
        } else {
            rescue(now: true)
        }
    }

    /// Typing that can't reach the file now: a conflict waits on the card,
    /// the file is gone, or the last save failed.
    var stranded: Bool { dirty && (conflict || phase == .missing || problem != nil) }

    /// Keeps stranded typing in the rescue file, or drops the file once the
    /// typing is saved or settled.
    func rescue(now: Bool = false) {
        let file = CodeDocs.rescueFile(path)
        let keep: CodeDocs.Rescued? = stranded ? CodeDocs.Rescued(path: path, text: storage.string, at: Date()) : nil
        // One queue, in the order asked, so a late keep never lands after
        // the drop that followed it.
        if now {
            CodeDocs.rescues.sync { CodeDocs.keep(keep, at: file) }
        } else {
            CodeDocs.rescues.async { CodeDocs.keep(keep, at: file) }
        }
    }

    /// Brings back typing a rescue file kept for this file: as a conflict
    /// with the disk's text when they differ.
    private func restore() {
        guard let r = CodeDocs.rescued(path) else { return }
        guard !r.text.utf8.elementsEqual(storage.string.utf8) else {
            let file = CodeDocs.rescueFile(path)
            CodeDocs.rescues.async { CodeDocs.keep(nil, at: file) }
            return
        }
        theirs = storage.string
        storage.replaceCharacters(in: NSRange(location: 0, length: storage.length),
                                  with: NSAttributedString(string: r.text, attributes: baseAttributes))
        dirty = true
        conflict = true
        problem = nil
    }

    /// Writes the text, unless the disk changed under it since it was last
    /// read or saved: that is a conflict, for the card to settle. Over is
    /// the card's Keep Mine, which writes over the disk's text, and its Save
    /// It Again for a file gone; typing on a file gone waits for that.
    func save(over: Bool = false) async {
        // Never across a look at the disk, which might take its text in.
        while checking { try? await Task.sleep(nanoseconds: 40_000_000) }
        guard dirty || over, !saving else { return }
        guard (phase == .ready && (!conflict || over)) || (phase == .missing && over) else {
            rescue()
            return
        }
        let text = storage.string, crlf = crlf, real = real
        let mark = edits
        let expect = over || phase == .missing ? nil : stamp
        saving = true
        let result = await Task.detached(priority: .userInitiated) { Self.write(text, crlf: crlf, to: real, expecting: expect) }.value
        saving = false
        switch result {
        case let .success(st):
            stamp = st
            base = text
            savedEdits = mark
            if edits == mark { dirty = false }
            savedAt = Date()
            problem = nil
            conflict = false
            theirs = nil
            if phase == .missing { phase = .ready }
            rescue()
            // Typed on while it saved: those keys save in turn.
            if dirty { saveSoon() }
        case .changed:
            await check()
        case let .failed(why):
            problem = why
            rescue()
        }
    }

    private enum Wrote: Sendable {
        case success(DiskStamp?)
        /// The disk's text changed since it was read: nothing written.
        case changed
        case failed(String)
    }

    /// Writes text to the file only whole: through a copy put in its place,
    /// which keeps the file's permissions, or else an atomic write in its
    /// folder with its permissions put back. A write that fails leaves the
    /// file as it was. A file gone is written new.
    nonisolated private static func write(_ text: String, crlf: Bool, to path: String, expecting: DiskStamp?) -> Wrote {
        if let expecting, DiskStamp.of(path) != expecting { return .changed }
        let data = Data((crlf ? text.replacingOccurrences(of: "\n", with: "\r\n") : text).utf8)
        let url = URL(fileURLWithPath: path)
        let fm = FileManager.default
        guard fm.fileExists(atPath: path) else {
            do { try data.write(to: url, options: .atomic) } catch { return .failed(error.localizedDescription) }
            return .success(DiskStamp.of(path))
        }
        if let dir = try? fm.url(for: .itemReplacementDirectory, in: .userDomainMask, appropriateFor: url, create: true) {
            defer { try? fm.removeItem(at: dir) }
            let copy = dir.appendingPathComponent(url.lastPathComponent)
            do { try data.write(to: copy) } catch { return .failed(error.localizedDescription) }
            if (try? fm.replaceItemAt(url, withItemAt: copy)) != nil { return .success(DiskStamp.of(path)) }
        }
        let perms = (try? fm.attributesOfItem(atPath: path))?[.posixPermissions]
        do { try data.write(to: url, options: .atomic) } catch { return .failed(error.localizedDescription) }
        if let perms { try? fm.setAttributes([.posixPermissions: perms], ofItemAtPath: path) }
        return .success(DiskStamp.of(path))
    }

    // MARK: The disk

    /// Looks whether the file changed on disk, as a card showing it does
    /// every second, and takes the change in: at once while nothing typed is
    /// unsaved, else as a conflict.
    func check() async {
        guard !checking, !saving, phase == .ready || phase == .missing else { return }
        checking = true
        defer { checking = false }
        // The disk is looked at, read and compared off the main thread, a
        // hung mount or a big file holding up no key.
        let real = real, known = stamp, base = base, mark = edits
        let seen = await Task.detached(priority: .utility) { Self.look(real, known: known, base: base) }.value
        // A save of ours landed meanwhile: what was read is older.
        guard stamp == known, !saving else { return }
        switch seen {
        case .same:
            return
        case .gone:
            if phase != .missing {
                phase = .missing
                rescue()
            }
        case .unreadable:
            if dirty {
                problem = "\(name) on disk is no longer text this card can edit."
                rescue()
            }
        case let .touched(st):
            stamp = st
            if phase == .missing { phase = .ready }
        case let .changed(text, crlf, st, hunks):
            if phase == .missing { phase = .ready }
            stamp = st
            self.base = text
            self.crlf = crlf
            if dirty || edits != mark {
                theirs = text
                conflict = true
                rescue()
            } else {
                take(hunks)
            }
        }
    }

    private enum Seen: Sendable {
        case same, gone, unreadable
        /// Its stamp changed and its text did not.
        case touched(DiskStamp?)
        case changed(String, crlf: Bool, stamp: DiskStamp?, hunks: [Hunk])
    }

    /// How the file stands against what was last read or saved, base, with
    /// the runs of lines that changed worked out.
    nonisolated private static func look(_ path: String, known: DiskStamp?, base: String) -> Seen {
        let now = DiskStamp.of(path)
        if now == known { return .same }
        if now == nil { return FileManager.default.fileExists(atPath: path) ? .unreadable : .gone }
        guard case let .text(text, crlf, st) = read(path) else { return .unreadable }
        if text.utf8.elementsEqual(base.utf8) { return .touched(st) }
        return .changed(text, crlf: crlf, stamp: st, hunks: hunks(from: base, to: text))
    }

    /// The card's Keep Mine: the typed text goes over the disk's.
    func keepMine() {
        Task { await save(over: true) }
    }

    /// The card's Use Theirs: the disk's text comes in, and ⌘Z brings the
    /// typed text back.
    func takeTheirs() {
        guard let t = theirs else { return }
        let old = storage.string, mark = edits
        Task {
            let hunks = await Task.detached(priority: .userInitiated) { Self.hunks(from: old, to: t) }.value
            // Typed on meanwhile: the disk's text stays waiting.
            guard edits == mark, theirs == t else { return }
            theirs = nil
            conflict = false
            take(hunks)
            dirty = false
            savedEdits = edits
            problem = nil
            rescue()
        }
    }

    /// Puts the disk's text in, a changed run of lines at a time, the last
    /// first, as one step ⌘Z takes back, and marks what changed. The hunks
    /// are against the text as it stands.
    private func take(_ hunks: [Hunk]) {
        guard !hunks.isEmpty, hunks.allSatisfy({ NSMaxRange($0.old) <= storage.length }) else { return }
        for v in views.allObjects { v.breakUndoCoalescing() }
        quiet = true
        undo.beginUndoGrouping()
        storage.beginEditing()
        for h in hunks.reversed() {
            let was = storage.attributedSubstring(from: h.old).string
            storage.replaceCharacters(in: h.old, with: NSAttributedString(string: h.text, attributes: baseAttributes))
            let put = NSRange(location: h.old.location, length: (h.text as NSString).length)
            undo.registerUndo(withTarget: self) { doc in doc.revert(put, to: was) }
        }
        storage.endEditing()
        // Where each hunk landed in the new text; one that only took lines
        // out is an empty mark where they were.
        var landed: [NSRange] = []
        var shift = 0
        for h in hunks {
            let n = (h.text as NSString).length
            landed.append(NSRange(location: h.old.location + shift, length: n))
            shift += n - h.old.length
        }
        undo.setActionName("Change on Disk")
        undo.endUndoGrouping()
        quiet = false
        lineStarts = nil
        marks = landed
        outsideAt = Date()
    }

    /// ⌘Z of a change from the disk: the old text goes back, and is typing
    /// to save, as the user chose it.
    private func revert(_ range: NSRange, to text: String) {
        guard NSMaxRange(range) <= storage.length else { return }
        let was = storage.attributedSubstring(from: range).string
        storage.replaceCharacters(in: range, with: NSAttributedString(string: text, attributes: baseAttributes))
        let put = NSRange(location: range.location, length: (text as NSString).length)
        undo.registerUndo(withTarget: self) { doc in doc.revert(put, to: was) }
    }

    /// A run of whole lines of the old text and what replaces it.
    struct Hunk: Equatable {
        var old: NSRange
        var text: String
    }

    /// The runs of lines that differ from old to new, each line with its
    /// newline, by a diff of the lines between the parts they share at
    /// their start and end; one run for all of that when it is very long.
    nonisolated static func hunks(from old: String, to new: String) -> [Hunk] {
        let a = units(old), b = units(new)
        var pre = 0
        while pre < a.count, pre < b.count, a[pre] == b[pre] { pre += 1 }
        var suf = 0
        while suf < a.count - pre, suf < b.count - pre, a[a.count - 1 - suf] == b[b.count - 1 - suf] { suf += 1 }
        let midA = Array(a[pre..<(a.count - suf)]), midB = Array(b[pre..<(b.count - suf)])
        guard !midA.isEmpty || !midB.isEmpty else { return [] }
        let startA = a[..<pre].reduce(0) { $0 + $1.utf16.count }
        func hunk(_ i0: Int, _ i1: Int, _ j0: Int, _ j1: Int) -> Hunk {
            let at = startA + midA[..<i0].reduce(0) { $0 + $1.utf16.count }
            let length = midA[i0..<i1].reduce(0) { $0 + $1.utf16.count }
            return Hunk(old: NSRange(location: at, length: length), text: midB[j0..<j1].joined())
        }
        // A diff's cost grows with both lengths: past a million pairs of
        // lines, the whole middle is one run.
        guard midA.count * midB.count <= 1_000_000, !midA.isEmpty, !midB.isEmpty else {
            return [hunk(0, midA.count, 0, midB.count)]
        }
        let diff = midB.difference(from: midA)
        var removed = Set<Int>(), inserted = Set<Int>()
        for c in diff {
            switch c {
            case let .remove(o, _, _): removed.insert(o)
            case let .insert(o, _, _): inserted.insert(o)
            }
        }
        var out: [Hunk] = []
        var i = 0, j = 0
        while i < midA.count || j < midB.count {
            if i < midA.count, j < midB.count, !removed.contains(i), !inserted.contains(j) {
                i += 1
                j += 1
                continue
            }
            let i0 = i, j0 = j
            while i < midA.count, removed.contains(i) { i += 1 }
            while j < midB.count, inserted.contains(j) { j += 1 }
            if i == i0, j == j0 { break }
            out.append(hunk(i0, i, j0, j))
        }
        return out
    }

    /// The lines of a text, each with its newline; the last one may have
    /// none.
    nonisolated static func units(_ s: String) -> [Substring] {
        var out: [Substring] = []
        var start = s.startIndex
        while let nl = s[start...].firstIndex(of: "\n") {
            let next = s.index(after: nl)
            out.append(s[start..<next])
            start = next
        }
        if start < s.endIndex { out.append(s[start...]) }
        return out
    }
}

/// Tells a document of its storage's changes; a class of its own, as the
/// document is not an NSObject.
@MainActor
private final class StorageWatch: NSObject, NSTextStorageDelegate {
    weak var doc: CodeDocument?

    init(doc: CodeDocument) { self.doc = doc }

    nonisolated func textStorage(_ storage: NSTextStorage, didProcessEditing mask: NSTextStorageEditActions, range: NSRange, changeInLength delta: Int) {
        guard mask.contains(.editedCharacters) else { return }
        MainActor.assumeIsolated { doc?.edited(range, delta: delta) }
    }
}

/// The documents open in code cards, by path: a file shown in two cards is
/// one text.
@MainActor
final class CodeDocs {
    static let shared = CodeDocs()
    private var open: [String: CodeDocument] = [:]
    /// A stop asked of the app, as an install or a restart asks: typing is
    /// saved, or kept in rescue files, before it goes.
    private var term: DispatchSourceSignal?

    /// Typing is saved as the app steps back, and before it quits or is
    /// stopped.
    private init() {
        let nc = NotificationCenter.default
        nc.addObserver(forName: NSApplication.willResignActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.saveAll() }
        }
        nc.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.saveAll(quitting: true) }
        }
        signal(SIGTERM, SIG_IGN)
        let t = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
        t.setEventHandler { [weak self] in
            MainActor.assumeIsolated { self?.saveAll(quitting: true) }
            exit(0)
        }
        t.resume()
        term = t
    }

    /// `--snapshot`'s file, with its text and no disk.
    func fixture(_ path: String, text: String, theme: CodeTheme, font: NSFont) -> CodeDocument {
        let d = CodeDocument(path: path, theme: theme, font: font)
        d.loadFixture(text)
        open[path] = d
        return d
    }

    /// The document for a path, read the first time it is asked for.
    func doc(_ path: String, theme: CodeTheme, font: NSFont) -> CodeDocument {
        if let d = open[path] { return d }
        let d = CodeDocument(path: path, theme: theme, font: font)
        open[path] = d
        Task { await d.load() }
        return d
    }

    func existing(_ path: String) -> CodeDocument? { open[path] }

    /// Saves every file with typing unsaved: as the app steps back, at once,
    /// or as it quits, before it goes.
    func saveAll(quitting: Bool = false) {
        for d in open.values {
            if quitting { d.saveBeforeQuitting() } else { d.saveNow() }
        }
    }

    /// Lets go of files no card shows any more, once saved; an editor still
    /// on one keeps it until it goes, and takes its layout off then.
    func keep(only paths: Set<String>) {
        for (p, d) in open where !paths.contains(p) && !d.dirty && !d.conflict {
            open[p] = nil
        }
    }

    /// Whether a file has typing that must stay: unsaved or in conflict.
    func holds(_ path: String) -> Bool {
        guard let d = open[path] else { return false }
        return d.dirty || d.conflict
    }

    // MARK: Rescue

    /// Typing kept for a file it couldn't reach.
    struct Rescued: Codable, Sendable {
        var path: String
        var text: String
        var at: Date
    }

    /// The queue rescue files are written and dropped on, in turn.
    nonisolated static let rescues = DispatchQueue(label: "code-rescue", qos: .utility)

    /// Where rescue files are: the app's own support folder, or the folder
    /// IRIE_CODE_RESCUE names, as a test's.
    nonisolated static var rescueFolder: URL {
        if let d = ProcessInfo.processInfo.environment[Brand.env("CODE_RESCUE")], !d.isEmpty { return URL(fileURLWithPath: d) }
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent("Library/Application Support")
        return base.appendingPathComponent(Brand.bundleID).appendingPathComponent("Recovered Code")
    }

    /// A file's rescue file, named by a hash of its path.
    nonisolated static func rescueFile(_ path: String) -> URL {
        let h = SHA256.hash(data: Data(path.utf8)).prefix(10).map { String(format: "%02x", $0) }.joined()
        return rescueFolder.appendingPathComponent(h + ".json")
    }

    /// Keeps a rescue, whole or not at all, or drops the file for none.
    nonisolated static func keep(_ r: Rescued?, at file: URL) {
        let fm = FileManager.default
        guard let r else {
            try? fm.removeItem(at: file)
            return
        }
        try? fm.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        if let data = try? e.encode(r) { try? data.write(to: file, options: .atomic) }
    }

    /// The typing a rescue file keeps for a path, if any.
    nonisolated static func rescued(_ path: String) -> Rescued? {
        guard let data = try? Data(contentsOf: rescueFile(path)) else { return nil }
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        guard let r = try? d.decode(Rescued.self, from: data), r.path == path else { return nil }
        return r
    }
}
