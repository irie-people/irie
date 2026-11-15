package fixture

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/mux"
)

func TestTheStandInAnswersAsIrieWould(t *testing.T) {
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	go func() {
		Serve(upR, downW)
		downW.Close()
	}()
	m, h, err := mux.Client(downR, upW, upW)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if h.Core != "running" {
		t.Fatalf("hello = %+v", h)
	}
	s, err := m.Open(mux.Open{Kind: mux.StreamCore})
	if err != nil {
		t.Fatal(err)
	}
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	io.WriteString(s, `{"id":1,"op":"subscribe","args":{"view":"app"}}`+"\n")
	r := bufio.NewReader(s)
	for _, want := range []string{`"ok":true`, `"t":"snapshot"`, `"t":"outside"`} {
		line, err := r.ReadString('\n')
		if err != nil || !strings.Contains(line, want) {
			t.Fatalf("got %.200q (%v), want %s", line, err, want)
		}
	}
	s.Close()

	term, err := m.Open(mux.Open{Kind: mux.StreamTmux, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	term.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 64<<10)
	n, _ := term.Read(buf)
	if !strings.Contains(string(buf[:n]), "Welcome to Claude Code") {
		t.Fatalf("the terminal drew %q", buf[:n])
	}
	term.Close()
}
