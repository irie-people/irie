package tmux

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Want is the layout the core keeps.
type Want struct {
	Bin          string // irie binary run by the panes Plan creates
	SidebarWidth int
	Version      string // recorded on a sidebar pane Plan starts
}

func (w Want) ui() []string          { return []string{w.Bin, "ui", "--supervise"} }
func (w Want) placeholder() []string { return []string{w.Bin, "placeholder"} }

func setOption(target, name, value string) []string {
	return []string{"set-option", "-p", "-t", target, name, value}
}

func isAgentPane(p Pane) bool { return p.Role == RoleAgent || p.Role == RoleMirror }

// Plan returns the commands that repair the first broken invariant of the
// layout in s, or nil when it is sound. The caller runs them, takes a new
// snapshot and plans again until Plan returns nil. The invariants
// (DESIGN §2.3):
//   - irie:0 has exactly two panes, and pane 0 is the sidebar;
//   - the slot holds the placeholder or exactly one agent or mirror pane;
//   - every agent pane is either in the slot or alone in its own window;
//   - the sidebar is SidebarWidth wide;
//   - every agent window has the slot's size.
//
// Plan never kills an agent pane.
func Plan(s Snapshot, w Want) [][]string {
	main := s.Window(MainSession, 0)
	if len(main) == 0 {
		create := []string{"new-session", "-d", "-s", MainSession, "-x", "80", "-y", "24", "--"}
		if s.HasSession(MainSession) {
			create = []string{"new-window", "-d", "-t", MainSession + ":0", "--"}
		}
		return [][]string{append(create, w.ui()...), setOption(Sidebar, OptRole, RoleUI), setOption(Sidebar, OptVersion, w.Version)}
	}

	// The sidebar: one live ui pane, at irie:0.0.
	var ui *Pane
	for i := range main {
		if main[i].Role == RoleUI {
			ui = &main[i]
			break
		}
	}
	if ui == nil {
		split := []string{"split-window", "-h", "-b", "-d", "-t", main[0].ID, "-l", strconv.Itoa(w.SidebarWidth), "--"}
		return [][]string{append(split, w.ui()...), setOption(Sidebar, OptRole, RoleUI), setOption(Sidebar, OptVersion, w.Version)}
	}
	for _, p := range s.ByRole(RoleUI) {
		if p.ID != ui.ID {
			return [][]string{{"kill-pane", "-t", p.ID}}
		}
	}
	if ui.Index != 0 {
		return [][]string{{"swap-pane", "-d", "-s", ui.ID, "-t", Sidebar}}
	}
	if ui.Dead {
		return [][]string{append([]string{"respawn-pane", "-k", "-t", ui.ID, "--"}, w.ui()...), setOption(ui.ID, OptVersion, w.Version)}
	}

	// The slot.
	if len(main) == 1 {
		if ps := s.ByRole(RolePlaceholder); len(ps) > 0 {
			return [][]string{{"join-pane", "-h", "-d", "-s", ps[0].ID, "-t", ui.ID}}
		}
		return [][]string{append([]string{"split-window", "-h", "-d", "-t", ui.ID, "--"}, w.placeholder()...), setOption(Slot, OptRole, RolePlaceholder)}
	}
	if len(main) > 2 {
		extra := main[2]
		if extra.Role == RolePlaceholder {
			return [][]string{{"kill-pane", "-t", extra.ID}}
		}
		return [][]string{{"break-pane", "-d", "-s", extra.ID, "-t", AgentsSession + ":"}}
	}
	slot := main[1]
	if slot.Role != RolePlaceholder && !isAgentPane(slot) {
		return [][]string{{"break-pane", "-d", "-s", slot.ID, "-t", AgentsSession + ":"}}
	}

	// Outside irie:0, an agent pane or the placeholder is alone in its window.
	for _, p := range s.Panes {
		if p.Session != AgentsSession || (!isAgentPane(p) && p.Role != RolePlaceholder) {
			continue
		}
		win := s.WindowOf(p.WindowID)
		if len(win) == 1 {
			continue
		}
		keep := ""
		for _, q := range win {
			if q.Role == RolePlaceholder && keep == "" {
				keep = q.ID
			}
		}
		for _, q := range win {
			if keep == "" && isAgentPane(q) {
				keep = q.ID
			}
		}
		for _, q := range win {
			switch {
			case q.ID == keep:
			case isAgentPane(q):
				return [][]string{{"break-pane", "-d", "-s", q.ID, "-t", AgentsSession + ":"}}
			case q.Role == RolePlaceholder:
				return [][]string{{"kill-pane", "-t", q.ID}}
			}
		}
	}

	// Exactly one placeholder: in the slot, or the shown pane's way home.
	placeholders := s.ByRole(RolePlaceholder)
	if len(placeholders) == 0 {
		name := "irie-home-" + strings.TrimPrefix(slot.ID, "%")
		return [][]string{
			append([]string{"new-window", "-d", "-t", AgentsSession + ":", "-n", name, "--"}, w.placeholder()...),
			setOption(AgentsSession+":="+name, OptRole, RolePlaceholder),
		}
	}
	keep := placeholders[0].ID
	if slot.Role == RolePlaceholder {
		keep = slot.ID
	}
	for _, p := range placeholders {
		if p.ID != keep {
			return [][]string{{"kill-pane", "-t", p.ID}}
		}
		if p.Dead {
			return [][]string{append([]string{"respawn-pane", "-k", "-t", p.ID, "--"}, w.placeholder()...)}
		}
	}

	// Sizes.
	if ui.Width != w.SidebarWidth && ui.WindowWidth > w.SidebarWidth+2 {
		return [][]string{{"resize-pane", "-t", ui.ID, "-x", strconv.Itoa(w.SidebarWidth)}}
	}
	var cmds [][]string
	seen := map[string]bool{}
	for _, p := range s.Panes {
		if slot.Width < 1 || slot.Height < 1 {
			break
		}
		if p.Session != AgentsSession || (!isAgentPane(p) && p.Role != RolePlaceholder) || seen[p.WindowID] {
			continue
		}
		seen[p.WindowID] = true
		if p.WindowWidth != slot.Width || p.WindowHeight != slot.Height {
			cmds = append(cmds, []string{"resize-window", "-t", p.WindowID, "-x", strconv.Itoa(slot.Width), "-y", strconv.Itoa(slot.Height)})
		}
	}
	return cmds
}

// SwapIn returns the commands that show pane in the slot. While an agent is
// shown its own window holds the placeholder, so it first goes home, then
// pane moves in (DESIGN §2.3). Run them as one command list.
func SwapIn(s Snapshot, pane string, focus bool) ([][]string, error) {
	target := s.Pane(pane)
	if target == nil {
		return nil, fmt.Errorf("tmux: no pane %s", pane)
	}
	slot := s.SlotPane()
	if slot == nil {
		return nil, errors.New("tmux: the slot is missing")
	}
	var cmds [][]string
	if slot.ID != target.ID {
		if slot.Role != RolePlaceholder {
			var home *Pane
			for _, p := range s.ByRole(RolePlaceholder) {
				if p.Session != MainSession {
					home = &p
					break
				}
			}
			if home == nil {
				return nil, errors.New("tmux: the shown pane has no placeholder to swap with")
			}
			cmds = append(cmds, []string{"swap-pane", "-d", "-s", Slot, "-t", home.ID})
		}
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", target.ID, "-t", Slot})
	}
	if focus {
		cmds = append(cmds, []string{"select-pane", "-t", Slot})
	}
	return cmds, nil
}
