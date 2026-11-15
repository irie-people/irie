package variorum

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/repo"
)

// Program is the door's program, in Variorum.app's Contents/MacOS.
const Program = "variorum-mcp"

// bundles are Variorum's bundle identifiers: the app's, and its debug
// build's.
var bundles = []string{"io.lduarte.Variorum", "io.lduarte.Variorum.debug"}

// Folders are where Irie looks for Variorum.app: the Applications folders
// a Mac app is dragged into. Tests point them elsewhere.
var Folders = []string{"/Applications", "~/Applications"}

// folders are Folders with ~ for the home folder.
func folders() []string {
	h, _ := os.UserHomeDir()
	var out []string
	for _, f := range Folders {
		out = append(out, filepath.Clean(repo.Expand(f, h)))
	}
	return out
}

// Installed says whether Variorum.app is in an Applications folder.
func Installed() bool {
	for _, f := range folders() {
		if st, err := os.Stat(filepath.Join(f, "Variorum.app")); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// errNotDoor refuses a program that isn't where Variorum keeps its door. It
// never names the program, which comes from what was pasted.
var errNotDoor = errors.New("That setup doesn't name Variorum's door: " + brand.Name + " runs only " + Program + " inside Variorum.app, in /Applications or ~/Applications.")

// Check says whether command is Variorum's door, which is all that Irie
// runs for a setup: an absolute path to a program named variorum-mcp,
// directly in <X>.app/Contents/MacOS, where <X>.app is in an Applications
// folder and its Info.plist names Variorum's bundle. Irie checks when it
// keeps a setup and again before every start, so a pasted setup can't name
// another program.
func Check(command string) error {
	if !filepath.IsAbs(command) || filepath.Clean(command) != command || filepath.Base(command) != Program {
		return errNotDoor
	}
	macOS := filepath.Dir(command)
	contents := filepath.Dir(macOS)
	app := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(app, ".app") {
		return errNotDoor
	}
	if !slices.Contains(folders(), filepath.Dir(app)) {
		return errNotDoor
	}
	if st, err := os.Stat(app); err != nil || !st.IsDir() {
		return fmt.Errorf("Variorum isn't at %s any more: is it still installed?", app)
	}
	id, err := bundleID(filepath.Join(contents, "Info.plist"))
	if err != nil {
		return fmt.Errorf("%s isn't Variorum: %v.", app, err)
	}
	if !slices.Contains(bundles, id) {
		return fmt.Errorf("%s isn't Variorum: its bundle is %q.", app, id)
	}
	fi, err := os.Lstat(command)
	switch {
	case err != nil:
		return fmt.Errorf("%s has no %s, Variorum's door.", app, Program)
	case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0:
		return fmt.Errorf("%s in %s isn't a program %s can run.", Program, app, brand.Name)
	}
	return nil
}

// maxPlist is the most of an Info.plist Irie reads.
const maxPlist = 1 << 20

// bundleID is the CFBundleIdentifier an Info.plist names, in XML or, with
// macOS's plutil, binary.
func bundleID(plist string) (string, error) {
	f, err := os.Open(plist)
	if err != nil {
		return "", errors.New("it has no Info.plist")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPlist))
	if err != nil {
		return "", errors.New("its Info.plist can't be read")
	}
	if bytes.HasPrefix(b, []byte("bplist")) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if b, err = exec.CommandContext(ctx, "/usr/bin/plutil", "-convert", "xml1", "-o", "-", plist).Output(); err != nil {
			return "", errors.New("its Info.plist can't be read")
		}
	}
	id, err := plistBundleID(b)
	if err != nil || id == "" {
		return "", errors.New("its Info.plist names no bundle")
	}
	return id, nil
}

// plistBundleID reads CFBundleIdentifier from the top dict of an XML
// property list, and never from a dict inside it.
func plistBundleID(b []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	depth, top, want := 0, 0, false
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case top == 0 && t.Name.Local == "dict":
				top = depth
			case top > 0 && depth == top+1 && t.Name.Local == "key":
				var k string
				if err := dec.DecodeElement(&k, &t); err != nil {
					return "", err
				}
				depth--
				want = strings.TrimSpace(k) == "CFBundleIdentifier"
			case top > 0 && depth == top+1 && want && t.Name.Local == "string":
				var v string
				if err := dec.DecodeElement(&v, &t); err != nil {
					return "", err
				}
				return strings.TrimSpace(v), nil
			case top > 0 && depth == top+1:
				want = false
			}
		case xml.EndElement:
			if depth--; top > 0 && depth < top {
				return "", nil
			}
		}
	}
}
