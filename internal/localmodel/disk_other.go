//go:build !darwin && !linux

package localmodel

import "errors"

func freeDisk(string) (int64, error) {
	return 0, errors.New("reading a disk's free space is not supported on this system")
}
