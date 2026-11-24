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

// PaneFounding is the kind of the Charters tab's pane: a view of the Mac
// app's own (docs/proposals/plugin-panes.md).
const PaneFounding = "founding"

func init() { built["founding"] = foundingPlugin }

// foundingPlugin is the charters (docs/proposals/founding-documents.md):
// texts of rights kept word for word from the bodies that keep them, each
// checked against other copies, from Magna Carta through the Declaration of
// Independence, the Constitution and the Bill of Rights to the Freedom of
// Information Act and India's Right to Information Act, to read in the
// Charters tab and for
// agents to quote, and kept on a local chain with the links that say how
// they bear on one another. Off until asked.
func foundingPlugin() Plugin {
	return Plugin{
		Name:    "founding",
		Title:   "Charters",
		Summary: "Thirty-eight texts of rights, word for word from the bodies that keep them and checked against other copies: from Magna Carta, the Petition of Right, the Body of Liberties, the English Bill of Rights and Sweden's Press Act of 1766, through the states' declarations of rights, the Declaration of Independence, the Constitution, the Bill of Rights, the Rights of Man, the Declaration of Sentiments and the Gettysburg Address, to the UN Charter, the Universal Declaration of Human Rights, the European Convention, the two Covenants, the Conventions against racial and sex discrimination and on the rights of the child, the Civil Rights, Voting Rights and Disabilities Acts and the freedom-of-information laws of the United States, the United Kingdom and India. To read and search in the Charters tab, to explore as a map in the Charters Graph, for agents to quote exactly, and kept on a local chain with how each bears on the others.",
		key:     "plugins.founding",
		on:      func(c config.Config) bool { return c.Plugins.Founding },
		Parts: []Part{
			{
				Name:    Founding,
				Title:   "The charters",
				Summary: "Read them in the Charters tab, search them whatever the spelling, and follow how each drew on the ones before it. Explore them as a map in the Charters Graph: the texts on a timeline, their articles and sections, the rights they name and the passages each link quotes. Agents quote them from the founding-documents skill's files, never from memory. Each file's SHA-512 shows a copy is this one, and a chain on this computer keeps every version of the texts and their links.",
				Brings:  []string{"the Charters tab in the right sidebar", "Window › Charters Graph", "/" + brand.Slug + ":" + founding.Skill, brand.Slug + " ctl charters", brand.Slug + " ctl charters graph"},
				Skills:  []string{founding.Skill},
				Pane:    &ipc.PluginPane{Title: "Charters", Icon: "building.columns", Kind: PaneFounding},
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
		return true, "Off: the Charters tab, the Charters Graph and the " + founding.Skill + " skill go with it."
	}
	docs, err := founding.Load()
	if err != nil {
		return false, "The texts don't read: " + err.Error() + "."
	}
	return true, "Ready: " + strconv.Itoa(len(docs)) + " texts in the Charters tab and the Charters Graph, and the " + founding.Skill + " skill in every session."
}
