package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/dms"
	"github.com/irie-people/irie/internal/skills"
)

// dmsEnv gives the helpers folders of their own, as a scratch HOME.
func dmsEnv(t *testing.T) dms.Store {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "state"), "XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_DATA_HOME": filepath.Join(home, "data")} {
		t.Setenv(k, v)
	}
	return dms.Store{Dir: filepath.Join(home, "state", "irie", "dms")}
}

// stdin feeds text to the command's standard input.
func stdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, text)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
}

// captured runs f and returns what it printed.
func captured(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	ferr := f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b), ferr
}

func TestDMSKeyIsKeptAndNeverPrinted(t *testing.T) {
	store := dmsEnv(t)
	const key = "hcw_thewholekeythatnevershows0a1b2"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"checks": []any{}})
	}))
	defer srv.Close()
	stdin(t, key+"\n")
	out, err := captured(t, func() error { return dmsCmd([]string{"key", "--api", srv.URL + "/api/v3/"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, key) || !strings.Contains(out, "(…a1b2)") || !strings.Contains(out, "It works") {
		t.Errorf("printed:\n%s", out)
	}
	if got, err := store.Key(); err != nil || got != key {
		t.Errorf("kept %q, %v", got, err)
	}
	if st, ok, _ := store.Load(); !ok || st.API != srv.URL+"/api/v3/" {
		t.Errorf("the API was not kept for the checks: %+v", st)
	}
}

func TestDMSNeedsChecksFirst(t *testing.T) {
	dmsEnv(t)
	for _, sub := range []string{"status", "ping", "pause"} {
		if err := dmsCmd([]string{sub}); err == nil || !strings.Contains(err.Error(), "irie dms checks") {
			t.Errorf("%s: err = %v", sub, err)
		}
	}
	if err := dmsCmd([]string{"checks"}); err == nil || !strings.Contains(err.Error(), "--alarm-name") {
		t.Errorf("checks with no alarm name: %v", err)
	}
}

func TestDMSCalendarFile(t *testing.T) {
	dmsEnv(t)
	path := filepath.Join(t.TempDir(), "Dead Man's Switch", "switch.ics")
	out, err := captured(t, func() error {
		return dmsCmd([]string{"calendar", "--out", path, "--first", "2026-11-08", "--at", "09:30", "--review", "2027-01-10"})
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"DTSTART:20261108T093000", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=SU", "DTSTART:20270110T093000", "RRULE:FREQ=YEARLY"} {
		if !strings.Contains(text, want) {
			t.Errorf("calendar lacks %s", want)
		}
	}
	if !strings.Contains(out, "every 14 days") {
		t.Errorf("printed %q", out)
	}
	if err := dmsCmd([]string{"calendar", "--out", path, "--at", "noon"}); err == nil {
		t.Error("--at noon")
	}
}

func TestListSkills(t *testing.T) {
	all := []skills.Skill{{Name: "dead-mans-switch", Description: "A guided setup of a dead man's switch. More."}}
	var b strings.Builder
	listSkills(&b, all, config.Skills{Enabled: true}, "")
	if got := b.String(); !strings.Contains(got, "dead-mans-switch (on)") || !strings.Contains(got, ":setup switch") || !strings.Contains(got, "A guided setup of a dead man's switch.\n") {
		t.Errorf("on:\n%s", got)
	}
	for want, c := range map[string]struct {
		cfg config.Skills
		env string
	}{
		"off: IRIE_SKILLS=off":            {config.Skills{Enabled: true}, "off"},
		"off: [skills] enabled = false":   {config.Skills{}, ""},
		"off: [skills] disabled names it": {config.Skills{Enabled: true, Disabled: []string{"dead-mans-switch"}}, ""},
	} {
		b.Reset()
		listSkills(&b, all, c.cfg, c.env)
		if !strings.Contains(b.String(), want) {
			t.Errorf("want %q:\n%s", want, b.String())
		}
	}
}

func TestDMSPhoneFiles(t *testing.T) {
	store := dmsEnv(t)
	st := dms.State{
		API:      dms.DefaultAPI,
		Rhythm:   dms.Suggested,
		Reminder: dms.Ref{Name: "R", UUID: "00000000-0000-4000-8000-000000000001", PingURL: "https://hc-ping.com/00000000-0000-4000-8000-000000000001"},
		Alarm:    dms.Ref{Name: "A", UUID: "00000000-0000-4000-8000-000000000002", PingURL: "https://hc-ping.com/00000000-0000-4000-8000-000000000002"},
		Token:    "t",
	}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Dead Man's Switch")
	if _, err := captured(t, func() error {
		return dmsCmd([]string{"shortcut", "--unsigned", "--out", filepath.Join(dir, "I'm OK.shortcut")})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := captured(t, func() error { return dmsCmd([]string{"contact", "--out", filepath.Join(dir, "I'm OK.vcf")}) }); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("the folder holds %v; want the two files and nothing beside them", entries)
	}
	for name, want := range map[string]string{"I'm OK.shortcut": st.Alarm.PingURL, "I'm OK.vcf": st.Alarm.UUID + "@hc-ping.com"} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %s", name, want)
		}
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, want 0600", name, fi.Mode().Perm())
		}
	}
}
