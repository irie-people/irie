package localmodel

import (
	"fmt"
	"math"
)

// GiB is a binary gigabyte.
const GiB = 1 << 30

// kvBytes is what one element of the context cache takes, by cache type:
// f16 is 2 bytes; q8_0 and q4_0 store blocks of 32 values in 34 and 18
// bytes.
var kvBytes = map[string]float64{"f16": 2, "q8_0": 34.0 / 32, "q4_0": 18.0 / 32}

// workspace is the room a loaded model takes besides its weights and its
// context cache, for the runtime's working buffers, as a share of the
// weights and a floor.
const (
	workspaceShare = 0.10
	workspaceFloor = 512 << 20
)

// Need is what a model takes in memory at a context.
type Need struct {
	Weights int64
	Cache   int64 // the context cache, for every parallel slot
	Total   int64 // weights, cache and working room
}

// Estimate is what a model of the given size takes in memory at context
// tokens with a cache of type kv, for parallel requests at once. A context
// longer than the model was trained for is cut to that, as Ollama does.
func Estimate(size int64, info Info, context int, kv string, parallel int) Need {
	if info.Context > 0 && context > info.Context {
		context = info.Context
	}
	per, ok := kvBytes[kv]
	if !ok {
		per = kvBytes["f16"]
	}
	var elems float64 // per token, over every layer that keeps a cache
	for _, h := range info.KVHeads {
		elems += float64(h * (info.KeyLen + info.ValueLen))
	}
	n := Need{Weights: size, Cache: int64(math.Ceil(elems * per * float64(context) * float64(max(parallel, 1))))}
	n.Total = n.Weights + n.Cache + max(int64(float64(size)*workspaceShare), workspaceFloor)
	return n
}

// Machine is the machine's memory at one moment.
type Machine struct {
	Total     int64 // physical memory
	Available int64 // what programs can take now without the machine swapping
}

// ReadMachine reads the machine's memory.
func ReadMachine() (Machine, error) { return readMachine() }

// Budget is the most memory local models may take: memory_gib when it is
// set, else two thirds of the machine's, which leaves room for the system,
// the agents and the user's own programs.
func Budget(memoryGiB int, m Machine) int64 {
	if memoryGiB > 0 {
		return int64(memoryGiB) * GiB
	}
	return m.Total / 3 * 2
}

// Reserve is the memory a model load must leave free for everything else:
// 2 GiB, or a tenth of the machine's memory when that is more.
func Reserve(m Machine) int64 { return max(2*GiB, m.Total/10) }

// Bytes renders a size in decimal units, as Ollama shows a model's size:
// 7.9 GB, 640 MB.
func Bytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%d MB", (n+5e5)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%d KB", (n+500)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}
