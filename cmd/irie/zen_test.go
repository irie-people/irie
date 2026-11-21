package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/zen"
)

func TestZenSetReadsWhatPeopleType(t *testing.T) {
	ch, err := parseZenSet([]string{"layout=Edge to Edge", "text=16pt", "breaks=off", "night=no", "full-screen=on", "columns=80"})
	if err != nil {
		t.Fatal(err)
	}
	p := zen.Apply(zen.Default(), ch)
	want := ipc.ZenProfile{Layout: "edge", Columns: 80, TextSize: 16, FullScreen: true, Gentle: true, Breaks: 0, Night: false}
	if p != want {
		t.Errorf("got %+v", p)
	}
	for _, bad := range [][]string{nil, {"mood=happy"}, {"breaks"}, {"gentle=maybe"}, {"columns=wide"}} {
		if _, err := parseZenSet(bad); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
}

func TestZenSaysYourZenInPlainWords(t *testing.T) {
	var b bytes.Buffer
	p := zen.Default()
	p.Breaks = 50
	printZen(&b, ipc.Zen{Profile: p, On: true, Offer: &ipc.ZenOffer{Text: "Got a minute?"}})
	out := b.String()
	for _, want := range []string{"Centred, 100 columns", "every 50 minutes", "hasn't changed yet", "Zen is on.", "Zen offers: Got a minute?"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	b.Reset()
	printZenLog(&b, nil)
	if !strings.Contains(b.String(), "no versions") {
		t.Error(b.String())
	}
}
