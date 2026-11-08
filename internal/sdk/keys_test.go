package sdk

import (
	"reflect"
	"testing"
)

func feedAll(chunks ...string) []key {
	var r keyReader
	var keys []key
	for _, c := range chunks {
		keys = append(keys, r.feed([]byte(c))...)
	}
	return keys
}

func TestKeys(t *testing.T) {
	r := func(s string) key { return key{kind: keyRune, text: s} }
	for name, c := range map[string]struct {
		chunks []string
		want   []key
	}{
		"typing":            {[]string{"hi\r"}, []key{r("h"), r("i"), {kind: keyEnter}}},
		"CR LF is one":      {[]string{"a\r\n"}, []key{r("a"), {kind: keyEnter}}},
		"paste and Enter":   {[]string{"\x1b[200~one\ntwo\x1b[201~", "\r"}, []key{{kind: keyPaste, text: "one\ntwo"}, {kind: keyEnter}}},
		"a paste in pieces": {[]string{"\x1b[20", "0~ab", "c\x1b[20", "1~"}, []key{{kind: keyPaste, text: "abc"}}},
		"a paste keeps Esc": {[]string{"\x1b[200~a\x1bb\x1b[201~"}, []key{{kind: keyPaste, text: "a\x1bb"}}},
		"lone Escape":       {[]string{"\x1b"}, []key{{kind: keyEscape}}},
		"Escape twice":      {[]string{"\x1b\x1b"}, []key{{kind: keyEscape}, {kind: keyEscape}}},
		"arrows":            {[]string{"\x1b[A\x1bOB\x1b[C\x1b[1;5D"}, []key{{kind: keyUp}, {kind: keyDown}}},
		"control keys":      {[]string{"\x03\x04\x7f\x08\x15\x17\x0c\x1a"}, []key{{kind: keyInterrupt}, {kind: keyEnd}, {kind: keyBackspace}, {kind: keyBackspace}, {kind: keyClearLine}, {kind: keyDeleteWord}, {kind: keyRedraw}}},
		"UTF-8 in pieces":   {[]string{"\xc3", "\xa9!"}, []key{r("é"), r("!")}},
		"Alt and a key":     {[]string{"\x1bx" + "y"}, []key{r("y")}},
	} {
		if got := feedAll(c.chunks...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: keys = %+v, want %+v", name, got, c.want)
		}
	}
}
