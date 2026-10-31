package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
)

// preflightTimeout bounds a check, which may load a model's weights.
const preflightTimeout = 4 * time.Minute

// localStatus asks the core about the local server, starting Irie's own.
func localStatus(sock string) (ipc.LocalResult, error) {
	var r ipc.LocalResult
	err := ipc.Call(sock, 30*time.Second, ipc.OpLocal, ipc.LocalArgs{Start: true}, &r)
	return r, err
}

func ctlLocal(args []string) error {
	fs := flag.NewFlagSet("ctl local", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print it as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	r, err := localStatus(p.Socket())
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	printLocal(os.Stdout, r)
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

// printLocal prints the server, the machine's memory and the models, the
// cheapest in memory first.
func printLocal(w io.Writer, r ipc.LocalResult) {
	whose := "your own"
	if r.Own {
		whose = "Irie's own"
	}
	fmt.Fprintf(w, "server   %s, %s", r.URL, whose)
	if r.Version != "" {
		fmt.Fprintf(w, ", Ollama %s", r.Version)
	}
	if r.Own {
		fmt.Fprintf(w, ", %s context, %s cache", localmodel.Tokens(r.Context), r.KVCache)
	}
	fmt.Fprintln(w)
	if m := r.Memory; m.Total > 0 {
		// Sizes are decimal, as Ollama gives them; a Mac's memory is sold in
		// binary gigabytes, so that is named too.
		fmt.Fprintf(w, "memory   %s (%.0f GiB), %s free; local models may take %s; agents take %s, local models %s\n",
			localmodel.Bytes(m.Total), float64(m.Total)/localmodel.GiB, localmodel.Bytes(m.Available), localmodel.Bytes(m.Budget), localmodel.Bytes(m.Agents), localmodel.Bytes(m.Local))
	}
	if r.Disk > 0 {
		fmt.Fprintf(w, "disk     %s free for models\n", localmodel.Bytes(r.Disk))
	}
	switch {
	case r.Chosen != "":
		fmt.Fprintf(w, "+local   %s\n", r.Chosen)
	case r.Pick != "":
		fmt.Fprintf(w, "+local   %s, the best model here that fits; `irie setup local` chooses one for good\n", r.Pick)
	}
	if len(r.Models) == 0 {
		return
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tWEIGHTS\tMEMORY\tNOTE")
	for _, m := range r.Models {
		need := localmodel.Bytes(m.Need)
		if !m.Measured {
			need = "~" + need
		}
		var note []string
		switch {
		case m.Remote:
			note = append(note, "cloud")
		case m.Why != "":
			note = append(note, m.Why)
		case !m.Fits:
			note = append(note, "too big for this machine")
		}
		if m.Loaded {
			note = append(note, "in memory")
		}
		if m.Name == r.Pick || m.Name == r.Chosen {
			note = append(note, "+local runs it")
		}
		if m.About != "" {
			note = append(note, m.About)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.Name, localmodel.Bytes(m.Size), need, strings.Join(note, ", "))
	}
	tw.Flush()
}

func ctlPreflight(args []string) error {
	fs := flag.NewFlagSet("ctl preflight", flag.ContinueOnError)
	model := fs.String("model", "", "check this model instead of the profile's")
	force := fs.Bool("force", false, "load the model even when the memory free now is short")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("usage: irie ctl preflight [--model MODEL] [--force] [PROFILE]")
	}
	profile := strings.TrimPrefix(fs.Arg(0), "+")
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	var r ipc.PreflightResult
	if err := ipc.Call(p.Socket(), preflightTimeout, ipc.OpPreflight, ipc.PreflightArgs{Profile: profile, Model: *model, Force: *force}, &r); err != nil {
		return err
	}
	printCheck(os.Stdout, r)
	if !r.OK {
		return errors.New("a spawn would be refused")
	}
	return nil
}

func printCheck(w io.Writer, r ipc.PreflightResult) {
	if r.Model != "" {
		line := r.Model + " at " + r.URL
		if r.Context > 0 {
			line += ": " + localmodel.Tokens(r.Context) + " context"
		}
		if r.Need > 0 {
			line += ", " + localmodel.Bytes(r.Need) + " in memory"
			if r.VRAM > 0 && r.VRAM < r.Need {
				line += " (" + localmodel.Bytes(r.VRAM) + " of it on the GPU)"
			}
		}
		fmt.Fprintln(w, line)
	}
	for _, s := range r.Problems {
		fmt.Fprintln(w, "  refused:", s)
	}
	for _, s := range r.Warnings {
		fmt.Fprintln(w, "  note:", s)
	}
	if r.OK {
		fmt.Fprintln(w, "  ok")
	}
}

func setup(args []string) error {
	if len(args) == 0 || args[0] != "local" {
		return errors.New("usage: irie setup local [--model MODEL] [--yes]")
	}
	return setupLocal(args[1:], os.Stdin, os.Stdout)
}

// setupLocal is `irie setup local`: it makes +local ready on this machine.
// It installs Ollama by its official route if it is missing, starts Irie's
// own server, picks a model that fits the machine's memory (one already
// there, else the largest suggestion that fits), pulls it, checks it as a
// spawn would, and makes it the model of +local. Nothing is installed or
// pulled without a yes.
func setupLocal(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("setup local", flag.ContinueOnError)
	model := fs.String("model", "", "the model to use, pulled when it is not there")
	yes := fs.Bool("yes", false, "install Ollama and pull the model without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: irie setup local [--model MODEL] [--yes]")
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	in := bufio.NewReader(stdin)
	ask := func(q string) bool {
		if *yes {
			return true
		}
		if !isTerminal(stdin) {
			return false
		}
		fmt.Fprintf(stdout, "%s [y/N] ", q)
		line, _ := in.ReadString('\n')
		a := strings.ToLower(strings.TrimSpace(line))
		return a == "y" || a == "yes"
	}

	if cfg.Local.URL == "" {
		if _, err := localmodel.FindOllama(cfg.Local.Ollama); err != nil {
			inst := localmodel.HowToInstallHere()
			fmt.Fprintf(stdout, "Ollama is not installed. Its route here: %s\n", inst.Hint)
			if inst.Argv == nil {
				return errors.New("install Ollama, then run `irie setup local` again")
			}
			if !ask("Run `" + strings.Join(inst.Argv, " ") + "` now?") {
				return errors.New("Ollama is not installed; run `irie setup local --yes` to install it")
			}
			cmd := exec.Command(inst.Argv[0], inst.Argv[1:]...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("installing Ollama: %w", err)
			}
			if _, err := localmodel.FindOllama(cfg.Local.Ollama); err != nil {
				return errors.New("Ollama is still not found after installing it; open a new terminal and run `irie setup local` again")
			}
		}
	}

	if err := up(nil); err != nil {
		return err
	}
	st, err := localStatus(p.Socket())
	if err != nil {
		return err
	}
	if st.Error != "" {
		return errors.New(st.Error)
	}
	printLocal(stdout, st)
	fmt.Fprintln(stdout)

	choice := *model
	if choice == "" {
		choice = pickModel(st)
		if choice == "" {
			return fmt.Errorf("no suggested model fits the %s local models may take here; set local.memory_gib, or name a model with --model", localmodel.Bytes(st.Memory.Budget))
		}
		if s, ok := localmodel.Suggested(choice); ok {
			n := localmodel.Estimate(s.Size, s.Info, st.Context, st.KVCache, 1)
			fmt.Fprintf(stdout, "The best model for this machine: %s (%s), %s of weights, about %s in memory at a %s context.\n",
				choice, s.About, localmodel.Bytes(s.Size), localmodel.Bytes(n.Total), localmodel.Tokens(st.Context))
		}
		if isTerminal(stdin) && !*yes {
			fmt.Fprintf(stdout, "Model for +local [%s]: ", choice)
			if line, _ := in.ReadString('\n'); strings.TrimSpace(line) != "" {
				choice = strings.TrimSpace(line)
			}
		}
	}
	if i := slices.IndexFunc(st.Models, func(m ipc.LocalModel) bool { return localmodel.SameModel(m.Name, choice) }); i >= 0 {
		choice = st.Models[i].Name
	} else {
		size := ""
		if s, ok := localmodel.Suggested(choice); ok {
			size = fmt.Sprintf(" (about %s to download)", localmodel.Bytes(s.Size))
			if !pullFits(st, s.Size) {
				return fmt.Errorf("%s takes %s and the disk has %s free; Irie leaves %s free, so free some space or name a smaller model with --model", choice, localmodel.Bytes(s.Size), localmodel.Bytes(st.Disk), localmodel.Bytes(localmodel.PullReserve))
			}
		}
		if !ask(fmt.Sprintf("Pull %s%s?", choice, size)) {
			return fmt.Errorf("%s is not there; run `irie setup local --model %s --yes` to pull it", choice, choice)
		}
		if err := pull(stdout, st.URL, choice); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "Checking %s as a spawn would, which loads it…\n", choice)
	var r ipc.PreflightResult
	if err := ipc.Call(p.Socket(), preflightTimeout, ipc.OpPreflight, ipc.PreflightArgs{Profile: "local", Model: choice, Remember: true}, &r); err != nil {
		return err
	}
	printCheck(stdout, r)
	if !r.OK {
		return errors.New("the model did not pass; +local is unchanged")
	}
	fmt.Fprintf(stdout, "Ready: +local runs Claude Code on %s through %s. Its tokens cost nothing; it takes %s of memory while loaded.\n", choice, r.URL, localmodel.Bytes(r.Need))
	return nil
}

// pickModel is the model setup offers: the best suggestion that fits the
// machine, which the server has or the disk can take; else the largest
// model there that an agent can run and the machine can hold.
func pickModel(st ipc.LocalResult) string {
	fitting, _ := localmodel.Fitting(st.Memory.Budget, st.Context, st.KVCache, 1)
	for _, s := range fitting {
		if slices.ContainsFunc(st.Models, func(m ipc.LocalModel) bool { return localmodel.SameModel(m.Name, s.Name) }) || pullFits(st, s.Size) {
			return s.Name
		}
	}
	best := ""
	var need int64
	for _, m := range st.Models {
		if m.Why == "" && m.Fits && !m.Remote && m.Need > need {
			best, need = m.Name, m.Need
		}
	}
	return best
}

// pullFits reports whether the disk can take a pull of size bytes and keep
// localmodel.PullReserve free. An unknown free space lets it.
func pullFits(st ipc.LocalResult, size int64) bool {
	return st.Disk == 0 || st.Disk-size >= localmodel.PullReserve
}

// pull pulls model through the server at url, printing its progress.
func pull(w io.Writer, url, model string) error {
	last := ""
	err := (localmodel.Ollama{URL: url}).Pull(context.Background(), model, func(p localmodel.Progress) {
		line := p.Status
		if p.Total > 0 {
			line = fmt.Sprintf("%s %3d%% of %s", strings.Fields(p.Status)[0], p.Completed*100/p.Total, localmodel.Bytes(p.Total))
		}
		if line != last {
			fmt.Fprintf(w, "\r\033[K%s", line)
			last = line
		}
	})
	fmt.Fprintln(w)
	if err != nil {
		return fmt.Errorf("pulling %s: %w", model, err)
	}
	return nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
