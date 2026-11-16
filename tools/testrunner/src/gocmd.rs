//! The go commands the runner starts: go env, go list, and the builds of
//! the test binaries and the helpers, and what it reads from a built binary.

use std::collections::HashMap;
use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::thread;

use crate::fnv::Fnv;

/// A package with tests, as go list describes it.
#[derive(Debug, Clone)]
pub struct Pkg {
    pub import: String,
    pub dir: PathBuf,
    /// The module's root: the files a test reads under it count as its
    /// inputs, as go test counts them.
    pub root: PathBuf,
    /// What its test files import, both in the package and outside it.
    pub test_imports: Vec<String>,
}

fn output(cmd: &mut Command, what: &str) -> Result<String, String> {
    let out = cmd
        .output()
        .map_err(|e| format!("{what}: cannot start go: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "{what} failed:\n{}",
            String::from_utf8_lossy(&out.stderr).trim_end()
        ));
    }
    Ok(String::from_utf8_lossy(&out.stdout).into_owned())
}

/// go env VAR.
pub fn env(var: &str) -> Result<String, String> {
    Ok(output(Command::new("go").args(["env", var]), "go env")?
        .trim()
        .to_string())
}

/// The packages among patterns that have tests.
pub fn list(build_flags: &[String], patterns: &[String]) -> Result<Vec<Pkg>, String> {
    let format = "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}\t{{.Dir}}\t{{with .Module}}{{.Dir}}{{end}}\t{{join .TestImports \" \"}} {{join .XTestImports \" \"}}{{end}}";
    let out = output(
        Command::new("go")
            .arg("list")
            .args(build_flags)
            .args(["-f", format])
            .args(patterns),
        "go list",
    )?;
    Ok(parse_list(&out))
}

fn parse_list(out: &str) -> Vec<Pkg> {
    out.lines()
        .filter_map(|line| {
            let mut f = line.splitn(4, '\t');
            let import = f.next()?.to_string();
            let dir = PathBuf::from(f.next()?);
            let root = f.next().filter(|r| !r.is_empty()).map(PathBuf::from)?;
            let test_imports = f
                .next()
                .unwrap_or_default()
                .split_whitespace()
                .map(str::to_string)
                .collect();
            Some(Pkg {
                import,
                dir,
                root,
                test_imports,
            })
        })
        .collect()
}

/// The name go test -c gives a package's test binary.
fn binary_name(import: &str) -> String {
    format!("{}.test", import.rsplit('/').next().unwrap_or(import))
}

/// Builds the test binaries of pkgs under dir: one go test -c for every
/// set of packages whose binaries' names differ, all at once. Each result
/// is the binary's path, or the output of the build that failed to make it.
pub fn build_tests(
    pkgs: &[Pkg],
    dir: &Path,
    build_flags: &[String],
) -> Vec<Result<PathBuf, String>> {
    // The nth package of a name goes into the nth batch.
    let mut seen: HashMap<String, usize> = HashMap::new();
    let mut batches: Vec<Vec<usize>> = Vec::new();
    for (i, p) in pkgs.iter().enumerate() {
        let n = seen.entry(binary_name(&p.import)).or_insert(0);
        if batches.len() <= *n {
            batches.push(Vec::new());
        }
        batches[*n].push(i);
        *n += 1;
    }
    let mut results: Vec<Result<PathBuf, String>> = vec![Err(String::new()); pkgs.len()];
    let outcomes: Vec<(Vec<usize>, PathBuf, Result<String, String>)> = thread::scope(|s| {
        let handles: Vec<_> = batches
            .into_iter()
            .enumerate()
            .map(|(b, members)| {
                let out = dir.join(format!("b{b}"));
                let imports: Vec<String> =
                    members.iter().map(|&i| pkgs[i].import.clone()).collect();
                s.spawn(move || {
                    let made = fs::create_dir_all(&out)
                        .map_err(|e| e.to_string())
                        .and_then(|()| {
                            let mut slash = out.clone().into_os_string();
                            slash.push("/");
                            output(
                                Command::new("go")
                                    .args(["test", "-c", "-o"])
                                    .arg(slash)
                                    .args(build_flags)
                                    .args(&imports),
                                "go test -c",
                            )
                        });
                    (members, out, made)
                })
            })
            .collect();
        handles
            .into_iter()
            .map(|h| h.join().expect("build thread"))
            .collect()
    });
    for (members, out, made) in outcomes {
        let why = match &made {
            Ok(_) => String::from("go test -c made no binary"),
            Err(e) => e.clone(),
        };
        for i in members {
            let bin = out.join(binary_name(&pkgs[i].import));
            results[i] = if bin.is_file() {
                Ok(bin)
            } else {
                Err(why.clone())
            };
        }
    }
    results
}

/// Builds the helpers' main packages into dir with one go build, and
/// returns the key of what it built: one key for the same binaries, another
/// when any of them changed.
pub fn build_helpers(
    patterns: &[String],
    dir: &Path,
    build_flags: &[String],
) -> Result<String, String> {
    fs::create_dir_all(dir).map_err(|e| format!("helpers: {e}"))?;
    let mut slash = dir.to_path_buf().into_os_string();
    slash.push("/");
    output(
        Command::new("go")
            .args(["build", "-o"])
            .arg(slash)
            .args(build_flags)
            .args(patterns),
        "go build of the helpers",
    )?;
    let mut names: Vec<_> = fs::read_dir(dir)
        .map_err(|e| format!("helpers: {e}"))?
        .filter_map(|e| e.ok())
        .map(|e| e.path())
        .filter(|p| p.is_file())
        .collect();
    names.sort();
    let mut h = Fnv::default();
    for p in &names {
        h.field(&p.file_name().unwrap_or_default().to_string_lossy());
        h.field(&build_id(p).map_err(|e| format!("helpers: {}: {e}", p.display()))?);
    }
    Ok(h.hex())
}

/// The Go build ID of a binary, which changes whenever its content does:
/// read from the start of its text, where the linker puts it, or else a
/// hash of the whole file.
pub fn build_id(bin: &Path) -> std::io::Result<String> {
    let mut f = fs::File::open(bin)?;
    let mut head = vec![0u8; 64 * 1024];
    let mut n = 0;
    while n < head.len() {
        let m = f.read(&mut head[n..])?;
        if m == 0 {
            break;
        }
        n += m;
    }
    let needle = b"\xff Go build ID: \"";
    if let Some(i) = head[..n].windows(needle.len()).position(|w| w == needle) {
        let rest = &head[i + needle.len()..n];
        if let Some(j) = rest.iter().position(|&c| c == b'"') {
            return Ok(String::from_utf8_lossy(&rest[..j]).into_owned());
        }
    }
    let mut h = Fnv::default();
    h.bytes(&head[..n]);
    let mut buf = vec![0u8; 1 << 20];
    loop {
        let m = f.read(&mut buf)?;
        if m == 0 {
            break;
        }
        h.bytes(&buf[..m]);
    }
    Ok(format!("content:{}", h.hex()))
}

/// The tests, fuzz targets and examples of a test binary, by
/// -test.list, which runs its TestMain as a run of its tests would.
pub fn list_tests(bin: &Path, dir: &Path, env: &[(String, String)]) -> Result<Vec<String>, String> {
    let out = Command::new(bin)
        .args(["-test.list", "."])
        .current_dir(dir)
        .envs(env.iter().map(|(k, v)| (k, v)))
        .output()
        .map_err(|e| format!("{}: {e}", bin.display()))?;
    if !out.status.success() {
        return Err(format!(
            "{} -test.list failed: {}{}",
            bin.display(),
            String::from_utf8_lossy(&out.stdout),
            String::from_utf8_lossy(&out.stderr)
        ));
    }
    Ok(parse_tests(&String::from_utf8_lossy(&out.stdout)))
}

fn parse_tests(out: &str) -> Vec<String> {
    out.lines()
        .map(str::trim)
        .filter(|n| {
            (n.starts_with("Test") || n.starts_with("Fuzz") || n.starts_with("Example"))
                && n.chars().all(|c| c.is_alphanumeric() || c == '_')
        })
        .map(str::to_string)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn list_lines() {
        let out = "example.com/m/a\t/home/alice/m/a\t/home/alice/m\texample.com/m/testutil testing \n\
                   \n\
                   example.com/m/b\t/home/alice/m/b\t/home/alice/m\t \n";
        let pkgs = parse_list(out);
        assert_eq!(pkgs.len(), 2);
        assert_eq!(pkgs[0].import, "example.com/m/a");
        assert_eq!(pkgs[0].dir, PathBuf::from("/home/alice/m/a"));
        assert_eq!(pkgs[0].root, PathBuf::from("/home/alice/m"));
        assert_eq!(pkgs[0].test_imports, ["example.com/m/testutil", "testing"]);
        assert!(pkgs[1].test_imports.is_empty());
    }

    #[test]
    fn test_names() {
        let out = "TestA\nBenchmarkB\nFuzzC\nExampleD\nok\nTest_e\n";
        assert_eq!(parse_tests(out), ["TestA", "FuzzC", "ExampleD", "Test_e"]);
    }

    #[test]
    fn binary_names_like_go_test() {
        assert_eq!(binary_name("example.com/m/internal/core"), "core.test");
        assert_eq!(binary_name("example.com/m"), "m.test");
    }

    #[test]
    fn build_id_from_the_text_or_the_content() {
        let dir = std::env::temp_dir().join(format!("testrunner-id-{}", std::process::id()));
        fs::create_dir_all(&dir).unwrap();
        let with = dir.join("with");
        fs::write(
            &with,
            b"\x7fELF....\xff Go build ID: \"abc/def\"\n \xff rest",
        )
        .unwrap();
        assert_eq!(build_id(&with).unwrap(), "abc/def");
        let without = dir.join("without");
        fs::write(&without, b"no id here").unwrap();
        let id = build_id(&without).unwrap();
        assert!(id.starts_with("content:"));
        fs::write(&without, b"no id there").unwrap();
        assert_ne!(build_id(&without).unwrap(), id);
        fs::remove_dir_all(&dir).unwrap();
    }
}
