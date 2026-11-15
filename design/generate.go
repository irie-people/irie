package design

// The files the library makes whole from its values, for pages that draw
// with them: design/web/tokens.css, every palette as CSS variables, and
// design/web/tokens.js, the palettes, roles and numbers for a page's
// scripts. tools/design.sh writes them; Check says when one is old.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type genFile struct {
	path string
	body []byte
}

func generated(string) []genFile {
	return []genFile{
		{"design/web/tokens.css", tokensCSS()},
		{"design/web/tokens.js", tokensJS()},
	}
}

const made = "Made by tools/design.sh from design/palettes and design/tokens.json: change those, not this."

// cssName is a role's CSS variable: needs_tint is --needs-tint.
func cssName(key string) string { return "--" + strings.ReplaceAll(key, "_", "-") }

// tokensCSS gives each palette's colours as CSS variables. A page picks a
// palette with data-palette, and a mode with data-mode on the same
// element; without data-mode the mode follows the system's, and without
// data-palette the page is in the default palette.
func tokensCSS() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "/* Irie's palettes as CSS variables. %s\n", made)
	b.WriteString("   data-palette on an element picks a palette for it and what is inside it, and\n")
	b.WriteString("   data-mode=\"light\" or \"dark\" on the same element picks its mode, which\n")
	b.WriteString("   otherwise follows the system's. */\n")
	set := func(sel string, s Set, dark bool) {
		fmt.Fprintf(&b, "%s {\n", sel)
		for _, r := range roles {
			fmt.Fprintf(&b, "  %s: %s;\n", cssName(r.Key), s[r.Key])
		}
		if dark {
			b.WriteString("  color-scheme: dark;\n")
		} else {
			b.WriteString("  color-scheme: light;\n")
		}
		b.WriteString("}\n")
	}
	t := TheTerminal()
	fmt.Fprintf(&b, "\n:root {\n  --term-ground: %s;\n  --term-ink: %s;\n}\n", t.Ground, t.Ink)
	for _, p := range lib.palettes {
		light := fmt.Sprintf("[data-palette=%q]", p.ID)
		dark := light + `[data-mode="dark"]`
		system := light + `:not([data-mode="light"])`
		if p.ID == Default() {
			light = ":root, " + light
			dark = `:root[data-mode="dark"], ` + dark
			system = `:root:not([data-mode="light"]), ` + system
		}
		fmt.Fprintf(&b, "\n/* %s */\n", p.Name)
		set(light, p.Light, false)
		set(dark, p.Dark, true)
		fmt.Fprintf(&b, "@media (prefers-color-scheme: dark) {\n")
		var in bytes.Buffer
		in.WriteString(system + " {\n")
		for _, r := range roles {
			fmt.Fprintf(&in, "  %s: %s;\n", cssName(r.Key), p.Dark[r.Key])
		}
		in.WriteString("  color-scheme: dark;\n}\n")
		for _, l := range strings.SplitAfter(strings.TrimSuffix(in.String(), "\n"), "\n") {
			b.WriteString("  " + l)
		}
		b.WriteString("\n}\n")
	}
	return b.Bytes()
}

// tokensJS gives the library to a page's scripts as one global, IRIE_DESIGN,
// so a page opened from a file can read it (a module could not).
func tokensJS() []byte {
	type role struct {
		Key   string `json:"key"`
		Title string `json:"title"`
		Group Group  `json:"group"`
		Use   Use    `json:"use"`
	}
	type palette struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Light Set    `json:"light"`
		Dark  Set    `json:"dark"`
	}
	out := struct {
		Roles    []role    `json:"roles"`
		Palettes []palette `json:"palettes"`
		Default  string    `json:"default"`
		Contrast struct {
			Text float64 `json:"text"`
			Mark float64 `json:"mark"`
		} `json:"contrast"`
		Terminal Terminal `json:"terminal"`
		Agents   Agents   `json:"agents"`
	}{Default: Default(), Terminal: TheTerminal(), Agents: TheAgents()}
	out.Contrast.Text, out.Contrast.Mark = TextContrast(), MarkContrast()
	for _, r := range roles {
		out.Roles = append(out.Roles, role{r.Key, r.Title, r.Group, r.Use})
	}
	for _, p := range lib.palettes {
		out.Palettes = append(out.Palettes, palette{p.ID, p.Name, p.Light, p.Dark})
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err) // only strings and numbers
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Irie's design library for a page's scripts. %s\n", made)
	b.WriteString("window.IRIE_DESIGN = ")
	b.Write(body)
	b.WriteString(";\n")
	return b.Bytes()
}
