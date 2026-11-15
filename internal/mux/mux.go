// Package mux carries several streams of bytes over one, both ways. The
// Windows app reaches Irie inside WSL 2 through a single wsl.exe, whose
// standard input and output carry every stream it needs: a connection to
// the core's socket for each call and subscription, and the terminal of
// the slot (docs/WINDOWS.md §5). `irie bridge` is the far end, inside
// Linux.
//
// Nothing here needs Windows. The bridge's end runs on Linux, the app's on
// Windows, and the tests run both ends in one process.
//
// The bridge writes Magic first. Whatever a login shell printed before
// the bridge started comes before it, and the app skips it. After Magic,
// both ways carry frames:
//
//	kind    1 byte: Hello, Open, Data, Size or End
//	stream  4 bytes, big-endian: the app numbers its streams from 1; 0 is the bridge's own
//	length  4 bytes, big-endian: at most MaxPayload
//	payload length bytes
//
// The app opens a stream with Open, whose payload says what the stream
// is (an Open, as JSON). Data frames carry its bytes either way. Size
// gives a terminal its size. End closes the stream for both sides,
// with an optional payload (an End, as JSON) that says why.
package mux

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Magic is the line the bridge writes before its first frame.
const Magic = "\x00IRIE-MUX/1\n"

// Proto is this protocol's version, which Hello carries.
const Proto = 1

// Frame kinds.
const (
	KindHello = 'h' // the bridge's first frame, on stream 0: a Hello
	KindOpen  = 'o' // the app opens a stream: an Open
	KindData  = 'd' // bytes of a stream, either way
	KindSize  = 's' // a terminal's new size: a Size
	KindEnd   = 'e' // the stream is over, for both sides: an End, or nothing
)

// MaxPayload is the longest payload a frame may carry. A longer write goes
// as several frames.
const MaxPayload = 1 << 20

// chunk is how much of a write goes in one data frame, so one stream's
// burst never holds the others up for long.
const chunk = 32 << 10

// MaxUnread is how much a stream keeps that its reader has not read yet.
// A stream that passes it ends with an error rather than take the memory.
const MaxUnread = 64 << 20

// noise is how much a login shell may print before Magic.
const noise = 1 << 20

// Hello is what the bridge says first: who it is, and how the core stands.
type Hello struct {
	Proto   int    `json:"proto"`
	Version string `json:"version"` // Irie's version inside Linux
	// Core is "running" when the core answered, "started" when the bridge
	// started it, and "" when it is not up: Problem then says why.
	Core    string `json:"core,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// Kinds of stream an Open asks for.
const (
	StreamCore = "core" // a connection to the core's socket
	StreamTmux = "tmux" // a tmux client in a terminal of its own
)

// Open is what a stream is to be.
type Open struct {
	Kind string `json:"kind"`
	// With StreamTmux: tmux -S Socket attach-session -t Target, in a
	// terminal of Cols by Rows, as the core's attach-info answers. Tmux is
	// the binary, from PATH when it is "".
	Tmux   string `json:"tmux,omitempty"`
	Socket string `json:"socket,omitempty"`
	Target string `json:"target,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

// Size is a terminal's size in cells.
type Size struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// End says why a stream ended: an error in words, or the exit code of the
// program a terminal ran.
type End struct {
	Error string `json:"error,omitempty"`
	Exit  *int   `json:"exit,omitempty"`
}

// Frame is one frame of the wire.
type Frame struct {
	Kind    byte
	Stream  uint32
	Payload []byte
}

// ErrBadFrame is a frame that is not one: the stream is not this
// protocol, or it was cut.
var ErrBadFrame = errors.New("mux: bad frame")

// WriteFrame writes f to w in one write.
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxPayload {
		return fmt.Errorf("mux: a payload of %d bytes is longer than %d", len(f.Payload), MaxPayload)
	}
	b := make([]byte, 9+len(f.Payload))
	b[0] = f.Kind
	binary.BigEndian.PutUint32(b[1:5], f.Stream)
	binary.BigEndian.PutUint32(b[5:9], uint32(len(f.Payload)))
	copy(b[9:], f.Payload)
	_, err := w.Write(b)
	return err
}

// ReadFrame reads one frame from r.
func ReadFrame(r io.Reader) (Frame, error) {
	var head [9]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Frame{}, err
	}
	f := Frame{Kind: head[0], Stream: binary.BigEndian.Uint32(head[1:5])}
	switch f.Kind {
	case KindHello, KindOpen, KindData, KindSize, KindEnd:
	default:
		return Frame{}, ErrBadFrame
	}
	n := binary.BigEndian.Uint32(head[5:9])
	if n > MaxPayload {
		return Frame{}, ErrBadFrame
	}
	if n > 0 {
		f.Payload = make([]byte, n)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return Frame{}, err
		}
	}
	return f, nil
}

// SkipToMagic reads r up to and through Magic, and gives what came before
// it, such as a login shell's greeting.
func SkipToMagic(r *bufio.Reader) (before []byte, err error) {
	var seen []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(seen) > 0 {
				err = io.ErrUnexpectedEOF
			}
			return seen, err
		}
		seen = append(seen, c)
		if bytes.HasSuffix(seen, []byte(Magic)) {
			return seen[:len(seen)-len(Magic)], nil
		}
		if len(seen) > noise {
			return seen, fmt.Errorf("mux: no bridge answered: %d bytes came before any sign of one", len(seen))
		}
	}
}

// Mux is one end of the wire.
type Mux struct {
	w   io.Writer
	wmu sync.Mutex

	mu      sync.Mutex
	streams map[uint32]*Stream
	next    uint32
	closed  bool
	err     error
	done    chan struct{}

	// The bridge's end: what it does with each stream the app opens.
	accept func(Open, *Stream)
	closer io.Closer
}

// Client is the app's end. It reads through Magic and the bridge's Hello,
// then serves the wire until it ends. closer, when not nil, is closed with
// the mux, as the pipes of the bridge's process are. A caller that must
// not wait forever for a bridge that never answers closes them itself.
func Client(r io.Reader, w io.Writer, closer io.Closer) (*Mux, Hello, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	if _, err := SkipToMagic(br); err != nil {
		return nil, Hello{}, err
	}
	f, err := ReadFrame(br)
	if err != nil {
		return nil, Hello{}, err
	}
	var h Hello
	if f.Kind != KindHello || f.Stream != 0 || json.Unmarshal(f.Payload, &h) != nil {
		return nil, Hello{}, ErrBadFrame
	}
	m := newMux(w, closer)
	go m.read(br)
	return m, h, nil
}

// Serve is the bridge's end. It writes Magic and h, then serves the
// streams the app opens, each with accept on a goroutine of its own, until
// the app's side of the wire ends. accept owns the stream and closes it.
func Serve(r io.Reader, w io.Writer, h Hello, accept func(Open, *Stream)) error {
	h.Proto = Proto
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, Magic); err != nil {
		return err
	}
	if err := WriteFrame(w, Frame{Kind: KindHello, Payload: b}); err != nil {
		return err
	}
	m := newMux(w, nil)
	m.accept = accept
	m.read(bufio.NewReaderSize(r, 64<<10))
	return m.Err()
}

func newMux(w io.Writer, closer io.Closer) *Mux {
	return &Mux{w: w, streams: map[uint32]*Stream{}, done: make(chan struct{}), closer: closer}
}

// Done is closed once the wire has ended.
func (m *Mux) Done() <-chan struct{} { return m.done }

// Err is why the wire ended: nil while it lasts, io.EOF when the other
// end closed it.
func (m *Mux) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// Close ends the wire and every stream on it.
func (m *Mux) Close() error {
	m.fail(errors.New("mux: closed"))
	return nil
}

func (m *Mux) fail(err error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.err = err
	streams := m.streams
	m.streams = map[uint32]*Stream{}
	close(m.done)
	m.mu.Unlock()
	for _, s := range streams {
		s.ended(End{Error: "the bridge went away"})
	}
	if m.closer != nil {
		m.closer.Close()
	}
}

func (m *Mux) send(f Frame) error {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	select {
	case <-m.done:
		return net.ErrClosed
	default:
	}
	if err := WriteFrame(m.w, f); err != nil {
		go m.fail(err)
		return err
	}
	return nil
}

// Open opens a stream: the app's side.
func (m *Mux) Open(o Open) (*Stream, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, net.ErrClosed
	}
	m.next++
	s := newStream(m, m.next)
	m.streams[s.id] = s
	m.mu.Unlock()
	if err := m.send(Frame{Kind: KindOpen, Stream: s.id, Payload: b}); err != nil {
		m.drop(s.id)
		return nil, err
	}
	return s, nil
}

func (m *Mux) drop(id uint32) {
	m.mu.Lock()
	delete(m.streams, id)
	m.mu.Unlock()
}

func (m *Mux) stream(id uint32) *Stream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[id]
}

func (m *Mux) read(r io.Reader) {
	for {
		f, err := ReadFrame(r)
		if err != nil {
			m.fail(err)
			return
		}
		switch f.Kind {
		case KindOpen:
			var o Open
			if m.accept == nil || f.Stream == 0 || json.Unmarshal(f.Payload, &o) != nil {
				m.send(Frame{Kind: KindEnd, Stream: f.Stream, Payload: endPayload(End{Error: "the bridge can't open that"})})
				continue
			}
			m.mu.Lock()
			if _, dup := m.streams[f.Stream]; dup || m.closed {
				m.mu.Unlock()
				continue
			}
			s := newStream(m, f.Stream)
			m.streams[f.Stream] = s
			m.mu.Unlock()
			go m.accept(o, s)
		case KindData:
			if s := m.stream(f.Stream); s != nil {
				s.take(f.Payload)
			}
		case KindSize:
			var z Size
			if s := m.stream(f.Stream); s != nil && json.Unmarshal(f.Payload, &z) == nil {
				s.sized(z)
			}
		case KindEnd:
			var e End
			if len(f.Payload) > 0 {
				json.Unmarshal(f.Payload, &e)
			}
			if s := m.stream(f.Stream); s != nil {
				m.drop(f.Stream)
				s.ended(e)
			}
		}
	}
}

func endPayload(e End) []byte {
	if e.Error == "" && e.Exit == nil {
		return nil
	}
	b, _ := json.Marshal(e)
	return b
}

// Stream is one stream of a Mux. It is a net.Conn: Read gives the bytes the
// other side wrote, in order, then io.EOF once it ended the stream, and
// honours a read deadline. Write sends at once; a write deadline is not
// kept, since every stream shares the one wire.
type Stream struct {
	id uint32
	m  *Mux

	mu       sync.Mutex
	cond     *sync.Cond
	buf      bytes.Buffer
	end      *End // set once the stream is over
	byUs     bool // we ended it
	deadline time.Time
	timer    *time.Timer
	onSize   func(Size)
	sizes    []Size // sizes that came before OnSize was set
}

func newStream(m *Mux, id uint32) *Stream {
	s := &Stream{id: id, m: m}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// ID is the stream's number on the wire.
func (s *Stream) ID() uint32 { return s.id }

func (s *Stream) take(p []byte) {
	s.mu.Lock()
	if s.end != nil {
		s.mu.Unlock()
		return
	}
	if s.buf.Len()+len(p) > MaxUnread {
		s.mu.Unlock()
		s.CloseWith(End{Error: "the stream was not read for too long"})
		return
	}
	s.buf.Write(p)
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *Stream) ended(e End) {
	s.mu.Lock()
	if s.end == nil {
		s.end = &e
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *Stream) sized(z Size) {
	s.mu.Lock()
	f := s.onSize
	if f == nil {
		s.sizes = append(s.sizes, z)
	}
	s.mu.Unlock()
	if f != nil {
		f(z)
	}
}

// OnSize calls f with each Size the other side sends, those that came
// before it was called first: the bridge's side, for a terminal.
func (s *Stream) OnSize(f func(Size)) {
	s.mu.Lock()
	s.onSize = f
	early := s.sizes
	s.sizes = nil
	s.mu.Unlock()
	for _, z := range early {
		f(z)
	}
}

// Read reads what the other side sent.
func (s *Stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.buf.Len() == 0 {
		if s.end != nil {
			if s.byUs {
				return 0, net.ErrClosed
			}
			return 0, io.EOF
		}
		if !s.deadline.IsZero() && !time.Now().Before(s.deadline) {
			return 0, os.ErrDeadlineExceeded
		}
		s.cond.Wait()
	}
	return s.buf.Read(p)
}

// Write sends p to the other side.
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	over := s.end != nil
	s.mu.Unlock()
	if over {
		return 0, net.ErrClosed
	}
	n := 0
	for n < len(p) {
		k := min(len(p)-n, chunk)
		if err := s.m.send(Frame{Kind: KindData, Stream: s.id, Payload: p[n : n+k]}); err != nil {
			return n, err
		}
		n += k
	}
	return n, nil
}

// Resize gives the terminal behind the stream a new size: the app's side.
func (s *Stream) Resize(cols, rows int) error {
	b, _ := json.Marshal(Size{Cols: cols, Rows: rows})
	return s.m.send(Frame{Kind: KindSize, Stream: s.id, Payload: b})
}

// Close ends the stream for both sides.
func (s *Stream) Close() error { return s.CloseWith(End{}) }

// CloseWith ends the stream, telling the other side why.
func (s *Stream) CloseWith(e End) error {
	s.mu.Lock()
	if s.end != nil {
		s.mu.Unlock()
		return nil
	}
	s.end = &e
	s.byUs = true
	s.buf.Reset()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	s.m.drop(s.id)
	err := s.m.send(Frame{Kind: KindEnd, Stream: s.id, Payload: endPayload(e)})
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	return err
}

// Ended says why the other side ended the stream; nil while it has not.
func (s *Stream) Ended() *End {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.end == nil || s.byUs {
		return nil
	}
	e := *s.end
	return &e
}

// SetReadDeadline makes a Read that waits past t fail with
// os.ErrDeadlineExceeded; the zero time waits for ever.
func (s *Stream) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadline = t
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !t.IsZero() {
		s.timer = time.AfterFunc(time.Until(t), func() {
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		})
	}
	s.cond.Broadcast()
	return nil
}

// SetDeadline sets the read deadline; writes have none.
func (s *Stream) SetDeadline(t time.Time) error { return s.SetReadDeadline(t) }

// SetWriteDeadline does nothing: every stream shares the one wire.
func (s *Stream) SetWriteDeadline(time.Time) error { return nil }

type addr string

func (a addr) Network() string { return "mux" }
func (a addr) String() string  { return string(a) }

func (s *Stream) LocalAddr() net.Addr  { return addr(fmt.Sprintf("stream %d", s.id)) }
func (s *Stream) RemoteAddr() net.Addr { return addr(fmt.Sprintf("stream %d", s.id)) }
