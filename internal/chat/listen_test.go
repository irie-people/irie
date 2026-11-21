//go:build !nochat

package chat

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// A WebSocket server for the tests: the server's end of RFC 6455, which
// writes its frames unmasked, as a server does, and reads the client's
// masked ones with the client's own reader.
type srvConn struct {
	c  net.Conn
	ws *wsConn
	mu sync.Mutex
}

func wsServer(t *testing.T, serve func(*srvConn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			w.WriteHeader(400)
			return
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + acceptKey(r.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n")
		rw.Flush()
		sc := &srvConn{c: c, ws: &wsConn{c: c, r: bufio.NewReader(rw.Reader)}}
		serve(sc)
		c.Close()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *srvConn) frame(op byte, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hdr := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	s.c.Write(append(hdr, p...))
}

func (s *srvConn) send(v any) {
	b, _ := json.Marshal(v)
	s.frame(opText, b)
}

func (s *srvConn) close(code int) {
	s.frame(opClose, binary.BigEndian.AppendUint16(nil, uint16(code)))
}

// read is the client's next message, as JSON.
func (s *srvConn) read() map[string]any {
	b, err := s.ws.ReadMessage()
	if err != nil {
		return nil
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

func dispatch(t string, s int, d any) map[string]any {
	return map[string]any{"op": 0, "t": t, "s": s, "d": d}
}

// discordGateway is a stand-in for Discord's Gateway: the first connection
// identifies, hears five messages, three of them for the bot or the
// person, is beaten and closed resumably; the second resumes, hears a
// mention it heard already, and closes with 4004, as for a token Discord
// no longer takes.
func discordGateway(t *testing.T, f *fakeDiscord) (got chan map[string]any) {
	t.Helper()
	got = make(chan map[string]any, 20)
	var conns atomic.Int32
	var url string
	gw := wsServer(t, func(s *srvConn) {
		n := conns.Add(1)
		s.send(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 40}})
		got <- s.read()
		mention := map[string]any{"id": "701", "channel_id": "201", "guild_id": guildID, "type": 0, "author": user("1", "alice", "Alice"),
			"content": "Hi <@900>", "timestamp": "2026-11-21T12:00:00Z", "mentions": []any{user(botID, "acme-bot", "Acme Bot")}}
		if n == 1 {
			s.send(dispatch("READY", 1, map[string]any{"session_id": "sess1", "resume_gateway_url": url}))
			s.send(dispatch("GUILD_CREATE", 2, map[string]any{"id": guildID, "channels": []any{map[string]any{"id": "201", "name": "general", "type": 0}}}))
			s.send(dispatch("MESSAGE_CREATE", 3, mention))
			s.send(dispatch("MESSAGE_CREATE", 4, map[string]any{"id": "702", "channel_id": "201", "guild_id": guildID, "type": 0, "author": user("2", "bob", ""), "content": "", "timestamp": "2026-11-21T12:01:00Z", "mentions": []any{}}))
			s.send(dispatch("MESSAGE_CREATE", 5, map[string]any{"id": "703", "channel_id": "600", "type": 0, "author": user("1", "alice", "Alice"), "content": "psst", "timestamp": "2026-11-21T12:02:00Z", "mentions": []any{}}))
			s.send(dispatch("MESSAGE_CREATE", 6, map[string]any{"id": "704", "channel_id": "201", "guild_id": guildID, "type": 0, "author": user("2", "bob", ""), "content": "", "timestamp": "2026-11-21T12:03:00Z", "mentions": []any{user("7", "you", "You")}}))
			s.send(dispatch("MESSAGE_CREATE", 7, map[string]any{"id": "705", "channel_id": "201", "guild_id": guildID, "type": 0, "author": user(botID, "acme-bot", "Acme Bot"), "content": "<@900> me", "timestamp": "2026-11-21T12:04:00Z", "mentions": []any{user(botID, "acme-bot", "Acme Bot")}}))
			// The client's heartbeat carries the last sequence.
			hb := s.read()
			got <- hb
			s.send(map[string]any{"op": 11})
			s.close(4000)
			return
		}
		s.send(dispatch("RESUMED", 8, nil))
		s.send(dispatch("MESSAGE_CREATE", 9, mention))
		time.Sleep(50 * time.Millisecond)
		s.close(4004)
	})
	url = "ws://" + gw.Listener.Addr().String()
	f.gateway = url
	return got
}

func TestDiscordGateway(t *testing.T) {
	listenWait = 10 * time.Millisecond
	f := newFakeDiscord(t)
	got := discordGateway(t, f)
	c := connect(t, f, fakeToken).(Listener)
	var mu sync.Mutex
	var heard []Event
	err := c.Listen(context.Background(), "7", func(e Event) {
		mu.Lock()
		heard = append(heard, e)
		mu.Unlock()
	})
	// 4004 is a token Discord no longer takes: no more tries.
	if !Auth(err) || !strings.Contains(err.Error(), "Reset Token") {
		t.Fatalf("Listen = %v", err)
	}
	identify := <-got
	if identify["op"] != float64(gwIdentify) {
		t.Fatalf("first = %v", identify)
	}
	d := identify["d"].(map[string]any)
	if d["token"] != fakeToken || d["intents"] != float64(intentGuilds|intentGuildMessages|intentDirectMessages) {
		t.Errorf("identify = %v", d)
	}
	if hb := <-got; hb["op"] != float64(gwHeartbeat) || hb["d"] != float64(7) {
		t.Errorf("heartbeat = %v", hb)
	}
	resume := <-got
	if r, _ := resume["d"].(map[string]any); resume["op"] != float64(gwResume) || r["session_id"] != "sess1" || r["seq"] != float64(7) {
		t.Errorf("resume = %v", resume)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 4 {
		t.Fatalf("heard = %+v", heard)
	}
	m, dm, you := heard[0], heard[1], heard[2]
	if m.ChannelName != "general" || m.Message.Text != "Hi @Acme Bot" || m.DM || m.You || m.Message.Link != discordApp+guildID+"/201/701" {
		t.Errorf("the mention = %+v", m)
	}
	if !dm.DM || dm.ChannelName != "DM with Alice" || dm.Message.Text != "psst" || dm.Message.Hidden {
		t.Errorf("the DM = %+v", dm)
	}
	// A mention of the person, its text hidden from a bot without the
	// intent.
	if !you.You || !you.Message.Hidden {
		t.Errorf("the person's mention = %+v", you)
	}
}

// slackSocket is a stand-in for Slack's Socket Mode: the first connection
// sends a mention, a DM and the bot's own DM, each to be acknowledged,
// then a disconnect; the second waits.
func slackSocket(t *testing.T, f *fakeSlack) (acks chan string, second chan struct{}) {
	t.Helper()
	acks, second = make(chan string, 10), make(chan struct{})
	var conns atomic.Int32
	sock := wsServer(t, func(s *srvConn) {
		s.send(map[string]any{"type": "hello", "num_connections": 1})
		if conns.Add(1) > 1 {
			close(second)
			s.read()
			return
		}
		for i, ev := range []map[string]any{
			{"type": "app_mention", "user": "U1", "text": "<@U900> standup?", "ts": "1700000005.000500", "channel": "C1"},
			{"type": "message", "channel_type": "im", "user": "U1", "text": "psst", "ts": "1700000006.000600", "channel": "D1"},
			{"type": "message", "channel_type": "im", "user": "U900", "bot_id": "B900", "text": "me", "ts": "1700000007.000700", "channel": "D1"},
		} {
			id := "e" + string(rune('1'+i))
			s.send(map[string]any{"envelope_id": id, "type": "events_api", "accepts_response_payload": false, "payload": map[string]any{"event": ev}})
			if a := s.read(); a != nil {
				acks <- a["envelope_id"].(string)
			}
		}
		s.send(map[string]any{"type": "disconnect", "reason": "refresh_requested"})
		s.read()
	})
	f.mu.Lock()
	f.socket = "ws://" + sock.Listener.Addr().String()
	f.mu.Unlock()
	return acks, second
}

func TestSlackSocketMode(t *testing.T) {
	listenWait = 10 * time.Millisecond
	f := newFakeSlack(t)
	acks, second := slackSocket(t, f)
	if err := slackConnect(f, slackBot, "").(Listener).Listen(context.Background(), "", func(Event) {}); !errors.Is(err, ErrNoListen) {
		t.Errorf("without an app token: %v", err)
	}
	c := slackConnect(f, slackBot, slackApp).(Listener)
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var heard []Event
	done := make(chan error, 1)
	go func() {
		done <- c.Listen(ctx, "", func(e Event) {
			mu.Lock()
			heard = append(heard, e)
			mu.Unlock()
		})
	}()
	select {
	case <-second:
	case <-time.After(10 * time.Second):
		t.Fatal("no second connection after the disconnect")
	}
	cancel()
	<-done
	close(acks)
	var got []string
	for a := range acks {
		got = append(got, a)
	}
	if strings.Join(got, " ") != "e1 e2 e3" {
		t.Errorf("acknowledged %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 2 {
		t.Fatalf("heard = %+v", heard)
	}
	if m := heard[0]; m.DM || m.Message.Text != "@acme-bot standup?" || m.Message.Author != "Alice" || m.Message.Link != "https://acme.slack.com/archives/C1/p1700000005000500" {
		t.Errorf("the mention = %+v", m)
	}
	if dm := heard[1]; !dm.DM || dm.ChannelName != "DM with Alice" || dm.Message.Text != "psst" {
		t.Errorf("the DM = %+v", dm)
	}
}

// TestServiceHearsMentions: the service starts a listener for an account,
// keeps what it hears once each, in memory, counts it until the person
// looks, and shows an agent none of it.
func TestServiceHearsMentions(t *testing.T) {
	listenWait = 10 * time.Millisecond
	f := newFakeDiscord(t)
	discordGateway(t, f)
	sv, _, _ := newService(t, f, acmeConfig+`user = "7"
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sv.Listen(ctx, func() bool { return true })
	var r ipc.ChatResult
	for range 200 {
		r = do(t, sv, person, ipc.ChatArgs{})
		if len(r.Mentions) == 3 && r.Accounts[0].Deaf != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(r.Mentions) != 3 || r.Unseen != 3 {
		t.Fatalf("mentions = %+v, unseen %d", r.Mentions, r.Unseen)
	}
	if r.Mentions[0].Message != "704" || !r.Mentions[0].You || r.Mentions[2].ChannelName != "general" {
		t.Errorf("mentions = %+v", r.Mentions)
	}
	// The stale token stops it, and the account says so.
	if a := r.Accounts[0]; a.Listening || !strings.Contains(a.Deaf, "Reset Token") {
		t.Errorf("account = %+v", a)
	}
	if !strings.Contains(r.Text, "3 new mentions") {
		t.Errorf("said %q", r.Text)
	}
	if a := do(t, sv, agent, ipc.ChatArgs{}); len(a.Mentions) != 0 || a.Unseen != 0 {
		t.Errorf("an agent sees %+v", a.Mentions)
	}
	refused(t, sv, agent, ipc.ChatArgs{Do: ipc.ChatSeen}, "the person's")
	if r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatSeen}); r.Unseen != 0 || len(r.Mentions) != 3 {
		t.Errorf("seen = %+v", r)
	}
	// Removing the account takes its mentions with it.
	if r := do(t, sv, person, ipc.ChatArgs{Do: ipc.ChatRemove, Account: "acme"}); len(r.Mentions) != 0 {
		t.Errorf("after remove = %+v", r.Mentions)
	}
}
