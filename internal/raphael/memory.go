package raphael

import (
	"fmt"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/sysload"
)

// Memory is the machine's memory and swap, as the system says.
type Memory struct {
	Total     int64 // the RAM
	SwapTotal int64
	SwapUsed  int64
	Pressure  int // the kernel's memory pressure, as sysload reads it: 1 normal, 2 warn, 4 critical; 0 unknown
}

// ReadMemory reads the machine's memory and swap; what the system does not
// say is 0.
func ReadMemory() Memory {
	m := readMemory()
	if l, err := sysload.Read(); err == nil {
		m.Pressure = l.Pressure
	}
	return m
}

// The thresholds for memory: swap counts as heavy from swapHeavy of the
// RAM, and a program takes much of it from hogShare.
const (
	swapHeavy = 0.25
	hogShare  = 0.10
)

// hog is the processes of one program, together.
type hog struct {
	name  string
	count int
	rss   int64
}

// memoryFinding says whether the Mac is short of memory, and what takes it.
func memoryFinding(m Memory, ps []Proc, o Owner) Finding {
	f := Finding{ID: "memory", Area: AreaMemory, Level: OK}
	if m.Total <= 0 {
		return Finding{}
	}
	heavy := m.SwapUsed > 0 && float64(m.SwapUsed) >= swapHeavy*float64(m.Total)
	hogs := hogsOf(ps, o)
	switch {
	case m.Pressure >= sysload.PressureCritical:
		f.Level, f.Text = Warn, "Your Mac is critically short of memory, so everything slows down"
	case m.Pressure >= sysload.PressureWarn:
		f.Level, f.Text = Warn, "Your Mac is short of memory, so everything slows down"
	case heavy:
		f.Level, f.Text = Note, "Your Mac has been short of memory"
	default:
		f.Text = fmt.Sprintf("Memory: %s of RAM, no pressure", MemSize(m.Total))
		if m.SwapUsed > 0 {
			f.Text += fmt.Sprintf(", %s of swap in use", MemSize(m.SwapUsed))
		}
		f.Text += "."
		return f
	}
	f.Text += fmt.Sprintf(": %s of swap is in use, with %s of RAM.", MemSize(m.SwapUsed), MemSize(m.Total))
	if len(hogs) > 0 {
		var what []string
		for _, h := range hogs[:min(len(hogs), 4)] {
			if h.count > 1 {
				what = append(what, fmt.Sprintf("%s ×%d (%s)", h.name, h.count, MemSize(h.rss)))
			} else {
				what = append(what, fmt.Sprintf("%s (%s)", h.name, MemSize(h.rss)))
			}
		}
		f.Evidence = []string{"Taking the most: " + strings.Join(what, ", ") + "."}
	}
	f.Advice = "Quit the apps you don't need now, and run fewer sessions at once. Gabriel's boost frees the memory of idle local models."
	return f
}

// hogsOf adds up what each program's processes take, and returns the
// programs that take the most, biggest first: those with hogShare of the
// RAM or more would show anyway, so it returns the top few. Irie's
// sessions count as one, by their agent's program.
func hogsOf(ps []Proc, o Owner) []hog {
	by := map[string]*hog{}
	for _, p := range ps {
		name := progName(p.Path)
		if app := appOf(p.Path); app != "" {
			name = appName(app)
		}
		if o.Session[p.PID] != "" {
			name = "Irie's sessions"
		}
		h := by[name]
		if h == nil {
			h = &hog{name: name}
			by[name] = h
		}
		h.count++
		h.rss += p.RSS
	}
	var out []hog
	for _, h := range by {
		if h.rss >= 200<<20 {
			out = append(out, *h)
		}
	}
	slices.SortFunc(out, func(a, b hog) int {
		switch {
		case a.rss > b.rss:
			return -1
		case a.rss < b.rss:
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	return out
}

// bigHogs are the single programs that take hogShare of the RAM or more,
// which deserve a line even when memory is not short.
func bigHogs(m Memory, ps []Proc, o Owner) []Finding {
	if m.Total <= 0 {
		return nil
	}
	var fs []Finding
	for _, h := range hogsOf(ps, o) {
		if float64(h.rss) < 2*hogShare*float64(m.Total) || h.name == "Irie's sessions" {
			continue
		}
		fs = append(fs, Finding{
			ID: "hog:" + h.name, Area: AreaMemory, Level: Note, Size: h.rss,
			Text:   fmt.Sprintf("%s takes %s of memory, %.0f%% of your RAM.", h.name, MemSize(h.rss), 100*float64(h.rss)/float64(m.Total)),
			Advice: "If you don't need it now, quitting it gives that memory back.",
		})
	}
	return fs
}
