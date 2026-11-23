//! Runs one unit: a test binary on some of its package's tests, the way go
//! test runs a test binary (cmd/go/internal/test): in the package's folder,
//! with GOROOT's bin first on PATH and PWD set, stdout and stderr as one
//! stream, -test.paniconexit0 and the timeout, and a last-ditch kill a
//! minute after the timeout. A binary built with -cover gets a folder for
//! its coverage data both as -test.gocoverdir and as GOCOVERDIR, as go
//! test gives it, so that a program the tests start from the binary
//! itself adds what it ran rather than a warning that it can't.

use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use crate::frame::Parser;

pub struct Job<'a> {
    pub bin: &'a Path,
    pub dir: &'a Path,
    pub env: &'a [(String, String)],
    pub tests: &'a [String],
    pub timeout: &'a str,
    pub kill_after: Duration,
    pub short: bool,
    pub testlog: Option<PathBuf>,
    /// The unit's coverage folder, for a binary built with -cover: its
    /// data goes in data/ and its profile in profile.txt (profile).
    pub cover: Option<&'a Path>,
}

/// Where a unit's text profile is, in its coverage folder.
pub fn profile(cover: &Path) -> PathBuf {
    cover.join("profile.txt")
}

pub struct Outcome {
    pub parsed: Parser,
    /// The exit code, or None when a signal ended the process or it could
    /// not start.
    pub code: Option<i32>,
    pub started: bool,
    pub error: Option<String>,
    pub secs: f64,
}

/// A -test.run pattern for exactly these top-level tests.
pub fn pattern(tests: &[String]) -> String {
    format!("^(?:{})$", tests.join("|"))
}

pub fn run(job: &Job) -> Outcome {
    let t0 = Instant::now();
    let mut args = vec![
        "-test.paniconexit0".to_string(),
        format!("-test.timeout={}", job.timeout),
        "-test.v=test2json".to_string(),
        format!("-test.run={}", pattern(job.tests)),
    ];
    if job.short {
        args.push("-test.short".into());
    }
    if let Some(log) = &job.testlog {
        args.push(format!("-test.testlogfile={}", log.display()));
    }
    let failed = |why: String| Outcome {
        parsed: Parser::default(),
        code: None,
        started: false,
        error: Some(why),
        secs: t0.elapsed().as_secs_f64(),
    };
    let mut env: Vec<(String, String)> = job.env.to_vec();
    if let Some(dir) = job.cover {
        let data = dir.join("data");
        if let Err(e) = std::fs::create_dir_all(&data) {
            return failed(format!("{}: {e}", data.display()));
        }
        args.push(format!("-test.gocoverdir={}", data.display()));
        args.push(format!("-test.coverprofile={}", profile(dir).display()));
        env.push(("GOCOVERDIR".into(), data.display().to_string()));
    }
    let (mut reader, writer) = match std::io::pipe() {
        Ok(p) => p,
        Err(e) => return failed(format!("pipe: {e}")),
    };
    let writer2 = match writer.try_clone() {
        Ok(w) => w,
        Err(e) => return failed(format!("pipe: {e}")),
    };
    let mut cmd = Command::new(job.bin);
    cmd.args(&args)
        .current_dir(job.dir)
        .envs(env.iter().map(|(k, v)| (k, v)))
        .stdin(Stdio::null())
        .stdout(writer)
        .stderr(writer2);
    let mut child = match cmd.spawn() {
        Ok(c) => c,
        Err(e) => return failed(format!("{}: {e}", job.bin.display())),
    };
    // The command holds the pipe's write ends; the reader sees the end of
    // the output only once every copy is closed.
    drop(cmd);

    let parsed = Arc::new(Mutex::new(Parser::default()));
    let (done_tx, done_rx) = mpsc::channel::<()>();
    {
        let parsed = Arc::clone(&parsed);
        thread::spawn(move || {
            let mut buf = vec![0u8; 64 * 1024];
            loop {
                match reader.read(&mut buf) {
                    Ok(0) | Err(_) => break,
                    Ok(n) => parsed.lock().expect("parser").feed(&buf[..n]),
                }
            }
            parsed.lock().expect("parser").finish();
            let _ = done_tx.send(());
        });
    }

    // A last-ditch kill for a binary its own timeout did not stop.
    let pid = child.id();
    let (exit_tx, exit_rx) = mpsc::channel::<()>();
    let kill_after = job.kill_after;
    thread::spawn(move || {
        if let Err(mpsc::RecvTimeoutError::Timeout) = exit_rx.recv_timeout(kill_after) {
            let _ = Command::new("kill")
                .args(["-KILL", &pid.to_string()])
                .status();
        }
    });
    let status = child.wait();
    let _ = exit_tx.send(());

    // What the binary started may hold the output open after it exits, as
    // go test allows for: wait a little, then go on without the rest.
    let _ = done_rx.recv_timeout(Duration::from_secs(5));
    let parsed = std::mem::take(&mut *parsed.lock().expect("parser"));
    let (code, error) = match status {
        Ok(s) => (s.code(), None),
        Err(e) => (None, Some(format!("wait: {e}"))),
    };
    Outcome {
        parsed,
        code,
        started: true,
        error,
        secs: t0.elapsed().as_secs_f64(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exact_names() {
        assert_eq!(
            pattern(&["TestA".into(), "TestB".into()]),
            "^(?:TestA|TestB)$"
        );
    }

    #[test]
    fn runs_a_process_and_reads_its_frames() {
        // A stand-in test binary: a shell script that prints frames on
        // stdout and stderr and exits 1.
        let d = std::env::temp_dir().join(format!("testrunner-exec-{}", std::process::id()));
        std::fs::create_dir_all(&d).unwrap();
        let bin = d.join("fake.test");
        std::fs::write(
            &bin,
            "#!/bin/sh\nprintf '\\026=== RUN   TestA\\n'\necho \"args: $*\" >&2\nprintf '\\026--- FAIL: TestA (0.25s)\\n'\nexit 1\n",
        )
        .unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&bin, std::fs::Permissions::from_mode(0o755)).unwrap();
        let tests = vec!["TestA".to_string()];
        let out = run(&Job {
            bin: &bin,
            dir: &d,
            env: &[],
            tests: &tests,
            timeout: "1m",
            kill_after: Duration::from_secs(60),
            short: true,
            testlog: None,
            cover: Some(&d),
        });
        assert_eq!(out.code, Some(1));
        assert_eq!(out.parsed.results["TestA"].0, crate::frame::Status::Fail);
        let report = out.parsed.report("TestA");
        assert!(report[0].contains("-test.run=^(?:TestA)$"), "{report:?}");
        assert!(report[0].contains("-test.short"), "{report:?}");
        let gcd = format!("-test.gocoverdir={}", d.join("data").display());
        assert!(report[0].contains(&gcd), "{report:?}");
        assert!(report[0].contains("-test.coverprofile="), "{report:?}");
        assert!(d.join("data").is_dir());
        std::fs::remove_dir_all(&d).unwrap();
    }
}
