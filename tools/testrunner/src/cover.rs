//! Coverage, as go test -cover measures it: the share of a package's
//! statements its tests ran. A test binary built with -cover writes its
//! package's blocks under -test.coverprofile, in go's text format, one
//! line a block: `file:line.col,line.col statements count`. The testing
//! package lists every block of the package, each once and in one order
//! for a binary (sorted by file and place, internal/coverage/cformat), so
//! the processes of one binary list the same blocks, and a package's
//! coverage is each block run by any of them.

use crate::fnv::Fnv;

/// A package's blocks, as one process or several merged saw them.
#[derive(Debug, Clone, PartialEq)]
pub struct Blocks {
    /// The places of the blocks, hashed: two profiles merge only when
    /// they list the same blocks.
    layout: String,
    statements: Vec<u32>,
    run: Vec<bool>,
}

impl Blocks {
    /// Reads a text profile; None when it is not one.
    pub fn parse(text: &str) -> Option<Blocks> {
        let mut lines = text.lines();
        lines.next()?.strip_prefix("mode: ")?;
        let mut h = Fnv::default();
        let mut statements = Vec::new();
        let mut run = Vec::new();
        for line in lines {
            if line.is_empty() {
                continue;
            }
            let mut f = line.rsplitn(3, ' ');
            let count = f.next()?.parse::<u64>().ok()?;
            let n = f.next()?.parse::<u32>().ok()?;
            h.field(f.next()?);
            statements.push(n);
            run.push(count > 0);
        }
        Some(Blocks {
            layout: h.hex(),
            statements,
            run,
        })
    }

    /// Adds the blocks another process of the same binary ran; false, and
    /// nothing added, when it listed other blocks.
    pub fn merge(&mut self, other: &Blocks) -> bool {
        if other.layout != self.layout {
            return false;
        }
        for (mine, theirs) in self.run.iter_mut().zip(&other.run) {
            *mine |= *theirs;
        }
        true
    }

    pub fn summary(&self, skipped: usize) -> Summary {
        let mut s = Summary {
            statements: 0,
            covered: 0,
            skipped,
        };
        for (n, run) in self.statements.iter().zip(&self.run) {
            s.statements += u64::from(*n);
            if *run {
                s.covered += u64::from(*n);
            }
        }
        s
    }
}

/// A package's coverage: its statements, those its tests ran, and how
/// many of its tests skipped, as a test that can't run on a machine does.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Summary {
    pub statements: u64,
    pub covered: u64,
    pub skipped: usize,
}

impl Summary {
    /// What go test -cover says of it.
    pub fn words(&self) -> String {
        if self.statements == 0 {
            return "coverage: [no statements]".to_string();
        }
        let pct = 100.0 * self.covered as f64 / self.statements as f64;
        format!("coverage: {pct:.1}% of statements")
    }

    /// Its line in the cache and in --coverout's file:
    /// `statements covered skipped`.
    pub fn line(&self) -> String {
        format!("{} {} {}", self.statements, self.covered, self.skipped)
    }

    pub fn parse(s: &str) -> Option<Summary> {
        let mut f = s.split_whitespace();
        let s = Summary {
            statements: f.next()?.parse().ok()?,
            covered: f.next()?.parse().ok()?,
            skipped: f.next()?.parse().ok()?,
        };
        (f.next().is_none() && s.covered <= s.statements).then_some(s)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const ONE: &str = "mode: set\n\
        example.com/m/a/a.go:3.14,5.2 2 1\n\
        example.com/m/a/a.go:7.14,9.2 1 0\n\
        example.com/m/a/b.go:3.20,6.2 3 0\n";
    const TWO: &str = "mode: set\n\
        example.com/m/a/a.go:3.14,5.2 2 0\n\
        example.com/m/a/a.go:7.14,9.2 1 0\n\
        example.com/m/a/b.go:3.20,6.2 3 1\n";

    #[test]
    fn a_profile_s_statements_and_those_run() {
        let b = Blocks::parse(ONE).unwrap();
        assert_eq!(
            b.summary(0),
            Summary {
                statements: 6,
                covered: 2,
                skipped: 0
            }
        );
        assert_eq!(b.summary(0).words(), "coverage: 33.3% of statements");
        assert!(Blocks::parse("not a profile").is_none());
        assert!(Blocks::parse("mode: set\na.go:1.1,2.2 x 1\n").is_none());
    }

    #[test]
    fn processes_of_one_binary_add_up() {
        let mut b = Blocks::parse(ONE).unwrap();
        assert!(b.merge(&Blocks::parse(TWO).unwrap()));
        assert_eq!(b.summary(1).covered, 5);
        assert_eq!(b.summary(1).words(), "coverage: 83.3% of statements");
        // Another binary's blocks don't merge.
        let other = Blocks::parse("mode: set\nexample.com/m/a/a.go:3.14,5.3 2 1\n").unwrap();
        assert!(!b.merge(&other));
        assert_eq!(b.summary(1).covered, 5);
    }

    #[test]
    fn a_package_with_nothing_to_run() {
        let b = Blocks::parse("mode: set\n").unwrap();
        assert_eq!(b.summary(0).words(), "coverage: [no statements]");
    }

    #[test]
    fn summaries_as_lines() {
        let s = Summary {
            statements: 120,
            covered: 90,
            skipped: 2,
        };
        assert_eq!(s.line(), "120 90 2");
        assert_eq!(Summary::parse(&s.line()), Some(s));
        assert_eq!(Summary::parse("1 2 0"), None);
        assert_eq!(Summary::parse("1 1"), None);
        assert_eq!(Summary::parse("1 1 0 0"), None);
    }
}
