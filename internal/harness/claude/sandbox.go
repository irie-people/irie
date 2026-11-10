package claude

// An anonymous research session's sandbox (DESIGN §4.16), as Claude Code's
// own makes it: Seatbelt on macOS, bubblewrap on Linux, around every shell
// command and what it starts. Its programs reach the network only through
// the session's forwarder, on the two local ports the settings name; no
// other port of the machine, no Unix socket, no name looked up on the Mac.
// A command can't be retried outside it. Passed with --settings and with
// allowUnsandboxedCommands false, the sandbox is "admin-required", so a
// repository's settings can't loosen it; the user's own settings and the
// managed ones still could, which Launch refuses (loosened).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/irie-people/irie/internal/harness"
)

type sandboxSettings struct {
	Enabled                      bool                `json:"enabled"`
	FailIfUnavailable            bool                `json:"failIfUnavailable"`
	AllowUnsandboxedCommands     bool                `json:"allowUnsandboxedCommands"`
	AllowAppleEvents             bool                `json:"allowAppleEvents"`
	EnableWeakerNetworkIsolation bool                `json:"enableWeakerNetworkIsolation"`
	EnableWeakerNestedSandbox    bool                `json:"enableWeakerNestedSandbox"`
	Network                      sandboxNetwork      `json:"network"`
	Filesystem                   *sandboxFilesystem  `json:"filesystem,omitempty"`
	Credentials                  *sandboxCredentials `json:"credentials,omitempty"`
}

type sandboxNetwork struct {
	HTTPProxyPort       int  `json:"httpProxyPort"`
	SOCKSProxyPort      int  `json:"socksProxyPort"`
	AllowLocalBinding   bool `json:"allowLocalBinding"`
	AllowAllUnixSockets bool `json:"allowAllUnixSockets"`
}

type sandboxFilesystem struct {
	DenyRead []string `json:"denyRead,omitempty"`
}

type sandboxCredentials struct {
	EnvVars []sandboxEnvVar `json:"envVars,omitempty"`
}

type sandboxEnvVar struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
}

// sandboxFor is the settings' sandbox block for b. Every switch is written
// out, false too, as --settings ranks above the user's own settings.
func sandboxFor(b *harness.Sandbox) *sandboxSettings {
	if b == nil {
		return nil
	}
	s := &sandboxSettings{
		Enabled: true, FailIfUnavailable: true,
		Network: sandboxNetwork{HTTPProxyPort: b.HTTPPort, SOCKSProxyPort: b.SOCKSPort},
	}
	if len(b.DenyRead) > 0 {
		s.Filesystem = &sandboxFilesystem{DenyRead: b.DenyRead}
	}
	if len(b.DenyEnv) > 0 {
		s.Credentials = &sandboxCredentials{}
		for _, name := range b.DenyEnv {
			s.Credentials.EnvVars = append(s.Credentials.EnvVars, sandboxEnvVar{Name: name, Mode: "deny"})
		}
	}
	return s
}

// ManagedSettings is where Claude Code's managed settings live, by GOOS.
var ManagedSettings = map[string]string{
	"darwin": "/Library/Application Support/ClaudeCode/managed-settings.json",
	"linux":  "/etc/claude-code/managed-settings.json",
}

// loosened says which of the settings files that rank beside or above
// --settings would open an anonymous session's sandbox, or nil. Lists
// merge from every file, so the user's own settings could add commands
// run outside it, sockets or services it may reach, or paths it may read;
// managed settings outrank --settings, so any switch there counts too.
func loosened(configDir string) error {
	lists := []string{"excludedCommands", "ignoreViolations", "network.allowUnixSockets", "network.allowMachLookup", "filesystem.allowRead"}
	check := func(path string, managed bool) error {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("can't read %s to check its sandbox settings: %v", path, err)
		}
		var f struct {
			Sandbox map[string]any `json:"sandbox"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("can't read %s to check its sandbox settings: %v", path, err)
		}
		for _, key := range lists {
			if v := dig(f.Sandbox, key); v != nil && !empty(v) {
				return fmt.Errorf("%s sets sandbox.%s, which would let an anonymous session's shell out of its sandbox", path, key)
			}
		}
		if !managed {
			return nil
		}
		for key, safe := range map[string]bool{
			"enabled": true, "failIfUnavailable": true, "allowUnsandboxedCommands": false, "allowAppleEvents": false,
			"enableWeakerNetworkIsolation": false, "enableWeakerNestedSandbox": false, "network.allowLocalBinding": false,
			"network.allowAllUnixSockets": false, "network.allowManagedDomainsOnly": false,
			"network.httpProxyPort": false, "network.socksProxyPort": false,
		} {
			v := dig(f.Sandbox, key)
			if v == nil {
				continue
			}
			if on, ok := v.(bool); !ok || on != safe {
				return fmt.Errorf("%s sets sandbox.%s, which would let an anonymous session's shell out of its sandbox", path, key)
			}
		}
		return nil
	}
	if err := check(filepath.Join(configDir, "settings.json"), false); err != nil {
		return err
	}
	if p := ManagedSettings[runtime.GOOS]; p != "" {
		return check(p, true)
	}
	return nil
}

func dig(m map[string]any, key string) any {
	var v any = m
	for _, k := range strings.Split(key, ".") {
		o, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = o[k]
	}
	return v
}

func empty(v any) bool {
	switch v := v.(type) {
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case string:
		return v == ""
	case bool:
		return !v
	}
	return false
}
