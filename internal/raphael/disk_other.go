//go:build !darwin && !linux

package raphael

import "errors"

func readDisk(path string) (total, free int64, err error) { return 0, 0, errors.ErrUnsupported }
