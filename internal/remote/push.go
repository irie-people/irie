package remote

import (
	"bufio"
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
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// Notifications reach a phone through its browser's push service, as Web
// Push has them (RFC 8030): the gateway encrypts each message for the
// phone (RFC 8291) and signs its request with the phone's application
// server key (VAPID, RFC 8292). Every machine a phone pairs with pushes to
// the same subscription, and a subscription is bound to one key, so the
// phone makes that key and gives it to each machine with the subscription.

// Push is where a phone takes notifications, and which.
type Push struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"` // the phone's public key, base64url
	Auth     string `json:"auth"`   // the phone's secret, base64url
	// VAPIDPublic and VAPIDPrivate are the key the phone subscribed with:
	// the public point, 65 bytes, and the private scalar, 32, base64url.
	VAPIDPublic  string `json:"vapid_public"`
	VAPIDPrivate string `json:"vapid_private"`
	Done         bool   `json:"done"` // also when a task's turn ends, not only when it needs you
}

// pushArgs is what the app sends: its PushSubscription.toJSON(), the key,
// and whether a finished turn is news.
type pushArgs struct {
	Subscription struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	} `json:"subscription"`
	VAPID struct {
		Public  string `json:"public"`
		Private string `json:"private"`
	} `json:"vapid"`
	Done bool `json:"done"`
}

func unb64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

var b64 = base64.RawURLEncoding.EncodeToString

// check says what is wrong with a push config, if anything.
func (p Push) check() error {
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("the subscription's endpoint must be an https URL")
	}
	ua, err := unb64(p.P256dh)
	if err != nil {
		return errors.New("the subscription's p256dh is not base64url")
	}
	if _, err := ecdh.P256().NewPublicKey(ua); err != nil {
		return errors.New("the subscription's p256dh is not a P-256 point")
	}
	if a, err := unb64(p.Auth); err != nil || len(a) != 16 {
		return errors.New("the subscription's auth must be 16 bytes, base64url")
	}
	if _, err := p.vapid(); err != nil {
		return err
	}
	return nil
}

// vapid is the phone's application server key.
func (p Push) vapid() (*ecdsa.PrivateKey, error) {
	d, err := unb64(p.VAPIDPrivate)
	if err != nil {
		return nil, errors.New("the VAPID private key is not base64url")
	}
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
	if err != nil {
		return nil, errors.New("the VAPID private key is not a P-256 scalar")
	}
	pub, err := unb64(p.VAPIDPublic)
	if err != nil {
		return nil, errors.New("the VAPID public key is not base64url")
	}
	mine, err := k.PublicKey.Bytes()
	if err != nil || !bytes.Equal(mine, pub) {
		return nil, errors.New("the VAPID public key is not the private key's")
	}
	return k, nil
}

// recordSize is the record size the header announces; a notification fits
// in one record.
const recordSize = 4096

// encrypt makes the body of a push message for a phone (RFC 8291 §3.4,
// RFC 8188): a header with the salt, the record size and the sender's
// ephemeral public key, then plaintext, ended by the last record's
// delimiter, sealed with AES-128-GCM. as and salt are fresh for each
// message; tests pass the RFC's.
func encrypt(plaintext, uaPublic, authSecret []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > recordSize-17-86 {
		return nil, errors.New("the message is too long to push")
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	secret, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := as.PublicKey().Bytes()
	// HKDF with SHA-256, written out as the RFC gives it: each expand here
	// wants one block or less.
	hm := func(key []byte, parts ...[]byte) []byte {
		m := hmac.New(sha256.New, key)
		for _, p := range parts {
			m.Write(p)
		}
		return m.Sum(nil)
	}
	prkKey := hm(authSecret, secret)
	keyInfo := append(append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...), 1)
	ikm := hm(prkKey, keyInfo)
	prk := hm(salt, ikm)
	cek := hm(prk, []byte("Content-Encoding: aes128gcm\x00\x01"))[:16]
	nonce := hm(prk, []byte("Content-Encoding: nonce\x00\x01"))[:12]

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, 86)
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	record := append(append([]byte{}, plaintext...), 2) // the last record's delimiter
	return gcm.Seal(header, nonce, record, nil), nil
}

// vapidSubject is the contact a push service may use about the pushes,
// which Apple wants as a URL or mailto: (RFC 8292 §2.1).
const vapidSubject = "https://github.com/irie-people/irie"

// jwtLife is how long a VAPID token is good for; Apple wants a day or
// less, and a token reused for at least an hour.
const jwtLife = 12 * time.Hour

// vapidToken is the JWT that signs requests to a push service's origin
// (RFC 8292 §2), ES256 over the phone's key.
func vapidToken(aud string, k *ecdsa.PrivateKey, exp time.Time) (string, error) {
	header := b64([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": aud, "exp": exp.Unix(), "sub": vapidSubject})
	if err != nil {
		return "", err
	}
	signed := header + "." + b64(claims)
	sum := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, k, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signed + "." + b64(sig), nil
}

// Note is one notification, as the app's service worker shows it.
type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"` // one per session, so a newer one replaces it
	URL   string `json:"url"` // where the app opens: the session
}

// errGone is a subscription the push service no longer has: the phone
// unsubscribed, or the app was removed.
var errGone = errors.New("the push service no longer has this subscription")

// pusher sends notifications, reusing each VAPID token for a while.
type pusher struct {
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	tokens map[string]cachedToken // by push service origin and key
}

type cachedToken struct {
	jwt string
	exp time.Time
}

func newPusher() *pusher {
	return &pusher{client: &http.Client{Timeout: 20 * time.Second}, now: time.Now, tokens: map[string]cachedToken{}}
}

// send pushes n to the phone at p. urgent asks the push service to wake
// the phone at once.
func (ps *pusher) send(ctx context.Context, p Push, n Note, urgent bool) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return err
	}
	ua, err := unb64(p.P256dh)
	if err != nil {
		return err
	}
	auth, err := unb64(p.Auth)
	if err != nil {
		return err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	body, err := encrypt(payload, ua, auth, as, salt)
	if err != nil {
		return err
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil {
		return err
	}
	aud := u.Scheme + "://" + u.Host
	jwt, err := ps.token(aud, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	ttl, urgency := "3600", "normal"
	if urgent {
		ttl, urgency = "14400", "high"
	}
	topic := sha256.Sum256([]byte(n.Tag))
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+p.VAPIDPublic)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", ttl)
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Topic", b64(topic[:24])) // 32 characters
	res, err := ps.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return errGone
	case res.StatusCode >= 300:
		return fmt.Errorf("the push service answered %s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

func (ps *pusher) token(aud string, p Push) (string, error) {
	key := aud + " " + p.VAPIDPublic
	now := ps.now()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if t, ok := ps.tokens[key]; ok && now.Before(t.exp.Add(-time.Hour)) {
		return t.jwt, nil
	}
	k, err := p.vapid()
	if err != nil {
		return "", err
	}
	exp := now.Add(jwtLife)
	jwt, err := vapidToken(aud, k, exp)
	if err != nil {
		return "", err
	}
	ps.tokens[key] = cachedToken{jwt: jwt, exp: exp}
	return jwt, nil
}

// seen is what the notifier last knew of a session.
type seen struct {
	name, state, detail string
}

// loud are the states that need a person: red on every view.
func loud(state string) bool {
	return state == ipc.StateNeedsYou || state == ipc.StateFailed || state == ipc.StateStalled
}

// news is what to tell a phone when a session goes from was to now, if
// anything: it needs you, failed or stalled, or, for phones that asked,
// its turn ended. A session first seen is news only when it needs you.
func news(was *seen, now seen, machine string) (n Note, urgent, done, ok bool) {
	verb := ""
	switch {
	case loud(now.state) && (was == nil || !loud(was.state) || now.state == ipc.StateNeedsYou && was.detail != now.detail):
		urgent = true
		switch now.state {
		case ipc.StateNeedsYou:
			verb = "needs you"
		case ipc.StateFailed:
			verb = "failed"
		default:
			verb = "stalled"
		}
	case now.state == ipc.StateDone && was != nil && was.state != ipc.StateDone:
		verb, done = "is done", true
	default:
		return Note{}, false, false, false
	}
	body := now.detail
	if body == "" {
		body = map[bool]string{true: "Open it to see what it asks.", false: "Open it to see what it did."}[urgent]
	}
	return Note{Title: now.name + " " + verb, Body: body + " · " + machine}, urgent, done, true
}

// notify follows the core's sessions and pushes what is news to every
// phone that asked for notifications, until ctx ends. It starts again
// whenever the core does; what changed while it was away is not news.
func (s *Server) notify(ctx context.Context) {
	wait := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := s.follow(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			wait = time.Second
		}
		s.log.Debug("notify: core gone", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(2*wait, 30*time.Second)
	}
}

// pushGap is the least time between two notifications of one kind about
// one session.
const pushGap = 20 * time.Second

func (s *Server) follow(ctx context.Context) error {
	conn, err := net.DialTimeout("unix", s.opt.Socket, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, err := conn.Write([]byte(`{"id":1,"op":"subscribe","args":{}}` + "\n")); err != nil {
		return err
	}
	rd := bufio.NewReaderSize(conn, 64<<10)
	if _, err := readLine(rd); err != nil {
		return err
	}
	known := map[string]seen{}
	first := true
	last := map[string]time.Time{}
	look := func(row ipc.Session) {
		now := seen{name: row.Name, state: row.State, detail: row.Detail}
		old, had := known[row.ID]
		known[row.ID] = now
		if first {
			return
		}
		var was *seen
		if had {
			was = &old
		}
		n, urgent, done, ok := news(was, now, s.opt.Name)
		if !ok {
			return
		}
		kind := row.ID + map[bool]string{true: " urgent", false: " done"}[urgent]
		if t, ok := last[kind]; ok && time.Since(t) < pushGap {
			return
		}
		last[kind] = time.Now()
		n.Tag = s.id + ":" + row.ID
		n.URL = "#/m/" + s.id + "/s/" + row.ID
		s.pushAll(ctx, n, urgent, done)
	}
	for {
		line, err := readLine(rd)
		if err != nil {
			return err
		}
		var f ipc.Frame
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		switch f.T {
		case ipc.FrameSnapshot:
			ids := map[string]bool{}
			for _, row := range f.Sessions {
				ids[row.ID] = true
				look(row)
			}
			for id := range known {
				if !ids[id] {
					delete(known, id)
				}
			}
			first = false
		case ipc.FrameUpsert:
			if f.Session != nil {
				look(*f.Session)
			}
		case ipc.FrameRemove:
			delete(known, f.ID)
		}
	}
}

// pushAll sends n to every phone that takes it, and forgets a
// subscription its push service no longer has.
func (s *Server) pushAll(ctx context.Context, n Note, urgent, done bool) {
	ds, err := s.store.Devices()
	if err != nil {
		s.log.Warn("notify: devices", "err", err)
		return
	}
	for _, d := range ds {
		if d.Push == nil || done && !d.Push.Done {
			continue
		}
		go func(d Device) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			err := s.pusher.send(ctx, *d.Push, n, urgent)
			switch {
			case errors.Is(err, errGone):
				s.log.Info("notify: subscription gone", "device", d.ID)
				s.store.SetPush(d.ID, nil)
			case err != nil:
				s.log.Warn("notify", "device", d.ID, "err", err)
			default:
				s.log.Info("notified", "device", d.ID, "title", n.Title)
			}
		}(d)
	}
}

// push takes a phone's subscription, or with DELETE drops it. A new one
// gets a notification at once, so the phone knows they work.
func (s *Server) push(w http.ResponseWriter, r *http.Request, d Device) {
	if r.Method == http.MethodDelete {
		if err := s.store.SetPush(d.ID, nil); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": ipc.Errorf(ipc.CodeInternal, "%v", err)})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var a pushArgs
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&a); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": ipc.Errorf(ipc.CodeBadRequest, "send the subscription as JSON")})
		return
	}
	p := Push{
		Endpoint: a.Subscription.Endpoint, P256dh: a.Subscription.Keys.P256dh, Auth: a.Subscription.Keys.Auth,
		VAPIDPublic: a.VAPID.Public, VAPIDPrivate: a.VAPID.Private, Done: a.Done,
	}
	if err := p.check(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": ipc.Errorf(ipc.CodeBadArgs, "%v", err)})
		return
	}
	// A subscription the phone sent before needs no first notification
	// again, as when it only asks for finished turns too.
	if d.Push != nil && d.Push.Endpoint == p.Endpoint && d.Push.P256dh == p.P256dh && d.Push.Auth == p.Auth && d.Push.VAPIDPublic == p.VAPIDPublic {
		if err := s.store.SetPush(d.ID, &p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": ipc.Errorf(ipc.CodeInternal, "%v", err)})
			return
		}
		s.log.Info("push kept", "device", d.ID, "done", p.Done)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	n := Note{Title: "Notifications from " + s.opt.Name + " are on", Body: "You'll hear when a session needs you" + map[bool]string{true: " or finishes.", false: "."}[p.Done],
		Tag: s.id + ":hello", URL: "#/"}
	if err := s.pusher.send(ctx, p, n, false); err != nil {
		s.log.Warn("push: first notification", "device", d.ID, "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": ipc.Errorf("push_failed", "%s couldn't reach your phone's push service: %v", s.opt.Name, err)})
		return
	}
	if err := s.store.SetPush(d.ID, &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": ipc.Errorf(ipc.CodeInternal, "%v", err)})
		return
	}
	s.log.Info("push on", "device", d.ID, "done", p.Done)
	w.WriteHeader(http.StatusNoContent)
}
