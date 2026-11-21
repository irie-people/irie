//go:build !nofounding

package plugins

import (
	"strconv"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
)

// Founding is the founding plugin's one part, by name.
const Founding = "founding"

// PaneFounding is the kind of the Founding tab's pane: a view of the Mac
// app's own (docs/proposals/plugin-panes.md).
const PaneFounding = "founding"

func init() { built["founding"] = foundingPlugin }

// foundingPlugin is the founding documents (docs/proposals/founding-documents.md):
// the Declaration of Independence, the Constitution, the Bill of Rights and
// the Freedom of Information Act, word for word from the National Archives
// and the United States Code, checked against other copies, to read in the
// Founding tab and for agents to quote, and kept on a local chain. Off
// until asked.
func foundingPlugin() Plugin {
	return Plugin{
		Name:    "founding",
		Title:   "Founding documents",
		Summary: "The Declaration of Independence, the Constitution, the Bill of Rights and the Freedom of Information Act, word for word from the National Archives and the United States Code and checked against other copies: to read and search in the Founding tab, for agents to quote exactly, and kept on a local chain.",
		key:     "plugins.founding",
		on:      func(c config.Config) bool { return c.Plugins.Founding },
		Parts: []Part{
			{
				Name:    Founding,
				Title:   "The founding documents",
				Summary: "Read the four in the Founding tab and search them whatever the spelling. Agents quote them from the founding-documents skill's files, never from memory. Each file's SHA-512 shows a copy is this one, and a chain on this computer keeps every version of them.",
				Brings:  []string{"the Founding tab in the right sidebar", "/" + brand.Slug + ":" + founding.Skill, brand.Slug + " ctl founding"},
				Skills:  []string{founding.Skill},
				Pane:    &ipc.PluginPane{Title: "Founding", Icon: "building.columns", Kind: PaneFounding},
				own:     always,
				check:   checkFounding,
			},
		},
	}
}

// checkFounding says whether the documents read: their texts are built
// into this binary, so it reads no file.
func checkFounding(e Env, on bool) (bool, string) {
	if !on {
		return true, "Off: the Founding tab and the " + founding.Skill + " skill go with it."
	}
	docs, err := founding.Load()
	if err != nil {
		return false, "The documents don't read: " + err.Error() + "."
	}
	return true, "Ready: " + strconv.Itoa(len(docs)) + " documents in the Founding tab, and the " + founding.Skill + " skill in every session."
}
