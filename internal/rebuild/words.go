package rebuild

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// Dollars is v in dollars and cents, as "$4.10"; "<$0.01" for less than a
// cent that is more than nothing.
func Dollars(v float64) string {
	if v > 0 && v < 0.005 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}

// Tokens is n tokens in a few characters: 950, 12k, 1.2M.
func Tokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 999_500:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1e6)
}

// Times is how often, in words: once, twice, 3 times.
func Times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	}
	return fmt.Sprintf("%d times", n)
}

// tailSize is how much of a transcript's end LastWake reads: enough for its
// last replies, however long the transcript.
const tailSize = 2 << 20

// LastWake is what the next request of the conversation in the transcript
// at path would cost in its cache, read from the transcript's end, and
// false when that says nothing of it, as for a closed session's transcript
// that no reading of it followed.
func LastWake(path string) (Wake, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Wake{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Wake{}, false
	}
	from := max(st.Size()-tailSize, 0)
	buf := make([]byte, st.Size()-from)
	if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
		return Wake{}, false
	}
	if from > 0 {
		// The first line is cut short.
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	var t Transcript
	for _, b := range bytes.Split(buf, []byte{'\n'}) {
		// A reply's later lines come right after its first, which its
		// chain tells apart by their message id.
		if l, ok := ParseLine(bytes.TrimSpace(b)); ok {
			t.Read(l, true)
		}
	}
	return t.Wake()
}
