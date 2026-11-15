// Package designcheck keeps Irie's copies of its design library's values
// in step with the library, Roots (github.com/irie-people/roots; DESIGN §2.11):
// the files that cannot read it where they run, which mark the library's
// colours in them with `design:` comments, and the site's themes page.
// tools/design.sh runs Check and Sync, and TestCopiesMatchTheLibrary runs
// Check with the other tests, so a copy changed by hand fails them.
package designcheck

import (
	"cmp"
	"slices"

	"github.com/irie-people/roots"
)

// swiftAgents are the agents' numbers in a Swift copy, named as
// Theme.swift names them.
var swiftAgents = []roots.Number{
	{Pattern: `let gap = ([\d.]+), step`, Paths: []string{"agents.needs_gap"}},
	{Pattern: `markChroma = ([\d.]+)`, Paths: []string{"agents.mark.chroma"}},
	{Pattern: `markLightness = \(light: ([\d.]+), dark: ([\d.]+)\)`, Paths: []string{"agents.mark.lightness.light", "agents.mark.lightness.dark"}},
	{Pattern: `inkLightness = \(light: ([\d.]+), dark: ([\d.]+)\)`, Paths: []string{"agents.ink.lightness.light", "agents.ink.lightness.dark"}},
	{Pattern: `washChroma = \(light: ([\d.]+), dark: ([\d.]+)\)`, Paths: []string{"agents.wash.chroma.light", "agents.wash.chroma.dark"}},
	{Pattern: `washLift = \(light: (-?[\d.]+), dark: (-?[\d.]+)\)`, Paths: []string{"agents.wash.lift.light", "agents.wash.lift.dark"}},
	{Pattern: `mix\(s\.card, full, t\)\), muted\) >= ([\d.]+)`, Paths: []string{"agents.wash.contrast"}},
}

// Copies are Irie's files that keep some of the library's values, each
// saying why. Go's own clients read the library itself (internal/ui,
// internal/config).
var Copies = []roots.Copy{
	{
		Path:   "mac/Sources/Irie/Theme.swift",
		Why:    "The Mac app builds its palettes in, so it draws its first frame without reading a file.",
		Blocks: append(roots.IDs(), "terminal"),
		Numbers: append([]roots.Number{
			{Pattern: `agentSlots = (\d+)`, Paths: []string{"agents.slots"}},
			{Pattern: `high \? 4\.5 : ([\d.]+)`, Paths: []string{"agents.mark.contrast"}},
			{Pattern: `high \? 7 : ([\d.]+)`, Paths: []string{"agents.ink.contrast"}},
		}, swiftAgents...),
	},
	{
		Path:   "ios/Irie.swiftpm/Sources/Look.swift",
		Why:    "The iPhone app draws in Evergreen until a core sends the Mac app's palette.",
		Blocks: []string{"evergreen", "evergreen", "terminal"},
		Numbers: append([]roots.Number{
			{Pattern: `static let slots = (\d+)`, Paths: []string{"agents.slots"}},
			{Pattern: `s\.selection\]\.map\(luminance\), ([\d.]+), dark\)`, Paths: []string{"agents.mark.contrast"}},
			{Pattern: `s\.panel\]\.map\(luminance\), ([\d.]+), dark\)`, Paths: []string{"agents.ink.contrast"}},
		}, swiftAgents...),
	},
	{
		Path:   "internal/remote/web/js/look.js",
		Why:    "The phone web app draws in Evergreen until the header brings the Mac app's palette.",
		Blocks: []string{"evergreen"},
		Numbers: []roots.Number{
			{Pattern: `const SLOTS = (\d+);`, Paths: []string{"agents.slots"}},
			{Pattern: `const gap = ([\d.]+), step`, Paths: []string{"agents.needs_gap"}},
			{Pattern: `MARK_C = ([\d.]+)`, Paths: []string{"agents.mark.chroma"}},
			{Pattern: `MARK_L = \[([\d.]+), ([\d.]+)\]`, Paths: []string{"agents.mark.lightness.light", "agents.mark.lightness.dark"}},
			{Pattern: `INK_L = \[([\d.]+), ([\d.]+)\]`, Paths: []string{"agents.ink.lightness.light", "agents.ink.lightness.dark"}},
			{Pattern: `WASH_C = \[([\d.]+), ([\d.]+)\]`, Paths: []string{"agents.wash.chroma.light", "agents.wash.chroma.dark"}},
			{Pattern: `WASH_LIFT = \[(-?[\d.]+), (-?[\d.]+)\]`, Paths: []string{"agents.wash.lift.light", "agents.wash.lift.dark"}},
			{Pattern: `mix\(s\.card, full, t\)\), muted\) >= ([\d.]+)`, Paths: []string{"agents.wash.contrast"}},
			{Pattern: `MARK_L\[dark \? 1 : 0\], grounds, ([\d.]+)`, Paths: []string{"agents.mark.contrast"}},
			{Pattern: `INK_L\[dark \? 1 : 0\], grounds, ([\d.]+)`, Paths: []string{"agents.ink.contrast"}},
		},
	},
	{
		Path:   "internal/remote/web/app.css",
		Why:    "The phone web app's first paint, before look.js sets the palette; .on-dark is the dark set for what sits on its bars.",
		Blocks: []string{"evergreen light", "evergreen dark", "evergreen dark", "terminal"},
	},
	{
		Path:   "internal/winapp/web/app.css",
		Why:    "The Windows app's first paint, before look.js sets the palette.",
		Blocks: []string{"evergreen light", "evergreen dark", "terminal"},
	},
	{
		Path:   "internal/remote/web/setup.html",
		Why:    "The phone's setup page stands alone: it loads before the app and its scripts.",
		Blocks: []string{"evergreen light", "evergreen dark"},
	},
}

// Check reads every copy under root, Irie's checkout, and the site's
// themes page, and says where one is not the library's.
func Check(root string) []roots.Problem {
	probs := append(roots.Check(root, Copies), checkThemesPage(root)...)
	slices.SortStableFunc(probs, func(a, b roots.Problem) int { return cmp.Compare(a.Path, b.Path) })
	return probs
}

// Sync writes the library's values into every copy under root and into
// the themes page's tables. What it cannot write, such as an old colour
// in one of the themes page's pictures, it says.
func Sync(root string) (changed []string, probs []roots.Problem) {
	changed, probs = roots.Sync(root, Copies)
	page, left := syncThemesPage(root)
	if page {
		changed = append(changed, ThemesPage)
	}
	return changed, append(probs, left...)
}
