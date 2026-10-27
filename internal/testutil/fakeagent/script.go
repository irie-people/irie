package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// step is one step of a script.
type step struct {
	kind string        // prompt, tool, permission, stop, stopfail, sub, sleep, exit, status
	arg  string        // the tool name, error, subagent id or cost
	d    time.Duration // sleep
	n    int           // exit code
	sub  []step        // the steps run inside a subagent
}

var (
	toolName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	agentID  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	errName  = regexp.MustCompile(`^[a-z_]+$`)
)

// parseScript reads a comma-separated step script. Inside sub:<id>:<steps>
// the steps are joined by "+" and may only be tool, ask, permission and
// sleep steps.
func parseScript(s string) ([]step, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty script")
	}
	var out []step
	for _, item := range strings.Split(s, ",") {
		st, err := parseStep(strings.TrimSpace(item), false)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func parseStep(item string, inSub bool) (step, error) {
	name, arg, hasArg := strings.Cut(item, ":")
	bad := func() (step, error) { return step{}, fmt.Errorf("bad step %q", item) }
	switch name {
	case "prompt", "stop":
		if hasArg || inSub {
			return bad()
		}
		return step{kind: name}, nil
	case "ask":
		if hasArg {
			return bad()
		}
		return step{kind: "tool", arg: "AskUserQuestion"}, nil
	case "permission":
		if hasArg {
			return bad()
		}
		return step{kind: name}, nil
	case "tool":
		if !toolName.MatchString(arg) {
			return bad()
		}
		return step{kind: name, arg: arg}, nil
	case "stopfail":
		if !errName.MatchString(arg) || inSub {
			return bad()
		}
		return step{kind: name, arg: arg}, nil
	case "sleep":
		d, err := time.ParseDuration(arg)
		if err != nil || d < 0 {
			return bad()
		}
		return step{kind: name, d: d}, nil
	case "exit":
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 || n > 255 || inSub {
			return bad()
		}
		return step{kind: name, n: n}, nil
	case "status":
		if f, err := strconv.ParseFloat(arg, 64); err != nil || f < 0 || inSub {
			return bad()
		}
		return step{kind: name, arg: arg}, nil
	case "sub":
		id, steps, ok := strings.Cut(arg, ":")
		if !ok || !agentID.MatchString(id) || inSub || steps == "" {
			return bad()
		}
		st := step{kind: name, arg: id}
		for _, inner := range strings.Split(steps, "+") {
			s, err := parseStep(strings.TrimSpace(inner), true)
			if err != nil {
				return bad()
			}
			st.sub = append(st.sub, s)
		}
		return st, nil
	}
	return bad()
}
