package proc

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// holders reads every process's working directory, program, open files and
// mapped files from /proc (proc(5)): the links cwd, exe and fd/*, and the
// paths in maps. What another user's processes hold the system hides,
// unless the core runs as root.
func holders() ([]Holder, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var hs []Holder
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := "/proc/" + e.Name()
		comm, _ := os.ReadFile(dir + "/comm")
		name := strings.TrimSpace(string(comm))
		add := func(what, path string) {
			if strings.HasPrefix(path, "/") {
				hs = append(hs, Holder{PID: pid, Name: name, What: what, Path: strings.TrimSuffix(path, " (deleted)")})
			}
		}
		if p, err := os.Readlink(dir + "/cwd"); err == nil {
			add("cwd", p)
		}
		if p, err := os.Readlink(dir + "/exe"); err == nil {
			add("txt", p)
		}
		if fds, err := os.ReadDir(dir + "/fd"); err == nil {
			for _, fd := range fds {
				if p, err := os.Readlink(dir + "/fd/" + fd.Name()); err == nil {
					add(fd.Name(), p)
				}
			}
		}
		if f, err := os.Open(dir + "/maps"); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				// address perms offset dev inode path
				if fields := strings.Fields(sc.Text()); len(fields) >= 6 {
					add("mem", strings.Join(fields[5:], " "))
				}
			}
			f.Close()
		}
	}
	return hs, nil
}
