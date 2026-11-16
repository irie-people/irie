//! The plan: how long each test took in earlier runs, the units of tests
//! that run in one process each, and the time they should take on the
//! workers, longest first.

use std::collections::HashMap;
use std::fs;
use std::path::Path;

use crate::cache::write_atomic;

const HEADER: &str = "testrunner times v1";

/// What starting a test process costs, on top of its tests: the exec of a
/// large binary and its TestMain.
pub const PROCESS: f64 = 0.15;

/// A test with no time of its own yet is guessed at this, unless others of
/// its package have times.
const UNKNOWN: f64 = 0.5;

/// The seconds each test took, by package and name, from earlier runs.
#[derive(Debug, Default)]
pub struct Times {
    map: HashMap<(String, String), f64>,
    changed: HashMap<(String, String), f64>,
}

impl Times {
    pub fn load(path: &Path) -> Times {
        let mut t = Times::default();
        if let Ok(s) = fs::read_to_string(path) {
            t.map = parse(&s);
        }
        t
    }

    pub fn get(&self, pkg: &str, test: &str) -> Option<f64> {
        self.map.get(&(pkg.to_string(), test.to_string())).copied()
    }

    /// Takes a new time for a test: the mean of it and the last, so one
    /// slow run moves the plan only halfway.
    pub fn record(&mut self, pkg: &str, test: &str, secs: f64) {
        let k = (pkg.to_string(), test.to_string());
        let v = match self.map.get(&k) {
            Some(old) => (old + secs) / 2.0,
            None => secs,
        };
        self.map.insert(k.clone(), v);
        self.changed.insert(k, v);
    }

    /// Writes this run's times over the file's, keeping what other runs
    /// wrote meanwhile for other tests.
    pub fn save(&self, path: &Path) {
        if self.changed.is_empty() {
            return;
        }
        let mut all = fs::read_to_string(path)
            .map(|s| parse(&s))
            .unwrap_or_default();
        all.extend(self.changed.iter().map(|(k, v)| (k.clone(), *v)));
        let mut rows: Vec<_> = all.into_iter().collect();
        rows.sort_by(|a, b| a.0.cmp(&b.0));
        let mut s = format!("{HEADER}\n");
        for ((p, t), v) in rows {
            s.push_str(&format!("{p}\t{t}\t{v:.3}\n"));
        }
        write_atomic(path, &s);
    }

    /// A guess for each test of a package: its own time, or else the mean
    /// of its package's known times.
    pub fn guess(&self, pkg: &str, tests: &[String]) -> Vec<f64> {
        let known: Vec<f64> = tests.iter().filter_map(|t| self.get(pkg, t)).collect();
        let fallback = if known.is_empty() {
            UNKNOWN
        } else {
            known.iter().sum::<f64>() / known.len() as f64
        };
        tests
            .iter()
            .map(|t| self.get(pkg, t).unwrap_or(fallback))
            .collect()
    }
}

fn parse(s: &str) -> HashMap<(String, String), f64> {
    let mut lines = s.lines();
    if lines.next() != Some(HEADER) {
        return HashMap::new();
    }
    lines
        .filter_map(|l| {
            let mut f = l.split('\t');
            let (p, t, v) = (f.next()?, f.next()?, f.next()?.parse::<f64>().ok()?);
            Some(((p.to_string(), t.to_string()), v))
        })
        .collect()
}

/// Tests of one package that run in one process.
#[derive(Debug, Clone)]
pub struct Unit {
    pub pkg: usize,
    pub tests: Vec<String>,
    pub predicted: f64,
}

/// How long a unit may take: short enough that the units even out on
/// the workers, long enough that starting processes costs little.
pub fn capacity(total: f64, workers: usize) -> f64 {
    (total / (workers as f64 * 4.0)).clamp(1.0, 30.0)
}

/// The capacity for a package most of whose tests have no time yet, as
/// on a first run: a few tests to a unit, since any of them may be the
/// slow one.
pub fn capacity_unknown(cap: f64) -> f64 {
    cap.min(UNKNOWN * 4.0)
}

impl Times {
    /// Whether most of the tests have no time of their own yet.
    pub fn mostly_unknown(&self, pkg: &str, tests: &[String]) -> bool {
        let known = tests.iter().filter(|t| self.get(pkg, t).is_some()).count();
        known * 2 < tests.len()
    }
}

/// Packs a package's tests into units of about `cap` seconds, longest
/// first; a test longer than that is a unit of its own.
pub fn split(pkg: usize, tests: &[(String, f64)], cap: f64) -> Vec<Unit> {
    let mut sorted: Vec<&(String, f64)> = tests.iter().collect();
    sorted.sort_by(|a, b| b.1.total_cmp(&a.1).then_with(|| a.0.cmp(&b.0)));
    let mut units: Vec<Unit> = Vec::new();
    for (name, secs) in sorted {
        match units.iter_mut().find(|u| u.predicted + secs <= cap) {
            Some(u) => {
                u.tests.push(name.clone());
                u.predicted += secs;
            }
            None => units.push(Unit {
                pkg,
                tests: vec![name.clone()],
                predicted: *secs,
            }),
        }
    }
    for u in &mut units {
        u.predicted += PROCESS;
    }
    units
}

/// The time the units take on `workers` workers that each take the
/// longest unit left: the wall time the plan predicts.
pub fn predict(units: &[Unit], workers: usize) -> f64 {
    let mut times: Vec<f64> = units.iter().map(|u| u.predicted).collect();
    times.sort_by(|a, b| b.total_cmp(a));
    let mut free = vec![0.0f64; workers.max(1)];
    for t in times {
        let (i, _) = free
            .iter()
            .enumerate()
            .min_by(|a, b| a.1.total_cmp(b.1))
            .expect("workers");
        free[i] += t;
    }
    free.into_iter().fold(0.0, f64::max)
}

/// No plan can finish sooner than its longest unit, nor sooner than all
/// the work shared out evenly.
pub fn lower_bound(units: &[Unit], workers: usize) -> f64 {
    let total: f64 = units.iter().map(|u| u.predicted).sum();
    let longest = units.iter().map(|u| u.predicted).fold(0.0, f64::max);
    longest.max(total / workers.max(1) as f64)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tests(ts: &[(&str, f64)]) -> Vec<(String, f64)> {
        ts.iter().map(|(n, s)| (n.to_string(), *s)).collect()
    }

    #[test]
    fn long_tests_alone_short_ones_together() {
        let units = split(
            0,
            &tests(&[("A", 70.0), ("B", 4.0), ("C", 3.0), ("D", 2.5), ("E", 0.5)]),
            5.0,
        );
        let names: Vec<Vec<&str>> = units
            .iter()
            .map(|u| u.tests.iter().map(String::as_str).collect())
            .collect();
        assert_eq!(names, [vec!["A"], vec!["B", "E"], vec!["C"], vec!["D"]]);
        assert!((units[1].predicted - (4.5 + PROCESS)).abs() < 1e-9);
    }

    #[test]
    fn a_first_run_packs_few_tests_to_a_unit() {
        let t = Times::default();
        let names: Vec<String> = (0..40).map(|i| format!("Test{i}")).collect();
        assert!(t.mostly_unknown("p", &names));
        let guesses = t.guess("p", &names);
        let tests: Vec<(String, f64)> = names.into_iter().zip(guesses).collect();
        let units = split(0, &tests, capacity_unknown(14.0));
        assert_eq!(units.len(), 10);
        assert!(units.iter().all(|u| u.tests.len() == 4));
    }

    #[test]
    fn prediction_and_bound() {
        let u = |s: f64| Unit {
            pkg: 0,
            tests: vec![],
            predicted: s,
        };
        let units = [u(70.0), u(30.0), u(30.0), u(20.0), u(20.0)];
        assert_eq!(predict(&units, 1), 170.0);
        assert_eq!(predict(&units, 2), 90.0);
        assert_eq!(predict(&units, 8), 70.0);
        assert_eq!(lower_bound(&units, 8), 70.0);
        assert_eq!(lower_bound(&units, 1), 170.0);
    }

    #[test]
    fn times_go_halfway_and_survive_other_runs() {
        let d = std::env::temp_dir().join(format!("testrunner-times-{}", std::process::id()));
        fs::create_dir_all(&d).unwrap();
        let path = d.join("times");
        let mut a = Times::load(&path);
        a.record("p", "TestA", 10.0);
        a.save(&path);
        let mut b = Times::load(&path);
        assert_eq!(b.get("p", "TestA"), Some(10.0));
        b.record("p", "TestA", 20.0);
        b.record("p", "TestB", 1.0);
        // Another run meanwhile writes a test of its own.
        let mut c = Times::load(&path);
        c.record("q", "TestC", 3.0);
        c.save(&path);
        b.save(&path);
        let t = Times::load(&path);
        assert_eq!(t.get("p", "TestA"), Some(15.0));
        assert_eq!(t.get("p", "TestB"), Some(1.0));
        assert_eq!(t.get("q", "TestC"), Some(3.0));
        assert_eq!(
            t.guess("p", &["TestA".into(), "TestNew".into()]),
            [15.0, 15.0]
        );
        assert_eq!(t.guess("z", &["TestNew".into()]), [UNKNOWN]);
        fs::remove_dir_all(&d).unwrap();
    }
}
