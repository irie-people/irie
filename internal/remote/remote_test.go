package remote

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func TestPairingTakesTheRightCodeOnce(t *testing.T) {
	now := time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC)
	s := &Store{Dir: t.TempDir(), Now: func() time.Time { return now }}

	if _, _, err := s.Pair("123456", "iPhone"); err != ErrNoCode {
		t.Fatalf("no code open: err = %v", err)
	}
	code, err := s.NewCode(CodeLife)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Fatalf("code %q is not six digits", code)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if _, _, err := s.Pair(wrong, "iPhone"); err != ErrBadCode {
		t.Fatalf("a wrong code: err = %v", err)
	}
	token, d, err := s.Pair(code[:3]+" "+code[3:], "  Alice's\tiPhone\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Alice's iPhone" || token == "" || d.Hash == token {
		t.Errorf("device %+v, token %q", d, token)
	}
	if got, err := s.Device(token); err != nil || got.ID != d.ID {
		t.Errorf("Device(token) = %+v, %v", got, err)
	}
	if _, err := s.Device("not-a-token"); err != ErrUnpaired {
		t.Errorf("an unknown token: err = %v", err)
	}
	// A code pairs one phone.
	if _, _, err := s.Pair(code, "iPad"); err != ErrNoCode {
		t.Errorf("the code again: err = %v", err)
	}
	// The file has no token, only its hash.
	raw, _ := os.ReadFile(filepath.Join(s.Dir, devicesFile))
	if bytes.Contains(raw, []byte(token)) {
		t.Error("devices.json holds the token itself")
	}

	gone, err := s.Forget(d.ID)
	if err != nil || len(gone) != 1 {
		t.Fatalf("Forget = %v, %v", gone, err)
	}
	if _, err := s.Device(token); err != ErrUnpaired {
		t.Errorf("a forgotten phone: err = %v", err)
	}
}

func TestPairingCodesRunOut(t *testing.T) {
	now := time.Date(2026, 10, 31, 21, 0, 0, 0, time.UTC)
	s := &Store{Dir: t.TempDir(), Now: func() time.Time { return now }}

	code, _ := s.NewCode(time.Minute)
	now = now.Add(2 * time.Minute)
	if _, _, err := s.Pair(code, "iPhone"); err != ErrNoCode {
		t.Errorf("an expired code: err = %v", err)
	}

	code, _ = s.NewCode(time.Minute)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i < codeTries; i++ {
		if _, _, err := s.Pair(wrong, "iPhone"); err != ErrBadCode {
			t.Fatalf("wrong code %d: err = %v", i, err)
		}
	}
	if _, _, err := s.Pair(wrong, "iPhone"); err != ErrTooMany {
		t.Errorf("the last wrong code: err = %v", err)
	}
	if _, _, err := s.Pair(code, "iPhone"); err != ErrNoCode {
		t.Errorf("the right code after too many wrong ones: err = %v", err)
	}
}

func TestTheAuthorityVouchesOnlyForLocalNames(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadAuthority(dir)
	if err != nil || !again.Cert.Equal(ca.Cert) {
		t.Fatalf("loading it again made another: %v", err)
	}
	if !ca.Cert.PermittedDNSDomainsCritical || !slices.Contains(ca.Cert.PermittedDNSDomains, "local") || len(ca.Cert.PermittedIPRanges) == 0 {
		t.Errorf("constraints: dns %v, ips %v", ca.Cert.PermittedDNSDomains, ca.Cert.PermittedIPRanges)
	}
	if fi, _ := os.Stat(filepath.Join(dir, caKeyFile)); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the key's file: %v", fi)
	}

	l := &Leaves{
		CA:    ca,
		Names: func() []string { return []string{"studio.local", "studio.example.com"} },
		Addrs: func() []net.IP { return []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("203.0.113.9"), net.ParseIP("fe80::1")} },
	}
	cert, err := l.get("", net.ParseIP("10.0.0.5"))
	if err != nil {
		t.Fatal(err)
	}
	leaf := cert.Leaf
	if slices.Contains(leaf.DNSNames, "studio.example.com") || slices.ContainsFunc(leaf.IPAddresses, net.ParseIP("203.0.113.9").Equal) {
		t.Errorf("the certificate has a public name or address: %v %v", leaf.DNSNames, leaf.IPAddresses)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	for _, name := range []string{"studio.local", "localhost", "192.168.1.20", "10.0.0.5"} {
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: roots}); err != nil {
			t.Errorf("verify for %s: %v", name, err)
		}
	}
	// A new address the network gave the machine gets a new certificate.
	again2, err := l.get("", net.ParseIP("10.0.0.77"))
	if err != nil || again2 == cert {
		t.Fatalf("a new address kept the old certificate: %v", err)
	}
	if _, err := again2.Leaf.Verify(x509.VerifyOptions{DNSName: "10.0.0.77", Roots: roots}); err != nil {
		t.Errorf("verify for the new address: %v", err)
	}
	// One the authority may not vouch for can't be had.
	bad, err := (&Leaves{CA: ca}).get("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Leaf.Verify(x509.VerifyOptions{DNSName: "example.com", Roots: roots}); err == nil {
		t.Error("a certificate verified for example.com")
	}

	// The profile carries the certificate, and the same identifiers each time.
	p := string(ca.Profile())
	if !strings.Contains(p, base64.StdEncoding.EncodeToString(ca.Cert.Raw)) || !strings.Contains(p, "com.apple.security.root") || p != string(ca.Profile()) {
		t.Error("the profile is not the authority's")
	}

	// Another machine imports it whole.
	pem, err := ca.Export()
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	imported, err := Import(other, pem)
	if err != nil || !imported.Cert.Equal(ca.Cert) {
		t.Fatalf("import: %v", err)
	}
	if _, err := Import(other, ca.PEM); err == nil {
		t.Error("an authority without its key was imported")
	}
}

// fakeCore answers on a unix socket as the core would.
type fakeCore struct {
	calls  []string
	frames chan ipc.Frame
}

func (f *fakeCore) Call(op string, args json.RawMessage) (any, error) {
	f.calls = append(f.calls, op+" "+string(args))
	switch op {
	case ipc.OpHello:
		return ipc.HelloResult{Version: "test", API: ipc.API, Features: []string{ipc.OpHello, ipc.OpSend, ipc.OpConfig, ipc.OpShow}}, nil
	case ipc.OpSend:
		return nil, ipc.Errorf(ipc.CodeBusy, "parser is waiting for you in its terminal: answer it there")
	}
	return map[string]string{"op": op}, nil
}

func (f *fakeCore) Subscribe(args json.RawMessage) (<-chan ipc.Frame, func(), error) {
	f.calls = append(f.calls, "subscribe "+string(args))
	return f.frames, func() {}, nil
}

func gateway(t *testing.T) (*httptest.Server, *Server, *fakeCore) {
	t.Helper()
	sockDir, err := os.MkdirTemp("", "irie-r")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeCore{frames: make(chan ipc.Frame, 4)}
	go ipc.Serve(l, f)
	srv, err := New(Options{Socket: sock, Dir: t.TempDir(), Port: 7387, Name: "Studio", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.App())
	t.Cleanup(ts.Close)
	return ts, srv, f
}

func post(t *testing.T, url, token, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func TestThePhoneCallsOnlyItsOps(t *testing.T) {
	ts, srv, f := gateway(t)

	res, _ := post(t, ts.URL+"/api/call", "", `{"op":"list"}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a call with no token: %s", res.Status)
	}
	code, _ := srv.store.NewCode(CodeLife)
	res, out := post(t, ts.URL+"/api/pair", "", `{"code":"`+code+`","device":"iPhone"}`)
	token, _ := out["token"].(string)
	if res.StatusCode != http.StatusOK || token == "" || out["machine"].(map[string]any)["name"] != "Studio" {
		t.Fatalf("pair: %s %v", res.Status, out)
	}

	_, out = post(t, ts.URL+"/api/call", token, `{"op":"park","args":{"session":"s1"}}`)
	if out["ok"] != true || out["result"].(map[string]any)["op"] != "park" {
		t.Errorf("park: %v", out)
	}
	_, out = post(t, ts.URL+"/api/call", token, `{"op":"send","args":{"session":"s1","text":"go on"}}`)
	if e, _ := out["error"].(map[string]any); out["ok"] != false || e["code"] != ipc.CodeBusy || !strings.Contains(e["msg"].(string), "waiting for you") {
		t.Errorf("a refused send comes back as the core said it: %v", out)
	}
	for _, op := range []string{"config", "show", "hook", "flip", "tool"} {
		_, out = post(t, ts.URL+"/api/call", token, `{"op":"`+op+`"}`)
		if e, _ := out["error"].(map[string]any); out["ok"] != false || e["code"] != ipc.CodeUnknownOp {
			t.Errorf("%s from a phone: %v", op, out)
		}
	}
	_, out = post(t, ts.URL+"/api/call", token, `{"op":"hello","args":{"client":"phone","api":1}}`)
	features := out["result"].(map[string]any)["features"].([]any)
	if len(features) != 2 || features[0] != "hello" || features[1] != "send" {
		t.Errorf("hello offers %v", features)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "config") || strings.HasPrefix(c, "show") {
			t.Errorf("the core got %q", c)
		}
	}

	// A preflight from another machine's copy of the app may ask.
	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/call", nil)
	req.Header.Set("Origin", "https://10.0.0.7:7387")
	pre, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if pre.StatusCode != http.StatusNoContent || pre.Header.Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(pre.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("preflight: %s %v", pre.Status, pre.Header)
	}

	// The phone forgets itself.
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/device", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	del, err := http.DefaultClient.Do(req)
	if err != nil || del.StatusCode != http.StatusNoContent {
		t.Fatalf("forget me: %v %v", del, err)
	}
	if res, _ := post(t, ts.URL+"/api/call", token, `{"op":"list"}`); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a forgotten phone's call: %s", res.Status)
	}
}

func TestASubscriptionStreamsTheCoresFrames(t *testing.T) {
	ts, srv, f := gateway(t)
	code, _ := srv.store.NewCode(CodeLife)
	_, out := post(t, ts.URL+"/api/pair", "", `{"code":"`+code+`"}`)
	token := out["token"].(string)

	f.frames <- ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{{ID: "s1", Name: "parser", State: ipc.StateWorking}}}
	req, _ := http.NewRequest("POST", ts.URL+"/api/subscribe", strings.NewReader(`{"session":"s1","conversation":true,"view":"app","events":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("content type %q", ct)
	}
	rd := bufio.NewReader(res.Body)
	line := func() map[string]any {
		t.Helper()
		done := make(chan string, 1)
		go func() {
			s, _ := rd.ReadString('\n')
			done <- s
		}()
		select {
		case s := <-done:
			var m map[string]any
			if err := json.Unmarshal([]byte(s), &m); err != nil {
				t.Fatalf("line %q: %v", s, err)
			}
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("no line came")
		}
		return nil
	}
	if first := line(); first["ok"] != true {
		t.Fatalf("first line %v", first)
	}
	if snap := line(); snap["t"] != "snapshot" || snap["sessions"].([]any)[0].(map[string]any)["name"] != "parser" {
		t.Errorf("snapshot %v", snap)
	}
	f.frames <- ipc.Frame{T: ipc.FrameRemove, ID: "s1"}
	if rm := line(); rm["t"] != "remove" || rm["id"] != "s1" {
		t.Errorf("remove %v", rm)
	}
	// Only what a phone may ask for reaches the core: not the Mac app's
	// view, nor every event.
	if c := f.calls[len(f.calls)-1]; strings.Contains(c, "view") || strings.Contains(c, "events") || !strings.Contains(c, `"conversation":true`) {
		t.Errorf("the core was asked %s", c)
	}
}

func TestTheAppsFilesAreServed(t *testing.T) {
	ts, _, _ := gateway(t)
	for path, want := range map[string]string{"/icon-180.png": "image/png", "/icon-512.png": "image/png"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != want || res.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: %s %v", path, res.Status, res.Header)
		}
	}
	res, err := http.Get(ts.URL + "/api/info")
	if err != nil {
		t.Fatal(err)
	}
	var info Info
	json.NewDecoder(res.Body).Decode(&info)
	res.Body.Close()
	if info.Name != "Studio" || !strings.HasPrefix(info.ID, "m") || info.API != ipc.API {
		t.Errorf("info %+v", info)
	}
	if res, _ := http.Get(ts.URL + "/../../etc/passwd"); res != nil && res.StatusCode == http.StatusOK {
		t.Error("a path out of the app's files was served")
	}
	if _, err := QRSVG("http://studio.local:7386/"); err != nil {
		t.Error(err)
	}
	if errors.Is(nil, io.EOF) {
		t.Skip()
	}
}
