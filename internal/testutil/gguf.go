package testutil

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"slices"
	"sort"
	"testing"
)

// GGUFTensor is one tensor of a GGUF file WriteGGUF writes.
type GGUFTensor struct {
	Name  string
	Type  uint32 // ggml's type number: 0 is F32, 12 Q4_K
	Bytes int64  // its data, which WriteGGUF leaves as a hole
}

// WriteGGUF writes a GGUF file (version 3) with the given metadata and
// tensors, as the GGUF specification lays one out. Metadata values may be
// strings, ints (written as uint32), float64s (as float32), bools, and
// slices of ints or strings. The tensors' data is a hole in a sparse file,
// so a file of gigabytes takes no space.
func WriteGGUF(t testing.TB, path string, meta map[string]any, tensors []GGUFTensor) {
	t.Helper()
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	str := func(s string) { w(uint64(len(s))); b.WriteString(s) }
	b.WriteString("GGUF")
	w(uint32(3))
	w(uint64(len(tensors)))
	w(uint64(len(meta)))
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		str(k)
		switch v := meta[k].(type) {
		case string:
			w(uint32(8))
			str(v)
		case int:
			w(uint32(4))
			w(uint32(v))
		case float64:
			w(uint32(6))
			w(math.Float32bits(float32(v)))
		case bool:
			w(uint32(7))
			w(v)
		case []int:
			w(uint32(9))
			w(uint32(5)) // int32
			w(uint64(len(v)))
			for _, x := range v {
				w(int32(x))
			}
		case []string:
			w(uint32(9))
			w(uint32(8))
			w(uint64(len(v)))
			for _, x := range v {
				str(x)
			}
		default:
			t.Fatalf("WriteGGUF: %s has a value of type %T", k, v)
		}
	}
	const align = 32
	var off int64
	for _, tn := range tensors {
		str(tn.Name)
		w(uint32(1))
		w(uint64(max(tn.Bytes, 1)))
		w(tn.Type)
		w(uint64(off))
		off += (tn.Bytes + align - 1) / align * align
	}
	for b.Len()%align != 0 {
		b.WriteByte(0)
	}
	header := slices.Clone(b.Bytes())
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(header); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(len(header)) + off); err != nil {
		t.Fatal(err)
	}
}
