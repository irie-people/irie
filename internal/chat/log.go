//go:build !nochat

package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
)

// The log of what agents posted, edited and deleted, on their own or
// through a draft the person sent. A post goes out under the person's bot,
// so the person must be able to see all that agents said in their name,
// after an edit or a delete too: the log keeps each, with its text, and the
// tab and irie chat log show it. It also counts the agents' posts in each
// channel against chat.agent_posts, so a restart of the core starts no new
// count.

// logKeep is how many entries the log keeps, the newest.
const logKeep = 2000

// The log's actions.
const (
	LogPost   = "post"
	LogEdit   = "edit"
	LogDelete = "delete"
)

// agentGap is the least time between two posts of agents in one channel.
const agentGap = time.Minute

type agentLog struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

// add writes e at the log's end, and keeps the newest logKeep.
func (l *agentLog) add(e ipc.ChatLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return filetx.Update(l.path, 0o600, func(old []byte) ([]byte, error) {
		lines := bytes.SplitAfter(old, []byte("\n"))
		lines = slices.DeleteFunc(lines, func(b []byte) bool { return len(bytes.TrimSpace(b)) == 0 })
		lines = append(lines, append(line, '\n'))
		if len(lines) > logKeep {
			lines = lines[len(lines)-logKeep:]
		}
		return bytes.Join(lines, nil), nil
	})
}

// read is the log, newest first; a line that doesn't read is left out.
func (l *agentLog) read() []ipc.ChatLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []ipc.ChatLogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e ipc.ChatLogEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Do != "" {
			out = append(out, e)
		}
	}
	slices.Reverse(out)
	return out
}

// room says whether agents may post in account's channel now, by their own
// posts and edits in the log: at most perHour in the last hour, and one a
// minute. A draft the person sent is theirs, and counts for neither.
func (l *agentLog) room(account, channel, name string, perHour int) error {
	now := l.now()
	var n int
	var last time.Time
	for _, e := range l.read() {
		if e.Account != account || e.Channel != channel || e.Draft != "" || e.Do == LogDelete {
			continue
		}
		if now.Sub(e.Time) < time.Hour {
			n++
		}
		if e.Time.After(last) {
			last = e.Time
		}
	}
	if gap := now.Sub(last); gap < agentGap {
		return ipc.Errorf(ipc.CodeBusy, "Agents post at most once a minute in #%s: the next may go in %s.", name, said(agentGap-gap))
	}
	if n >= perHour {
		return ipc.Errorf(ipc.CodeBusy, "Agents have posted %s in #%s in the last hour, the most chat.agent_posts lets: wait, or write a draft for the person instead.", times(n), name)
	}
	return nil
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}
