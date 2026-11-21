//! testrunner runs a Go module's tests in the least wall time (tools/test.sh).
//!
//! go test runs the packages side by side, but each package's tests one
//! after another in a single process, so the package with the most tests
//! sets the time while the other CPUs wait. The runner builds every test
//! binary first, then splits each package's tests into units, one process
//! each, and runs as many at once as there are CPUs, the longest first by
//! the times of earlier runs: no package's tests wait on each other.
//!
//! It also builds once the programs the tests run (the helpers, such as
//! the binary under test), which each test process would otherwise build
//! for itself, and keeps each passing test's result until its binary or
//! what it read changes, as go test's cache does for a whole package.

mod cache;
mod exec;
mod fnv;
mod frame;
mod gocmd;
mod opts;
mod plan;

use std::collections::{HashMap, HashSet};
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex, mpsc};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use frame::Status;
use opts::{Opts, Parsed};

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let cpus = thread::available_parallelism()
        .map(|n| n.get())
        .unwrap_or(4);
    let goflags = std::env::var("GOFLAGS").unwrap_or_default();
    let code = match opts::parse(&args, &goflags, default_workers(cpus)) {
        Ok(Parsed::Run(o)) => match run(&o) {
            Ok(code) => code,
            Err(e) => {
                eprintln!("testrunner: {e}");
                2
            }
        },
        Ok(Parsed::Unsupported(why)) => {
            eprintln!("testrunner: {why} is for go test, which runs instead");
            3
        }
        Err(e) => {
            eprintln!("testrunner: {e}");
            2
        }
    };
    let _ = std::io::stdout().flush();
    std::process::exit(code);
}

/// This run's place among the runs of the runner going on at once: a file
/// it holds a lock on while it runs, in the state folder they share.
struct Active(PathBuf, #[allow(dead_code)] fs::File);

impl Active {
    /// Joins the runs going on, and says how many others there are.
    fn join(state: &Path) -> (Option<Active>, usize) {
        let dir = state.join("active");
        if fs::create_dir_all(&dir).is_err() {
            return (None, 0);
        }
        let mut others = 0;
        if let Ok(rd) = fs::read_dir(&dir) {
            for e in rd.filter_map(|e| e.ok()) {
                let Ok(f) = fs::File::open(e.path()) else {
                    continue;
                };
                match f.try_lock() {
                    // A run that ended without removing its file.
                    Ok(()) => {
                        let _ = fs::remove_file(e.path());
                    }
                    Err(fs::TryLockError::WouldBlock) => others += 1,
                    Err(_) => {}
                }
            }
        }
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or(0);
        let path = dir.join(format!("{}-{nanos}", std::process::id()));
        // A file of its own: no other run holds a lock on it, so taking
        // the lock never waits.
        let mine = fs::File::create(&path)
            .ok()
            .filter(|f| f.try_lock().is_ok())
            .map(|f| Active(path, f));
        (mine, others)
    }
}

impl Drop for Active {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

/// The workers for a machine with `cpus` CPUs when -p does not say. A
/// worker's test may run a whole test Irie, a core with its tmux server,
/// sidebar and agents, which keep more than one CPU busy as they start: a
/// 4-CPU CI runner with a worker per CPU had 9.6 CPUs' worth of work
/// waiting, and tests with time bounds failed, while the 12-CPU Mac stayed
/// under its CPUs. So small machines run half as many, never fewer than two.
fn default_workers(cpus: usize) -> usize {
    if cpus >= 8 {
        cpus
    } else {
        (cpus / 2).max(2).min(cpus.max(1))
    }
}

/// The workers for a run beside `others`: an even share of the CPUs, but
/// never fewer than two.
fn share(cpus: usize, others: usize) -> usize {
    (cpus / (others + 1)).max(2).min(cpus.max(1))
}

/// A folder of the run's own, removed when the run ends.
struct Work(PathBuf);

impl Work {
    fn new() -> Result<Work, String> {
        let tmp = std::env::temp_dir();
        sweep(&tmp);
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or(0);
        let dir = tmp.join(format!("testrunner-run-{}-{nanos}", std::process::id()));
        fs::create_dir_all(&dir).map_err(|e| format!("{}: {e}", dir.display()))?;
        Ok(Work(dir))
    }
}

impl Drop for Work {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

/// Removes what runs that were killed left in tmp: their folders hold a
/// build of every test binary.
fn sweep(tmp: &Path) {
    let Ok(rd) = fs::read_dir(tmp) else {
        return;
    };
    for e in rd.filter_map(|e| e.ok()) {
        if !e
            .file_name()
            .to_string_lossy()
            .starts_with("testrunner-run-")
        {
            continue;
        }
        let stale = e
            .metadata()
            .and_then(|m| m.modified())
            .ok()
            .and_then(|t| t.elapsed().ok())
            .is_some_and(|age| age > Duration::from_secs(6 * 3600));
        if stale {
            let _ = fs::remove_dir_all(e.path());
        }
    }
}

fn default_state() -> PathBuf {
    match std::env::var_os("XDG_CACHE_HOME") {
        Some(d) if Path::new(&d).is_absolute() => PathBuf::from(d).join("testrunner"),
        _ => PathBuf::from(std::env::var_os("HOME").unwrap_or_default()).join(".cache/testrunner"),
    }
}

/// A Go duration, such as 10m0s, 40m, 1h30m or 1.5s; None for 0 or what
/// does not read as one.
fn go_duration(s: &str) -> Option<Duration> {
    let mut total = 0.0f64;
    let mut rest = s.trim();
    if rest.is_empty() || rest == "0" {
        return None;
    }
    while !rest.is_empty() {
        let n = rest
            .find(|c: char| !(c.is_ascii_digit() || c == '.'))
            .unwrap_or(rest.len());
        let value: f64 = rest[..n].parse().ok()?;
        rest = &rest[n..];
        let u = rest
            .find(|c: char| c.is_ascii_digit() || c == '.')
            .unwrap_or(rest.len());
        let scale = match &rest[..u] {
            "h" => 3600.0,
            "m" => 60.0,
            "s" => 1.0,
            "ms" => 1e-3,
            "us" | "µs" => 1e-6,
            "ns" => 1e-9,
            _ => return None,
        };
        total += value * scale;
        rest = &rest[u..];
    }
    (total > 0.0).then(|| Duration::from_secs_f64(total))
}

/// "1 test", "2 tests", "3 processes".
fn count(n: usize, word: &str) -> String {
    match (n, word.ends_with('s')) {
        (1, _) => format!("1 {word}"),
        (_, true) => format!("{n} {word}es"),
        _ => format!("{n} {word}s"),
    }
}

fn clock(secs: f64) -> String {
    if secs < 60.0 {
        format!("{secs:.1}s")
    } else {
        let s = secs.round() as u64;
        format!("{}m{:02}s", s / 60, s % 60)
    }
}

/// Runs f on 0..n with up to `workers` threads at once.
fn parallel<T: Send, F: Fn(usize) -> T + Sync>(n: usize, workers: usize, f: F) -> Vec<T> {
    let next = AtomicUsize::new(0);
    let out: Vec<Mutex<Option<T>>> = (0..n).map(|_| Mutex::new(None)).collect();
    thread::scope(|s| {
        for _ in 0..workers.clamp(1, n.max(1)) {
            s.spawn(|| {
                loop {
                    let i = next.fetch_add(1, Ordering::Relaxed);
                    if i >= n {
                        break;
                    }
                    let v = f(i);
                    *out[i].lock().expect("slot") = Some(v);
                }
            });
        }
    });
    out.into_iter()
        .map(|m| m.into_inner().expect("slot").expect("every index ran"))
        .collect()
}

/// A package's part in the run.
struct Package {
    pkg: gocmd::Pkg,
    bin: Option<PathBuf>,
    /// Its build ID with the flags that change results: the cache's key.
    key: String,
    env: Vec<(String, String)>,
    env_map: HashMap<String, String>,
    tests: Vec<String>,
    cached: usize,
    units: usize,
    done: usize,
    /// What its processes took, added up: about what go test's single
    /// process would take.
    secs: f64,
    failed: Vec<String>,
    reports: Vec<String>,
    problem: Option<String>,
}

/// What a worker needs of a package.
struct Spec {
    bin: Option<PathBuf>,
    dir: PathBuf,
    env: Vec<(String, String)>,
}

struct Done {
    unit: plan::Unit,
    outcome: exec::Outcome,
    testlog: Option<PathBuf>,
}

fn run(o: &Opts) -> Result<i32, String> {
    let t0 = Instant::now();
    let goroot = gocmd::env("GOROOT")?;
    let mut build_flags = Vec::new();
    if let Some(t) = &o.tags {
        build_flags.push(format!("-tags={t}"));
    }
    if o.race {
        build_flags.push("-race".to_string());
    }
    let pkgs = gocmd::list(&build_flags, &o.packages)?;
    if pkgs.is_empty() {
        println!("testrunner: no tests in {}", o.packages.join(" "));
        return Ok(0);
    }
    let state = o.state.clone().unwrap_or_else(default_state);
    fs::create_dir_all(&state).map_err(|e| format!("{}: {e}", state.display()))?;
    let times_path = state.join("times");
    let mut times = plan::Times::load(&times_path);
    let results = cache::Cache::new(&state);
    let work = Work::new()?;
    // Runs at once, such as two installs testing side by side, share the
    // CPUs rather than each start a process for every one.
    let (_active, others) = Active::join(&state);
    let mut o = o.clone();
    if !o.workers_set && others > 0 {
        o.workers = share(o.workers, others);
        println!(
            "testrunner: {} going on, so {}",
            count(others, "other run"),
            count(o.workers, "worker")
        );
    }
    let o = &o;

    // The helpers and the test binaries build at once.
    let want_helpers = !o.helpers.is_empty()
        && o.helpers_env.is_some()
        && o.helpers_if
            .as_ref()
            .is_none_or(|imp| pkgs.iter().any(|p| p.test_imports.iter().any(|i| i == imp)));
    let helpers_dir = work.0.join("helpers");
    let (bins, helpers) = thread::scope(|s| {
        let h = want_helpers
            .then(|| s.spawn(|| gocmd::build_helpers(&o.helpers, &helpers_dir, &build_flags)));
        let bins = gocmd::build_tests(&pkgs, &work.0.join("bins"), &build_flags);
        (bins, h.map(|h| h.join().expect("helpers build")))
    });
    let helpers_key = match helpers {
        Some(Ok(k)) => Some(k),
        Some(Err(e)) => {
            println!("{e}");
            println!("FAIL\t{} [build failed]", o.helpers.join(" "));
            return Ok(1);
        }
        None => None,
    };
    let built = t0.elapsed().as_secs_f64();

    let mut base_env: Vec<(String, String)> = Vec::new();
    let path = std::env::var("PATH").unwrap_or_default();
    base_env.push(("PATH".into(), format!("{goroot}/bin:{path}")));
    if let (Some(var), Some(_)) = (&o.helpers_env, &helpers_key) {
        base_env.push((var.clone(), helpers_dir.display().to_string()));
    }
    let inherited: HashMap<String, String> = std::env::vars().collect();

    let mut packages: Vec<Package> = Vec::new();
    let mut shown_errors = HashSet::new();
    let mut failed_build = false;
    for (pkg, bin) in pkgs.into_iter().zip(bins) {
        let mut env = base_env.clone();
        env.push(("PWD".into(), pkg.dir.display().to_string()));
        let mut env_map = inherited.clone();
        env_map.extend(env.iter().cloned());
        let (bin, problem) = match bin {
            Ok(b) => (Some(b), None),
            Err(e) => {
                failed_build = true;
                if shown_errors.insert(e.clone()) {
                    println!("{e}");
                }
                (None, Some("build failed".to_string()))
            }
        };
        packages.push(Package {
            pkg,
            bin,
            key: String::new(),
            env,
            env_map,
            tests: Vec::new(),
            cached: 0,
            units: 0,
            done: 0,
            secs: 0.0,
            failed: Vec::new(),
            reports: Vec::new(),
            problem,
        });
    }

    // Every binary's tests, and the build IDs.
    let listed = parallel(packages.len(), o.workers, |i| {
        let p = &packages[i];
        let bin = p.bin.as_ref()?;
        let id = gocmd::build_id(bin).map_err(|e| e.to_string());
        let tests = gocmd::list_tests(bin, &p.pkg.dir, &p.env);
        Some((id, tests))
    });
    for (p, l) in packages.iter_mut().zip(listed) {
        match l {
            None => {}
            Some((Ok(id), Ok(tests))) => {
                p.key = fnv::key(&["v1", &id, if o.short { "short" } else { "" }]);
                p.tests = tests;
            }
            Some((Err(e), _)) | Some((_, Err(e))) => {
                println!("{e}");
                p.problem = Some("list failed".to_string());
            }
        }
    }

    // What the cache already holds, and the units for the rest.
    // Each package's tests to run, with their guessed times, and whether
    // most have none yet.
    type ToRun = (usize, Vec<(String, f64)>, bool);
    let mut to_run: Vec<ToRun> = Vec::new();
    let mut total = 0.0;
    let mut cached_tests = 0;
    for (i, p) in packages.iter_mut().enumerate() {
        if p.problem.is_some() {
            continue;
        }
        let held = if o.fresh {
            HashSet::new()
        } else {
            let cx = cache::Context {
                env: &p.env_map,
                helpers: helpers_key
                    .as_deref()
                    .zip(o.helpers_env.as_deref())
                    .map(|(k, v)| (v, k)),
            };
            results.passed(&p.key, &p.tests, &cx)
        };
        p.cached = held.len();
        cached_tests += held.len();
        let rest: Vec<String> = p
            .tests
            .iter()
            .filter(|t| !held.contains(*t))
            .cloned()
            .collect();
        let guesses = times.guess(&p.pkg.import, &rest);
        let unknown = times.mostly_unknown(&p.pkg.import, &rest);
        total += guesses.iter().sum::<f64>();
        to_run.push((i, rest.into_iter().zip(guesses).collect(), unknown));
    }
    let cap = plan::capacity(total, o.workers);
    let mut units: Vec<plan::Unit> = Vec::new();
    for (i, tests, unknown) in &to_run {
        let cap = if *unknown {
            plan::capacity_unknown(cap)
        } else {
            cap
        };
        let us = plan::split(*i, tests, cap);
        packages[*i].units = us.len();
        units.extend(us);
    }
    let predicted = plan::predict(&units, o.workers);
    let bound = plan::lower_bound(&units, o.workers);
    let all_tests: usize = packages.iter().map(|p| p.tests.len()).sum();
    println!(
        "testrunner: {} in {}, {cached_tests} cached: {} on {}, predicted {} (no sooner than {}) after {} of builds",
        count(all_tests, "test"),
        count(packages.len(), "package"),
        count(units.len(), "process"),
        count(o.workers, "worker"),
        clock(predicted),
        clock(bound),
        clock(built),
    );
    if o.plan {
        print_plan(&packages, &units);
        return Ok(0);
    }

    let mut failed_packages = 0;
    let finish = |p: &mut Package, failed_packages: &mut usize| {
        let ran = p.tests.len() - p.cached;
        let span = p.secs;
        let mut notes = Vec::new();
        if p.units > 1 {
            notes.push(count(p.units, "process"));
        }
        if p.cached > 0 && ran > 0 {
            notes.push(format!("{} cached", p.cached));
        }
        let notes = if notes.is_empty() {
            String::new()
        } else {
            format!(" ({})", notes.join(", "))
        };
        if p.problem.is_some() || !p.failed.is_empty() {
            for line in &p.reports {
                println!("{line}");
            }
            match &p.problem {
                Some(why) => println!("FAIL\t{} [{why}]", p.pkg.import),
                None => println!("FAIL\t{}\t{:.3}s{notes}", p.pkg.import, span),
            }
            *failed_packages += 1;
        } else if p.tests.is_empty() {
            println!("ok  \t{}\t{:.3}s [no tests to run]", p.pkg.import, span);
        } else if ran == 0 {
            println!("ok  \t{}\t(cached)", p.pkg.import);
        } else {
            println!("ok  \t{}\t{:.3}s{notes}", p.pkg.import, span);
        }
        if let Some(bin) = &p.bin {
            let _ = fs::remove_file(bin);
        }
    };
    for p in packages.iter_mut() {
        if p.units == 0 {
            finish(p, &mut failed_packages);
        }
    }

    // The longest unit left goes to the next free worker.
    units.sort_by(|a, b| a.predicted.total_cmp(&b.predicted));
    let processes = units.len();
    let queue = Arc::new(Mutex::new(units));
    let kill_after =
        go_duration(&o.timeout).unwrap_or(Duration::from_secs(24 * 3600)) + Duration::from_secs(60);
    let logs = work.0.join("logs");
    let _ = fs::create_dir_all(&logs);
    let serial = AtomicUsize::new(0);
    let (tx, rx) = mpsc::channel::<Done>();
    let mut failed_tests = 0;
    let mut ran_tests = 0;
    // What the workers read; the bookkeeping stays with this thread.
    let specs: Vec<Spec> = packages
        .iter()
        .map(|p| Spec {
            bin: p.bin.clone(),
            dir: p.pkg.dir.clone(),
            env: p.env.clone(),
        })
        .collect();
    let helpers_cx = helpers_key
        .as_deref()
        .zip(o.helpers_env.as_deref())
        .map(|(k, v)| (v, k));
    thread::scope(|s| {
        for _ in 0..o.workers.min(processes) {
            let tx = tx.clone();
            let queue = Arc::clone(&queue);
            let (specs, logs, serial) = (&specs, &logs, &serial);
            s.spawn(move || {
                loop {
                    let Some(unit) = queue.lock().expect("queue").pop() else {
                        break;
                    };
                    let Spec { bin, dir, env } = &specs[unit.pkg];
                    let testlog =
                        logs.join(format!("{}.log", serial.fetch_add(1, Ordering::Relaxed)));
                    let outcome = exec::run(&exec::Job {
                        bin: bin.as_deref().expect("a unit has a binary"),
                        dir,
                        env,
                        tests: &unit.tests,
                        timeout: &o.timeout,
                        kill_after,
                        short: o.short,
                        testlog: Some(testlog.clone()),
                    });
                    let done = Done {
                        unit,
                        outcome,
                        testlog: Some(testlog),
                    };
                    if tx.send(done).is_err() {
                        break;
                    }
                }
            });
        }
        drop(tx);
        // Each unit's results as it ends; a package's line once its last
        // unit has.
        for d in rx {
            let p = &mut packages[d.unit.pkg];
            p.secs += d.outcome.secs;
            let out = &d.outcome;
            if o.verbose {
                print_unit(&p.pkg.import, &d.unit, out);
            }
            let mut passed = Vec::new();
            let mut unexplained = out.code != Some(0);
            for t in &d.unit.tests {
                ran_tests += 1;
                match out.parsed.results.get(t) {
                    Some((Status::Fail, _)) => {
                        failed_tests += 1;
                        unexplained = false;
                        p.failed.push(t.clone());
                        p.reports
                            .extend(out.parsed.report(t).into_iter().map(str::to_string));
                    }
                    Some((_, secs)) => {
                        times.record(&p.pkg.import, t, *secs);
                        passed.push(t.clone());
                    }
                    None if t.starts_with("Example") && out.code == Some(0) => {
                        passed.push(t.clone())
                    }
                    None => {
                        failed_tests += 1;
                        p.failed.push(t.clone());
                        // What it printed holds why, such as a timeout's
                        // panic, which names the tests still running.
                        p.reports
                            .extend(out.parsed.report(t).into_iter().map(str::to_string));
                        p.reports.push(format!(
                            "--- FAIL: {t} (no result: its process ended before it did)"
                        ));
                    }
                }
            }
            if out.code != Some(0) || out.error.is_some() {
                if let Some(e) = &out.error {
                    p.reports.push(format!("testrunner: {e}"));
                }
                p.reports.extend(out.parsed.stray.iter().cloned());
                if unexplained {
                    match out.code {
                        Some(c) => p.reports.push(format!("exit status {c}")),
                        None if out.started => {
                            p.reports.push("signal: the process was killed".to_string())
                        }
                        None => {}
                    }
                    p.problem
                        .get_or_insert_with(|| "the process failed".to_string());
                }
            }
            // A pass counts unless the process failed with no failed test
            // to say why, such as a race found after the tests.
            if !unexplained
                && let Some(log) = &d.testlog
                && let Ok(text) = fs::read_to_string(log)
                && let Some(inputs) = cache::inputs(&text, &p.pkg.dir, &p.pkg.root)
            {
                let cx = cache::Context {
                    env: &p.env_map,
                    helpers: helpers_cx,
                };
                results.record(&p.key, &d.unit.tests, passed, inputs, &cx);
            }
            if let Some(log) = &d.testlog {
                let _ = fs::remove_file(log);
            }
            p.done += 1;
            if p.done == p.units {
                finish(p, &mut failed_packages);
            }
            let _ = std::io::stdout().flush();
        }
    });

    times.save(&times_path);
    results.prune();
    let actual = t0.elapsed().as_secs_f64();
    let passed_all = failed_packages == 0 && !failed_build;
    let line = format!(
        "{}\t{}\t{}\t{}\t{}\t{:.1}\t{:.1}\t{}\n",
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0),
        o.workers,
        all_tests,
        cached_tests,
        processes,
        built + predicted,
        actual,
        if passed_all { "ok" } else { "fail" }
    );
    if let Ok(mut f) = fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(state.join("runs"))
    {
        let _ = f.write_all(line.as_bytes());
    }
    if passed_all {
        println!(
            "testrunner: {} passed, {cached_tests} cached, in {} on {} (predicted {})",
            count(ran_tests, "test"),
            clock(actual),
            count(o.workers, "worker"),
            clock(built + predicted)
        );
        Ok(0)
    } else {
        let unbuilt = packages
            .iter()
            .filter(|p| p.bin.is_none() || p.problem.as_deref() == Some("list failed"))
            .count();
        let mut why = Vec::new();
        if failed_tests > 0 {
            why.push(format!(
                "{failed_tests} of {} failed",
                count(ran_tests, "test")
            ));
        }
        if unbuilt > 0 {
            why.push(format!("{} did not build", count(unbuilt, "package")));
        }
        if why.is_empty() {
            why.push(format!("{} failed", count(failed_packages, "package")));
        }
        println!("FAIL");
        println!(
            "testrunner: {}, in {} on {}",
            why.join(", "),
            clock(actual),
            count(o.workers, "worker")
        );
        Ok(1)
    }
}

/// Under -v, a unit's whole output once it ends.
fn print_unit(import: &str, unit: &plan::Unit, out: &exec::Outcome) {
    let mut stdout = std::io::stdout().lock();
    for t in &unit.tests {
        for line in out.parsed.output.get(t).into_iter().flatten() {
            let _ = writeln!(stdout, "{line}");
        }
    }
    for line in &out.parsed.stray {
        let _ = writeln!(stdout, "{line}");
    }
    let _ = writeln!(
        stdout,
        "testrunner: {import}: {} tests in {:.2}s",
        unit.tests.len(),
        out.secs
    );
}

/// Under --plan: each package's share and the longest units.
fn print_plan(packages: &[Package], units: &[plan::Unit]) {
    let mut by_pkg: Vec<(f64, usize, &str)> = packages
        .iter()
        .enumerate()
        .map(|(i, p)| {
            let mine: Vec<&plan::Unit> = units.iter().filter(|u| u.pkg == i).collect();
            (
                mine.iter().map(|u| u.predicted).sum(),
                mine.len(),
                p.pkg.import.as_str(),
            )
        })
        .collect();
    by_pkg.sort_by(|a, b| b.0.total_cmp(&a.0));
    for (secs, n, import) in by_pkg.iter().take(10) {
        println!("plan: {secs:7.1}s in {n:3} processes  {import}");
    }
    let mut longest: Vec<&plan::Unit> = units.iter().collect();
    longest.sort_by(|a, b| b.predicted.total_cmp(&a.predicted));
    for u in longest.iter().take(5) {
        let names = if u.tests.len() == 1 {
            u.tests[0].clone()
        } else {
            format!("{} tests", u.tests.len())
        };
        println!(
            "plan: {:7.1}s  {}  {names}",
            u.predicted, packages[u.pkg].pkg.import
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn go_durations() {
        assert_eq!(go_duration("10m0s"), Some(Duration::from_secs(600)));
        assert_eq!(go_duration("40m"), Some(Duration::from_secs(2400)));
        assert_eq!(go_duration("1h30m"), Some(Duration::from_secs(5400)));
        assert_eq!(go_duration("1.5s"), Some(Duration::from_millis(1500)));
        assert_eq!(go_duration("0"), None);
        assert_eq!(go_duration("soon"), None);
    }

    #[test]
    fn counts() {
        assert_eq!(count(1, "test"), "1 test");
        assert_eq!(count(2, "test"), "2 tests");
        assert_eq!(count(1, "process"), "1 process");
        assert_eq!(count(97, "process"), "97 processes");
        assert_eq!(count(0, "other run"), "0 other runs");
    }

    #[test]
    fn small_machines_run_fewer_workers() {
        assert_eq!(default_workers(12), 12);
        assert_eq!(default_workers(8), 8);
        assert_eq!(default_workers(6), 3);
        assert_eq!(default_workers(4), 2);
        assert_eq!(default_workers(3), 2);
        assert_eq!(default_workers(1), 1);
    }

    #[test]
    fn runs_at_once_share_the_cpus() {
        assert_eq!(share(12, 0), 12);
        assert_eq!(share(12, 1), 6);
        assert_eq!(share(12, 5), 2);
        assert_eq!(share(12, 20), 2);
        assert_eq!(share(1, 3), 1);
    }

    #[test]
    fn a_run_sees_the_others_and_not_the_ended() {
        let state = std::env::temp_dir().join(format!("testrunner-active-{}", std::process::id()));
        let _ = fs::remove_dir_all(&state);
        fs::create_dir_all(state.join("active")).unwrap();
        // One that ended without removing its file holds no lock.
        fs::write(state.join("active").join("1"), "").unwrap();
        let (a, others) = Active::join(&state);
        assert!(a.is_some());
        assert_eq!(others, 0);
        assert!(!state.join("active").join("1").exists());
        // Locks are per open file, so a second join here sees the first.
        let path = state.join("active").join("other");
        let held = fs::File::create(&path).unwrap();
        held.lock().unwrap();
        let (_b, others) = Active::join(&state);
        assert_eq!(others, 2, "the first run and the held file");
        drop(held);
        drop(a);
        fs::remove_dir_all(&state).unwrap();
    }
}
