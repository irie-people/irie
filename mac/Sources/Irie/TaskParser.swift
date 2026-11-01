import Foundation

/// What the task bar's menu shows of the text as it is typed. The core
/// reads the words themselves when the task is sent (the input op, DESIGN
/// §4.2, docs/CORE.md); this only names the repo the menu points to.
enum TaskParser {
    /// The repo or path of the first @ word on the first line, without its
    /// @ and !, for the task bar's menu to show.
    static func target(_ input: String) -> String? {
        let first = input.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).first ?? ""
        guard let w = first.split(whereSeparator: { $0 == " " || $0 == "\t" }).first(where: { $0.hasPrefix("@") && $0.count > 1 }) else {
            return nil
        }
        let v = w.dropFirst()
        return String(v.hasSuffix("!") ? v.dropLast() : v)
    }
}
