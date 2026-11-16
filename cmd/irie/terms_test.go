//go:build !noextension

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/terms"
)

const termsPage = `<html><head><title>Terms of Service</title></head><body><main>
<h1>Terms of Service</h1>
<p>These Terms are an agreement between you and Example, Inc. ("Example", "we"). By using the Service you agree to them.</p>
<h2>Payments</h2><p>All fees are non-refundable. Your subscription will automatically renew until you cancel.</p>
<h2>Disputes</h2><p>Any dispute will be resolved by binding arbitration administered by JAMS, and not in court.</p>
</main></body></html>`

// TestIrieTermsReadsAFile: irie terms prints a file's points, --json the
// document, ask an answer from Irie's own code; standard input is read too;
// and with the plugin off it says how to turn it on.
func TestIrieTermsReadsAFile(t *testing.T) {
	bare(t)
	file := filepath.Join(t.TempDir(), "terms.html")
	if err := os.WriteFile(file, []byte(termsPage), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(stdin string, args ...string) (string, error) {
		var out bytes.Buffer
		err := termsCmd(args, strings.NewReader(stdin), &out)
		return out.String(), err
	}
	out, err := run("", file)
	if err != nil || !strings.Contains(out, "✕ No refunds") || !strings.Contains(out, "Disputes go to private arbitration") || !strings.Contains(out, "Not legal advice") {
		t.Errorf("irie terms FILE: %v\n%s", err, out)
	}
	out, err = run("", "--json", file)
	var d terms.Doc
	if err != nil || json.Unmarshal([]byte(out), &d) != nil || d.Kind != terms.KindTerms || len(d.Points) < 3 {
		t.Errorf("--json: %v\n%s", err, out)
	}
	out, err = run("", "ask", file, "can", "I", "get", "a", "refund?")
	if err != nil || !strings.Contains(out, "they don't give refunds") || !strings.Contains(out, "non-refundable") {
		t.Errorf("ask: %v\n%s", err, out)
	}
	out, err = run(termsPage, "-")
	if err != nil || !strings.Contains(out, "No refunds") {
		t.Errorf("stdin: %v\n%s", err, out)
	}
	if _, err := run("", "ask", file); err == nil {
		t.Error("ask without a question")
	}
	if _, err := run("", "nonsense-without-a-dot"); err == nil || !strings.Contains(err.Error(), "neither a file nor a web address") {
		t.Errorf("not an address: %v", err)
	}
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(p.Config, 0o700)
	if err := os.WriteFile(p.ConfigFile(), []byte("[plugins]\nextension = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run("", file); err == nil || !strings.Contains(err.Error(), "plugins on extension") {
		t.Errorf("off: %v", err)
	}
}
