//! The command line: the go test flags the runner takes, read the way go
//! test reads them, from GOFLAGS first and then the arguments, and the
//! runner's own.

use std::path::PathBuf;

#[derive(Debug, Clone)]
pub struct Opts {
    pub packages: Vec<String>,
    /// -count=1: run every test, whatever the cache holds.
    pub fresh: bool,
    /// -timeout, for each test process; go test's default.
    pub timeout: String,
    pub tags: Option<String>,
    pub short: bool,
    pub race: bool,
    pub verbose: bool,
    /// -p: how many test processes run at once.
    pub workers: usize,
    /// Whether -p set workers; otherwise runs at once share the CPUs.
    pub workers_set: bool,
    /// --state: where the times of earlier runs and the cached results live.
    pub state: Option<PathBuf>,
    /// --helpers: main packages the tests run, built once before them.
    pub helpers: Vec<String>,
    /// --helpers-if: build the helpers only when a test imports this.
    pub helpers_if: Option<String>,
    /// --helpers-env: the variable that names the helpers' folder.
    pub helpers_env: Option<String>,
    /// --plan: print the plan and its predicted time, and run nothing.
    pub plan: bool,
    /// -cover: build the test binaries with -cover and say each package's
    /// coverage, as go test -cover does, its processes' merged.
    pub cover: bool,
    /// --coverout: also write each package's coverage to this file.
    pub coverout: Option<PathBuf>,
}

#[derive(Debug)]
pub enum Parsed {
    Run(Box<Opts>),
    /// A go test flag the runner does not take: go test should run.
    Unsupported(String),
}

/// go test flags that change what runs or how it reports, which the runner
/// leaves to go test itself.
const GO_TEST_ONLY: &[&str] = &[
    "run",
    "skip",
    "bench",
    "benchtime",
    "benchmem",
    "json",
    "cover",
    "covermode",
    "coverpkg",
    "coverprofile",
    "failfast",
    "list",
    "fuzz",
    "fuzztime",
    "fuzzminimizetime",
    "cpu",
    "parallel",
    "shuffle",
    "c",
    "o",
    "exec",
    "args",
    "outputdir",
    "cpuprofile",
    "memprofile",
    "blockprofile",
    "mutexprofile",
    "trace",
    "fullpath",
    "artifacts",
];

pub fn parse(args: &[String], goflags: &str, cpus: usize) -> Result<Parsed, String> {
    let mut o = Opts {
        packages: Vec::new(),
        fresh: false,
        timeout: "10m0s".into(),
        tags: None,
        short: false,
        race: false,
        verbose: false,
        workers: cpus.max(1),
        workers_set: false,
        state: None,
        helpers: Vec::new(),
        helpers_if: None,
        helpers_env: None,
        plan: false,
        cover: false,
        coverout: None,
    };
    // GOFLAGS holds -flag=value words for every go command. The runner
    // reads the ones about running tests; the build flags among them reach
    // the go commands it starts, which read GOFLAGS themselves.
    for word in goflags.split_whitespace() {
        let Some((name, value)) = split_flag(word) else {
            continue;
        };
        match apply(&mut o, name, value, true)? {
            Applied::Yes | Applied::NotOurs => {}
            Applied::Unsupported => {
                return Ok(Parsed::Unsupported(format!("GOFLAGS has -{name}")));
            }
            Applied::NeedsValue => {
                return Err(format!("GOFLAGS: -{name} needs a value, as -{name}=X"));
            }
        }
    }
    let mut i = 0;
    while i < args.len() {
        let a = &args[i];
        i += 1;
        if a == "--" {
            o.packages.extend(args[i..].iter().cloned());
            break;
        }
        let Some((name, value)) = split_flag(a) else {
            o.packages.push(a.clone());
            continue;
        };
        match apply(&mut o, name, value, false)? {
            Applied::Yes => {}
            Applied::NeedsValue => {
                let Some(v) = args.get(i) else {
                    return Err(format!("flag -{name} needs a value"));
                };
                i += 1;
                apply(&mut o, name, Some(v), false)?;
            }
            Applied::Unsupported | Applied::NotOurs => {
                return Ok(Parsed::Unsupported(format!("flag -{name}")));
            }
        }
    }
    if o.packages.is_empty() {
        o.packages.push("./...".into());
    }
    Ok(Parsed::Run(Box::new(o)))
}

enum Applied {
    Yes,
    NeedsValue,
    Unsupported,
    /// A flag the runner does not know: from GOFLAGS, another go command's.
    NotOurs,
}

/// Splits -name, --name, -name=value and --name=value.
fn split_flag(a: &str) -> Option<(&str, Option<&str>)> {
    let rest = a.strip_prefix("--").or_else(|| a.strip_prefix('-'))?;
    if rest.is_empty() {
        return None;
    }
    Some(match rest.split_once('=') {
        Some((n, v)) => (n, Some(v)),
        None => (rest, None),
    })
}

fn boolean(name: &str, value: Option<&str>) -> Result<bool, String> {
    match value {
        None | Some("true") | Some("1") => Ok(true),
        Some("false") | Some("0") => Ok(false),
        Some(v) => Err(format!("flag -{name}: {v:?} is not true or false")),
    }
}

fn apply(o: &mut Opts, name: &str, value: Option<&str>, goflags: bool) -> Result<Applied, String> {
    let name = name.strip_prefix("test.").unwrap_or(name);
    let need = |v: Option<&str>| v.map(str::to_string);
    match name {
        "v" => o.verbose = boolean(name, value)?,
        "short" => o.short = boolean(name, value)?,
        "race" => o.race = boolean(name, value)?,
        "plan" if !goflags => o.plan = boolean(name, value)?,
        // GOFLAGS' -cover would reach the helpers' build too: go test runs.
        "cover" if !goflags => o.cover = boolean(name, value)?,
        "count" | "timeout" | "tags" | "p" | "state" | "helpers" | "helpers-if" | "helpers-env"
        | "coverout"
            if value.is_none() =>
        {
            return Ok(Applied::NeedsValue);
        }
        "count" => match value {
            Some("1") => o.fresh = true,
            _ => return Ok(Applied::Unsupported),
        },
        "timeout" => o.timeout = need(value).unwrap_or_default(),
        "tags" => o.tags = need(value).filter(|t| !t.is_empty()),
        "p" => {
            let v = value.unwrap_or_default();
            o.workers = v
                .parse::<usize>()
                .ok()
                .filter(|&n| n > 0)
                .ok_or_else(|| format!("flag -p: {v:?} is not a positive number"))?;
            o.workers_set = true;
        }
        "state" if !goflags => o.state = value.map(PathBuf::from),
        "helpers" if !goflags => {
            o.helpers = value
                .unwrap_or_default()
                .split(',')
                .filter(|s| !s.is_empty())
                .map(str::to_string)
                .collect();
        }
        "helpers-if" if !goflags => o.helpers_if = need(value),
        "helpers-env" if !goflags => o.helpers_env = need(value),
        "coverout" if !goflags => {
            o.coverout = value.map(PathBuf::from);
            o.cover = true;
        }
        n if GO_TEST_ONLY.contains(&n) => return Ok(Applied::Unsupported),
        _ => return Ok(Applied::NotOurs),
    }
    Ok(Applied::Yes)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn run(args: &[&str], goflags: &str) -> Opts {
        let args: Vec<String> = args.iter().map(|s| s.to_string()).collect();
        match parse(&args, goflags, 12).unwrap() {
            Parsed::Run(o) => *o,
            Parsed::Unsupported(why) => panic!("unsupported: {why}"),
        }
    }

    fn unsupported(args: &[&str], goflags: &str) -> String {
        let args: Vec<String> = args.iter().map(|s| s.to_string()).collect();
        match parse(&args, goflags, 12).unwrap() {
            Parsed::Run(_) => panic!("ran"),
            Parsed::Unsupported(why) => why,
        }
    }

    #[test]
    fn defaults() {
        let o = run(&[], "");
        assert_eq!(o.packages, ["./..."]);
        assert_eq!(o.workers, 12);
        assert!(!o.workers_set);
        assert_eq!(o.timeout, "10m0s");
        assert!(!o.fresh && !o.short && !o.verbose && !o.plan);
    }

    #[test]
    fn go_test_flags_both_ways() {
        let o = run(
            &[
                "-count=1",
                "-timeout",
                "40m",
                "-p",
                "4",
                "-short",
                "./internal/core",
            ],
            "",
        );
        assert!(o.fresh && o.short);
        assert_eq!(o.timeout, "40m");
        assert_eq!(o.workers, 4);
        assert!(o.workers_set);
        assert_eq!(o.packages, ["./internal/core"]);
        let o = run(&["--tags=a,b", "-v=true", "./x", "./y"], "");
        assert_eq!(o.tags.as_deref(), Some("a,b"));
        assert!(o.verbose);
        assert_eq!(o.packages, ["./x", "./y"]);
    }

    #[test]
    fn goflags_first_then_arguments() {
        let o = run(&["-timeout=5m"], "-timeout=40m -p=4 -mod=mod -trimpath");
        assert_eq!(o.timeout, "5m");
        assert_eq!(o.workers, 4);
        let o = run(&[], "-count=1");
        assert!(o.fresh);
    }

    #[test]
    fn what_go_test_keeps() {
        assert_eq!(unsupported(&["-run", "TestX"], ""), "flag -run");
        assert_eq!(unsupported(&["-count=3"], ""), "flag -count");
        assert_eq!(unsupported(&["-json"], ""), "flag -json");
        assert_eq!(unsupported(&["-frobnicate"], ""), "flag -frobnicate");
        assert_eq!(unsupported(&[], "-cover"), "GOFLAGS has -cover");
        assert_eq!(
            unsupported(&["-coverprofile=c.out"], ""),
            "flag -coverprofile"
        );
        assert_eq!(unsupported(&["-covermode=count"], ""), "flag -covermode");
    }

    #[test]
    fn coverage_is_the_runner_s() {
        let o = run(&["-cover", "./x"], "");
        assert!(o.cover && o.coverout.is_none());
        let o = run(&["--coverout", "/home/alice/cover.txt"], "");
        assert!(o.cover);
        assert_eq!(o.coverout, Some(PathBuf::from("/home/alice/cover.txt")));
        assert!(!run(&[], "").cover);
    }

    #[test]
    fn runner_flags() {
        let o = run(
            &[
                "--state=/home/alice/.state",
                "--helpers=./cmd/a,./b/...",
                "--helpers-if=example.com/m/testutil",
                "--helpers-env=M_TEST_BINS",
                "--plan",
            ],
            "",
        );
        assert_eq!(o.state, Some(PathBuf::from("/home/alice/.state")));
        assert_eq!(o.helpers, ["./cmd/a", "./b/..."]);
        assert_eq!(o.helpers_if.as_deref(), Some("example.com/m/testutil"));
        assert_eq!(o.helpers_env.as_deref(), Some("M_TEST_BINS"));
        assert!(o.plan);
    }

    #[test]
    fn bad_values() {
        let args = |a: &[&str]| a.iter().map(|s| s.to_string()).collect::<Vec<_>>();
        assert!(parse(&args(&["-p", "0"]), "", 4).is_err());
        assert!(parse(&args(&["-timeout"]), "", 4).is_err());
        assert!(parse(&args(&["-v=maybe"]), "", 4).is_err());
    }
}
