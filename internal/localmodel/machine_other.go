//go:build !darwin && !linux

package localmodel

import "errors"

func readMachine() (Machine, error) {
	return Machine{}, errors.New("reading the machine's memory is not supported on this system")
}
