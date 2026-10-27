import Foundation

/// Turns the task bar's text into spawn args, as internal/ui.ParseTask does
/// (DESIGN §4.2). Only the first line is read for tokens: @name or @path runs
/// the task in a new worktree of that git repo, or in place in a directory
/// git does not know; @name! runs it in the repo's own checkout; +profile
/// picks the profile. The rest of the input is the task, verbatim.
enum TaskParser {
    struct Problem: LocalizedError {
        var errorDescription: String?
    }

    static func parse(_ input: String, home: String) throws -> SpawnArgs {
        var a = SpawnArgs()
        let parts = input.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false)
        let first = String(parts.first ?? "")
        let rest = parts.count > 1 ? String(parts[1]) : nil
        let trimmed = first.trimmingCharacters(in: .whitespaces)
        if trimmed.hasPrefix(":") {
            let cmd = trimmed.split(separator: " ").first.map(String.init) ?? trimmed
            throw Problem(errorDescription: "unknown command \(cmd)")
        }

        var words: [String] = []
        var tokens = false
        for w in first.split(whereSeparator: { $0 == " " || $0 == "\t" }).map(String.init) {
            guard w.hasPrefix("@") || w.hasPrefix("+") else {
                words.append(w)
                continue
            }
            tokens = true
            try token(&a, w, home: home)
        }

        // Without a token the first line is kept as typed, inner spacing and all.
        let line = tokens ? words.joined(separator: " ") : trimmed
        switch (rest, line.isEmpty) {
        case (nil, _): a.task = line
        case let (r?, true): a.task = r
        case let (r?, false): a.task = line + "\n" + r
        }
        if a.task.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            throw Problem(errorDescription: "Type what you want done after the @ or + words.")
        }
        return a
    }

    private static func token(_ a: inout SpawnArgs, _ w: String, home: String) throws {
        var v = String(w.dropFirst())
        if w.hasPrefix("+") {
            if a.profile != nil { throw Problem(errorDescription: "only one +profile per task") }
            if v.isEmpty { throw Problem(errorDescription: "+ needs a profile name") }
            a.profile = v
            return
        }
        if a.repo != nil { throw Problem(errorDescription: "only one @ target per task") }
        let inPlace = v.hasSuffix("!")
        if inPlace { v.removeLast() }
        switch true {
        case v.isEmpty:
            throw Problem(errorDescription: "@ needs a repo name or a path")
        case v.contains("!"):
            throw Problem(errorDescription: "\(w): one ! at the end runs the task in the repo’s own checkout")
        case v == "~":
            v = home
        case v.hasPrefix("~/"):
            v = (home as NSString).appendingPathComponent(String(v.dropFirst(2)))
        case v.hasPrefix("/"):
            v = (v as NSString).standardizingPath
        case v.hasPrefix("~") || v == "." || v == ".." || v.hasPrefix("./") || v.hasPrefix("../"):
            throw Problem(errorDescription: "\(w): give an absolute path, a ~/ path or a repo name")
        default:
            break
        }
        a.repo = v
        a.inPlace = inPlace ? true : nil
    }
}
