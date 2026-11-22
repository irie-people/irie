package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/peer"
	"github.com/irie-people/irie/internal/remote"
)

const peerUsage = `usage: ` + brand.Slug + ` peer <command>

Your other machines' ` + brand.Name + `, which this one shows in its list and starts
tasks on with ^NAME (docs/proposals/multi-host.md). Each runs its phone
app's gateway: ` + brand.Slug + ` remote on, there.

  ` + brand.Slug + ` peer add [NAME] LINK
                          pair with the machine whose ` + brand.Slug + ` remote pair --link
                          printed LINK (` + brand.Slug + `://pair?…), and call it NAME here;
                          NAME is the machine's own name when left out
  ` + brand.Slug + ` peer add [NAME] --ssh HOST [--` + brand.Slug + ` PATH]
                          the same, running ` + brand.Slug + ` remote pair --link on HOST
                          over ssh (PATH: ~/.local/share/` + brand.Slug + `/bin/` + brand.Slug + `)
  ` + brand.Slug + ` peer list          the machines this one is paired with, and how each is
  ` + brand.Slug + ` peer fetch [SESSION]
                          fetch the branch of another machine's session, or
                          of each whose turn ended, into the repo of the same
                          name here, as <machine>/<branch> (` + brand.Name + ` does this by
                          itself when a turn ends)
  ` + brand.Slug + ` peer remove NAME   unpair one
`

func peerCmd(args []string) error {
	if len(args) == 0 {
		fmt.Print(aligned(peerUsage))
		return nil
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "add":
		fs := flag.NewFlagSet("peer add", flag.ContinueOnError)
		ssh := fs.String("ssh", "", "run "+brand.Slug+" remote pair --link on this ssh host")
		bin := fs.String(brand.Slug, "$HOME/.local/share/"+brand.Slug+"/bin/"+brand.Slug, brand.Slug+" on the ssh host")
		var pos []string
		// NAME and LINK may come before or after the flags.
		for len(args) > 0 {
			if err := fs.Parse(args); err != nil {
				return err
			}
			if fs.NArg() == 0 {
				break
			}
			pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
		}
		var name, text string
		switch {
		case *ssh != "" && len(pos) <= 1:
			if len(pos) == 1 {
				name = pos[0]
			}
			if text, err = linkOverSSH(*ssh, *bin); err != nil {
				return err
			}
		case *ssh == "" && len(pos) == 1:
			text = pos[0]
		case *ssh == "" && len(pos) == 2:
			name, text = pos[0], pos[1]
		default:
			return errors.New("usage: " + brand.Slug + " peer add [NAME] LINK, or " + brand.Slug + " peer add [NAME] --ssh HOST")
		}
		link, err := peer.ParseLink(text)
		if err != nil {
			return err
		}
		if name == "" {
			name = link.Name
		}
		if err := peer.CheckName(name); err != nil {
			return fmt.Errorf("%w; name it with "+brand.Slug+" peer add NAME …", err)
		}
		me := cfg.Remote.Name
		if me == "" {
			me = remote.MachineName()
		}
		if strings.EqualFold(name, me) {
			return fmt.Errorf("%s is this machine's own name; name the other one", name)
		}
		cl, err := peer.New(link.URL, "", link.Fingerprint)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		info, err := cl.Info(ctx)
		if err != nil {
			return fmt.Errorf("%s doesn't answer at %s: %w", name, link.URL, err)
		}
		if info.API != ipc.API {
			return fmt.Errorf("%s runs "+brand.Name+"'s API %d and this machine %d: install the same "+brand.Name+" on both", name, info.API, ipc.API)
		}
		token, err := cl.Pair(ctx, link.Code, me+" ("+brand.Name+")")
		if err != nil {
			return fmt.Errorf("pairing with %s: %w", name, err)
		}
		if err := peer.Save(p.State, name, peer.Peer{URL: link.URL, Token: token, Fingerprint: link.Fingerprint, Machine: info.ID, SSH: *ssh, Paired: time.Now().UTC()}); err != nil {
			return err
		}
		fmt.Printf("Paired with %s (%s, %s, "+brand.Name+" %s).\n", name, link.URL, info.OS, info.Version)
		reloadPeers(p.Socket(), name)
		return nil
	case "list":
		var res ipc.PeersResult
		if err := ipc.Call(p.Socket(), 5*time.Second, ipc.OpPeers, ipc.PeersArgs{}, &res); err != nil {
			// Without a core, what is kept.
			ps, lerr := peer.Load(p.State)
			if lerr != nil {
				return lerr
			}
			for _, n := range peer.Names(ps) {
				res.Peers = append(res.Peers, ipc.PeerInfo{Name: n, URL: ps[n].URL, Problem: brand.Name + " isn't running here"})
			}
		}
		if len(res.Peers) == 0 {
			fmt.Println("This machine is paired with no other. " + brand.Slug + " peer add pairs one.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSTATE\tSESSIONS\tLOAD\tMEMORY FREE\tTAKES TASKS\tURL")
		for _, i := range res.Peers {
			state := "up since " + i.Since.Local().Format("15:04")
			if !i.Up {
				state = "down: " + i.Problem
			}
			load, mem := "-", "-"
			if i.Load != nil {
				load = fmt.Sprintf("%g on %d CPUs", i.Load.RunQueue, i.Load.CPUs)
			}
			if i.Memory != nil && i.Memory.Total > 0 {
				mem = fmt.Sprintf("%.1f of %.0f GiB", float64(i.Memory.Available)/(1<<30), float64(i.Memory.Total)/(1<<30))
			}
			takes := "yes"
			if i.Refuses != "" {
				takes = "no: " + i.Refuses
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t%s\n", i.Name, state, i.Sessions, load, mem, takes, i.URL)
		}
		return w.Flush()
	case "fetch":
		what := "all"
		switch len(args) {
		case 0:
		case 1:
			what = args[0]
		default:
			return errors.New("usage: " + brand.Slug + " peer fetch [SESSION]")
		}
		var res ipc.PeersResult
		if err := ipc.Call(p.Socket(), 10*time.Minute, ipc.OpPeers, ipc.PeersArgs{Fetch: what}, &res); err != nil {
			return err
		}
		if len(res.Fetched) == 0 {
			fmt.Println("Nothing to fetch: no session of another machine's has ended with a branch.")
			return nil
		}
		for _, f := range res.Fetched {
			if f.Problem != "" {
				fmt.Printf("%s: %s\n", f.Session, f.Problem)
			} else {
				fmt.Printf("%s: fetched as %s (%.12s)\n", f.Session, f.Ref, f.Head)
			}
		}
		return nil
	case "remove":
		if len(args) != 1 {
			return errors.New("usage: " + brand.Slug + " peer remove NAME")
		}
		name := args[0]
		ps, err := peer.Load(p.State)
		if err != nil {
			return err
		}
		pc, ok := ps[name]
		if !ok {
			return fmt.Errorf("this machine is paired with no %q", name)
		}
		// Unpair there too, when it answers; it can also forget this
		// machine itself, with irie remote forget.
		if cl, err := peer.New(pc.URL, pc.Token, pc.Fingerprint); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := cl.Forget(ctx); err != nil {
				fmt.Fprintf(os.Stderr, brand.Slug+": %s didn't hear it (%v); "+brand.Slug+" remote forget there unpairs this machine\n", name, err)
			}
			cancel()
		}
		if err := peer.Remove(p.State, name); err != nil {
			return err
		}
		fmt.Printf("Unpaired from %s.\n", name)
		reloadPeers(p.Socket(), "")
		return nil
	}
	return fmt.Errorf("unknown peer command %q\n\n%s", sub, peerUsage)
}

// linkOverSSH runs irie remote pair --link on host and returns the link.
func linkOverSSH(host, bin string) (string, error) {
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, bin+" remote pair --link")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ssh %s "+brand.Slug+" remote pair --link: %w", host, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, brand.Slug+"://") {
			return l, nil
		}
	}
	return "", fmt.Errorf("%s printed no pairing link; is its "+brand.Name+" from this version?", host)
}

// reloadPeers asks the core to follow the peers as they are now kept.
func reloadPeers(socket, added string) {
	var res ipc.PeersResult
	if err := ipc.Call(socket, 5*time.Second, ipc.OpPeers, ipc.PeersArgs{Reload: true}, &res); err != nil {
		fmt.Println(brand.Name + " isn't running here; it follows its peers once it starts.")
		return
	}
	if added != "" {
		fmt.Printf("Its sessions show in this machine's list as %s/…, and ^%s starts a task there.\n", added, added)
	}
}
