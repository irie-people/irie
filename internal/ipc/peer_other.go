//go:build !darwin && !linux

package ipc

import "net"

// peerPID can't tell a client's process here.
func peerPID(net.Conn) int { return 0 }
