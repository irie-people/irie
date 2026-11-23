package main

import (
	"strings"
	"testing"
)

// mailFlags are the Swift conditions of a copy that leaves Mail out.
var mailFlags = swiftFlags([]string{"nomail", "netgo"})

func TestSwiftFlagsAreTheTagsInCapitals(t *testing.T) {
	if len(mailFlags) != 1 || !mailFlags["NOMAIL"] {
		t.Errorf("flags %v, want NOMAIL alone", mailFlags)
	}
}

func TestPruneSwiftDeletesAFileLeftWithoutCode(t *testing.T) {
	for _, src := range []string{
		// A plugin's file as the Mac app has them.
		"#if !NOMAIL\nimport SwiftUI\n\nstruct Mail: View {\n    var body: some View { Text(\"#endif\") }\n}\n#endif\n",
		"// Mail.swift: the Mail tab.\n\n#if !NOMAIL\nimport SwiftUI\nstruct Mail {}\n#endif // !NOMAIL\n",
		// Its imports outside the block, which leave no code of their own.
		"import SwiftUI\n\n#if !NOMAIL\nstruct Mail {}\n#endif\n",
		"#if !NOMAIL && !NOVPN\nstruct Mail {}\n#endif\n",
		"#if !NOMAIL\nstruct Mail {}\n#endif\n#if !NOMAIL\nstruct MailViews {}\n#endif\n",
	} {
		out, blocks, err := pruneSwift([]byte(src), mailFlags)
		if err != nil || blocks == 0 || swiftHasCode(out) {
			t.Errorf("%q: %d blocks settled, left %q, %v; want no code left", src, blocks, out, err)
		}
	}
	for _, c := range []struct{ src, want string }{
		{"#if NOMAIL\nstruct MailOff {}\n#endif\n", "struct MailOff {}\n"},
		{"@_exported import Mail\n#if !NOMAIL\nstruct Mail {}\n#endif\n", "@_exported import Mail\n"},
	} {
		out, _, err := pruneSwift([]byte(c.src), mailFlags)
		if err != nil || string(out) != c.want || !swiftHasCode(out) {
			t.Errorf("%q: left %q, %v; want %q, with code", c.src, out, err, c.want)
		}
	}
}

func TestPruneSwiftSettlesBlocks(t *testing.T) {
	src := `import SwiftUI

struct Store {
    var a = 1

    #if !NOMAIL
    var mail = Mail()
    #endif

    var b = 2
    #if NOMAIL
    var off = true
    #else
    var off = false
    #endif
    #if !NOMAIL
    func one() {}
    #elseif !NOVPN
    func two() {}
    #else
    func three() {}
    #endif
    #if !NOVPN
    func vpn() {}
    #elseif NOMAIL
    func four() {}
    #else
    func five() {}
    #endif
    #if canImport(GhosttyKit) // the terminal
    #if !NOMAIL
    let g = 1
    #endif
    let h = 2
    #elseif !NOMAIL
    let i = 3
    #else
    let j = 4
    #endif
    #if !NOMAIL || !NOVPN
    let k = 5
    #endif
    #if NOMAIL
    #if !NOMAIL
    let l = 6
    #endif
    let m = 7
    #endif
}
`
	want := `import SwiftUI

struct Store {
    var a = 1

    var b = 2
    var off = true
    #if !NOVPN
    func two() {}
    #else
    func three() {}
    #endif
    #if !NOVPN
    func vpn() {}
    #else
    func four() {}
    #endif
    #if canImport(GhosttyKit) // the terminal
    let h = 2
    #else
    let j = 4
    #endif
    #if !NOMAIL || !NOVPN
    let k = 5
    #endif
    let m = 7
}
`
	out, blocks, err := pruneSwift([]byte(src), mailFlags)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if blocks != 8 {
		t.Errorf("%d blocks settled, want 8", blocks)
	}
}

func TestPruneSwiftLeavesWhatItCannotSettle(t *testing.T) {
	for _, src := range []string{
		"#if !NOVPN\nlet vpn = 1\n#endif\n",
		"#if os(macOS)\nlet mac = 1\n#elseif canImport(UIKit)\nlet ui = 1\n#endif\n",
		"#if DEBUG && !NOMAIL\nlet debug = 1\n#endif\n",
		// Directives that are not: in comments and strings.
		"/* #if !NOMAIL\n#endif */\nlet s = \"\"\"\n#if !NOMAIL\n\"\"\"\n// #if !NOMAIL\nlet t = \"#if !NOMAIL\"\n",
		"let r = #\"\"\"\n#if !NOMAIL\n\"\"\"#\n",
	} {
		out, blocks, err := pruneSwift([]byte(src), mailFlags)
		if err != nil || blocks != 0 || string(out) != src {
			t.Errorf("%q: %d blocks settled, left %q, %v; want it as it was", src, blocks, out, err)
		}
	}
}

func TestPruneSwiftKeepsOneBlankLineWhereABlockWent(t *testing.T) {
	block := "#if !NOMAIL\nlet m = 1\n#endif\n"
	for _, c := range []struct{ src, want string }{
		{"let a = 1\n\n" + block + "\nlet b = 2\n", "let a = 1\n\nlet b = 2\n"},
		{"let a = 1\n" + block + "\nlet b = 2\n", "let a = 1\n\nlet b = 2\n"},
		{"let a = 1\n\n" + block, "let a = 1\n"},
		{block + "\nlet b = 2\n", "let b = 2\n"},
		{"let a = 1\n\n" + block + block + "\nlet b = 2\n", "let a = 1\n\nlet b = 2\n"},
	} {
		out, _, err := pruneSwift([]byte(c.src), mailFlags)
		if err != nil || string(out) != c.want {
			t.Errorf("%q: got %q, %v; want %q", c.src, out, err, c.want)
		}
	}
}

func TestPruneSwiftRefusesUnbalancedBlocks(t *testing.T) {
	for _, src := range []string{
		"#endif\n",
		"#else\n",
		"#if !NOMAIL\nlet m = 1\n",
		"#if !NOMAIL\n#else\n#elseif !NOVPN\n#endif\n",
	} {
		_, _, err := pruneSwift([]byte(src), mailFlags)
		if err == nil || !strings.Contains(err.Error(), "line ") {
			t.Errorf("%q: error %v, want one with its line", src, err)
		}
	}
}
