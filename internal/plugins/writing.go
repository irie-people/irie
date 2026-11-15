//go:build !nowriting

package plugins

import (
	"encoding/json"
	"os"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/variorum"
)

// The writing plugin's parts, by name.
const (
	Variorum    = "variorum"
	VariorumTab = "tab"
)

// variorumSkill is the writing session's skill.
const variorumSkill = "variorum"

// claudeJSON is where Claude Code keeps the MCP servers it adds for every
// folder. Tests point it elsewhere.
var claudeJSON = "~/.claude.json"

func init() { built["writing"] = writing }

// writing is the writing plugin: agents that help a writer in Variorum
// through its door for AI, beside the writer's words and never over them.
func writing() Plugin {
	return Plugin{
		Name:    "writing",
		Title:   "Writing",
		Summary: "Agents that help with your writing in Variorum, Lillian Duarte's writing app, with notes and other versions beside your words, never over them.",
		key:     "plugins.writing",
		on:      func(c config.Config) bool { return c.Plugins.Writing },
		Parts: []Part{
			{
				Name:    Variorum,
				Title:   "Writing in Variorum",
				Summary: "`:write` starts a session that works in Variorum through its door for AI: editor's notes as comments, rewrites and translations as takes beside your paragraphs, books kept tidy.",
				Setup:   ":write",
				Brings:  []string{":write", "Plugins › Writing in Variorum", "/" + brand.Slug + ":variorum"},
				Refused: "Writing in Variorum is left out: skills.disabled names variorum.",
				Skills:  []string{variorumSkill},
				own: func(c config.Config) bool {
					return c.Skills.Enabled && !has(c.Skills.Disabled, variorumSkill)
				},
				check: checkVariorum,
			},
			{
				Name:    VariorumTab,
				Title:   "Variorum tab",
				Summary: "The note a writing session works on, in the Mac app's right sidebar as Variorum holds it, each paragraph with its takes and comments and Show in Variorum beside it.",
				// No words set it up, so no Set Up… button: the tab's
				// Connect button does, which the detail names.
				Brings: []string{"the Variorum tab in the right sidebar", "Variorum's door in `:write` sessions", brand.Slug + " variorum"},
				own:    func(config.Config) bool { return true },
				check:  checkTab,
			},
		},
	}
}

func checkVariorum(e Env, on bool) (bool, string) {
	if !on {
		return true, "Left out: skills.disabled names variorum."
	}
	if variorum.Installed() {
		return true, "Ready: :write, or Plugins › Writing in Variorum › Start Writing in Variorum… in the Mac app, starts a writing session. The first one walks you through Variorum's door for AI."
	}
	return false, "Variorum isn't installed: it's free from " + variorum.Site + ", for macOS 26 or later on Apple silicon. Then :write walks you through its door for AI."
}

// checkTab is the Variorum tab's state from Irie's files alone: ready once
// Variorum is installed and both its setups are kept, their programs its
// door. It calls no door, which would put a line in Variorum's log.
func checkTab(e Env, on bool) (bool, string) {
	st := variorum.Status(e.Paths.Data)
	text := st.Text
	if userScope() {
		text += " Variorum's door is also set up for every folder in Claude Code's config: that fills Variorum's 16 places with every session, and it and " + brand.Name + "'s setup shut each other out, since Variorum lets claude-code in with one key at a time. Remove it with `claude mcp remove -s user variorum`."
	}
	return st.State == ipc.VariorumReady, text
}

// userScope says whether Claude Code adds Variorum's door to every
// session: whether ~/.claude.json names a server variorum among its own.
func userScope() bool {
	b, err := os.ReadFile(expand(claudeJSON))
	if err != nil {
		return false
	}
	var top struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &top) != nil {
		return false
	}
	_, ok := top.Servers["variorum"]
	return ok
}
