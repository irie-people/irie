package extension

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/version"
)

// Browser is a browser the host can be set up for: its name, whether it
// is built like Chrome or like Firefox, and the folder it finds native
// messaging hosts in.
type Browser struct {
	Name    string
	Firefox bool
	Dir     string
	// Home is the browser's own folder, there once the browser has run.
	Home string
}

// Browsers are the browsers Irie knows on this system, under home.
func Browsers(home string) []Browser {
	type b struct {
		name, home, hosts string
		firefox           bool
	}
	var list []b
	switch runtime.GOOS {
	case "darwin":
		as := filepath.Join(home, "Library", "Application Support")
		list = []b{
			{"Chrome", "Google/Chrome", "Google/Chrome/NativeMessagingHosts", false},
			{"Chrome Beta", "Google/Chrome Beta", "Google/Chrome Beta/NativeMessagingHosts", false},
			{"Chrome Canary", "Google/Chrome Canary", "Google/Chrome Canary/NativeMessagingHosts", false},
			{"Chromium", "Chromium", "Chromium/NativeMessagingHosts", false},
			{"Brave", "BraveSoftware/Brave-Browser", "BraveSoftware/Brave-Browser/NativeMessagingHosts", false},
			{"Edge", "Microsoft Edge", "Microsoft Edge/NativeMessagingHosts", false},
			{"Vivaldi", "Vivaldi", "Vivaldi/NativeMessagingHosts", false},
			{"Arc", "Arc/User Data", "Arc/User Data/NativeMessagingHosts", false},
			{"Firefox", "Firefox", "Mozilla/NativeMessagingHosts", true},
		}
		for i := range list {
			list[i].home = filepath.Join(as, list[i].home)
			list[i].hosts = filepath.Join(as, list[i].hosts)
		}
	default:
		cfg := filepath.Join(home, ".config")
		list = []b{
			{"Chrome", filepath.Join(cfg, "google-chrome"), filepath.Join(cfg, "google-chrome", "NativeMessagingHosts"), false},
			{"Chromium", filepath.Join(cfg, "chromium"), filepath.Join(cfg, "chromium", "NativeMessagingHosts"), false},
			{"Brave", filepath.Join(cfg, "BraveSoftware", "Brave-Browser"), filepath.Join(cfg, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"), false},
			{"Edge", filepath.Join(cfg, "microsoft-edge"), filepath.Join(cfg, "microsoft-edge", "NativeMessagingHosts"), false},
			{"Vivaldi", filepath.Join(cfg, "vivaldi"), filepath.Join(cfg, "vivaldi", "NativeMessagingHosts"), false},
			{"Firefox", filepath.Join(home, ".mozilla", "firefox"), filepath.Join(home, ".mozilla", "native-messaging-hosts"), true},
		}
	}
	out := make([]Browser, 0, len(list))
	for _, x := range list {
		out = append(out, Browser{Name: x.name, Firefox: x.firefox, Dir: x.hosts, Home: x.home})
	}
	return out
}

// files are the extension's files in this build, as they are written:
// manifest.json and brand.js with this build's names.
var files = sync.OnceValue(func() map[string][]byte {
	out := map[string][]byte{}
	err := fs.WalkDir(web, "web", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := web.ReadFile(p)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(p, "web/")] = b
		return nil
	})
	if err != nil {
		panic("extension: reading its files: " + err.Error())
	}
	// The version names the files themselves, so the folder is written
	// again only when they change, not with every build of Irie.
	names := slices.Sorted(func(yield func(string) bool) {
		for n := range out {
			if !yield(n) {
				return
			}
		}
	})
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(out[n]))
		h.Write(out[n])
	}
	fmt.Fprintf(h, "%s\x00%s\x00%s", brand.Name, HostName(), brand.Site)
	sum := hex.EncodeToString(h.Sum(nil))[:8]
	out["manifest.json"] = manifest(out["manifest.json"], version.Base+" "+sum)
	out["brand.js"] = brandJS()
	return out
})

// Version is the version of the extension this build writes: Irie's
// release and a hash of the extension's files.
func Version() string {
	var m struct {
		Version string `json:"version_name"`
	}
	_ = json.Unmarshal(files()["manifest.json"], &m)
	return m.Version
}

// manifest fills in the manifest's names: the product's, its version and
// IDs.
func manifest(src []byte, name string) []byte {
	var m map[string]any
	if err := json.Unmarshal(src, &m); err != nil {
		panic("extension: manifest.json: " + err.Error())
	}
	m["name"] = brand.Name
	m["version"] = version.Base
	m["version_name"] = name
	m["key"] = Key
	m["homepage_url"] = brand.Site
	if a, ok := m["action"].(map[string]any); ok {
		a["default_title"] = brand.Name
	}
	if s, ok := m["sidebar_action"].(map[string]any); ok {
		s["default_title"] = brand.Name
	}
	m["browser_specific_settings"] = map[string]any{"gecko": map[string]any{"id": GeckoID(), "strict_min_version": "128.0"}}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

// brandJS gives the extension's pages this build's names.
func brandJS() []byte {
	v := map[string]string{"name": brand.Name, "slug": brand.Slug, "host": HostName(), "site": brand.Site}
	b, _ := json.Marshal(v)
	return []byte("// Written by " + brand.Slug + " setup extension: this build's names.\nglobalThis.IRIE_BRAND = " + string(b) + ";\n")
}

// Write writes the extension's files into dir, taking out what an older
// build left there. It writes nothing when they are the same.
func Write(dir string) (changed bool, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	want := files()
	for name, b := range want {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, b) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return changed, err
		}
		if err := filetx.WriteFile(p, b, 0o644); err != nil {
			return changed, err
		}
		changed = true
	}
	// Files of an older build's that this one hasn't.
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if _, ok := want[filepath.ToSlash(rel)]; !ok && !strings.HasPrefix(path.Base(rel), ".") {
			changed = true
			return os.Remove(p)
		}
		return nil
	})
	return changed, err
}

// Setup writes the extension's folder, the host's program, which runs bin,
// and the host's file for each browser here, and says which browsers it
// set up.
func Setup(p paths.Paths, home, bin string) ([]string, error) {
	if !filepath.IsAbs(bin) {
		return nil, fmt.Errorf("the host must run %s by its full path, not %q", brand.Slug, bin)
	}
	if _, err := Write(Folder(p)); err != nil {
		return nil, fmt.Errorf("writing the extension's files: %w", err)
	}
	host := HostFile(p)
	script := "#!/bin/sh\n# " + brand.Name + "'s browser extension's way into " + brand.Name + " (docs/proposals/browser-extension.md).\n# " + brand.Slug + " setup extension wrote it; the browser runs it when the extension\n# asks, with the extension's address.\nexec " + shellQuote(bin) + " extension host \"$@\"\n"
	if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
		return nil, err
	}
	if err := filetx.WriteFile(host, []byte(script), 0o755); err != nil {
		return nil, err
	}
	var done []string
	var errs []error
	for _, b := range Browsers(home) {
		if _, err := os.Stat(b.Home); err != nil {
			continue
		}
		if err := writeHost(b, host); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", b.Name, err))
			continue
		}
		done = append(done, b.Name)
	}
	if len(done) == 0 && len(errs) == 0 {
		return nil, errors.New("no browser here that " + brand.Name + " knows: Chrome, Chromium, Brave, Edge, Vivaldi, Arc or Firefox")
	}
	return done, errors.Join(errs...)
}

// writeHost writes the file a browser finds the host by.
func writeHost(b Browser, host string) error {
	m := map[string]any{
		"name":        HostName(),
		"description": brand.Name + " in your browser: the way its extension asks " + brand.Name + " on this computer",
		"path":        host,
		"type":        "stdio",
	}
	if b.Firefox {
		m["allowed_extensions"] = []string{GeckoID()}
	} else {
		m["allowed_origins"] = []string{"chrome-extension://" + ChromeID() + "/"}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return err
	}
	return filetx.WriteFile(filepath.Join(b.Dir, HostName()+".json"), append(data, '\n'), 0o644)
}

// Remove takes out what Setup wrote: the host's files, its program and the
// extension's folder. The browser keeps the extension until it is removed
// there.
func Remove(p paths.Paths, home string) error {
	var errs []error
	for _, b := range Browsers(home) {
		if err := os.Remove(filepath.Join(b.Dir, HostName()+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for _, x := range []string{HostFile(p), seenFile(p)} {
		if err := os.Remove(x); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(Folder(p)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// shellQuote quotes s for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
