package terms

import (
	"regexp"
	"sync"
)

// pattern is a regular expression compiled on its first use. This package
// holds hundreds of them, which, compiled as package variables, took
// 4.6 MB and 4.6 ms at the start of every irie process, each hook call
// and each agent's host among them, where only a reading of terms uses
// them.
type pattern struct {
	src  string
	once sync.Once
	rx   *regexp.Regexp
}

// made are the patterns made as the package started, its variables and
// its catalog's, for the test that compiles each, as starting did; started
// keeps out those made later.
var (
	made    []*pattern
	started bool
)

func init() { started = true }

// lazy is a pattern of src, compiled on its first use.
func lazy(src string) *pattern {
	p := &pattern{src: src}
	if !started {
		made = append(made, p)
	}
	return p
}

// re is a pattern that ignores case.
func re(p string) *pattern { return lazy(`(?i)` + p) }

// get compiles the pattern, once.
func (p *pattern) get() *regexp.Regexp {
	p.once.Do(func() { p.rx = regexp.MustCompile(p.src) })
	return p.rx
}

func (p *pattern) MatchString(s string) bool { return p.get().MatchString(s) }

func (p *pattern) FindString(s string) string { return p.get().FindString(s) }

func (p *pattern) FindStringSubmatch(s string) []string { return p.get().FindStringSubmatch(s) }

func (p *pattern) FindAllString(s string, n int) []string { return p.get().FindAllString(s, n) }

func (p *pattern) FindAllStringIndex(s string, n int) [][]int {
	return p.get().FindAllStringIndex(s, n)
}

func (p *pattern) FindAllStringSubmatch(s string, n int) [][]string {
	return p.get().FindAllStringSubmatch(s, n)
}

func (p *pattern) ReplaceAllString(src, repl string) string {
	return p.get().ReplaceAllString(src, repl)
}

func (p *pattern) Split(s string, n int) []string { return p.get().Split(s, n) }

func (p *pattern) String() string { return p.src }
