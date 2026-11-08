package worktree

import "time"

// Git runs git in dir as the package does: no other repo's variables, no
// prompts, no optional locks, a process group of its own that a timeout
// kills whole. It returns what git printed, and on failure what it said.
func Git(timeout time.Duration, dir string, stdin []byte, args ...string) (string, error) {
	return git(timeout, dir, stdin, args...)
}
