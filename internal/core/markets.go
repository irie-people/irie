//go:build !nomarkets

package core

import (
	"encoding/json"
	"os"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/repo"
)

// marketsBuilt says whether this build has the markets plugin (DESIGN
// §4.17); go build -tags nomarkets leaves it out, and markets_off.go stands
// in.
const marketsBuilt = true

// marketsOp is the markets plugin's map (DESIGN §4.17). It says where the
// page in markets.folder is and how it stands, for :markets and the Mac
// app's Markets window, which shows what the map lacks rather than an
// error. With a task, it starts the task in the map's folder, in place
// rather than in a worktree, so each rebuild of the page shows in the
// window; that needs the plugin on and the folder there.
func (c *Core) marketsOp(a ipc.MarketsArgs) (ipc.MarketsResult, error) {
	cfg := *c.cfg()
	if why := plugins.Why(cfg, plugins.Map); why != "" {
		if a.Task != nil {
			return ipc.MarketsResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", why)
		}
		return ipc.MarketsResult{State: ipc.PartPluginOff, Text: why}, nil
	}
	w, lacks := plugins.FindMap(cfg)
	r := ipc.MarketsResult{State: ipc.PartReady, Folder: w.Folder, Page: w.Page, Changed: w.Changed}
	if a.Task == nil {
		if lacks != "" {
			r.State, r.Text = ipc.PartNeeds, lacks
			return r, nil
		}
		h, _ := os.UserHomeDir()
		r.Text = "Your market map is " + repo.Tilde(w.Page, h) + ". In the Mac app, Window › Markets shows it."
		return r, nil
	}
	if !w.HasFolder() {
		return ipc.MarketsResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", lacks)
	}
	sa := *a.Task
	sa.Repo, sa.InPlace, sa.Cwd = w.Folder, true, ""
	// It is a task the person typed, as a spawn op from their client is:
	// one step of the history, and their word to Gabriel.
	raw, err := json.Marshal(sa)
	if err != nil {
		return ipc.MarketsResult{}, err
	}
	res, err := c.Call(ipc.OpSpawn, raw)
	if err != nil {
		return ipc.MarketsResult{}, err
	}
	sr, ok := res.(ipc.SpawnResult)
	if !ok {
		return ipc.MarketsResult{}, ipc.Errorf(ipc.CodeBadArgs, "the task went to the session before it rather than starting one in the map's folder")
	}
	r.Session, r.Text = &sr.Session, "Started "+sr.Session.Name+" in your market map's folder."
	return r, nil
}
