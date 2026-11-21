package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParseTask turns what was typed into spawn args (DESIGN §4.2), as Read
// does with a View that names no profiles. Only the first line is read for
// words: @name or @path runs the task in a new worktree of that git repo,
// or in place in a directory git does not know; @name! runs it in the
// repo's own checkout; +profile picks the profile; +explain or +teach picks
// a tutor mode (docs/proposals/learning.md); +anon or +tor runs it as
// anonymous research (DESIGN §4.16). The rest of the input is the task,
// verbatim. home is what ~ means.
func ParseTask(input, home string) (SpawnArgs, error) { return View{Home: home}.parseTask(input) }

// parseTask is ParseTask, with the + words read against v.Profiles.
//
// A + word picks only when it names something Irie runs, and only where it
// stands apart from the task's own words: at the start of the first line
// or at its end (DESIGN §4.2, "Plus words"). Any other + is the task's,
// kept as typed: a lone +, as in "fn+x + fn+l"; one before a digit or a
// sign, as in +1, ++ or +/-; one that names nothing; and one among the
// task's words, which talks about a profile rather than picks it, as in
// "why does +dory stall". So is one at the end of a line that named one of
// its kind before it: "compare +haiku and +sonnet" picks neither. +anon and
// +tor pick wherever they stand, so that research meant to be anonymous
// never leaves by the machine's own way out.
func (v View) parseTask(input string) (SpawnArgs, error) {
	var a SpawnArgs
	first, rest, multi := strings.Cut(input, "\n")
	if strings.HasPrefix(strings.TrimSpace(first), ":") {
		cmd, _, _ := strings.Cut(strings.TrimSpace(first), " ")
		return a, fmt.Errorf("unknown command %s", cmd)
	}

	fields := strings.Fields(first)
	// The task's own words lie from lo to hi: every word but the @ words
	// and the + words that name something. Without one, every word leads.
	plus := make([]plusWord, len(fields))
	lo, hi := len(fields), len(fields)
	for i, w := range fields {
		if w[0] == '+' {
			plus[i] = v.plus(w)
		}
		if !isTarget(w) && plus[i].kind == plusText {
			lo, hi = min(lo, i), i+1
		}
	}
	var words []string
	tokens := false
	// named holds the kinds of + word the line named before its end: at
	// its start, or among the task's words.
	named := map[plusKind]bool{}
	for i, w := range fields {
		p, apart := plus[i], i < lo || i >= hi
		switch {
		case isTarget(w):
			// An @ word picks wherever it stands.
		case p.kind == plusText:
			words = append(words, w)
			continue
		case p.kind == plusSlip && (apart || p.anon):
			return a, fmt.Errorf("did you mean %s? Nothing is called +%s", p.word, plusName(w))
		case p.kind == plusSlip:
			words = append(words, w)
			continue
		case p.kind == plusAnon:
			// +anon and +tor pick wherever they stand.
		case !apart, i >= hi && named[p.kind]:
			// The task talks about a profile or a tutor mode.
			named[p.kind] = true
			words = append(words, w)
			continue
		case i < lo:
			named[p.kind] = true
		}
		tokens = true
		if w[0] == '+' {
			w = p.word
		}
		if err := applyToken(&a, w, v.Home); err != nil {
			return a, err
		}
	}

	// Without a word the first line is kept as typed, inner spacing and all.
	line := strings.TrimSpace(first)
	if tokens {
		line = strings.Join(words, " ")
	}
	switch {
	case !multi:
		a.Task = line
	case line == "":
		a.Task = rest
	default:
		a.Task = line + "\n" + rest
	}
	if strings.TrimSpace(a.Task) == "" {
		if tokens {
			return a, errors.New("empty task: type what you want done after the @ or + words")
		}
		return a, errors.New("empty task: type what you want done")
	}
	return a, nil
}

// What a + word names.
type plusKind int

const (
	plusText    plusKind = iota // nothing Irie runs: the word is the task's
	plusProfile                 // a profile, +auto or a local model
	plusTutor                   // +explain or +teach
	plusAnon                    // +anon or +tor, with a compartment's name or not
	plusSlip                    // nothing, but one slip from a name, as +hiaku is from +haiku
)

// A plusWord is what a + word names: its kind, and the word that picks it
// as the core knows it, such as +haiku for +Haiku, or, for a slip, the word
// it most likely meant.
type plusWord struct {
	kind plusKind
	word string
	anon bool // a slip from +anon or +tor
}

// plus reads w, a word that starts with +.
func (v View) plus(w string) plusWord {
	name := plusName(w)
	if name == "" {
		return plusWord{}
	}
	// +anon:, its compartment's name left out, is refused, not taken for
	// +anon: the colon was meant to be followed by a name.
	if stem, _, _ := strings.Cut(strings.ToLower(name), ":"); stem == AnonWord || stem == TorWord {
		return plusWord{kind: plusAnon, word: "+" + stem + name[len(stem):]}
	}
	// Any other word may end in a colon, as in "+haiku: fix it".
	name = strings.TrimSuffix(name, ":")
	lower := strings.ToLower(name)
	switch {
	case lower == TutorExplain || lower == TutorTeach:
		return plusWord{kind: plusTutor, word: "+" + lower}
	case v.Profiles == nil:
		// Not knowing the profiles, it takes the name for one's, and the
		// core says when it has none of that name.
		return plusWord{kind: plusProfile, word: "+" + name}
	case slices.Contains(v.Profiles, name):
		return plusWord{kind: plusProfile, word: "+" + name}
	}
	for _, p := range v.Profiles {
		prefix, takesModel := strings.CutSuffix(p, ":")
		switch {
		case !takesModel && strings.EqualFold(p, name):
			return plusWord{kind: plusProfile, word: "+" + p}
		case takesModel && len(name) > len(p) && strings.EqualFold(name[:len(p)], p):
			// A prefix such as ollama: takes a model's name after it.
			return plusWord{kind: plusProfile, word: "+" + p + name[len(p):]}
		case takesModel && strings.EqualFold(name, prefix):
			// Its model left out, the core says what it takes.
			return plusWord{kind: plusProfile, word: "+" + name}
		}
	}
	// One slip of the fingers from a name, it is far more likely that name
	// mistyped than a word of the task's: +hiaku, +sonet, +aonn.
	stem, model, colon := strings.Cut(name, ":")
	for _, c := range append([]string{AnonWord, TorWord, TutorExplain, TutorTeach}, v.Profiles...) {
		anon := c == AnonWord || c == TorWord
		prefix, takesModel := strings.CutSuffix(c, ":")
		switch {
		case colon && (takesModel || anon) && slip(strings.ToLower(stem), strings.ToLower(prefix)):
			return plusWord{kind: plusSlip, word: "+" + prefix + ":" + model, anon: anon}
		case !colon && !takesModel && slip(lower, strings.ToLower(c)):
			return plusWord{kind: plusSlip, word: "+" + c, anon: anon}
		}
	}
	return plusWord{}
}

// plusName is the name in a + word, without its + and the marks that end a
// sentence after it, or "" when the word names nothing: a lone +, or one
// before a digit or a sign, as in +1, ++ or +/-.
func plusName(w string) string {
	name, ok := strings.CutPrefix(w, "+")
	if !ok {
		return ""
	}
	name = strings.TrimRight(name, ".,;!?…")
	if r, _ := utf8.DecodeRuneInString(name); !unicode.IsLetter(r) {
		return ""
	}
	return name
}

// slip reports whether a and b differ by one slip of the fingers: a letter
// added, left out or changed, or two side by side swapped. The shorter has
// four letters or more, or three for tor, the shortest of Irie's words:
// the shorter a name, the more words lie one slip from it.
func slip(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if len(x) > len(y) {
		x, y = y, x
	}
	if len(x) < 4 && (len(x) < 3 || a != TorWord && b != TorWord) {
		return false
	}
	i := 0
	for i < len(x) && x[i] == y[i] {
		i++
	}
	switch len(y) - len(x) {
	case 0:
		return i < len(x) && (string(x[i+1:]) == string(y[i+1:]) ||
			i+1 < len(x) && x[i] == y[i+1] && x[i+1] == y[i] && string(x[i+2:]) == string(y[i+2:]))
	case 1:
		return string(x[i:]) == string(y[i+1:])
	}
	return false
}

// SpawnTokens applies the @ and + words among the first of words, as `irie
// ctl spawn` takes them, and returns the words that follow.
func SpawnTokens(a *SpawnArgs, words []string, home string) ([]string, error) {
	for i, w := range words {
		if !isToken(w) {
			return words[i:], nil
		}
		// One quoted word that starts with tokens, as in "+anon:acme find
		// who runs it": its leading words are tokens too. Taken as the
		// task, a +anon in it would be lost, and the task would run
		// outside any compartment, from the Mac's own exit.
		if strings.ContainsAny(w, " \t\n") {
			toks, rest := leadingTokens(w)
			for _, t := range toks {
				if err := take(a, t, home); err != nil {
					return nil, err
				}
			}
			return append([]string{rest}, words[i+1:]...), nil
		}
		if err := take(a, w, home); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// leadingTokens splits the @ and + words off the front of s, and leaves
// the rest as it was.
func leadingTokens(s string) (toks []string, rest string) {
	for {
		s = strings.TrimLeft(s, " \t")
		end := strings.IndexAny(s, " \t\n")
		if end < 0 {
			end = len(s)
		}
		if w := s[:end]; w == "" || !isToken(w) {
			return toks, s
		} else {
			toks = append(toks, w)
		}
		s = s[end:]
	}
}

// isToken reports whether w is an @ word or a + word, as irie ctl spawn
// takes them: a + word only when a name follows its +.
func isToken(w string) bool { return isTarget(w) || plusName(w) != "" }

// isTarget reports whether w is an @ word, which names a repo or a path. A
// lone @, as in "meet @ noon", is text.
func isTarget(w string) bool { return len(w) > 1 && w[0] == '@' }

// take applies one @ or + word to a, as irie ctl spawn takes them.
func take(a *SpawnArgs, w, home string) error {
	if w[0] == '+' {
		w = View{}.plus(w).word
	}
	return applyToken(a, w, home)
}

// The tutor words, which are also learn.tutor's modes besides off.
const (
	TutorExplain = "explain"
	TutorTeach   = "teach"
)

// The anonymous research words: +anon, on anon.route, and +tor, on Tor.
const (
	AnonWord = "anon"
	TorWord  = "tor"
)

// applyToken applies one @ word, or one + word as plus reads it, to a.
// Paths are made absolute here; names are the core's to resolve.
func applyToken(a *SpawnArgs, w, home string) error {
	v := w[1:]
	if w[0] == '+' {
		// The tutor words set how the task works with you, beside its
		// profile (learn.tutor, docs/proposals/learning.md).
		if v == TutorExplain || v == TutorTeach {
			if a.Tutor != "" {
				return errors.New("one of +explain and +teach per task")
			}
			a.Tutor = v
			return nil
		}
		// +anon and +tor run the task as anonymous research, in a
		// compartment of its own or, as +anon:name, the named one
		// (DESIGN §4.16).
		if word, name, _ := strings.Cut(v, ":"); word == AnonWord || word == TorWord {
			if a.Anon != "" {
				return errors.New("one of +anon and +tor per task")
			}
			a.Anon = name
			if !strings.Contains(v, ":") {
				a.Anon = AnonNew
			} else if name == "" {
				return fmt.Errorf("+%s: needs a compartment's name, such as +%s:acme", word, word)
			}
			if word == TorWord {
				a.AnonRoute = "tor"
			}
			return nil
		}
		if a.Profile != "" {
			return errors.New("only one +profile per task")
		}
		a.Profile = v
		return nil
	}
	if a.Repo != "" {
		return errors.New("only one @ target per task")
	}
	inPlace := strings.HasSuffix(v, "!")
	v = strings.TrimSuffix(v, "!")
	switch {
	case v == "":
		return errors.New("@ needs a repo name or a path")
	case strings.Contains(v, "!"):
		return fmt.Errorf("%s: one ! at the end runs the task in the repo's own checkout", w)
	case v == "~":
		v = home
	case strings.HasPrefix(v, "~/"):
		v = filepath.Join(home, v[2:])
	case filepath.IsAbs(v):
		v = filepath.Clean(v)
	case strings.HasPrefix(v, "~") || v == "." || v == ".." || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../"):
		return fmt.Errorf("%s: give an absolute path, a ~/ path or a repo name", w)
	}
	a.Repo, a.InPlace = v, inPlace
	return nil
}

// Unpaste puts each paste back whole in place of its label. Labels match
// pastes in the order they were made; a paste whose label was deleted is
// dropped, and a label typed by hand, with no paste left for it, stays text.
func Unpaste(task string, pastes []Paste) string {
	used := make([]bool, len(pastes))
	var b strings.Builder
	for {
		at, which := -1, -1
		for i, p := range pastes {
			if used[i] || p.Label == "" {
				continue
			}
			if j := strings.Index(task, p.Label); j >= 0 && (at < 0 || j < at) {
				at, which = j, i
			}
		}
		if which < 0 {
			break
		}
		b.WriteString(task[:at])
		b.WriteString(pastes[which].Text)
		task = task[at+len(pastes[which].Label):]
		used[which] = true
	}
	b.WriteString(task)
	return b.String()
}
