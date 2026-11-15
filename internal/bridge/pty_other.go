//go:build !linux && !darwin

package bridge

import (
	"os"
	"syscall"
)

func openPTY() (master, slave *os.File, err error) { return nil, nil, errNoPTY }

func setSize(master *os.File, cols, rows int) error { return errNoPTY }

func ttyAttr() *syscall.SysProcAttr { return nil }

func hangUp(p *os.Process) { p.Kill() }
