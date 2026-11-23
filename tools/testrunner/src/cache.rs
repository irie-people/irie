//! Results kept for tests that passed, so that a run skips a test whose
//! code and inputs have not changed since, as go test's cache does
//! (`go help testflag`), but for each test rather than each package.
//!
//! A test's code is its binary's build ID. Its inputs are what its process
//! read, which the testing package writes to -test.testlogfile: the
//! environment variables it looked up, and the files it opened or looked
//! at. As go test does, the runner counts only files under the module's
//! root, takes a file's size and modification time rather than its content,
//! keeps nothing when a file it read had changed in the last two seconds,
//! and always counts GODEBUG. The helpers' variable counts as the key of
//! the helpers' content, since its value is a folder made for each run.

use std::collections::{HashMap, HashSet};
use std::fs;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime};

use crate::cover::Summary;
use crate::fnv::Fnv;

const MAGIC: &str = "# test log\n";
const HEADER: &str = "testrunner results v1";

/// What a unit's tests read, from its test log: lines of `getenv NAME`,
/// `chdir DIR`, `stat PATH` and `open PATH`, paths absolute and only those
/// under the module's root, each once.
#[derive(Debug, Clone, PartialEq)]
pub struct Inputs(pub Vec<String>);

/// The environment the tests ran in, and the helpers' key.
pub struct Context<'a> {
    pub env: &'a HashMap<String, String>,
    pub helpers: Option<(&'a str, &'a str)>,
}

/// Reads a test log; None when it is not one.
pub fn inputs(log: &str, pkg_dir: &Path, root: &Path) -> Option<Inputs> {
    let body = log.strip_prefix(MAGIC)?;
    let mut pwd = pkg_dir.to_path_buf();
    let mut seen = HashSet::new();
    let mut lines = Vec::new();
    for line in body.lines() {
        if line.is_empty() {
            continue;
        }
        let (op, name) = line.split_once(' ')?;
        let kept = match op {
            "getenv" => format!("getenv {name}"),
            "chdir" => {
                pwd = PathBuf::from(name);
                format!("chdir {name}")
            }
            "stat" | "open" => {
                let p = pwd.join(name);
                if !p.starts_with(root) {
                    continue;
                }
                format!("{op} {}", p.display())
            }
            _ => return None,
        };
        if seen.insert(kept.clone()) {
            lines.push(kept);
        }
    }
    Some(Inputs(lines))
}

fn meta(m: std::io::Result<fs::Metadata>) -> String {
    match m {
        Ok(m) => format!(
            "{} {:o} {}.{} {}",
            m.len(),
            m.mode(),
            m.mtime(),
            m.mtime_nsec(),
            m.is_dir()
        ),
        Err(e) => format!("err {:?}", e.kind()),
    }
}

fn stat(p: &Path) -> String {
    format!(
        "{} {}",
        meta(fs::metadata(p)),
        meta(fs::symlink_metadata(p))
    )
}

/// A file's or folder's state, or None when a file changed too lately to
/// tell a later change from it by its time.
fn open(p: &Path) -> Option<String> {
    let m = match fs::metadata(p) {
        Ok(m) => m,
        Err(e) => return Some(format!("err {:?}", e.kind())),
    };
    let mut s = meta(Ok(m.clone()));
    if m.is_dir() {
        let mut names: Vec<_> = match fs::read_dir(p) {
            Ok(rd) => rd.filter_map(|e| e.ok()).collect(),
            Err(e) => return Some(format!("{s} err {:?}", e.kind())),
        };
        names.sort_by_key(|e| e.file_name());
        for e in names {
            s.push_str(&format!(
                " file {} {}",
                e.file_name().to_string_lossy(),
                meta(e.metadata())
            ));
        }
    } else if m.is_file() {
        let modified = m.modified().ok()?;
        let age = SystemTime::now()
            .duration_since(modified)
            .unwrap_or_default();
        if age < Duration::from_secs(2) {
            return None;
        }
    }
    Some(s)
}

/// The key of the inputs as they stand now, or None when they cannot be
/// trusted to show a change.
pub fn fingerprint(inputs: &Inputs, cx: &Context) -> Option<String> {
    let mut h = Fnv::default();
    let env = |h: &mut Fnv, name: &str| {
        h.field("env").field(name);
        match cx.env.get(name) {
            Some(v) => h.field("1").field(v),
            None => h.field("0"),
        };
    };
    env(&mut h, "GODEBUG");
    for line in &inputs.0 {
        let (op, name) = line.split_once(' ')?;
        match op {
            "getenv" => match cx.helpers {
                Some((var, key)) if var == name => {
                    h.field("helpers").field(key);
                }
                _ => env(&mut h, name),
            },
            "chdir" | "stat" => {
                h.field(op).field(name).field(&stat(Path::new(name)));
            }
            "open" => {
                h.field(op).field(name).field(&open(Path::new(name))?);
            }
            _ => return None,
        }
    }
    Some(h.hex())
}

/// The tests that passed with one binary: units of tests that ran in one
/// process, each with the inputs it read and their key then; and, when
/// every test of the binary ran in one run, the package's coverage then.
#[derive(Debug, Default)]
pub struct Entry {
    units: Vec<(String, Inputs, Vec<String>)>,
    cover: Option<Summary>,
}

impl Entry {
    fn parse(s: &str) -> Entry {
        let mut e = Entry::default();
        let mut lines = s.lines();
        if lines.next() != Some(HEADER) {
            return e;
        }
        for line in lines {
            if let Some(c) = line.strip_prefix("cover ") {
                e.cover = Summary::parse(c);
            } else if let Some(fp) = line.strip_prefix("unit ") {
                e.units
                    .push((fp.to_string(), Inputs(Vec::new()), Vec::new()));
            } else if let (Some(i), Some(u)) = (line.strip_prefix("in "), e.units.last_mut()) {
                u.1.0.push(i.to_string());
            } else if let (Some(t), Some(u)) = (line.strip_prefix("ok "), e.units.last_mut()) {
                u.2.push(t.to_string());
            }
        }
        e
    }

    fn render(&self) -> String {
        let mut s = format!("{HEADER}\n");
        if let Some(c) = &self.cover {
            s.push_str(&format!("cover {}\n", c.line()));
        }
        for (fp, inputs, tests) in &self.units {
            if tests.is_empty() {
                continue;
            }
            s.push_str(&format!("unit {fp}\n"));
            for i in &inputs.0 {
                s.push_str(&format!("in {i}\n"));
            }
            for t in tests {
                s.push_str(&format!("ok {t}\n"));
            }
        }
        s
    }
}

pub struct Cache {
    dir: PathBuf,
}

impl Cache {
    pub fn new(state: &Path) -> Cache {
        let dir = state.join("results");
        let _ = fs::create_dir_all(&dir);
        Cache { dir }
    }

    fn path(&self, key: &str) -> PathBuf {
        self.dir.join(key)
    }

    /// The tests among `tests` whose kept pass still holds: same binary,
    /// same flags (key), and inputs unchanged.
    pub fn passed(&self, key: &str, tests: &[String], cx: &Context) -> HashSet<String> {
        let Ok(s) = fs::read_to_string(self.path(key)) else {
            return HashSet::new();
        };
        let want: HashSet<&str> = tests.iter().map(String::as_str).collect();
        let mut held = HashSet::new();
        for (fp, inputs, ok) in Entry::parse(&s).units {
            if !ok.iter().any(|t| want.contains(t.as_str())) {
                continue;
            }
            if fingerprint(&inputs, cx).as_deref() == Some(fp.as_str()) {
                held.extend(ok.into_iter().filter(|t| want.contains(t.as_str())));
            }
        }
        held
    }

    /// Records a unit's tests: `passed` hold with these inputs, and every
    /// test in `ran` loses whatever was kept for it before.
    pub fn record(
        &self,
        key: &str,
        ran: &[String],
        passed: Vec<String>,
        inputs: Inputs,
        cx: &Context,
    ) {
        let path = self.path(key);
        let mut e = fs::read_to_string(&path)
            .map(|s| Entry::parse(&s))
            .unwrap_or_default();
        let gone: HashSet<&str> = ran.iter().map(String::as_str).collect();
        for u in &mut e.units {
            u.2.retain(|t| !gone.contains(t.as_str()));
        }
        e.units.retain(|u| !u.2.is_empty());
        if !passed.is_empty()
            && let Some(fp) = fingerprint(&inputs, cx)
        {
            e.units.push((fp, inputs, passed));
        }
        write_atomic(&path, &e.render());
    }

    /// The package's coverage, kept when every test of this binary ran.
    pub fn cover(&self, key: &str) -> Option<Summary> {
        let s = fs::read_to_string(self.path(key)).ok()?;
        Entry::parse(&s).cover
    }

    /// Keeps the package's coverage with the binary's tests.
    pub fn set_cover(&self, key: &str, c: Summary) {
        let path = self.path(key);
        let mut e = fs::read_to_string(&path)
            .map(|s| Entry::parse(&s))
            .unwrap_or_default();
        e.cover = Some(c);
        write_atomic(&path, &e.render());
    }

    /// Drops what no run has touched for a week.
    pub fn prune(&self) {
        let Ok(rd) = fs::read_dir(&self.dir) else {
            return;
        };
        let week = Duration::from_secs(7 * 24 * 3600);
        for e in rd.filter_map(|e| e.ok()) {
            let old = e
                .metadata()
                .and_then(|m| m.modified())
                .ok()
                .and_then(|t| t.elapsed().ok())
                .is_some_and(|age| age > week);
            if old {
                let _ = fs::remove_file(e.path());
            }
        }
    }
}

/// Writes a file whole or not at all, beside other runs that may write it.
pub fn write_atomic(path: &Path, body: &str) {
    let tmp = path.with_extension(format!("tmp{}", std::process::id()));
    if fs::write(&tmp, body).is_ok() && fs::rename(&tmp, path).is_err() {
        let _ = fs::remove_file(&tmp);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scratch(name: &str) -> PathBuf {
        let d =
            std::env::temp_dir().join(format!("testrunner-cache-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&d);
        fs::create_dir_all(&d).unwrap();
        d
    }

    fn old(p: &Path) {
        let f = fs::File::options().write(true).open(p).unwrap();
        f.set_modified(SystemTime::now() - Duration::from_secs(60))
            .unwrap();
    }

    #[test]
    fn reads_a_test_log_like_go_test() {
        let root = Path::new("/home/alice/m");
        let log = "# test log\n\
            getenv HOME\n\
            getenv HOME\n\
            open testdata/a.txt\n\
            stat /tmp/irie-t-123/x\n\
            chdir /home/alice/m/b\n\
            open c.txt\n\
            open /home/alice/elsewhere\n";
        let got = inputs(log, Path::new("/home/alice/m/a"), root).unwrap();
        assert_eq!(
            got.0,
            [
                "getenv HOME",
                "open /home/alice/m/a/testdata/a.txt",
                "chdir /home/alice/m/b",
                "open /home/alice/m/b/c.txt",
            ]
        );
        assert!(inputs("not a log", root, root).is_none());
        assert!(inputs("# test log\nfrobnicate x\n", root, root).is_none());
    }

    #[test]
    fn a_change_in_what_a_test_read_shows() {
        let d = scratch("fp");
        let f = d.join("input.txt");
        fs::write(&f, "one").unwrap();
        old(&f);
        let mut env = HashMap::from([("HOME".to_string(), "/home/alice".to_string())]);
        let ins = Inputs(vec!["getenv HOME".into(), format!("open {}", f.display())]);
        let fp = |env: &HashMap<String, String>| fingerprint(&ins, &Context { env, helpers: None });
        let before = fp(&env).unwrap();
        assert_eq!(fp(&env).unwrap(), before);
        env.insert("HOME".into(), "/home/bob".into());
        assert_ne!(fp(&env).unwrap(), before);
        env.insert("HOME".into(), "/home/alice".into());
        fs::write(&f, "two!").unwrap();
        assert!(
            fp(&env).is_none(),
            "a file changed just now cannot be trusted"
        );
        old(&f);
        assert_ne!(fp(&env).unwrap(), before);
        fs::remove_dir_all(&d).unwrap();
    }

    #[test]
    fn the_helpers_variable_counts_as_their_key() {
        let env = HashMap::from([("BINS".to_string(), "/tmp/run-1".to_string())]);
        let env2 = HashMap::from([("BINS".to_string(), "/tmp/run-2".to_string())]);
        let ins = Inputs(vec!["getenv BINS".into()]);
        let cx = |env, key| Context {
            env,
            helpers: Some(("BINS", key)),
        };
        let a = fingerprint(&ins, &cx(&env, "k1")).unwrap();
        assert_eq!(fingerprint(&ins, &cx(&env2, "k1")).unwrap(), a);
        assert_ne!(fingerprint(&ins, &cx(&env, "k2")).unwrap(), a);
    }

    #[test]
    fn keeps_passes_until_they_run_again() {
        let d = scratch("store");
        let c = Cache::new(&d);
        let env = HashMap::new();
        let cx = Context {
            env: &env,
            helpers: None,
        };
        let all: Vec<String> = ["TestA", "TestB", "TestC"].map(String::from).to_vec();
        c.record(
            "k",
            &all,
            vec!["TestA".into(), "TestB".into()],
            Inputs(vec![]),
            &cx,
        );
        assert_eq!(
            c.passed("k", &all, &cx),
            HashSet::from(["TestA".into(), "TestB".into()])
        );
        assert!(c.passed("other", &all, &cx).is_empty());
        // TestB runs again and fails: only TestA stays.
        c.record("k", &["TestB".into()], vec![], Inputs(vec![]), &cx);
        assert_eq!(c.passed("k", &all, &cx), HashSet::from(["TestA".into()]));
        fs::remove_dir_all(&d).unwrap();
    }

    #[test]
    fn coverage_stays_with_the_binary_s_tests() {
        let d = scratch("cover");
        let c = Cache::new(&d);
        let env = HashMap::new();
        let cx = Context {
            env: &env,
            helpers: None,
        };
        let all: Vec<String> = vec!["TestA".into(), "TestB".into()];
        assert_eq!(c.cover("k"), None);
        c.record("k", &all, all.clone(), Inputs(vec![]), &cx);
        let s = Summary {
            statements: 10,
            covered: 7,
            skipped: 1,
        };
        c.set_cover("k", s);
        assert_eq!(c.cover("k"), Some(s));
        assert_eq!(c.passed("k", &all, &cx).len(), 2);
        // A test that runs again keeps the coverage with the rest.
        c.record(
            "k",
            &["TestB".into()],
            vec!["TestB".into()],
            Inputs(vec![]),
            &cx,
        );
        assert_eq!(c.cover("k"), Some(s));
        assert_eq!(c.passed("k", &all, &cx).len(), 2);
        assert_eq!(c.cover("other"), None);
        fs::remove_dir_all(&d).unwrap();
    }

    #[test]
    fn a_changed_input_drops_the_whole_unit() {
        let d = scratch("unit");
        let c = Cache::new(&d);
        let mut env = HashMap::from([("X".to_string(), "1".to_string())]);
        let ins = Inputs(vec!["getenv X".into()]);
        let all: Vec<String> = vec!["TestA".into(), "TestB".into()];
        c.record(
            "k",
            &all,
            all.clone(),
            ins,
            &Context {
                env: &env,
                helpers: None,
            },
        );
        env.insert("X".into(), "2".into());
        assert!(
            c.passed(
                "k",
                &all,
                &Context {
                    env: &env,
                    helpers: None
                }
            )
            .is_empty()
        );
        fs::remove_dir_all(&d).unwrap();
    }
}
