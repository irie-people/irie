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

func TestAnonSection(t *testing.T) {
	d := Default().Anon
	if d.On || d.Route != "mullvad" || !d.Owned || !d.Search || d.SearchPort != 8888 || d.SearchRoute != "mullvad" || !d.Browser || d.TorPort != 9150 || d.Countries == nil {
		t.Errorf("default anon = %+v", d)
	}
	c, err := Load(writeFile(t, "[anon]\non = true\nroute = \"tor\"\ncountries = [\"se\", \"ch\"]\nowned = false\n"))
	if err != nil || !c.Anon.On || c.Anon.Route != "tor" || len(c.Anon.Countries) != 2 || c.Anon.Owned || c.Anon.SearchPort != 8888 {
		t.Errorf("anon = %+v, %v", c.Anon, err)
	}
	if _, err := Load(writeFile(t, "[profiles.tor]\ncommand = \"claude\"\n")); err == nil || !strings.Contains(err.Error(), "anonymous research's word") {
		t.Errorf("a profile named tor: %v", err)
	}
	for body, key := range map[string]string{
		"route = \"vpn\"\n":         "anon.route",
		"search_route = \"none\"\n": "anon.search_route",
		"countries = [\"SE\"]\n":    "anon.countries[0]",
		"countries = [\"swe\"]\n":   "anon.countries[0]",
		"search_port = 80\n":        "anon.search_port",
		"tor_port = 70000\n":        "anon.tor_port",
		"search_port = 9160\n":      "Tor compartments'",
		"tor = \"tor\"\n":           "anon.tor",
	} {
		if _, err := Load(writeFile(t, "[anon]\n"+body)); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("[anon] %s: %v, want an error about %s", strings.TrimSpace(body), err, key)
		}
	}
}
