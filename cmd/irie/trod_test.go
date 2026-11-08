package main

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func TestTheTrodReportSaysTheDayLine(t *testing.T) {
	line, week := 20.0, 15.0
	r := ipc.TrodResult{On: true, Start: "23:00", End: "07:00", From: time.Now(), MaxPlanUse: 70, Line: &line, Week: &week}
	var b strings.Builder
	if err := printTrod(&b, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	want := "Day line: today the week may reach 20%; it is at 15%. Each day of the plan's week adds 10%, up to 70%.\n"
	if !strings.Contains(b.String(), want) {
		t.Errorf("report:\n%s\nwants %q", b.String(), want)
	}
	// Without trod.pace there is no line.
	r.Line = nil
	b.Reset()
	if err := printTrod(&b, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "Day line") {
		t.Errorf("report without pace:\n%s", b.String())
	}
}
