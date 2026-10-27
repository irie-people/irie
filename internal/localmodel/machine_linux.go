package localmodel

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

// readMachine reads MemTotal and MemAvailable from /proc/meminfo.
func readMachine() (Machine, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Machine{}, err
	}
	defer f.Close()
	return parseMeminfo(f)
}

func parseMeminfo(r io.Reader) (Machine, error) {
	var m Machine
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "MemTotal":
			m.Total = n << 10
		case "MemAvailable":
			m.Available = n << 10
		}
	}
	if m.Total == 0 {
		return m, errors.New("/proc/meminfo: no MemTotal")
	}
	return m, sc.Err()
}
