package ctxgit

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// gitTimeout bounds every git command the store runs.
const gitTimeout = 30 * time.Second

// ident is who writes the store's commits and reflogs: Irie, never the
// user, whose git identity has no place in it.
const (
	identName  = "irie"
	identEmail = "irie@irie.invalid"
)

// repoEnv are the variables that point git at another repo than the one it
// is given; they are dropped, as internal/worktree does.
var repoEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS",
	"GIT_REPLACE_REF_BASE", "GIT_PREFIX", "GIT_SHALLOW_FILE", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS",
	"GIT_CONFIG_COUNT", "GIT_NAMESPACE",
}

// Env is the environment the store's git runs with: the process's own
// without the variables that point elsewhere, the user's global and system
// config left out, so that nothing of theirs (a hooks path, an identity, a
// pager) reaches the store, and Irie as the committer of every reflog line.
func Env() []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(repoEnv, k) && !strings.HasPrefix(k, "GIT_CONFIG_KEY_") && !strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_COMMITTER_NAME="+identName, "GIT_COMMITTER_EMAIL="+identEmail,
		"GIT_AUTHOR_NAME="+identName, "GIT_AUTHOR_EMAIL="+identEmail, "GIT_PAGER=cat", "PAGER=cat",
	)
}

// gitError is a git command that failed: what git said.
type gitError struct {
	args []string
	code int
	msg  string
}

func (e *gitError) Error() string { return "git " + strings.Join(e.args, " ") + ": " + e.msg }

// exitCode is the exit status of a failed git command, or -1.
func exitCode(err error) int {
	var g *gitError
	if errors.As(err, &g) {
		return g.code
	}
	return -1
}

// git runs git on the store with stdin and returns what it printed.
func (s *Store) git(stdin []byte, args ...string) ([]byte, error) { return run(s.dir, stdin, args...) }

// run runs git on the repository gitDir, or on none when it is "", as for
// init.
func run(gitDir string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := []string{"-c", "core.hooksPath=" + os.DevNull}
	if gitDir != "" {
		full = append(full, "--git-dir="+gitDir)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = Env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		msg := strings.TrimSpace(stderr.String())
		switch {
		case ctx.Err() != nil:
			msg = fmt.Sprintf("no answer within %v", gitTimeout)
		case msg == "":
			msg = err.Error()
		}
		return stdout.Bytes(), &gitError{args: args, code: code, msg: msg}
	}
	return stdout.Bytes(), nil
}

// write stores one object, as git would loose, and returns its id. The
// format is git's own: a header of the kind and the body's length, a NUL,
// the body; the id is the SHA-1 of all of it, and the file under objects/
// holds it zlib-compressed. An object the store has already is not written
// again. Writing objects here rather than through git saves a process per
// object; git reads them, and packs them on gc, like any other.
func (s *Store) write(kind string, body []byte) (string, error) {
	header := fmt.Appendf(nil, "%s %d\x00", kind, len(body))
	h := sha1.New()
	h.Write(header)
	h.Write(body)
	id := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(s.dir, "objects", id[:2])
	path := filepath.Join(dir, id[2:])
	if _, err := os.Stat(path); err == nil {
		return id, nil
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(header)
	zw.Write(body)
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "tmp_obj_")
	if err != nil {
		return "", err
	}
	_, err = tmp.Write(buf.Bytes())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o400)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		// Another writer may have got there first.
		if _, serr := os.Stat(path); serr == nil {
			return id, nil
		}
		return "", err
	}
	return id, nil
}

// treeEntry is one name in a tree: a file (blob) or a folder (tree).
type treeEntry struct {
	name string
	id   string // hex
	dir  bool
}

// writeTree stores a tree of entries. Git sorts a tree's names as bytes,
// a folder's as if it ended in "/", and a mode of 40000 marks a folder.
func (s *Store) writeTree(entries []treeEntry) (string, error) {
	sorted := slices.Clone(entries)
	key := func(e treeEntry) string {
		if e.dir {
			return e.name + "/"
		}
		return e.name
	}
	slices.SortFunc(sorted, func(a, b treeEntry) int { return strings.Compare(key(a), key(b)) })
	var body bytes.Buffer
	for _, e := range sorted {
		raw, err := hex.DecodeString(e.id)
		if err != nil || len(raw) != sha1.Size {
			return "", fmt.Errorf("ctxgit: bad object id %q", e.id)
		}
		mode := "100644"
		if e.dir {
			mode = "40000"
		}
		body.WriteString(mode + " " + e.name + "\x00")
		body.Write(raw)
	}
	return s.write("tree", body.Bytes())
}

// writeCommit stores a commit of tree on parents, at when, with message.
func (s *Store) writeCommit(tree string, parents []string, when time.Time, message string) (string, error) {
	var b strings.Builder
	b.WriteString("tree " + tree + "\n")
	for _, p := range parents {
		if p != "" {
			b.WriteString("parent " + p + "\n")
		}
	}
	stamp := fmt.Sprintf("%d +0000", when.Unix())
	b.WriteString("author " + identName + " <" + identEmail + "> " + stamp + "\n")
	b.WriteString("committer " + identName + " <" + identEmail + "> " + stamp + "\n\n")
	b.WriteString(strings.TrimRight(message, "\n") + "\n")
	return s.write("commit", []byte(b.String()))
}

// exists reports whether p exists.
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}
