package core

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/irie-hq/irie/internal/ipc"
)

// tokenTally counts a session's model usage from its Claude Code
// transcripts (JSONL). A reply spans one line per content block and each
// line repeats the reply's usage, so each message id counts once. Each file
// is read on from where the last read stopped.
type tokenTally struct {
	files map[string]*transcriptCount
}

type transcriptCount struct {
	follower
	byID map[string]ipc.Tokens // usage per message id
	sum  ipc.Tokens
}

// maxRead caps one read, so a long transcript is caught up over a few ticks.
const maxRead = 8 << 20

// follower reads a growing JSONL file, such as a transcript, a line at a
// time from where its last read stopped. The token tally and the
// conversations read transcripts this way.
type follower struct {
	offset  int64
	partial []byte // an unfinished last line
}

// read calls line for each complete line the file gained since the last
// read, reading at most maxRead bytes. A file that shrank was replaced:
// reset is called first, and the file is read from its start.
func (f *follower) read(path string, reset func(), line func([]byte)) {
	fh, err := os.Open(path)
	if err != nil {
		return // not written yet, or gone
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return
	}
	if st.Size() < f.offset {
		*f = follower{}
		reset()
	}
	if st.Size() == f.offset {
		return
	}
	buf := make([]byte, min(st.Size()-f.offset, maxRead))
	n, _ := fh.ReadAt(buf, f.offset)
	if n == 0 {
		return
	}
	f.offset += int64(n)
	data := append(f.partial, buf[:n]...)
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		f.partial = data
		return
	}
	f.partial = append([]byte(nil), data[end+1:]...)
	for _, l := range bytes.Split(data[:end], []byte{'\n'}) {
		line(l)
	}
}

// update reads what the transcripts gained since the last call and returns
// the totals, or false while none of them has any usage.
func (t *tokenTally) update(paths []string) (ipc.Tokens, bool) {
	if t.files == nil {
		t.files = map[string]*transcriptCount{}
	}
	var total ipc.Tokens
	seen := false
	for _, p := range paths {
		f := t.files[p]
		if f == nil {
			f = &transcriptCount{byID: map[string]ipc.Tokens{}}
			t.files[p] = f
		}
		f.read(p)
		if len(f.byID) > 0 {
			seen = true
			total = addTokens(total, f.sum, 1)
		}
	}
	return total, seen
}

func (f *transcriptCount) read(path string) {
	// Replaced or cut short: count it again from the start.
	f.follower.read(path, func() { f.byID, f.sum = map[string]ipc.Tokens{}, ipc.Tokens{} }, f.line)
}

func (f *transcriptCount) line(b []byte) {
	if !bytes.Contains(b, []byte(`"usage"`)) {
		return
	}
	var rec struct {
		Message struct {
			ID    string `json:"id"`
			Usage *struct {
				Input      int64 `json:"input_tokens"`
				Output     int64 `json:"output_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(b, &rec) != nil || rec.Message.ID == "" || rec.Message.Usage == nil {
		return
	}
	u := rec.Message.Usage
	now := ipc.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
	if old, ok := f.byID[rec.Message.ID]; ok {
		f.sum = addTokens(f.sum, old, -1)
	}
	f.byID[rec.Message.ID] = now
	f.sum = addTokens(f.sum, now, 1)
}

// addTokens returns a plus sign times b.
func addTokens(a, b ipc.Tokens, sign int64) ipc.Tokens {
	return ipc.Tokens{
		Input:      a.Input + sign*b.Input,
		Output:     a.Output + sign*b.Output,
		CacheRead:  a.CacheRead + sign*b.CacheRead,
		CacheWrite: a.CacheWrite + sign*b.CacheWrite,
	}
}
