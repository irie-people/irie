package plugins

import (
	"os"

	"github.com/irie-people/irie/internal/config"
)

// Variorum is the writing plugin's one part, by name.
const Variorum = "variorum"

// variorumSkill is the writing session's skill.
const variorumSkill = "variorum"

// variorumSite is where Variorum, Lillian Duarte's writing app for the Mac,
// comes from (docs/proposals/writers.md).
const variorumSite = "https://stargazing.tech/variorum"

// variorumApps are where the check looks for Variorum: the Applications
// folders a Mac app is dragged into. Tests point them elsewhere.
var variorumApps = []string{"/Applications/Variorum.app", "~/Applications/Variorum.app"}

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
				Brings:  []string{":write", "File › Write with Variorum…", "/irie:variorum"},
				Refused: "Writing in Variorum is left out: skills.disabled names variorum.",
				Skills:  []string{variorumSkill},
				own: func(c config.Config) bool {
					return c.Skills.Enabled && !has(c.Skills.Disabled, variorumSkill)
				},
				check: checkVariorum,
			},
		},
	}
}

func checkVariorum(e Env, on bool) (bool, string) {
	if !on {
		return true, "Left out: skills.disabled names variorum."
	}
	for _, app := range variorumApps {
		if st, err := os.Stat(expand(app)); err == nil && st.IsDir() {
			return true, "Ready: :write, or File › Write with Variorum… in the Mac app, starts a writing session. The first one walks you through Variorum's door for AI."
		}
	}
	return false, "Variorum isn't installed: it's free from " + variorumSite + ", for macOS 26 or later on Apple silicon. Then :write walks you through its door for AI."
}
