package testutil

import (
	"fmt"
	"net"
	"testing"
)

func TestFreePortsAreFreeInARow(t *testing.T) {
	first := FreePorts(t, 3)
	for i := range 3 {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", first+i))
		if err != nil {
			t.Fatalf("port %d of the 3 from %d: %v", i, first, err)
		}
		l.Close()
	}
}
