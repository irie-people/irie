package lock

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// The ecosystems a lock's packages come from, as OSV.dev names them.
const (
	NPM  = "npm"
	PyPI = "PyPI"
)

// ClaudeCode is the npm package Claude Code ships as, whose versions its
// native builds share: what a check asks OSV.dev about for its harness.
const ClaudeCode = "@anthropic-ai/claude-code"

// semver is an exact npm version, such as 2.1.250 or 1.0.0-beta.2: not a
// range such as 1 or ^1.2, nor a tag such as latest.
var semver = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// pep440 is an exact PyPI version, such as 1.2 or 2.0rc1, which == and
// uvx's @ name: not a wildcard such as 1.2.*.
var pep440 = regexp.MustCompile(`(?i)^v?[0-9]+(\.[0-9]+)*([._-]?(a|b|c|rc|alpha|beta|pre|preview|post|dev)[0-9]*)*(\+[a-z0-9.]+)?$`)

// npmName is a package's name on npm, scoped or not.
var npmName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)

// pypiName is a project's name on PyPI.
var pypiName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// Spec is the package an MCP server's command runs, as the server's config
// writes it: npx -y @scope/pkg@1.2.3 is npm's @scope/pkg at 1.2.3, and
// uvx pkg==1.2, uvx pkg@1.2 and uvx --from pkg==1.2 cmd are PyPI's pkg at
// 1.2. written is the package as the command names it, version and all;
// pkg is nil when the command runs no package of either, or names no
// exact version of it, as npx pkg or pkg@latest do, which run whatever
// is newest.
func Spec(command string, args []string) (pkg *ipc.LockPackage, written string) {
	switch base := strings.TrimSuffix(filepath.Base(command), ".cmd"); base {
	case "npx":
		return npmSpec(args)
	case "npm":
		// npm exec and npm x take what npx takes.
		if len(args) > 0 && (args[0] == "exec" || args[0] == "x") {
			return npmSpec(args[1:])
		}
	case "uvx":
		return uvSpec(args)
	case "uv":
		if len(args) > 1 && args[0] == "tool" && args[1] == "run" {
			return uvSpec(args[2:])
		}
	}
	return nil, ""
}

// npmSpec reads npx's arguments: its own flags, then the package and the
// command's arguments; --package (-p) names the package instead (npm's
// npx page).
func npmSpec(args []string) (*ipc.LockPackage, string) {
	var from string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if from == "" && i+1 < len(args) {
				from = args[i+1]
			}
			return npm(from)
		case a == "-p" || a == "--package":
			if i+1 < len(args) && from == "" {
				from = args[i+1]
			}
			i++
		case strings.HasPrefix(a, "--package="):
			if from == "" {
				from = strings.TrimPrefix(a, "--package=")
			}
		case a == "-c" || a == "--call":
			i++
		case strings.HasPrefix(a, "-"):
			// -y, --yes, --no, -q and the like take no value.
		default:
			if from == "" {
				from = a
			}
			return npm(from)
		}
	}
	return npm(from)
}

// npm is the package an npm spec names: name@version, @scope/name@version,
// or a name alone.
func npm(spec string) (*ipc.LockPackage, string) {
	spec = strings.TrimPrefix(spec, "npm:")
	if spec == "" {
		return nil, ""
	}
	name, version := spec, ""
	if i := strings.LastIndex(spec, "@"); i > 0 {
		name, version = spec[:i], spec[i+1:]
	}
	if !npmName.MatchString(name) {
		return nil, ""
	}
	if !semver.MatchString(version) {
		return nil, spec
	}
	return &ipc.LockPackage{Ecosystem: NPM, Name: name, Version: strings.TrimPrefix(version, "v")}, spec
}

// uvSpec reads uvx's arguments: its own flags, then the command, which
// may carry the version as name@version; --from names the package instead,
// as name==version (uv's tools guide).
func uvSpec(args []string) (*ipc.LockPackage, string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--from":
			if i+1 < len(args) {
				return pypi(args[i+1])
			}
			return nil, ""
		case strings.HasPrefix(a, "--from="):
			return pypi(strings.TrimPrefix(a, "--from="))
		case a == "--with" || a == "--python" || a == "-p" || a == "--index" || a == "--index-url" || a == "--with-requirements":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return pypi(a)
		}
	}
	return nil, ""
}

// pypi is the project a uv spec names: name==version, name@version, or a
// name alone, any [extras] left out.
func pypi(spec string) (*ipc.LockPackage, string) {
	name, version := spec, ""
	if n, v, ok := strings.Cut(spec, "=="); ok {
		name, version = n, v
	} else if n, v, ok := strings.Cut(spec, "@"); ok {
		name, version = n, v
	} else if strings.ContainsAny(spec, "<>=!~ ") {
		name = strings.TrimSpace(spec[:strings.IndexAny(spec, "<>=!~ ")])
		version = "~" // a range
	}
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	if !pypiName.MatchString(name) {
		return nil, ""
	}
	if !pep440.MatchString(version) {
		return nil, spec
	}
	return &ipc.LockPackage{Ecosystem: PyPI, Name: name, Version: version}, spec
}
