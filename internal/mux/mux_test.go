package mux

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// wire is the two ends of a mux in one process: the bridge serving
// accept, and the app's client, with the noise a login shell prints first.
func wire(t *testing.T, noise string, accept func(Open, *Stream)) (*Mux, Hello) {
	t.Helper()
	upR, upW := io.Pipe()     // app → bridge
	downR, downW := io.Pipe() // bridge → app
	served := make(chan error, 1)
	go func() {
		io.WriteString(downW, noise)
		served <- Serve(upR, downW, Hello{Version: "9.9.9", Core: "running"}, accept)
		downW.Close()
	}()
	m, h, err := Client(downR, upW, closers{upW, downR})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("the bridge's end did not stop once the app's closed")
		}
	})
	return m, h
}

type closers []io.Closer

func (c closers) Close() error {
	for _, x := range c {
		x.Close()
	}
	return nil
}

func echo(o Open, s *Stream) {
	defer s.Close()
	io.Copy(s, s)
}

func TestHelloAfterAShellsGreeting(t *testing.T) {
	_, h := wire(t, "Welcome to Ubuntu\r\n\x1b[0m$ ", echo)
	if h.Proto != Proto || h.Version != "9.9.9" || h.Core != "running" {
		t.Fatalf("hello = %+v", h)
	}
}

func TestStreamsCarryBytesBothWaysAndApart(t *testing.T) {
	m, _ := wire(t, "", echo)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := m.Open(Open{Kind: StreamCore})
			if err != nil {
				t.Error(err)
				return
			}
			defer s.Close()
			// More than one frame's worth, so writes split and join again.
			want := bytes.Repeat([]byte{byte('a' + i)}, 3*chunk+17)
			go s.Write(want)
			got := make([]byte, len(want))
			if _, err := io.ReadFull(s, got); err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(got, want) {
				t.Errorf("stream %d came back changed", i)
			}
		}(i)
	}
	wg.Wait()
}

func TestEndCarriesWhy(t *testing.T) {
	m, _ := wire(t, "", func(o Open, s *Stream) {
		code := 3
		s.Write([]byte("bye"))
		s.CloseWith(End{Exit: &code})
	})
	s, err := m.Open(Open{Kind: StreamTmux})
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(s)
	if err != nil || string(b) != "bye" {
		t.Fatalf("read %q, %v", b, err)
	}
	if e := s.Ended(); e == nil || e.Exit == nil || *e.Exit != 3 {
		t.Fatalf("ended = %+v", e)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write after the end = %v", err)
	}
}

func TestSizesReachTheBridgeInOrder(t *testing.T) {
	got := make(chan Size, 4)
	m, _ := wire(t, "", func(o Open, s *Stream) {
		if o.Cols != 80 || o.Rows != 24 {
			t.Errorf("open = %+v", o)
		}
		s.OnSize(func(z Size) { got <- z })
		io.Copy(io.Discard, s)
		s.Close()
	})
	s, err := m.Open(Open{Kind: StreamTmux, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	s.Resize(100, 30)
	s.Resize(120, 40)
	for _, want := range []Size{{100, 30}, {120, 40}} {
		select {
		case z := <-got:
			if z != want {
				t.Fatalf("size %+v, want %+v", z, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no size")
		}
	}
	s.Close()
}

func TestReadDeadline(t *testing.T) {
	m, _ := wire(t, "", func(o Open, s *Stream) {
		io.Copy(io.Discard, s)
		s.Close()
	})
	s, err := m.Open(Open{Kind: StreamCore})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	start := time.Now()
	if _, err := s.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the deadline was late")
	}
}

func TestTheBridgeGoingEndsEveryStream(t *testing.T) {
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	go Serve(upR, downW, Hello{}, func(o Open, s *Stream) { io.Copy(io.Discard, s) })
	m, _, err := Client(downR, upW, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.Open(Open{Kind: StreamCore})
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := s.Read(make([]byte, 1))
		read <- err
	}()
	downW.Close() // the bridge's process died
	select {
	case err := <-read:
		if err != io.EOF {
			t.Fatalf("read = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream outlived its wire")
	}
	select {
	case <-m.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the mux outlived its wire")
	}
	if _, err := m.Open(Open{Kind: StreamCore}); err == nil {
		t.Fatal("opened a stream on a dead wire")
	}
	upW.Close()
}

func TestNoBridgeAnswers(t *testing.T) {
	_, _, err := Client(strings.NewReader("sh: 1: irie: not found\n"), io.Discard, nil)
	if err == nil {
		t.Fatal("a shell's error passed for a bridge")
	}
	before, err := SkipToMagic(bufio.NewReader(strings.NewReader("motd\n" + Magic + "rest")))
	if err != nil || string(before) != "motd\n" {
		t.Fatalf("before = %q, %v", before, err)
	}
}

func TestFramesRefuseWhatIsNotOne(t *testing.T) {
	var b bytes.Buffer
	WriteFrame(&b, Frame{Kind: KindData, Stream: 7, Payload: []byte("hi")})
	f, err := ReadFrame(&b)
	if err != nil || f.Kind != KindData || f.Stream != 7 || string(f.Payload) != "hi" {
		t.Fatalf("frame = %+v, %v", f, err)
	}
	if _, err := ReadFrame(strings.NewReader("x\x00\x00\x00\x01\x00\x00\x00\x00")); err != ErrBadFrame {
		t.Fatalf("an unknown kind = %v", err)
	}
	if _, err := ReadFrame(strings.NewReader("d\x00\x00\x00\x01\xff\xff\xff\xff")); err != ErrBadFrame {
		t.Fatalf("a huge payload = %v", err)
	}
	if err := WriteFrame(io.Discard, Frame{Kind: KindData, Payload: make([]byte, MaxPayload+1)}); err == nil {
		t.Fatal("wrote a payload past the limit")
	}
}
