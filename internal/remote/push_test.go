package remote

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// The example of RFC 8291 §5 and Appendix A.
const (
	rfcPlaintext = "When I grow up, I want to be a watermelon"
	rfcASPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcUAPrivate = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfcUAPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcSalt      = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcAuth      = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcBody      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

// dec decodes base64url, or fails the test.
func dec(t *testing.T, s string) []byte {
	t.Helper()
	b, err := unb64(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEncryptMatchesRFC8291(t *testing.T) {
	asD := dec(t, rfcASPrivate)
	as, err := ecdh.P256().NewPrivateKey(asD)
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt([]byte(rfcPlaintext), dec(t, rfcUAPublic), dec(t, rfcAuth), as, dec(t, rfcSalt))
	if err != nil {
		t.Fatal(err)
	}
	if want := dec(t, rfcBody); !bytes.Equal(got, want) {
		t.Errorf("encrypt =\n%s\nwant\n%s", b64(got), rfcBody)
	}
}

// decrypt opens a push message as the phone would (RFC 8291 §3.4).
func decrypt(t *testing.T, body, uaPrivate, auth []byte) []byte {
	t.Helper()
	ua, err := ecdh.P256().NewPrivateKey(uaPrivate)
	if err != nil {
		t.Fatal(err)
	}
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != recordSize || idlen != 65 {
		t.Fatalf("header: rs %d, idlen %d", rs, idlen)
	}
	asPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ua.ECDH(asPub)
	if err != nil {
		t.Fatal(err)
	}
	hm := func(key []byte, parts ...[]byte) []byte {
		m := hmac.New(sha256.New, key)
		for _, p := range parts {
			m.Write(p)
		}
		return m.Sum(nil)
	}
	ikm := hm(hm(auth, secret), []byte("WebPush: info\x00"), ua.PublicKey().Bytes(), asPub.Bytes(), []byte{1})
	prk := hm(salt, ikm)
	block, _ := aes.NewCipher(hm(prk, []byte("Content-Encoding: aes128gcm\x00\x01"))[:16])
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, hm(prk, []byte("Content-Encoding: nonce\x00\x01"))[:12], body[21+idlen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[len(plain)-1] != 2 {
		t.Fatalf("no last-record delimiter: %q", plain)
	}
	return plain[:len(plain)-1]
}

func TestVAPIDTokenIsSignedByThePhonesKey(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	exp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	jwt, err := vapidToken("https://web.push.apple.com", k, exp)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwt)
	}
	var claims map[string]any
	json.Unmarshal(dec(t, parts[1]), &claims)
	if claims["aud"] != "https://web.push.apple.com" || claims["sub"] != vapidSubject || int64(claims["exp"].(float64)) != exp.Unix() {
		t.Errorf("claims %v", claims)
	}
	sig := dec(t, parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&k.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("the signature does not verify")
	}
}

func TestNewsIsWhatAPersonWouldWantToHear(t *testing.T) {
	working := seen{name: "parser", state: ipc.StateWorking, detail: "Running the tests"}
	asks := seen{name: "parser", state: ipc.StateNeedsYou, detail: "Asks: may it run rm -rf build?"}
	asksMore := seen{name: "parser", state: ipc.StateNeedsYou, detail: "Asks: may it push?"}
	done := seen{name: "parser", state: ipc.StateDone}
	for _, tc := range []struct {
		name         string
		was          *seen
		now          seen
		title        string
		urgent, done bool
	}{
		{"it needs you", &working, asks, "parser needs you", true, false},
		{"it asks something else", &asks, asksMore, "parser needs you", true, false},
		{"it asks the same again", &asks, asks, "", false, false},
		{"its turn ended", &working, done, "parser is done", false, true},
		{"it failed", &working, seen{name: "parser", state: ipc.StateFailed}, "parser failed", true, false},
		{"a new session at work", nil, working, "", false, false},
		{"a new session that needs you", nil, asks, "parser needs you", true, false},
		{"still done", &done, done, "", false, false},
	} {
		n, urgent, isDone, ok := news(tc.was, tc.now, "Studio")
		if ok != (tc.title != "") || n.Title != tc.title || urgent != tc.urgent || isDone != tc.done {
			t.Errorf("%s: %+v urgent %v done %v ok %v", tc.name, n, urgent, isDone, ok)
		}
		if ok && !strings.HasSuffix(n.Body, " · Studio") {
			t.Errorf("%s: body %q", tc.name, n.Body)
		}
	}
}

func TestAPhoneHearsWhenASessionNeedsIt(t *testing.T) {
	// The push service: it takes what a phone would get.
	got := make(chan *http.Request, 8)
	bodies := make(chan []byte, 8)
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r
		bodies <- b
		w.WriteHeader(http.StatusCreated)
	}))
	defer service.Close()

	ts, srv, f := gateway(t)
	srv.pusher.client = service.Client()
	allowEndpoint = func(*url.URL) bool { return true } // the test's own push service
	defer func() { allowEndpoint = pushService }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.notify(ctx) // as Run does
	code, _ := srv.store.NewCode(CodeLife)
	_, out := post(t, ts.URL+"/api/pair", "", `{"code":"`+code+`"}`)
	token := out["token"].(string)

	// The phone's key for the subscription, which it makes itself.
	vapid, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	vpub, _ := vapid.PublicKey.Bytes()
	vpriv := make([]byte, 32)
	vapid.D.FillBytes(vpriv)
	sub := map[string]any{
		"subscription": map[string]any{"endpoint": service.URL + "/push/abc", "keys": map[string]string{"p256dh": rfcUAPublic, "auth": rfcAuth}},
		"vapid":        map[string]string{"public": b64(vpub), "private": b64(vpriv)},
		"done":         false,
	}
	body, _ := json.Marshal(sub)
	req, _ := http.NewRequest("POST", ts.URL+"/api/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("push on: %v %v", res, err)
	}

	open := func() (Note, *http.Request) {
		t.Helper()
		select {
		case r := <-got:
			var n Note
			if err := json.Unmarshal(decrypt(t, <-bodies, dec(t, rfcUAPrivate), dec(t, rfcAuth)), &n); err != nil {
				t.Fatal(err)
			}
			return n, r
		case <-time.After(5 * time.Second):
			t.Fatal("no notification came")
		}
		return Note{}, nil
	}
	hello, r := open()
	if !strings.Contains(hello.Title, "Studio") || r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" ||
		!strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || !strings.HasSuffix(r.Header.Get("Authorization"), ", k="+b64(vpub)) ||
		len(r.Header.Get("Topic")) != 32 {
		t.Errorf("first notification %+v, headers %v", hello, r.Header)
	}

	// The same subscription again, asking for finished turns too, gets no
	// first notification again.
	sub["done"] = true
	body, _ = json.Marshal(sub)
	req, _ = http.NewRequest("POST", ts.URL+"/api/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("push again: %v %v", res, err)
	}
	select {
	case <-got:
		t.Error("a subscription sent again got a first notification again")
	case <-time.After(300 * time.Millisecond):
	}
	if ds, _ := srv.store.Devices(); len(ds) != 1 || ds[0].Push == nil || !ds[0].Push.Done {
		t.Errorf("devices after asking for finished turns: %+v", ds)
	}
	sub["done"] = false
	body, _ = json.Marshal(sub)
	req, _ = http.NewRequest("POST", ts.URL+"/api/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("push without finished turns: %v %v", res, err)
	}

	// The core's sessions: at work at first, which is not news; then one
	// needs you, which is; then its turn ends, which this phone did not
	// ask for.
	f.frames <- ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{{ID: "s1", Name: "parser", State: ipc.StateWorking}}}
	f.frames <- ipc.Frame{T: ipc.FrameUpsert, Session: &ipc.Session{ID: "s1", Name: "parser", State: ipc.StateNeedsYou, Detail: "Asks: may it run rm -rf build?"}}
	n, r := open()
	if n.Title != "parser needs you" || n.Body != "Asks: may it run rm -rf build? · Studio" || !strings.HasSuffix(n.URL, "/s/s1") || r.Header.Get("Urgency") != "high" {
		t.Errorf("needs you: %+v, urgency %q", n, r.Header.Get("Urgency"))
	}
	f.frames <- ipc.Frame{T: ipc.FrameUpsert, Session: &ipc.Session{ID: "s1", Name: "parser", State: ipc.StateDone}}
	select {
	case <-got:
		t.Error("a finished turn was pushed to a phone that did not ask for it")
	case <-time.After(300 * time.Millisecond):
	}

	// The phone turns them off.
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/push", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("push off: %v %v", res, err)
	}
	if ds, _ := srv.store.Devices(); len(ds) != 1 || ds[0].Push != nil {
		t.Errorf("devices after push off: %+v", ds)
	}
}

func TestABadSubscriptionIsRefused(t *testing.T) {
	ts, srv, _ := gateway(t)
	code, _ := srv.store.NewCode(CodeLife)
	_, out := post(t, ts.URL+"/api/pair", "", `{"code":"`+code+`"}`)
	token := out["token"].(string)
	vapid, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	vpub, _ := other.PublicKey.Bytes() // not vapid's
	vpriv := make([]byte, 32)
	vapid.D.FillBytes(vpriv)
	for _, sub := range []string{
		`{"subscription":{"endpoint":"https://127.0.0.1:8443/router-admin","keys":{"p256dh":"` + rfcUAPublic + `","auth":"` + rfcAuth + `"}}}`,
		`{"subscription":{"endpoint":"https://web.push.apple.com:8443/x","keys":{"p256dh":"` + rfcUAPublic + `","auth":"` + rfcAuth + `"}}}`,
		`{"subscription":{"endpoint":"http://push.example.com/x","keys":{"p256dh":"` + rfcUAPublic + `","auth":"` + rfcAuth + `"}}}`,
		`{"subscription":{"endpoint":"https://web.push.apple.com/x","keys":{"p256dh":"AAAA","auth":"` + rfcAuth + `"}}}`,
		`{"subscription":{"endpoint":"https://web.push.apple.com/x","keys":{"p256dh":"` + rfcUAPublic + `","auth":"` + rfcAuth + `"}},"vapid":{"public":"` + b64(vpub) + `","private":"` + b64(vpriv) + `"}}`,
	} {
		res, out := post(t, ts.URL+"/api/push", token, sub)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %s %v", sub, res.Status, out)
		}
	}
}
