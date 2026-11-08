package config

import (
	"strings"
	"testing"
	"time"
)

func TestBrowserSection(t *testing.T) {
	d := Default().Browser
	if d.Connector != "off" || !d.Headless || d.Idle != 10*time.Minute || !strings.Contains(d.Search, "%s") {
		t.Errorf("default browser = %+v, want off, headless, 10 minutes idle and a search", d)
	}
	c, err := Load(writeFile(t, "[browser]\nconnector = \"on\"\npath = \"~/bin/firefox\"\nidle = \"5m\"\nproxy = \"socks5://10.64.0.1:1080\"\n"))
	if err != nil || c.Browser.Connector != "on" || c.Browser.Idle != 5*time.Minute || c.Browser.Proxy != "socks5://10.64.0.1:1080" || !c.Browser.Headless {
		t.Errorf("browser = %+v, %v", c.Browser, err)
	}
	for body, key := range map[string]string{
		"connector = \"auto\"\n":                      "browser.connector",
		"path = \"mullvadbrowser\"\n":                 "browser.path",
		"path = \"/Applications/Some Browser.app\"\n": "names an app",
		"proxy = \"http://10.64.0.1:1080\"\n":         "browser.proxy",
		"search = \"https://duckduckgo.com/html/\"\n": "browser.search",
		"idle = \"-1m\"\n":                            "browser.idle",
	} {
		if _, err := Load(writeFile(t, "[browser]\n"+body)); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("[browser] %s: %v, want an error about %s", strings.TrimSpace(body), err, key)
		}
	}
}
