package dms

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testPings = []string{
	"https://hc-ping.com/00000000-0000-4000-8000-000000000001",
	"https://hc-ping.com/00000000-0000-4000-8000-000000000002",
}

func TestShortcutGolden(t *testing.T) {
	b, err := Shortcut(testPings)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "im-ok.shortcut.plist", b)
	// It is well-formed XML, and a property list to Apple's own checker.
	if err := xml.Unmarshal(b, new(struct{ XMLName xml.Name })); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("plutil"); err == nil {
		f := filepath.Join(t.TempDir(), "x.shortcut")
		os.WriteFile(f, b, 0o600)
		if out, err := exec.Command("plutil", "-lint", f).CombinedOutput(); err != nil {
			t.Errorf("plutil -lint: %v\n%s", err, out)
		}
	}
	text := string(b)
	for _, u := range testPings {
		if strings.Count(text, "<string>"+u+"</string>") != 1 {
			t.Errorf("no URL action for %s", u)
		}
	}
	if strings.Count(text, "is.workflow.actions.downloadurl") != 2 {
		t.Error("want a Get Contents of URL after each URL")
	}
}

func TestShortcutRefusesWhatIsNoPingURL(t *testing.T) {
	for _, bad := range [][]string{nil, {"http://hc-ping.com/x"}, {"https://"}, {"not a url"}} {
		if _, err := Shortcut(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	b, err := Shortcut([]string{"https://hc.example.com/ping/a?b=1&c=<2>"})
	if err != nil || !strings.Contains(string(b), "a?b=1&amp;c=&lt;2&gt;") {
		t.Errorf("escaping: %v\n%s", err, b)
	}
}

func TestContactGolden(t *testing.T) {
	var addrs []string
	for i, u := range testPings {
		a, err := CheckAddress(Ref{Name: "c", UUID: strings.TrimPrefix(u, "https://hc-ping.com/"), PingURL: u}, "")
		if err != nil {
			t.Fatal(err)
		}
		if want := "00000000-0000-4000-8000-00000000000" + string(rune('1'+i)) + "@hc-ping.com"; a != want {
			t.Errorf("address %s, want %s", a, want)
		}
		addrs = append(addrs, a)
	}
	b, err := Contact(addrs)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "im-ok.vcf", b)
	text := strings.ReplaceAll(string(b), "\r\n ", "")
	if !strings.Contains(text, "URL:mailto:"+addrs[0]+","+addrs[1]+"?subject=I%27m%20OK") {
		t.Errorf("no link to both:\n%s", text)
	}
	if _, err := Contact(nil); err == nil {
		t.Error("a contact with no addresses")
	}
	if _, err := CheckAddress(Ref{Name: "x", UUID: "a@b"}, "hc-ping.com"); err == nil {
		t.Error("an address from a UUID with @ in it")
	}
	if a, _ := CheckAddress(Ref{UUID: "u", PingURL: "https://hc.example.com/ping/u"}, "mail.example.com"); a != "u@mail.example.com" {
		t.Errorf("own domain: %s", a)
	}
}

func TestPingReadsTheAnswer(t *testing.T) {
	for body, want := range map[string]string{"OK": "", "OK (not found)": "no check", "OK (rate limited)": "too many"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, body)
		}))
		err := Ping(nil, srv.URL+"/x")
		srv.Close()
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
	}
}

func TestReportFlagsAReminderToMany(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	st := State{Reminder: Ref{Name: "R", UUID: "r"}, Alarm: Ref{Name: "A", UUID: "a"}}
	chans := []Channel{{ID: "c1", Name: "Me", Kind: "email"}, {ID: "c2", Name: "Bob", Kind: "email"}}
	got := strings.Join(Report(st, []Check{
		{Name: "R", UUID: "r", Status: "up", Channels: "c1,c2"},
		{Name: "A", UUID: "a", Status: "up", Channels: "c1,c2"},
	}, chans, now), "\n")
	if !strings.Contains(got, "emails 2 addresses, and should email only you") {
		t.Errorf("report:\n%s", got)
	}
}
