package core

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/peer"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/testutil/peertest"
)

// startPeerLlama starts the fake llama-server that stands for a peer's,
// on the port after a pair found free, as the tunnel reaches it, and
// returns the first of the pair once it answers. On a busy machine another
// process can take a port found free before the server binds it. The
// server then exits at once, which a wait for it to answer took for five
// minutes of slowness (an install on 2026-11-21, at a load of 16-20), so
// it starts again on other ports, and says why it exited if it never ran.
func startPeerLlama(t *testing.T, model string) int {
	t.Helper()
	bin := testutil.BuildFakeLlama(t)
	why := "it never answered"
	for range 5 {
		first := testutil.FreePorts(t, 2)
		url := fmt.Sprintf("http://127.0.0.1:%d", first+1)
		var stderr bytes.Buffer
		server := exec.Command(bin, "-m", model, "--alias", "dory-4b", "--port", fmt.Sprint(first+1), "-c", "196608", "-np", "3")
		server.Stderr = &stderr
		if err := server.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		go func() { server.Wait(); close(exited) }()
		answers := func() bool {
			for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
				select {
				case <-exited:
					return false
				case <-time.After(50 * time.Millisecond):
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				ready, err := localmodel.Llama{URL: url}.Ready(ctx)
				cancel()
				if err == nil && ready {
					// Another's server may hold the port and answer: this one
					// must still be running.
					select {
					case <-exited:
						return false
					default:
						return true
					}
				}
			}
			return false
		}
		if answers() {
			t.Cleanup(func() { server.Process.Kill(); <-exited })
			return first
		}
		server.Process.Kill()
		<-exited
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			why = msg
		}
	}
	t.Fatalf("the fake llama-server did not start: %s", why)
	return 0
}

func TestTunnelPorts(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["dory"] = config.Profile{Harness: "claude-code", Local: &config.LocalModel{Runtime: "llama-server", Peer: "Asahi"}}
	cfg.Profiles["other"] = config.Profile{Harness: "claude-code", Local: &config.LocalModel{Runtime: "llama-server", Peer: "Box"}}
	if got := tunnelPeers(&cfg); !slices.Equal(got, []string{"Asahi", "Box"}) {
		t.Errorf("tunnel peers = %v", got)
	}
	if tunnelPort(&cfg, "Asahi") != 11437 || tunnelPort(&cfg, "Box") != 11438 || tunnelURL(&cfg, "Box") != "http://127.0.0.1:11438" {
		t.Errorf("ports = %d, %d", tunnelPort(&cfg, "Asahi"), tunnelPort(&cfg, "Box"))
	}
	if modelOf("/home/alice/.local/share/irie/models/dory-4b.gguf") != "dory-4b" {
		t.Error(modelOf("/home/alice/.local/share/irie/models/dory-4b.gguf"))
	}
}

// TestAModelOnAPeerRunsThroughATunnel: a profile whose model runs on a
// peer's llama-server reaches it through an ssh tunnel the core keeps,
// and its agent, here, points at the tunnel. A fake ssh holds the tunnel
// open, and a fake llama-server on the tunnel's port stands for the
// peer's.
func TestAModelOnAPeerRunsThroughATunnel(t *testing.T) {
	g := peertest.Start(t)
	g.Core.Start(nil, &ipc.Header{Version: "peer-v", Machine: &ipc.Machine{Name: "Asahi", Claude: "ok"}})
	// The peer's llama-server, as the tunnel reaches it: on the port after
	// llama_port (tunnelPort).
	model := filepath.Join(t.TempDir(), "dory-4b.gguf")
	if err := os.WriteFile(model, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	llamaPort := startPeerLlama(t, model)
	tunnel := llamaPort + 1
	fake := testutil.BuildFakeAgent(t)
	c := prepare(t, agentConfig{
		extra: fmt.Sprintf(`[profiles.dory]
command = %q
model = "dory-4b"
local = { runtime = "llama-server", peer = "Asahi" }

[peers.Asahi]
ssh = "asahi-host"

[route]
profiles = ["dory", "fake"]

[route.accuracy]
dory = { small = 0.86, medium = 0.35, hard = 0.05 }
fake = { small = 0.95, medium = 0.9, hard = 0.9 }

[route.cost]
dory = 0.0
fake = 1.0
`, fake),
		local: fmt.Sprintf("ollama = \"/nonexistent/ollama\"\nllama_server = \"/nonexistent/llama-server\"\nllama_port = %d\n", llamaPort),
	})

	// The fake ssh: with -N it holds a tunnel open and logs its words;
	// with a command, it says it ran it.
	bin := t.TempDir()
	log := filepath.Join(bin, "ssh.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nfor a in \"$@\"; do [ \"$a\" = -N ] && exec sleep 3600; done\necho loaded\n", log)
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Pair with the peer before Irie starts, as `irie peer add` would.
	cl, err := peer.New(g.URL, "", g.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	token, err := cl.Pair(ctx, g.Code(t), "Mac (Irie)")
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.Save(c.paths.State, "Asahi", peer.Peer{URL: g.URL, Token: token, Fingerprint: g.Fingerprint, SSH: "asahi-host"}); err != nil {
		t.Fatal(err)
	}

	c.up()

	// A spawn needs the peer followed, and its server answering, which
	// startPeerLlama waited for; on a busy machine following takes a
	// while, so wait for it rather than race it.
	waitUntil(t, "the core follows the peer", func() bool {
		var pr ipc.PeersResult
		return c.call("peers", ipc.PeersArgs{}, &pr) == nil && len(pr.Peers) == 1 && pr.Peers[0].Up
	})

	var r ipc.SpawnResult
	if err := c.call("spawn", ipc.SpawnArgs{Task: "prompt", Profile: "dory"}, &r); err != nil {
		t.Fatal(err)
	}
	if row := c.waitState(r.Session.ID, ipc.StateWorking); !row.Local || row.Model != "dory-4b" {
		t.Errorf("row = %+v", row)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(r.Session.ID))
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"ANTHROPIC_BASE_URL": fmt.Sprintf("http://127.0.0.1:%d", tunnel), "ANTHROPIC_DEFAULT_OPUS_MODEL": "dory-4b",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "65536", "ANTHROPIC_API_KEY": "",
	} {
		if v := spec.Env[k]; v != want {
			t.Errorf("agent %s = %q, want %q", k, v, want)
		}
	}

	// The tunnel is a window of Irie's own, forwarding the tunnel's port
	// to the peer's llama-server over ssh, with no prompt.
	var tunnels []map[string]string
	for _, p := range c.panes() {
		if p["@irie_role"] == "tunnel" {
			tunnels = append(tunnels, p)
		}
	}
	if len(tunnels) != 1 || tunnels[0]["@irie_name"] != "Asahi" || tunnels[0]["session_name"] != brand.Slug+"-agents" {
		t.Fatalf("tunnel panes = %v", tunnels)
	}
	// The fake server answers before the tunnel's ssh has started, so its
	// words take a moment.
	var data []byte
	waitUntil(t, "the tunnel's ssh runs", func() bool {
		data, _ = os.ReadFile(log)
		return len(data) > 0
	})
	if words := string(data); !strings.Contains(words, fmt.Sprintf("-L 127.0.0.1:%d:127.0.0.1:%d", tunnel, llamaPort)) || !strings.Contains(words, "BatchMode=yes") || !strings.Contains(words, "-- asahi-host") {
		t.Errorf("ssh was run as %q", words)
	}
	// Its model was loaded there already, so the peer was not asked.
	if strings.Contains(string(data), "ctl preflight") {
		t.Errorf("the peer was asked to load a model it runs: %q", data)
	}

	// The router counts it usable, free and held to the local floor, so
	// small work goes to it.
	ch := c.routeDry(smallTask)
	i := slices.IndexFunc(ch.Candidates, func(cand route.Candidate) bool { return cand.Profile == "dory" })
	if ch.Profile != "dory" || i < 0 || !ch.Candidates[i].Usable || ch.Candidates[i].Floor != 0.6 || ch.Candidates[i].Cost != 0 || !strings.Contains(ch.Candidates[i].Note, "Asahi's llama-server") {
		t.Errorf("small work = %+v", ch)
	}
}

// waitUntil waits until ok holds: up to 30 s, or 5 min on a machine too
// busy for a time bound (slow).
func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	limit := 30 * time.Second
	if slow(t) {
		limit = 5 * time.Minute
	}
	for deadline := time.Now().Add(limit); !ok(); time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %v", what, limit)
		}
	}
}
