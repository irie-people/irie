package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
)

func TestSetupPicksTheBestModelThatFits(t *testing.T) {
	const gib = localmodel.GiB
	// Two thirds of an 18 GiB Mac, at Irie's own server's settings.
	st := ipc.LocalResult{Context: 65536, KVCache: "q8_0", Memory: ipc.Memory{Budget: 12 * gib}}
	runnable := func(names ...string) []ipc.LocalModel {
		var ms []ipc.LocalModel
		for _, n := range names {
			ms = append(ms, ipc.LocalModel{Name: n, Need: 5e9, Fits: true})
		}
		return ms
	}
	for _, c := range []struct {
		name   string
		disk   int64
		models []ipc.LocalModel
		want   string
	}{
		{"nothing there, room to pull the best", 100 * gib, nil, "bonsai-2:27b"},
		{"unknown free space", 0, nil, "bonsai-2:27b"},
		{"the best is there", 1 * gib, runnable("qwen3.5:9b", "bonsai-2:27b"), "bonsai-2:27b"},
		{"a better one to pull", 100 * gib, runnable("qwen3.5:9b"), "bonsai-2:27b"},
		// 15 GiB less 7.7 GiB of Bonsai 2 27B would leave under 10 GiB.
		{"room only for a smaller pull", 15 * gib, nil, "bonsai-2:9b"},
		{"no room to pull: the best there", 5 * gib, runnable("other:7b", "qwen3.5:9b"), "qwen3.5:9b"},
		{"no room and no suggestion there", 5 * gib, runnable("other:7b"), "other:7b"},
		{"a name in another case", 5 * gib, runnable("other:7b", "Bonsai-2:9B"), "bonsai-2:9b"},
	} {
		st.Disk, st.Models = c.disk, c.models
		if got := pickModel(st); got != c.want {
			t.Errorf("%s: picked %q, want %q", c.name, got, c.want)
		}
	}

	// An 8 GiB laptop gets a 9B model in 3.2 GB.
	st.Memory.Budget, st.Disk, st.Models = localmodel.Budget(0, localmodel.Machine{Total: 8 * gib}), 100*gib, nil
	if got := pickModel(st); got != "bonsai-2:9b" {
		t.Errorf("8 GiB: picked %q", got)
	}
}

func TestTheModelTableSaysWhatPlusLocalRuns(t *testing.T) {
	var b bytes.Buffer
	printLocal(&b, ipc.LocalResult{
		URL: "http://127.0.0.1:11435", Own: true, Running: true, Context: 65536, KVCache: "q8_0", Pick: "bonsai-2:27b", Disk: 25e9,
		Models: []ipc.LocalModel{{Name: "bonsai-2:27b", Size: 8_255_266_099, Need: 11_400_000_000, Fits: true, About: "Qwen3.8-27B, ternary"}},
	})
	out := b.String()
	for _, want := range []string{"disk     25.0 GB free for models", "+local   bonsai-2:27b, the best model here that fits", "+local runs it, Qwen3.8-27B, ternary"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
