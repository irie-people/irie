package localmodel

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// GGUF is what Irie reads of a model file in GGUF, the format of
// llama.cpp and Ollama: its metadata and where each tensor's data lies.
type GGUF struct {
	Meta    map[string]any // numbers as float64 or uint64/int64, strings, bools, and arrays of them as []any
	Tensors []Tensor       // in file order
	Size    int64          // the file's size
}

// Tensor is one tensor of a GGUF file.
type Tensor struct {
	Name   string
	Type   uint32 // ggml's type number
	Offset int64  // from the start of the data section
	Bytes  int64  // the bytes up to the next tensor's data, or the file's end
}

// GGUF value types (the GGUF specification).
const (
	ggufUint8 = iota
	ggufInt8
	ggufUint16
	ggufInt16
	ggufUint32
	ggufInt32
	ggufFloat32
	ggufBool
	ggufString
	ggufArray
	ggufUint64
	ggufInt64
	ggufFloat64
)

// ggufMaxArray bounds the elements of a metadata array Irie keeps; longer
// ones, such as a tokenizer's vocabulary, are skipped.
const ggufMaxArray = 4096

// ReadGGUF reads the header of the GGUF file at path. It reads the
// metadata and the tensor table, never the weights.
func ReadGGUF(path string) (GGUF, error) {
	f, err := os.Open(path)
	if err != nil {
		return GGUF{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return GGUF{}, err
	}
	g, err := readGGUF(f, fi.Size())
	if err != nil {
		return GGUF{}, fmt.Errorf("%s: %w", path, err)
	}
	return g, nil
}

type ggufReader struct {
	r   *bufio.Reader
	n   int64 // bytes read
	err error
}

func (r *ggufReader) read(p []byte) {
	if r.err != nil {
		return
	}
	k, err := io.ReadFull(r.r, p)
	r.n += int64(k)
	if err != nil {
		r.err = err
	}
}

func (r *ggufReader) skip(n uint64) {
	if r.err != nil {
		return
	}
	if n > math.MaxInt64/2 {
		r.err = errors.New("a length past the file's end")
		return
	}
	k, err := r.r.Discard(int(n))
	r.n += int64(k)
	if err != nil {
		r.err = err
	}
}

func (r *ggufReader) u32() uint32 {
	var b [4]byte
	r.read(b[:])
	return binary.LittleEndian.Uint32(b[:])
}

func (r *ggufReader) u64() uint64 {
	var b [8]byte
	r.read(b[:])
	return binary.LittleEndian.Uint64(b[:])
}

func (r *ggufReader) str() string {
	n := r.u64()
	if r.err != nil {
		return ""
	}
	if n > 1<<20 {
		r.skip(n)
		return ""
	}
	b := make([]byte, n)
	r.read(b)
	return string(b)
}

// value reads one value of type t, or skips it when keep is false.
func (r *ggufReader) value(t uint32, keep bool) any {
	switch t {
	case ggufUint8, ggufInt8, ggufBool:
		var b [1]byte
		r.read(b[:])
		switch t {
		case ggufBool:
			return b[0] != 0
		case ggufInt8:
			return float64(int8(b[0]))
		}
		return float64(b[0])
	case ggufUint16, ggufInt16:
		var b [2]byte
		r.read(b[:])
		v := binary.LittleEndian.Uint16(b[:])
		if t == ggufInt16 {
			return float64(int16(v))
		}
		return float64(v)
	case ggufUint32:
		return float64(r.u32())
	case ggufInt32:
		return float64(int32(r.u32()))
	case ggufFloat32:
		return float64(math.Float32frombits(r.u32()))
	case ggufUint64:
		return float64(r.u64())
	case ggufInt64:
		return float64(int64(r.u64()))
	case ggufFloat64:
		return math.Float64frombits(r.u64())
	case ggufString:
		if !keep {
			r.skip(r.u64())
			return nil
		}
		return r.str()
	case ggufArray:
		et, n := r.u32(), r.u64()
		keep = keep && n <= ggufMaxArray
		var out []any
		if keep {
			out = make([]any, 0, n)
		}
		if w := fixedWidth(et); w > 0 && !keep {
			r.skip(n * w)
			return nil
		}
		for i := uint64(0); i < n && r.err == nil; i++ {
			v := r.value(et, keep)
			if keep {
				out = append(out, v)
			}
		}
		if keep {
			return out
		}
		return nil
	}
	r.err = fmt.Errorf("unknown metadata type %d", t)
	return nil
}

func fixedWidth(t uint32) uint64 {
	switch t {
	case ggufUint8, ggufInt8, ggufBool:
		return 1
	case ggufUint16, ggufInt16:
		return 2
	case ggufUint32, ggufInt32, ggufFloat32:
		return 4
	case ggufUint64, ggufInt64, ggufFloat64:
		return 8
	}
	return 0
}

func readGGUF(rd io.Reader, size int64) (GGUF, error) {
	r := &ggufReader{r: bufio.NewReaderSize(rd, 1<<20)}
	var magic [4]byte
	r.read(magic[:])
	if r.err != nil || string(magic[:]) != "GGUF" {
		return GGUF{}, errors.New("not a GGUF file")
	}
	if v := r.u32(); v < 2 || v > 3 {
		return GGUF{}, fmt.Errorf("GGUF version %d is not supported", v)
	}
	nTensors, nKV := r.u64(), r.u64()
	if r.err != nil || nTensors > 1<<20 || nKV > 1<<20 {
		return GGUF{}, errors.New("a damaged GGUF header")
	}
	g := GGUF{Meta: map[string]any{}, Size: size}
	for i := uint64(0); i < nKV && r.err == nil; i++ {
		key := r.str()
		t := r.u32()
		// The tokenizer's tables are long and Irie needs only its chat
		// template, which says whether the model takes tools.
		v := r.value(t, !strings.HasPrefix(key, "tokenizer.") || key == "tokenizer.chat_template")
		if v != nil {
			g.Meta[key] = v
		}
	}
	for i := uint64(0); i < nTensors && r.err == nil; i++ {
		name := r.str()
		dims := r.u32()
		if dims > 8 {
			r.err = errors.New("a tensor with more than 8 dimensions")
			break
		}
		r.skip(8 * uint64(dims))
		t := r.u32()
		off := r.u64()
		g.Tensors = append(g.Tensors, Tensor{Name: name, Type: t, Offset: int64(off)})
	}
	if r.err != nil {
		return GGUF{}, fmt.Errorf("reading the header: %w", r.err)
	}
	align := int64(32)
	if a, ok := g.Meta["general.alignment"].(float64); ok && a > 0 {
		align = int64(a)
	}
	data := (r.n + align - 1) / align * align
	// A tensor's bytes run to the next tensor's data.
	order := make([]int, len(g.Tensors))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return int(g.Tensors[a].Offset - g.Tensors[b].Offset) })
	for k, i := range order {
		end := size - data
		if k+1 < len(order) {
			end = g.Tensors[order[k+1]].Offset
		}
		g.Tensors[i].Bytes = max(end-g.Tensors[i].Offset, 0)
	}
	return g, nil
}

// Arch is the model's architecture, such as qwen35.
func (g GGUF) Arch() string { s, _ := g.Meta["general.architecture"].(string); return s }

// Num is a number of the architecture's metadata, such as block_count.
func (g GGUF) Num(key string) int {
	v, _ := g.Meta[g.Arch()+"."+key].(float64)
	return int(v)
}

// Info is the model's context cache shape and trained context, as
// Ollama's /api/show gives them, with tools among its capabilities when its
// chat template takes them.
func (g GGUF) Info() Info {
	info := parseModelInfo(g.Meta)
	info.Capabilities = []string{"completion"}
	if t, _ := g.Meta["tokenizer.chat_template"].(string); strings.Contains(t, "tools") {
		info.Capabilities = append(info.Capabilities, "tools")
	}
	return info
}

// Prism reports what of PrismML's llama.cpp a model needs: its Hadamard
// rotation, which Bonsai models' metadata names (prism.hadamard.*), and
// tensor types only PrismML's build has, such as PTQ1_0 and PQ2_0, which
// it numbers from 142.
func (g GGUF) Prism() (rotation, types bool) {
	for k := range g.Meta {
		if strings.HasPrefix(k, "prism.hadamard.") {
			rotation = true
			break
		}
	}
	for _, t := range g.Tensors {
		if t.Type >= 100 {
			types = true
			break
		}
	}
	return rotation, types
}

// Weights are all the tensors' bytes.
func (g GGUF) Weights() int64 {
	var n int64
	for _, t := range g.Tensors {
		n += t.Bytes
	}
	return n
}

// expertTensor matches the tensors of a mixture of experts' experts, the
// ones llama.cpp's --cpu-moe keeps in system memory.
var expertTensor = regexp.MustCompile(`^blk\.(\d+)\.ffn_(up|down|gate|gate_up)_(ch|)exps`)

var layerTensor = regexp.MustCompile(`^blk\.(\d+)\.`)

// Layers splits the weights by layer: what each repeating layer takes,
// and what its experts take of that, and what the rest takes, such as the
// token embeddings and the output.
func (g GGUF) Layers() (layers, experts []int64, rest int64) {
	n := g.Num("block_count")
	layers, experts = make([]int64, n), make([]int64, n)
	for _, t := range g.Tensors {
		m := layerTensor.FindStringSubmatch(t.Name)
		if m == nil {
			rest += t.Bytes
			continue
		}
		i, err := strconv.Atoi(m[1])
		if err != nil || i >= n {
			rest += t.Bytes
			continue
		}
		layers[i] += t.Bytes
		if expertTensor.MatchString(t.Name) {
			experts[i] += t.Bytes
		}
	}
	return layers, experts, rest
}
