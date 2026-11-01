package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/core"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/remote"
	"github.com/irie-people/irie/internal/version"
)

const remoteUsage = `usage: irie remote <command>

The phone app (docs/PHONE.md): this machine's Irie on your phone, over your
local network. Turn it on with remote.on = true in the config; the core then
keeps it running.

  irie remote on [--name NAME]
                          turn the phone app on for this machine (remote.on),
                          and name it for the phone
  irie remote off         turn it off
  irie remote pair [--for DURATION] [--open]
                          open a pairing code for a phone (10 minutes by
                          default) and say where the phone sets up; --open
                          shows it with a QR code in the browser
  irie remote devices     list the phones paired with this machine
  irie remote forget ID|NAME|all
                          unpair a phone
  irie remote ca export   print the certificate authority with its key, for
                          another machine of yours to import
  irie remote ca import   take an authority from stdin, so one profile on the
                          phone serves both machines
  irie remote ca profile  print the authority as an iOS profile
  irie remote serve [--supervise]
                          the gateway itself, which the core runs
`

func remoteCmd(args []string) error {
	if len(args) == 0 {
		fmt.Print(remoteUsage)
		return nil
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	dir := p.Remote()
	store := &remote.Store{Dir: dir}
	sub, args := args[0], args[1:]
	switch sub {
	case "serve":
		fs := flag.NewFlagSet("remote serve", flag.ContinueOnError)
		supervise := fs.Bool("supervise", false, "restart it whenever it exits")
		if err := fs.Parse(args); err != nil {
			return err
		}
		log := openLog(p, p.RemoteLog()).With("proc", "remote")
		if *supervise {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			os.Exit(core.Supervise(exe, []string{"remote", "serve"}, log))
		}
		srv, err := remote.New(remote.Options{
			Socket: p.Socket(), Dir: dir, Listen: cfg.Remote.Listen, Port: cfg.Remote.Port,
			Name: cfg.Remote.Name, Version: version.String(), Log: log,
		})
		if err != nil {
			log.Error("start", "err", err)
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		defer stop()
		if err := srv.Run(ctx); err != nil {
			log.Error("serve", "err", err)
			return err
		}
		return nil
	case "pair":
		fs := flag.NewFlagSet("remote pair", flag.ContinueOnError)
		life := fs.Duration("for", remote.CodeLife, "how long the code works")
		open := fs.Bool("open", false, "show the code and a QR code in the browser")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *life <= 0 || *life > 7*24*time.Hour {
			return errors.New("--for takes a duration up to a week, such as 10m or 2h")
		}
		return pair(cfg.Remote.On, cfg.Remote.Port, cfg.Remote.Name, dir, store, *life, *open)
	case "on", "off":
		fs := flag.NewFlagSet("remote "+sub, flag.ContinueOnError)
		name := fs.String("name", "", "what the phone calls this machine")
		if err := fs.Parse(args); err != nil {
			return err
		}
		set := []ipc.ConfigSet{{Key: "remote.on", Value: json.RawMessage(strconv.FormatBool(sub == "on"))}}
		if *name != "" {
			b, _ := json.Marshal(*name)
			set = append(set, ipc.ConfigSet{Key: "remote.name", Value: b})
		}
		// The core edits the file in place and runs on it at once, which
		// starts or stops the gateway within a second.
		var res ipc.ConfigResult
		if err := ipc.Call(p.Socket(), 5*time.Second, ipc.OpConfig, ipc.ConfigArgs{Set: set}, &res); err != nil {
			return fmt.Errorf("the core didn't take it (%w); set remote.on in %s by hand, then run irie up", err, p.ConfigFile())
		}
		if sub == "off" {
			fmt.Println("The phone app is off on this machine.")
			return nil
		}
		fmt.Printf("The phone app is on, on port %d. Pair a phone with `irie remote pair`.\n", cfg.Remote.Port)
		return nil
	case "devices":
		ds, err := store.Devices()
		if err != nil {
			return err
		}
		if len(ds) == 0 {
			fmt.Println("No phone is paired with this machine. `irie remote pair` pairs one.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPAIRED\tLAST USED")
		for _, d := range ds {
			seen := "-"
			if !d.Seen.IsZero() {
				seen = d.Seen.Local().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Created.Local().Format("2006-01-02 15:04"), seen)
		}
		return w.Flush()
	case "forget":
		if len(args) != 1 {
			return errors.New("usage: irie remote forget ID|NAME|all")
		}
		gone, err := store.Forget(args[0])
		if err != nil {
			return err
		}
		if len(gone) == 0 {
			return fmt.Errorf("no paired phone is %q; `irie remote devices` lists them", args[0])
		}
		for _, d := range gone {
			fmt.Printf("Forgot %s (%s).\n", d.Name, d.ID)
		}
		return nil
	case "ca":
		if len(args) != 1 {
			return errors.New("usage: irie remote ca export|import|profile")
		}
		switch args[0] {
		case "export":
			a, err := remote.LoadAuthority(dir)
			if err != nil {
				return err
			}
			b, err := a.Export()
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(b)
			return err
		case "import":
			data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
			if err != nil {
				return err
			}
			a, err := remote.Import(dir, data)
			if err != nil {
				return err
			}
			fmt.Printf("This machine now uses %s (%s).\nRestart the gateway to serve with it: it restarts with the core.\n", a.Cert.Subject.CommonName, a.Fingerprint())
			return nil
		case "profile":
			a, err := remote.LoadAuthority(dir)
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(a.Profile())
			return err
		}
		return errors.New("usage: irie remote ca export|import|profile")
	case "help", "-h", "--help":
		fmt.Print(remoteUsage)
		return nil
	}
	return fmt.Errorf("unknown remote command %q\n\n%s", sub, remoteUsage)
}

// pair opens a pairing code and says where a phone sets up, in the
// terminal, and with --open in the browser too.
func pair(on bool, port int, name, dir string, store *remote.Store, life time.Duration, open bool) error {
	code, err := store.NewCode(life)
	if err != nil {
		return err
	}
	if name == "" {
		name = remote.MachineName()
	}
	setup := strconv.Itoa(port - 1)
	var urls []string
	for _, n := range remote.LocalNames() {
		urls = append(urls, "http://"+net.JoinHostPort(n, setup)+"/")
	}
	if ip := remote.LANAddr(); ip != nil {
		urls = append(urls, "http://"+net.JoinHostPort(ip.String(), setup)+"/")
	}
	if len(urls) == 0 {
		urls = append(urls, "http://<this machine's address>:"+setup+"/")
	}
	until := time.Now().Add(life)
	spaced := code[:3] + " " + code[3:]
	if !on {
		fmt.Println("The phone app is off on this machine: set remote.on = true in the config, and the core starts it.")
		fmt.Println()
	}
	fmt.Printf("Pair a phone with %s\n\n", name)
	fmt.Println("1. On the phone, on this machine's Wi-Fi, open:")
	for i, u := range urls {
		if i == 0 {
			fmt.Printf("     %s\n", u)
		} else {
			fmt.Printf("     or %s\n", u)
		}
	}
	fmt.Println("   and follow its steps: install Irie's certificate, open Irie, add it to the Home Screen.")
	if ca, err := remote.LoadAuthority(dir); err == nil {
		fmt.Printf("   Before you trust the certificate, check under its More Details that its SHA-256\n   begins %s. A stranger on the Wi-Fi could hand you another.\n", ca.FingerprintStart())
	}
	fmt.Printf("2. In the app, enter the code  %s  (it works until %s).\n\n", spaced, until.Format("15:04"))
	if q, err := terminalQR(urls[0]); err == nil {
		fmt.Println(q)
	}
	if open {
		page := filepath.Join(dir, "pair.html")
		fp := ""
		if ca, err := remote.LoadAuthority(dir); err == nil {
			fp = ca.FingerprintStart()
		}
		html, err := pairPage(name, urls, spaced, until, fp)
		if err != nil {
			return err
		}
		if err := filetx.WriteFile(page, []byte(html), 0o600); err != nil {
			return err
		}
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "/usr/bin/open"
		}
		if err := exec.Command(opener, page).Start(); err != nil {
			fmt.Printf("Open %s in a browser to see the QR code.\n", page)
		}
	}
	return nil
}

// terminalQR draws a QR code of text in half blocks, two rows of modules a
// line, dark on light whatever the terminal's colours, with the quiet zone
// a scanner needs.
func terminalQR(text string) (string, error) {
	c, err := remote.QR(text)
	if err != nil {
		return "", err
	}
	const quiet = 2
	var b strings.Builder
	for y := -quiet; y < c.Size+quiet; y += 2 {
		b.WriteString("\x1b[30;47m")
		for x := -quiet; x < c.Size+quiet; x++ {
			top, bottom := c.Black(x, y), c.Black(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}

// pairPage is a page that shows the code and a QR code of the setup page,
// for a phone's camera.
func pairPage(name string, urls []string, code string, until time.Time, fingerprint string) (string, error) {
	svg, err := remote.QRSVG(urls[0])
	if err != nil {
		return "", err
	}
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
	}
	var more strings.Builder
	for _, u := range urls[1:] {
		fmt.Fprintf(&more, "<div class=alt>or %s</div>", esc(u))
	}
	return fmt.Sprintf(`<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1">
<title>Pair a phone with %[1]s</title>
<style>
:root{color-scheme:light dark;--ink:#1E2B22;--muted:#56635A;--ground:#F4F4EC;--card:#FBFBF6;--accent:#2F5D46}
@media (prefers-color-scheme:dark){:root{--ink:#ECF0E6;--muted:#B3BFB5;--ground:#141C17;--card:#1C2620;--accent:#9CC5A1}}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--ground);color:var(--ink);font:16px/1.45 -apple-system,BlinkMacSystemFont,"SF Pro Text",system-ui,sans-serif}
main{background:var(--card);border-radius:28px;padding:40px 44px;max-width:440px;box-shadow:0 20px 60px #0002;text-align:center}
h1{font-size:24px;margin:0 0 6px}p{color:var(--muted);margin:0 0 22px}
.qr{background:#fff;border-radius:18px;padding:14px;display:inline-block}.qr svg{width:240px;height:240px;display:block}
.url{font:15px ui-monospace,SFMono-Regular,Menlo,monospace;margin-top:16px;word-break:break-all}.alt{font:13px ui-monospace,Menlo,monospace;color:var(--muted)}
.code{font:600 44px ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.08em;color:var(--accent);margin:22px 0 4px}
small{color:var(--muted)}
</style>
<main>
<h1>Pair a phone with %[1]s</h1>
<p>Scan this with the phone's camera, on this machine's Wi-Fi, and follow the steps it shows.</p>
<div class=qr>%[2]s</div>
<div class=url>%[3]s</div>%[4]s
<div class=code>%[5]s</div>
<small>Enter this code in the app. It works until %[6]s.</small>
<p style="margin:20px 0 0;font-size:14px">Before you trust Irie's certificate on the phone, check under its More Details that its SHA-256 begins<br><b style="font:600 15px ui-monospace,Menlo,monospace">%[7]s</b></p>
</main>`, esc(name), svg, esc(urls[0]), more.String(), esc(code), until.Format("15:04"), esc(fingerprint)), nil
}
