import Foundation

/// A status question typed in the task bar, as internal/ui.ParseHows,
/// FindSession and Hows do: "hows the parser?" says how that agent is doing
/// and starts nothing.
enum Hows {
    private static let pattern = try! NSRegularExpression(
        pattern: #"^how(?:'s|’s|s| is| are)\s+(.+?)(?:\s+(?:going|doing|coming along|looking))?\s*\??$"#,
        options: [.caseInsensitive])

    /// What the question is about, or nil if the text is not one.
    static func parse(_ input: String) -> String? {
        let t = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let m = pattern.firstMatch(in: t, range: NSRange(t.startIndex..., in: t)),
              let r = Range(m.range(at: 1), in: t) else { return nil }
        var q = String(t[r]).trimmingCharacters(in: .whitespaces)
        for p in ["the ", "my ", "that "] where q.lowercased().hasPrefix(p) { q.removeFirst(p.count) }
        return q.isEmpty ? nil : q
    }

    /// The session a question is about: every word must match its name,
    /// branch, repo, agent or a subagent's type, and a name match outranks
    /// the rest. The first of equals wins.
    static func find(_ sessions: [Session], _ query: String) -> Session? {
        let words = query.lowercased().split(separator: " ").map(String.init)
        guard !words.isEmpty else { return nil }
        var best: Session?
        var bestScore = 0
        for s in sessions {
            let name = s.name.lowercased()
            let other = ([s.branch, s.repo, s.agent, s.profile] + (s.subagents ?? []).map(\.type))
                .compactMap { $0?.lowercased() }.joined(separator: " ")
            var score = 0
            for w in words {
                if name.contains(w) { score += 2 } else if other.contains(w) { score += 1 } else { score = -1; break }
            }
            if score > bestScore { best = s; bestScore = score }
        }
        return best
    }

    /// One line: name, state, what it is doing or needs, subagents.
    static func line(_ s: Session) -> String {
        var out = s.name + ": " + s.state.replacingOccurrences(of: "_", with: " ")
        if let r = s.reason, !r.isEmpty { out += " · " + r } else if let d = s.detail, !d.isEmpty { out += " · " + d }
        if let c = s.exitCode, c != 0 { out += " · exit \(c)" }
        let subs = s.subagents ?? []
        if !subs.isEmpty {
            out += " · \(subs.count) subagent" + (subs.count > 1 ? "s" : "")
            if subs.contains(where: { $0.needsYou == true }) { out += " (one needs you)" }
        }
        return out
    }
}
