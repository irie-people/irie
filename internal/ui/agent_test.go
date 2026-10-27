package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/irie-hq/irie/internal/config"
)

// macMarks are the marks of the six slots on each palette's dark working
// cards, as the Mac app's Theme.agentHex(slot:palette:dark:high:) gives
// them with high false.
var macMarks = map[string][agentSlots]string{
	"rosewater":  {"#899b3b", "#54a43e", "#3fa566", "#3da09e", "#5691e3", "#8265ff"},
	"terracotta": {"#4fa23d", "#3da167", "#3c9d9f", "#578ce5", "#8a69ff", "#ce58e8"},
	"lilac":      {"#b28a43", "#8b983a", "#61a13d", "#3ea44c", "#3da27d", "#419dab"},
	"evergreen":  {"#568ce3", "#7975ff", "#a563ff", "#ca56e3", "#dd54bd", "#c2954a"},
}

// inSlot is a session ID in the slot.
func inSlot(slot int) string {
	for i := 0; ; i++ {
		if id := fmt.Sprintf("s%04d", i); agentSlot(id) == slot {
			return id
		}
	}
}

// TestAgentColorsMatchTheMacApp: a session has the colour its card's mark
// has in the Mac app, from the same slot.
func TestAgentColorsMatchTheMacApp(t *testing.T) {
	// Slots as Theme.agentSlot gives them.
	for id, slot := range map[string]int{"s02docsguide": 5, "s03convoview": 1, "s05bigupm000": 3, "a": 4, "": 1} {
		if got := agentSlot(id); got != slot {
			t.Errorf("slot of %q = %d, want %d", id, got, slot)
		}
	}
	for _, name := range config.Themes {
		want, ok := macMarks[name]
		if !ok {
			t.Errorf("no marks to check for the palette %s", name)
			continue
		}
		for slot := range agentSlots {
			if got := AgentColor(name, inSlot(slot)); got != want[slot] {
				t.Errorf("%s slot %d: %s, the Mac app's is %s", name, slot, got, want[slot])
			}
		}
	}
	if got, want := AgentColor("no-such-palette", inSlot(0)), macMarks["rosewater"][0]; got != want {
		t.Errorf("an unknown palette gives %s, want rosewater's %s", got, want)
	}
}

// TestAgentColorsReadOnTheTerminal: on the Mac app's terminal, every colour
// reads as text at 4.5:1, and a palette's six differ.
func TestAgentColorsReadOnTheTerminal(t *testing.T) {
	ground := terminalGround.luminance()
	for _, name := range config.Themes {
		var seen []string
		for slot := range agentSlots {
			c := AgentColor(name, inSlot(slot))
			var x rgb8
			if _, err := fmt.Sscanf(c, "#%02x%02x%02x", &x.r, &x.g, &x.b); err != nil {
				t.Fatalf("%s slot %d: %q: %v", name, slot, c, err)
			}
			if r := ratio(x.luminance(), ground); r < 4.5 {
				t.Errorf("%s slot %d: %s is %.2f:1 on the terminal", name, slot, c, r)
			}
			if slices.Contains(seen, c) {
				t.Errorf("%s: slot %d has %s, as an earlier slot does", name, slot, c)
			}
			seen = append(seen, c)
		}
	}
}
