//! Reads what a test binary prints under -test.v=test2json, as go test
//! -json does: the testing package marks each line it writes about a test
//! (=== RUN, --- PASS and the like) with ^V, escapes those marks in the
//! tests' own output with ESC, and brackets an error's text with ^O and ^N.
//! Each line of output belongs to the top-level test last named.

use std::collections::HashMap;

const FRAME: u8 = 0x16; // ^V
const ERR_BEGIN: u8 = 0x0f; // ^O
const ERR_END: u8 = 0x0e; // ^N
const ESCAPE: u8 = 0x1b; // ESC

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    Pass,
    Fail,
    Skip,
}

#[derive(Debug, Default)]
pub struct Parser {
    current: Option<String>,
    /// Each top-level test's result and its own measure of its time.
    pub results: HashMap<String, (Status, f64)>,
    /// Each top-level test's output, framing lines and all, cleaned.
    pub output: HashMap<String, Vec<String>>,
    /// Output outside any test: TestMain's, a panic's, a timeout's.
    pub stray: Vec<String>,
    partial: Vec<u8>,
}

/// The top-level test a test's name belongs to.
fn top(name: &str) -> &str {
    name.split('/').next().unwrap_or(name)
}

/// The position of the first framing mark in b that is not escaped.
fn frame_at(b: &[u8]) -> Option<usize> {
    let mut i = 0;
    while i < b.len() {
        match b[i] {
            ESCAPE => i += 2,
            FRAME => return Some(i),
            _ => i += 1,
        }
    }
    None
}

/// b without its marks: escapes undone, error brackets dropped.
fn clean(b: &[u8]) -> String {
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        match b[i] {
            ESCAPE if i + 1 < b.len() => {
                out.push(b[i + 1]);
                i += 2;
            }
            ERR_BEGIN | ERR_END | FRAME => i += 1,
            c => {
                out.push(c);
                i += 1;
            }
        }
    }
    let s = String::from_utf8_lossy(&out);
    s.trim_end_matches(['\n', '\r']).to_string()
}

/// "--- PASS: TestName (1.23s)" as (status, name, seconds).
fn result(frame: &str) -> Option<(Status, &str, f64)> {
    let rest = frame.strip_prefix("--- ")?;
    let (word, rest) = rest.split_once(": ")?;
    let status = match word {
        "PASS" => Status::Pass,
        "FAIL" => Status::Fail,
        "SKIP" => Status::Skip,
        _ => return None,
    };
    let (name, time) = rest.rsplit_once(" (")?;
    let secs = time.strip_suffix("s)")?.parse::<f64>().ok()?;
    Some((status, name, secs))
}

impl Parser {
    /// Takes the next bytes of output, in any pieces.
    pub fn feed(&mut self, mut b: &[u8]) {
        while let Some(nl) = b.iter().position(|&c| c == b'\n') {
            let mut line = std::mem::take(&mut self.partial);
            line.extend_from_slice(&b[..nl]);
            self.line(&line);
            b = &b[nl + 1..];
        }
        self.partial.extend_from_slice(b);
    }

    /// Takes what is left after the output ends.
    pub fn finish(&mut self) {
        if !self.partial.is_empty() {
            let line = std::mem::take(&mut self.partial);
            self.line(&line);
        }
    }

    fn line(&mut self, b: &[u8]) {
        match frame_at(b) {
            None => self.out(clean(b)),
            Some(i) => {
                if i > 0 {
                    // Output that did not end its line before the frame.
                    self.out(clean(&b[..i]));
                }
                self.frame(clean(&b[i + 1..]));
            }
        }
    }

    fn out(&mut self, s: String) {
        match &self.current {
            Some(t) => self.output.entry(t.clone()).or_default().push(s),
            None => self.stray.push(s),
        }
    }

    fn frame(&mut self, f: String) {
        for head in [
            "=== RUN   ",
            "=== PAUSE ",
            "=== CONT  ",
            "=== NAME  ",
            "=== NAME",
        ] {
            if let Some(name) = f.strip_prefix(head) {
                let name = name.trim();
                self.current = (!name.is_empty()).then(|| top(name).to_string());
                if head == "=== RUN   " && !name.contains('/') {
                    self.output
                        .entry(name.to_string())
                        .or_default()
                        .push(f.clone());
                }
                return;
            }
        }
        if let Some((status, name, secs)) = result(&f) {
            let t = top(name).to_string();
            self.output.entry(t.clone()).or_default().push(f.clone());
            if !name.contains('/') {
                self.results.insert(t, (status, secs));
                self.current = None;
            }
            return;
        }
        if f == "PASS" || f == "FAIL" {
            self.current = None;
            return;
        }
        self.out(f);
    }

    /// The lines worth showing for a failed test: its output and the
    /// results of it and its subtests, without the lines that only say a
    /// test started.
    pub fn report(&self, test: &str) -> Vec<&str> {
        self.output
            .get(test)
            .map(|lines| {
                lines
                    .iter()
                    .map(String::as_str)
                    .filter(|l| !l.starts_with("=== "))
                    .collect()
            })
            .unwrap_or_default()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(s: &[u8]) -> Parser {
        let mut p = Parser::default();
        // In uneven pieces, as a pipe hands them over.
        for chunk in s.chunks(7) {
            p.feed(chunk);
        }
        p.finish();
        p
    }

    #[test]
    fn results_and_output() {
        let out = b"\x16=== RUN   TestA\n\
            \x16=== RUN   TestA/sub\n\
            \x0f    a_test.go:10: boom\x0e\n\
            \x16--- FAIL: TestA/sub (0.01s)\n\
            \x16--- FAIL: TestA (0.02s)\n\
            \x16=== RUN   TestB\n\
            raw print\n\
            \x16--- PASS: TestB (1.50s)\n\
            \x16=== RUN   TestC\n\
            \x16--- SKIP: TestC (0.00s)\n\
            \x16FAIL\n\
            exit status 1\n";
        let p = parse(out);
        assert_eq!(p.results["TestA"], (Status::Fail, 0.02));
        assert_eq!(p.results["TestB"], (Status::Pass, 1.5));
        assert_eq!(p.results["TestC"], (Status::Skip, 0.0));
        assert!(!p.results.contains_key("TestA/sub"));
        assert_eq!(
            p.report("TestA"),
            [
                "    a_test.go:10: boom",
                "--- FAIL: TestA/sub (0.01s)",
                "--- FAIL: TestA (0.02s)"
            ]
        );
        assert_eq!(p.report("TestB"), ["raw print", "--- PASS: TestB (1.50s)"]);
        assert_eq!(p.stray, ["exit status 1"]);
    }

    #[test]
    fn escaped_marks_are_output() {
        // A test that prints ^V itself: the testing package escapes it.
        let out = b"\x16=== RUN   TestA\n\
            not a frame: \x1b\x16--- PASS: TestA (9.00s)\n\
            \x16--- PASS: TestA (0.10s)\n";
        let p = parse(out);
        assert_eq!(p.results["TestA"], (Status::Pass, 0.1));
        assert_eq!(
            p.report("TestA")[0],
            "not a frame: \x16--- PASS: TestA (9.00s)"
        );
    }

    #[test]
    fn a_frame_after_unfinished_output() {
        let out = b"\x16=== RUN   TestA\nno newline\x16--- PASS: TestA (0.30s)\n";
        let p = parse(out);
        assert_eq!(p.results["TestA"], (Status::Pass, 0.3));
        assert_eq!(p.report("TestA"), ["no newline", "--- PASS: TestA (0.30s)"]);
    }

    #[test]
    fn name_switches_the_owner() {
        let out = b"\x16=== RUN   TestA\n\
            \x16=== PAUSE TestA\n\
            \x16=== RUN   TestB\n\
            \x16=== NAME  TestA\n\
            from a\n\
            \x16=== CONT  TestB\n\
            from b\n\
            \x16--- PASS: TestB (0.00s)\n\
            \x16--- PASS: TestA (0.00s)\n";
        let p = parse(out);
        assert_eq!(p.report("TestA"), ["from a", "--- PASS: TestA (0.00s)"]);
        assert_eq!(p.report("TestB"), ["from b", "--- PASS: TestB (0.00s)"]);
    }

    #[test]
    fn a_panic_leaves_the_test_without_a_result() {
        let out = b"\x16=== RUN   TestA\n\
            \x16--- FAIL: TestA (0.00s)\n\
            panic: oh no [recovered]\n\
            goroutine 1 [running]:\n";
        let p = parse(out);
        assert_eq!(p.results["TestA"].0, Status::Fail);
        assert_eq!(
            p.stray,
            ["panic: oh no [recovered]", "goroutine 1 [running]:"]
        );
    }
}
