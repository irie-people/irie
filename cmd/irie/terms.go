//go:build !noterms

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/extension"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/terms"
)

// irie terms: Terms in short (docs/proposals/browser-extension.md), in a
// terminal. go build -tags noterms leaves this file out, and terms_off.go
// stands in.

// termsUsage is Terms in short's lines in the usage.
const termsUsage = `  ` + brand.Slug + ` terms [ask|chat|serve] ADDRESS|FILE [QUESTION]
                          a site's terms of service in plain words, and answers
                          to questions about them, by ` + brand.Name + `'s own code
                          (` + brand.Slug + ` terms help)
`

const termsHelp = `usage:
  ` + brand.Slug + ` terms [--json] [--privacy] [--allow-local] ADDRESS|FILE|-
                       a site's terms of service in plain words: the points
                       that matter, each with the clause it comes from. An
                       address of any page of the site finds its terms; a
                       file or - (standard input) is the terms' own HTML or
                       text
  ` + brand.Slug + ` terms ask [--model] [--json] ADDRESS|FILE|- QUESTION...
                       what the terms say to a question, such as "can I get
                       a refund?"; --model words the answer with a local
                       model (extension.model, else the best Ollama has),
                       from the passages ` + brand.Name + `'s code finds
  ` + brand.Slug + ` terms chat [--model] ADDRESS|FILE|-
                       ask one question a line, until an empty line
  ` + brand.Slug + ` terms serve [--port N]
                       the Terms tab's page on this computer, at
                       http://127.0.0.1:N/ (7393 unless you say), which the
                       core serves itself while the plugin is on
` + brand.Name + `'s own code reads the terms on this computer: no tokens, nothing sent
anywhere but the fetch of the page. Not legal advice.
`

func init() {
	pluginCmds["terms"] = pluginCmd{run: func(args []string) error { return termsCmd(args, os.Stdin, os.Stdout) }}
}

func termsCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, _, err := load()
	if err != nil {
		return err
	}
	if why := plugins.Why(cfg, plugins.Terms); why != "" {
		return errors.New(why)
	}
	sub := ""
	if len(args) > 0 && (args[0] == "ask" || args[0] == "chat" || args[0] == "serve" || args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("terms", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print JSON")
	privacy := fs.Bool("privacy", false, "read the site's privacy policy rather than its terms")
	local := fs.Bool("allow-local", false, "fetch from this computer or its local network")
	useModel := fs.Bool("model", false, "word answers with a local model")
	port := fs.Int("port", terms.PagePort, "the port the page is served on")
	if sub == "help" || sub == "-h" || sub == "--help" {
		fmt.Fprint(stdout, termsHelp)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%v\n\n%s", err, termsHelp)
	}
	rest := fs.Args()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if sub == "serve" {
		return termsServe(ctx, *port, stdout)
	}
	if len(rest) == 0 || (sub == "ask" && len(rest) < 2) {
		return errors.New(strings.TrimSpace(termsHelp))
	}
	want := ""
	if *privacy {
		want = terms.KindPrivacy
	}
	d, err := termsDoc(ctx, rest[0], stdin, *local, want)
	if err != nil {
		return err
	}
	var m *terms.Model
	if *useModel {
		got, err := terms.FindModel(ctx, terms.Servers(cfg), cfg.Extension.Model)
		if err != nil {
			return err
		}
		m = &got
	}
	switch sub {
	case "ask":
		return termsAnswer(ctx, stdout, d, m, strings.Join(rest[1:], " "), *asJSON)
	case "chat":
		if rest[0] == "-" {
			return errors.New("terms chat reads its questions from standard input, so its terms need an address or a file")
		}
		fmt.Fprintf(stdout, "%s\n%s\n\nAsk about these terms, one question a line; an empty line ends.\n", d.Title, d.Tally())
		in := bufio.NewScanner(stdin)
		for {
			fmt.Fprint(stdout, "\n? ")
			if !in.Scan() || strings.TrimSpace(in.Text()) == "" {
				return in.Err()
			}
			if err := termsAnswer(ctx, stdout, d, m, in.Text(), false); err != nil {
				return err
			}
		}
	}
	if *asJSON {
		return json.NewEncoder(stdout).Encode(d)
	}
	_, err = fmt.Fprint(stdout, d.Text())
	return err
}

// termsDoc reads the terms that src names: an address, a file, or - for
// standard input.
func termsDoc(ctx context.Context, src string, stdin io.Reader, local bool, want string) (*terms.Doc, error) {
	read := func(name string, b []byte) *terms.Doc {
		if looksHTML(b) {
			return terms.FromHTML("", b)
		}
		return terms.FromText("", name, string(b))
	}
	if src == "-" {
		b, err := io.ReadAll(io.LimitReader(stdin, terms.MaxPage))
		if err != nil {
			return nil, err
		}
		return read("", b), nil
	}
	if st, err := os.Stat(src); err == nil && !st.IsDir() {
		f, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(f, terms.MaxPage))
		f.Close()
		if err != nil {
			return nil, err
		}
		d := read(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)), b)
		return d, nil
	}
	address := src
	if !strings.Contains(address, "://") {
		if !strings.Contains(address, ".") {
			return nil, fmt.Errorf("%q is neither a file nor a web address", src)
		}
		address = "https://" + address
	}
	d, err := terms.Locate(ctx, terms.Fetcher{Local: local}, address, nil, want)
	var nf *terms.NotFoundError
	if errors.As(err, &nf) && len(nf.Others) > 0 {
		var b strings.Builder
		b.WriteString(err.Error())
		b.WriteString(". Links that may be them:")
		for _, l := range nf.Others {
			fmt.Fprintf(&b, "\n  %s  %s", l.Text, l.URL)
		}
		return nil, errors.New(b.String())
	}
	return d, err
}

// looksHTML says whether b is HTML rather than plain text.
func looksHTML(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 4096)]))
	return strings.Contains(head, "<html") || strings.Contains(head, "<body") || strings.Contains(head, "<!doctype html") || strings.Count(head, "<p") > 2 || strings.Count(head, "<div") > 2
}

func termsAnswer(ctx context.Context, stdout io.Writer, d *terms.Doc, m *terms.Model, q string, asJSON bool) error {
	var a terms.Answer
	if m != nil {
		var err error
		part := func(s string) { fmt.Fprint(stdout, s) }
		if asJSON {
			part = nil
		}
		a, err = d.AskModel(ctx, *m, q, part)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s couldn't answer (%v), so %s's code did\n", brand.Slug, m.Name, err, brand.Name)
		} else if !asJSON {
			fmt.Fprintln(stdout)
		}
		if err == nil && !asJSON {
			printQuotes(stdout, a.Quotes, true)
			return nil
		}
	} else {
		a = d.Ask(q)
	}
	if asJSON {
		return json.NewEncoder(stdout).Encode(a)
	}
	fmt.Fprintln(stdout, a.Says)
	printQuotes(stdout, a.Quotes, false)
	return nil
}

func printQuotes(stdout io.Writer, qs []terms.Quote, numbered bool) {
	for i, q := range qs {
		lead := "  "
		if numbered {
			lead = "  [" + strconv.Itoa(i+1) + "] "
		}
		s := lead + "\"" + q.Text + "\""
		if q.Section != "" {
			s += " (" + q.Section + ")"
		}
		fmt.Fprintln(stdout, s)
	}
}

// termsServe serves the panel as a page on this computer (irie terms
// serve), until it is stopped.
func termsServe(ctx context.Context, port int, stdout io.Writer) error {
	_, p, err := load()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		if port == terms.PagePort && servedHere(port) {
			fmt.Fprintf(stdout, "%s's core serves it already, at http://127.0.0.1:%d/: the Terms tab in the Mac app's right sidebar shows it.\n", brand.Name, port)
			return nil
		}
		return fmt.Errorf("serving on port %d: %w; --port picks another", port, err)
	}
	fmt.Fprintf(stdout, "Terms in short is at http://127.0.0.1:%d/ on this computer. Ctrl-C stops it.\n", port)
	return extension.ServeWeb(ctx, ln, extension.Options{
		Paths: p,
		Load: func() (config.Config, error) {
			cfg, _, err := load()
			return cfg, err
		},
		Why:     func(cfg config.Config, part string) string { return plugins.Why(cfg, part) },
		Servers: terms.Servers,
	})
}

// servedHere says whether Irie's page answers at port on this computer.
func servedHere(port int) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/brand.js")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode == http.StatusOK && strings.Contains(string(b), "IRIE_BRAND")
}
