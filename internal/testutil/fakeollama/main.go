// Command fakeollama stands in for `ollama serve` in Irie's tests: the
// server of package ollamafake, on the address OLLAMA_HOST names, loading
// models with the context OLLAMA_CONTEXT_LENGTH names, as Ollama's FAQ
// describes those variables.
//
// Environment:
//
//	OLLAMA_HOST              where to listen, such as 127.0.0.1:11435
//	OLLAMA_CONTEXT_LENGTH    the context a loaded model gets (default 4096)
//	IRIE_FAKE_OLLAMA_MODELS  a JSON file listing the models, as ollamafake.Model
//	IRIE_FAKE_OLLAMA_LOG     a file it writes its OLLAMA_* environment to
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/irie-hq/irie/internal/testutil/ollamafake"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: fakeollama serve")
		os.Exit(2)
	}
	var models []ollamafake.Model
	if f := os.Getenv("IRIE_FAKE_OLLAMA_MODELS"); f != "" {
		b, err := os.ReadFile(f)
		if err == nil {
			err = json.Unmarshal(b, &models)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeollama:", err)
			os.Exit(1)
		}
	}
	if f := os.Getenv("IRIE_FAKE_OLLAMA_LOG"); f != "" {
		env := map[string]string{}
		for _, kv := range os.Environ() {
			if k, v, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "OLLAMA_") {
				env[k] = v
			}
		}
		b, _ := json.Marshal(env)
		os.WriteFile(f, append(b, '\n'), 0o600)
	}
	ctx, _ := strconv.Atoi(os.Getenv("OLLAMA_CONTEXT_LENGTH"))
	addr := os.Getenv("OLLAMA_HOST")
	if addr == "" {
		addr = "127.0.0.1:11434"
	}
	fmt.Println("fakeollama listening on", addr)
	if err := http.ListenAndServe(addr, ollamafake.New(ctx, models).Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "fakeollama:", err)
		os.Exit(1)
	}
}
