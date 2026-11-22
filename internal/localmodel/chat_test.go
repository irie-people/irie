package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAChatIsHeldToItsSchema(t *testing.T) {
	var asked struct{ Format json.RawMessage }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&asked)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"{}"},"done":true}`)
	}))
	defer srv.Close()
	o := Ollama{URL: srv.URL}
	schema := json.RawMessage(`{"type":"object","required":["title"]}`)
	for _, tc := range []struct {
		opt  ChatOptions
		want string
	}{
		{ChatOptions{JSON: true, Schema: schema}, string(schema)},
		{ChatOptions{JSON: true}, `"json"`},
		{ChatOptions{}, ``},
	} {
		asked.Format = nil
		if _, err := o.Chat(context.Background(), "bonsai-2:9b", []ChatMessage{{Role: "user", Content: "a card"}}, tc.opt, nil); err != nil {
			t.Fatal(err)
		}
		if string(asked.Format) != tc.want {
			t.Errorf("format %s; want %s", asked.Format, tc.want)
		}
	}
}
