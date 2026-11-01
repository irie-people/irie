package core

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// freePorts finds a port whose port below is free too: the gateway's
// HTTPS port and its setup page's.
func freePorts(t *testing.T) int {
	t.Helper()
	for range 50 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if below, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port-1)); err == nil {
			below.Close()
			return port
		}
	}
	t.Fatal("no two free ports in a row")
	return 0
}

func TestTheCoreKeepsTheGatewayWhileRemoteIsOn(t *testing.T) {
	c := setup(t)
	gateways := func() int {
		n := 0
		for _, p := range c.panes() {
			if p["@irie_role"] == "remote" {
				n++
			}
		}
		return n
	}
	if n := gateways(); n != 0 {
		t.Fatalf("%d gateways with remote.on unset", n)
	}

	port := freePorts(t)
	on := ipc.ConfigArgs{Set: []ipc.ConfigSet{
		{Key: "remote.port", Value: json.RawMessage(strconv.Itoa(port))},
		{Key: "remote.name", Value: json.RawMessage(`"Studio"`)},
		{Key: "remote.on", Value: json.RawMessage("true")},
	}}
	if err := c.call(ipc.OpConfig, on, nil); err != nil {
		t.Fatal(err)
	}
	// Its own authority signs it; only that it answers matters here.
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	info := fmt.Sprintf("https://127.0.0.1:%d/api/info", port)
	testutil.Eventually(t, 15*time.Second, "the gateway never answered", func() bool {
		res, err := client.Get(info)
		if err != nil {
			return false
		}
		defer res.Body.Close()
		var i struct{ Name string }
		return json.NewDecoder(res.Body).Decode(&i) == nil && i.Name == "Studio"
	})
	if n := gateways(); n != 1 {
		t.Errorf("%d gateways, want 1", n)
	}

	off := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "remote.on", Value: json.RawMessage("false")}}}
	if err := c.call(ipc.OpConfig, off, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the gateway kept running with remote.on off", func() bool {
		_, err := client.Get(info)
		return gateways() == 0 && err != nil
	})
}
