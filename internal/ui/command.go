package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// chipMin is the size above which a paste becomes a chip.
const chipMin = 8 << 10

// chip is a paste kept out of the textarea, which would cap it and turn its
// tabs into spaces: the bar shows its label, and submitting puts the paste
// back whole in its place.
type chip struct{ label, content string }

func newChip(content string) chip {
	// Lines end in LF, whatever the terminal sent; nothing else changes.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return chip{label: chipLabel(content), content: content}
}

// chipLabel describes a paste: [paste 2.1 MB · 31,204 lines].
func chipLabel(s string) string {
	lines := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		lines++
	}
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	return fmt.Sprintf("[paste %s · %s %s]", size(len(s)), thousands(lines), unit)
}

func size(n int) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// pastes are the chips as the core takes them: each label, and the paste
// it stands for, which the core puts back in its place once it has read the
// words.
func pastes(chips []chip) []ipc.Paste {
	var out []ipc.Paste
	for _, c := range chips {
		out = append(out, ipc.Paste{Label: c.label, Text: c.content})
	}
	return out
}
