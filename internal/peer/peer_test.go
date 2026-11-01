package peer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/remote"
	"github.com/irie-people/irie/internal/testutil/peertest"
)

func TestParseLinkReadsWhatPairLinkWrites(t *testing.T) {
	fp := strings.Repeat("AB", 32)
	l, err := ParseLink(remote.PairLink("https://studio.local:7387", "123456", fp, "Studio"))
	if err != nil {
		t.Fatal(err)
	}
	if l != (Link{URL: "https://studio.local:7387", Code: "123456", Fingerprint: fp, Name: "Studio"}) {
		t.Errorf("link = %+v", l)
	}
	for _, bad := range []string{
		"https://studio.local:7387",
		"irie://pair?u=http://studio.local:7387&c=1&f=" + fp,
		"irie://pair?u=https://studio.local:7387&f=" + fp,
		"irie://pair?u=https://studio.local:7387&c=1&f=abc",
	} {
		if _, err := ParseLink(bad); err == nil {
			t.Errorf("ParseLink(%q) passed", bad)
		}
	}
}

func TestAClientPairsCallsAndFollowsTheOtherMachine(t *testing.T) {
	g := peertest.Start(t)
	rows := []ipc.Session{{ID: "s1", Name: "fix the build", State: ipc.StateWorking}}
	g.Core.Start(rows, nil)
	g.Core.Set(ipc.OpList, ipc.ListResult{Sessions: rows})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cl, err := New(g.URL, "", g.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	info, err := cl.Info(ctx)
	if err != nil || info.Name != "Peer" || info.API != ipc.API {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if _, err := cl.Pair(ctx, "000000", "Mac (Irie)"); err == nil {
		t.Fatal("a wrong code paired")
	}
	token, err := cl.Pair(ctx, g.Code(t), "Mac (Irie)")
	if err != nil || token == "" {
		t.Fatalf("pair: %q, %v", token, err)
	}

	var list ipc.ListResult
	if err := cl.Call(ctx, ipc.OpList, nil, &list); err != nil || len(list.Sessions) != 1 || list.Sessions[0].ID != "s1" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	g.Core.Set(ipc.OpSend, ipc.Errorf(ipc.CodeBusy, "it waits for you"))
	var e *ipc.Error
	if err := cl.Call(ctx, ipc.OpSend, ipc.SendArgs{Session: "s1", Text: "hi"}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBusy {
		t.Errorf("send: %v; want the core's own error", err)
	}

	frames, _, err := cl.Subscribe(ctx, ipc.SubscribeArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if f := <-frames; f.T != ipc.FrameSnapshot || len(f.Sessions) != 1 {
		t.Fatalf("first frame = %+v", f)
	}
	g.Core.Send(ipc.Frame{T: ipc.FrameRemove, ID: "s1"})
	if f := <-frames; f.T != ipc.FrameRemove || f.ID != "s1" {
		t.Fatalf("next frame = %+v", f)
	}

	// Unpaired, the token works no more.
	if err := cl.Forget(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cl.Call(ctx, ipc.OpList, nil, &list); !errors.As(err, &e) || e.Code != "unpaired" {
		t.Errorf("list after forget: %v", err)
	}
}

func TestAClientTrustsOnlyTheAuthorityItPinned(t *testing.T) {
	g := peertest.Start(t)
	other := sha256.Sum256([]byte("another authority"))
	cl, err := New(g.URL, "", hex.EncodeToString(other[:]))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cl.Info(ctx); err == nil || !strings.Contains(err.Error(), "not from the Irie authority") {
		t.Errorf("info under another authority: %v", err)
	}
	if _, err := New("http://127.0.0.1:1", "", g.Fingerprint); err == nil {
		t.Error("a plain-HTTP address passed")
	}
}

func TestPeersAreKeptPrivately(t *testing.T) {
	dir := t.TempDir()
	if ps, err := Load(dir); err != nil || len(ps) != 0 {
		t.Fatalf("no file: %v, %v", ps, err)
	}
	p := Peer{URL: "https://asahi.local:7387", Token: "t", Fingerprint: strings.Repeat("0", 64)}
	if err := Save(dir, "Asahi", p); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, "Studio", p); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, File)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("peers.json: %v, %v", fi.Mode(), err)
	}
	if err := Remove(dir, "Studio"); err != nil {
		t.Fatal(err)
	}
	ps, err := Load(dir)
	if err != nil || len(ps) != 1 || ps["Asahi"] != p {
		t.Errorf("peers = %+v, %v", ps, err)
	}
	for _, bad := range []string{"", "a/b", "two words", strings.Repeat("x", 31), "^x"} {
		if CheckName(bad) == nil {
			t.Errorf("CheckName(%q) passed", bad)
		}
	}
}
