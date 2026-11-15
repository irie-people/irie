package localmodel

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/irie-people/irie/internal/testutil/ollamafake"
)

func TestEmbedGivesAVectorForEachInput(t *testing.T) {
	f := ollamafake.New(0, []ollamafake.Model{{Name: "embeddinggemma-2:270m", Size: 378 << 20, Embeds: true}, {Name: "qwen3.5:9b", Size: 6 << 30, Tools: true}})
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	o := Ollama{URL: srv.URL}
	ctx := context.Background()
	vs, err := o.Embed(ctx, "embeddinggemma-2:270m", []string{"fix the typo", "design a plugin system"}, "30m")
	if err != nil || len(vs) != 2 || len(vs[0]) == 0 {
		t.Fatalf("embed = %d vectors, %v", len(vs), err)
	}
	if f.Embeds != 2 {
		t.Errorf("the server embedded %d texts", f.Embeds)
	}
	// A chat model, or one that is not there, embeds nothing.
	for _, m := range []string{"qwen3.5:9b", "none:1b"} {
		if _, err := o.Embed(ctx, m, []string{"x"}, ""); !NotFound(err) {
			t.Errorf("embed on %s: %v", m, err)
		}
	}
}
