// Command fakellama stands in for llama.cpp's llama-server in Irie's
// tests: it takes the flags Irie passes, answers /health (503 while it
// "loads", then 200), /props and /v1/messages as llama-server's README
// describes them, and prints its version like an upstream build.
//
// Environment:
//
//	IRIE_FAKE_LLAMA_LOG    a file it writes its arguments to, as JSON
//	IRIE_FAKE_LLAMA_BUILD  the build number --version gives (default 12000)
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

func main() {
	build := os.Getenv("IRIE_FAKE_LLAMA_BUILD")
	if build == "" {
		build = "12000"
	}
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(os.Stderr, "version: %s (fake)\nbuilt with a test\n", build)
		return
	}
	if f := os.Getenv("IRIE_FAKE_LLAMA_LOG"); f != "" {
		b, _ := json.Marshal(args)
		os.WriteFile(f, b, 0o600)
	}
	flag := func(names ...string) string {
		for i := 0; i+1 < len(args); i++ {
			for _, n := range names {
				if args[i] == n {
					return args[i+1]
				}
			}
		}
		return ""
	}
	model, alias, port := flag("-m", "--model"), flag("--alias", "-a"), flag("--port")
	ctx, _ := strconv.Atoi(flag("-c", "--ctx-size"))
	if _, err := os.Stat(model); err != nil {
		fmt.Fprintln(os.Stderr, "fakellama: failed to load model:", err)
		os.Exit(1)
	}
	var ready atomic.Bool
	time.AfterFunc(300*time.Millisecond, func() { ready.Store(true) })
	reply := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			reply(w, map[string]any{"error": map[string]any{"code": 503, "message": "Loading model", "type": "unavailable_error"}})
			return
		}
		reply(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /props", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"default_generation_settings": map[string]any{"n_ctx": ctx}, "total_slots": 1, "model_path": model, "build_info": "b" + build + "-fake"})
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"id": "msg_fake", "type": "message", "role": "assistant", "model": alias,
			"content": []map[string]string{{"type": "text", "text": "OK"}}, "stop_reason": "end_turn"})
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		fmt.Fprintln(os.Stderr, "fakellama:", err)
		os.Exit(1)
	}
}
