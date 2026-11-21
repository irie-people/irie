package founding

import (
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/ipc"
)

// Find gives the passages that hold query's words, in order, whatever their
// spelling, capitals or punctuation: in headings, the documents' words,
// notes and lists of names alike. It matches whole words, so "arm" doesn't
// find "Armies". A query that folds to nothing finds nothing.
func Find(docs []ipc.FoundingDoc, query string) []ipc.FoundingHit {
	q := fold(query)
	if q == "" {
		return nil
	}
	q = " " + q + " "
	var hits []ipc.FoundingHit
	for _, d := range docs {
		var h2, h3, h4 string
		for i, b := range d.Blocks {
			switch b.Kind {
			case "h2":
				h2, h3, h4 = b.Text, "", ""
			case "h3":
				h3, h4 = b.Text, ""
			case "h4":
				h4 = b.Text
			}
			text := b.Text
			if b.Kind == "list" {
				text = strings.Join(b.Items, "\n")
			}
			if strings.Contains(" "+fold(text)+" ", q) {
				hits = append(hits, ipc.FoundingHit{Doc: d.ID, Block: i, Where: where(h2, h3, h4)})
			}
		}
	}
	return hits
}

// where joins the headings a passage sits under, such as
// "Article. I. › Section. 8.".
func where(headings ...string) string {
	var keep []string
	for _, h := range headings {
		if h != "" {
			keep = append(keep, h)
		}
	}
	return strings.Join(keep, " › ")
}

// oldSpellings are the documents' words that are spelled otherwise today,
// each with today's spelling. fold turns both the passage and the query to
// today's, so a search for either finds both. It is a list of these texts'
// words rather than a rule, such as -ence to -ense, which would also turn
// "sentence" and "science" into words no one types.
var oldSpellings = map[string]string{
	"chuse": "choose", "chusing": "choosing",
	"defence": "defense", "offence": "offense", "offences": "offenses",
	"labour": "labor", "behaviour": "behavior", "endeavoured": "endeavored", "neighbouring": "neighboring",
	"encreased": "increased", "controul": "control", "compleat": "complete", "shewn": "shown",
	"harrass": "harass", "brittish": "british", "independance": "independence",
	// The Articles of Confederation.
	"stile": "style", "pretence": "pretense",
	// The English Bill of Rights of 1689, in the Statutes of the Realm's
	// spelling.
	"parlyament": "parliament", "kingdome": "kingdom", "lawes": "laws", "crowne": "crown",
	"baile": "bail", "cruell": "cruel", "unusuall": "unusual", "illegall": "illegal", "regall": "regal",
	"criminall": "criminal", "freedome": "freedom", "declareing": "declaring", "setleing": "settling",
	"beene": "been", "hath": "has",
	// The originals write "any Thing", "re-examined" and "co-operation",
	// which fold splits at their hyphens.
	"anything": "any thing", "reexamined": "re examined", "cooperation": "co operation",
}

// fold makes a passage and a query comparable, so a search finds a passage
// whatever its spelling, capitals or punctuation. It gives the words in
// lower case, with one space between each and none before or after: Find
// pads the result with spaces to match whole words. Punctuation, markup
// and hyphens part words ("Vice-President" is vice president), an
// apostrophe doesn't ("Nature's" is natures), "&" is and, and each old
// spelling is today's.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’':
		case r == '&':
			b.WriteString(" and ")
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	words := strings.Fields(b.String())
	for i, w := range words {
		if today, ok := oldSpellings[w]; ok {
			words[i] = today
		}
	}
	return strings.Join(words, " ")
}
