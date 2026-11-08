package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// App is the Mac app's own figures: how busy its main thread was in a
// sample, and its memory footprint.
type App struct {
	PID int `json:"pid"`
	// Samples is how many times sample caught the main thread, and Busy
	// the share of them in which it was not waiting for its next event.
	Samples int     `json:"samples,omitempty"`
	Busy    float64 `json:"busy,omitempty"`
	// Footprint and its peak, in bytes, as footprint reports them.
	Footprint int64  `json:"footprint,omitempty"`
	Peak      int64  `json:"peak,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

// sampleApp runs sample(1) on pid for d, every 10 ms, and reads how busy the
// main thread was. sample suspends the app for a moment at each look, so
// this runs after the window whose CPU is measured.
func sampleApp(pid int, d time.Duration, dir string) (samples int, busy float64, err error) {
	file := filepath.Join(dir, fmt.Sprintf("sample-%d.txt", pid))
	defer os.Remove(file)
	secs := strconv.Itoa(max(int(d.Round(time.Second)/time.Second), 1))
	if out, err := exec.Command("sample", strconv.Itoa(pid), secs, "10", "-file", file).CombinedOutput(); err != nil {
		return 0, 0, fmt.Errorf("sample: %v: %s", err, firstLine(out))
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return 0, 0, err
	}
	samples, idle := mainThread(b)
	if samples == 0 {
		return 0, 0, fmt.Errorf("sample found no main thread")
	}
	return samples, float64(samples-idle) / float64(samples), nil
}

// sampleLine is one line of a sample call graph: its count and its frame,
// after the tree's marks.
var sampleLine = regexp.MustCompile(`^\s*[+!:| ]*\s*(\d+)\s+(.*)$`)

// mainThread reads a sample report's call graph: how many samples caught
// the main thread, and how many of them found it waiting in its run loop
// for the next event (__CFRunLoopServiceMachPort), which is idle.
func mainThread(report []byte) (samples, idle int) {
	sc := bufio.NewScanner(bytes.NewReader(report))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	in := false
	for sc.Scan() {
		line := sc.Text()
		root := strings.HasPrefix(line, "    ") && len(line) > 4 && line[4] >= '0' && line[4] <= '9'
		if root {
			if in {
				break // the next thread
			}
			if strings.Contains(line, "com.apple.main-thread") {
				m := sampleLine.FindStringSubmatch(line)
				if m != nil {
					samples, _ = strconv.Atoi(m[1])
					in = true
				}
			}
			continue
		}
		if !in {
			continue
		}
		if m := sampleLine.FindStringSubmatch(line); m != nil && strings.HasPrefix(m[2], "__CFRunLoopServiceMachPort ") {
			n, _ := strconv.Atoi(m[1])
			idle += n
		}
	}
	return samples, min(idle, samples)
}

// footprintLine is footprint(1)'s summary, and its peak, in bytes.
var (
	footprintLine = regexp.MustCompile(`Footprint:\s+(\d+) B`)
	peakLine      = regexp.MustCompile(`phys_footprint_peak:\s+(\d+) B`)
)

// footprint reads pid's memory footprint and its peak.
func footprint(pid int) (now, peak int64, err error) {
	out, err := exec.Command("footprint", "-f", "bytes", "--noCategories", strconv.Itoa(pid)).CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("footprint: %v: %s", err, firstLine(out))
	}
	return parseFootprint(out)
}

func parseFootprint(out []byte) (now, peak int64, err error) {
	m := footprintLine.FindSubmatch(out)
	if m == nil {
		return 0, 0, fmt.Errorf("footprint printed no footprint")
	}
	now, _ = strconv.ParseInt(string(m[1]), 10, 64)
	if p := peakLine.FindSubmatch(out); p != nil {
		peak, _ = strconv.ParseInt(string(p[1]), 10, 64)
	}
	return now, peak, nil
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return s
}
