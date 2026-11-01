package dms

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "hcw_k3y0nlyth3t3stkn0ws0000000000"

// fakeHC is a Healthchecks Management API v3 that keeps its checks in
// memory, as https://healthchecks.io/docs/api/ describes it.
type fakeHC struct {
	mu       sync.Mutex
	srv      *httptest.Server
	checks   []Check
	channels []Channel
	readOnly bool
	pings    map[string]int
	paused   []string
	bodies   []map[string]any
}

func newFakeHC(t *testing.T) *fakeHC {
	f := &fakeHC{pings: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/checks/", f.checksHandler)
	mux.HandleFunc("GET /api/v3/channels/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"channels": f.channels})
	})
	mux.HandleFunc("/ping/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.pings[strings.TrimPrefix(r.URL.Path, "/ping/")]++
		f.mu.Unlock()
		io.WriteString(w, "OK")
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHC) client() Client { return Client{API: f.srv.URL + "/api/v3/", Key: testKey} }

func (f *fakeHC) auth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Api-Key") != testKey {
		w.WriteHeader(http.StatusUnauthorized)
		// A careless server echoes what it was sent.
		fmt.Fprintf(w, `{"error": "wrong api key %s"}`, r.Header.Get("X-Api-Key"))
		return false
	}
	return true
}

func (f *fakeHC) checksHandler(w http.ResponseWriter, r *http.Request) {
	if !f.auth(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rest := strings.TrimPrefix(r.URL.Path, "/api/v3/checks/")
	switch {
	case r.Method == http.MethodGet && rest == "":
		out := f.checks
		if f.readOnly {
			out = nil
			for _, c := range f.checks {
				c.UUID, c.PingURL, c.Channels, c.UniqueKey = "", "", "", "u-"+c.Name
				out = append(out, c)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"checks": out})
	case r.Method == http.MethodPost && rest == "":
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.bodies = append(f.bodies, body)
		var n NewCheck
		b, _ := json.Marshal(body)
		json.Unmarshal(b, &n)
		for i, c := range f.checks {
			if c.Name == n.Name {
				f.checks[i].Desc, f.checks[i].Timeout, f.checks[i].Grace = n.Desc, n.Timeout, n.Grace
				f.reply(w, http.StatusOK, f.checks[i])
				return
			}
		}
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", len(f.checks)+1)
		c := Check{Name: n.Name, Desc: n.Desc, Timeout: n.Timeout, Grace: n.Grace, Status: "new", UUID: id, PingURL: f.srv.URL + "/ping/" + id}
		f.checks = append(f.checks, c)
		f.reply(w, http.StatusCreated, c)
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/pause"):
		id := strings.TrimSuffix(rest, "/pause")
		for i, c := range f.checks {
			if c.UUID == id {
				f.checks[i].Status = "paused"
				f.paused = append(f.paused, id)
				f.reply(w, http.StatusOK, f.checks[i])
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeHC) reply(w http.ResponseWriter, code int, c Check) {
	if f.readOnly {
		c.UUID, c.PingURL, c.Channels = "", "", ""
	}
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(c)
}

func alarmText() Text {
	return Text{Name: "Alice has not checked in for 30 days: read this", Desc: "Open READ ME FIRST in the password manager. Then call Bob."}
}

func TestMakeCreatesTwoChecksAndNoIntegrations(t *testing.T) {
	f := newFakeHC(t)
	st, did, err := Make(f.client(), Suggested, DefaultReminder(Suggested), alarmText(), State{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.checks) != 2 || len(did) != 2 || !strings.HasPrefix(did[0], "Made the reminder") {
		t.Fatalf("checks %+v, did %q", f.checks, did)
	}
	for i, grace := range []int{2 * day, 16 * day} {
		c := f.checks[i]
		if c.Timeout != 14*day || c.Grace != grace {
			t.Errorf("check %q: timeout %d, grace %d", c.Name, c.Timeout, c.Grace)
		}
	}
	for _, b := range f.bodies {
		if _, ok := b["channels"]; ok {
			t.Errorf("sent channels: %v", b)
		}
		if u, _ := json.Marshal(b["unique"]); string(u) != `["name"]` {
			t.Errorf("unique = %s", u)
		}
	}
	if st.Reminder.PingURL == "" || st.Alarm.UUID == "" || st.Token == "" || st.Rhythm != Suggested {
		t.Errorf("state %+v", st)
	}
	// Again: the same two checks, brought up to date.
	again, did, err := Make(f.client(), Suggested, DefaultReminder(Suggested), alarmText(), st)
	if err != nil || len(f.checks) != 2 || !strings.HasPrefix(did[0], "Updated") || again.Token != st.Token {
		t.Errorf("again: %v, %d checks, %q, token %s→%s", err, len(f.checks), did, st.Token, again.Token)
	}
}

func TestMakeRefusesWhatHealthchecksWould(t *testing.T) {
	f := newFakeHC(t)
	long := Text{Name: strings.Repeat("x", 101)}
	for name, tc := range map[string]struct {
		r     Rhythm
		alarm Text
		want  string
	}{
		"alarm before reminder": {Rhythm{14, 5, 5}, alarmText(), "more days than --remind"},
		"a year and a day":      {Rhythm{366, 2, 16}, alarmText(), "1 to 365"},
		"no days":               {Rhythm{14, 0, 16}, alarmText(), "--remind 0"},
		"long name":             {Suggested, long, "at most 100"},
		"two lines":             {Suggested, Text{Name: "a\nb"}, "one line"},
		"same names":            {Suggested, DefaultReminder(Suggested), "names of their own"},
	} {
		if _, _, err := Make(f.client(), tc.r, DefaultReminder(Suggested), tc.alarm, State{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if len(f.checks) != 0 {
		t.Errorf("made checks anyway: %+v", f.checks)
	}
}

func TestTheKeyNeverShows(t *testing.T) {
	f := newFakeHC(t)
	bad := f.client()
	bad.Key = "hcw_wrongkeythatmustnotbeprinted00"
	_, err := bad.Checks()
	if err == nil || strings.Contains(err.Error(), bad.Key) || !strings.Contains(err.Error(), "refused the API key") {
		t.Errorf("err = %v", err)
	}
	f.readOnly = true
	if _, _, err := Make(f.client(), Suggested, DefaultReminder(Suggested), alarmText(), State{}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("read-only key: err = %v", err)
	}
}

func TestPauseAndPing(t *testing.T) {
	f := newFakeHC(t)
	st, _, err := Make(f.client(), Suggested, DefaultReminder(Suggested), alarmText(), State{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []Ref{st.Reminder, st.Alarm} {
		if err := Ping(nil, ref.PingURL); err != nil {
			t.Fatal(err)
		}
		if err := f.client().Pause(ref.UUID); err != nil {
			t.Fatal(err)
		}
	}
	if f.pings[st.Reminder.UUID] != 1 || f.pings[st.Alarm.UUID] != 1 || len(f.paused) != 2 {
		t.Errorf("pings %v, paused %v", f.pings, f.paused)
	}
	if err := f.client().Pause("../../checks"); err == nil {
		t.Error("paused a path")
	}
	if err := Ping(nil, "http://example.com/ping/x"); err == nil {
		t.Error("pinged over plain http")
	}
}

func TestReportSaysWhatIsLeft(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	next := now.Add(13 * 24 * time.Hour)
	st := State{Reminder: Ref{Name: "R", UUID: "r"}, Alarm: Ref{Name: "A", UUID: "a"}}
	chans := []Channel{{ID: "c1", Name: "Alice", Kind: "email"}, {ID: "c2", Name: "Bob", Kind: "email"}}
	got := strings.Join(Report(st, []Check{
		{Name: "R", UUID: "r", Status: "new", Channels: "c1"},
		{Name: "A", UUID: "a", Status: "new"},
	}, chans, now), "\n")
	for _, want := range []string{"notifies Alice (email)", "It notifies nobody yet", "an email integration for each person", "Tap I'm OK once"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	got = strings.Join(Report(st, []Check{
		{Name: "R", UUID: "r", Status: "up", Channels: "c1", NextPing: &next},
		{Name: "A", UUID: "a", Status: "up", Channels: "c1,c2", NextPing: &next},
	}, chans, now), "\n")
	if !strings.Contains(got, "Nothing left to do") || !strings.Contains(got, "due by Sat 14 Nov") {
		t.Errorf("all set:\n%s", got)
	}
	if got := strings.Join(Report(st, nil, chans, now), "\n"); !strings.Contains(got, "is gone") {
		t.Errorf("gone:\n%s", got)
	}
}

func TestStoreKeepsTheKeyPrivate(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "dms")}
	if _, err := s.Key(); err == nil || !strings.Contains(err.Error(), "irie dms key --paste") {
		t.Errorf("no key: %v", err)
	}
	for _, bad := range []string{"", "two words", "a\nb"} {
		if err := s.SaveKey(bad); err == nil {
			t.Errorf("saved %q", bad)
		}
	}
	if err := s.SaveKey("  " + testKey + "\n"); err != nil {
		t.Fatal(err)
	}
	if k, err := s.Key(); err != nil || k != testKey {
		t.Errorf("key %q, %v", k, err)
	}
	for _, p := range []string{s.Dir, s.KeyPath()} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: mode %v, want only the user", p, fi.Mode().Perm())
		}
	}
	st := State{Reminder: Ref{Name: "R"}, Token: "t"}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Load(); err != nil || !ok || got.Reminder.Name != "R" {
		t.Errorf("load %+v %v %v", got, ok, err)
	}
}

func TestRhythmWords(t *testing.T) {
	want := "Check in every 14 days. A missed check-in reminds you 2 days later; 14 days after that, the people you chose are emailed (30 days after your last check-in)."
	if got := Suggested.Words(); got != want {
		t.Errorf("got %q", got)
	}
}
