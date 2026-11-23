package terms

import (
	"github.com/irie-people/irie/internal/brand"
	"slices"
	"strconv"
	"strings"
)

// The verdicts a point takes, from the reader's side.
const (
	Against = "against" // it takes something from you, or leaves you worse off than the law would
	Watch   = "watch"   // worth checking before you agree
	Know    = "know"    // usual, and good to know
	For     = "for"     // in your favour
)

// rank orders the verdicts, the ones to read first first.
var rank = map[string]int{Against: 0, Watch: 1, Know: 2, For: 3}

// The groups points come in.
const (
	Disputes = "Disputes"
	Account  = "Your account"
	Content  = "What you post"
	Data     = "Your data"
	Money    = "Money"
	Wrong    = "If things go wrong"
	Changes  = "Changes"
	Using    = "Using it"
)

// rx is a pattern a clause on the topic shows, with how much it counts.
// A match that skip matches doesn't count, such as "you may close your
// account" for a pattern of what the company may do.
type rx struct {
	re   *pattern
	w    float64
	skip *pattern
}

// matches says whether text shows the pattern.
func (f rx) matches(text string) bool {
	if f.skip == nil {
		return f.re.MatchString(text)
	}
	for _, m := range f.re.FindAllString(text, 16) {
		if !f.skip.MatchString(m) {
			return true
		}
	}
	return false
}

// topic is a kind of clause terms of service hold, such as binding
// arbitration, with how to find it and how to say it in plain words.
type topic struct {
	id, group, title string
	verdict          string
	// find are the patterns a clause on the topic shows; a clause counts
	// once the weights of those it matches reach need.
	find []rx
	need float64
	// head matches the headings of sections on the topic: a clause under
	// one counts one more.
	head *pattern
	// not matches clauses that look like the topic but aren't.
	not *pattern
	// neg matches a clause that says the opposite, such as "we do not sell
	// your data": the point then takes flip's words.
	neg  *pattern
	flip *variant
	// says is the point in plain words, made from what the clauses say;
	// it may change the match's verdict.
	says func(m *match) string
	// ask matches the words of a question about the topic, and lead those
	// that make it the first topic of the answer, as "who owns" does
	// ownership.
	ask, lead *pattern
	// about is what the topic is about, for "nothing about X".
	about string
	// expect says whether terms like d should say something on the topic,
	// so its absence is worth a line; missing is that line.
	expect  func(d *Doc) bool
	missing string
}

// variant is a topic's opposite.
type variant struct {
	title, verdict string
	says           func(m *match) string
}

// match is what a document says on a topic.
type match struct {
	text    string   // the clause that says it best
	texts   []string // every clause on the topic, the best first
	verdict string
	doc     *Doc
}

// all is every clause on the topic as one text, for the details.
func (m *match) all() string { return strings.Join(m.texts, " ") }

// near is a pattern of a, then b within n words.
func near(a, b string, n int) string {
	return `(?:` + a + `)\W+(?:[\w'-]+\W+){0,` + strconv.Itoa(n) + `}?(?:` + b + `)`
}

// either is a pattern of a near b in either order.
func either(a, b string, n int) string { return near(a, b, n) + `|` + near(b, a, n) }

// At will: what lets a company act whenever it likes.
const atWill = `at any time|for any reason|for no reason|any or no reason|with or without (?:cause|notice|reason)|without (?:prior |advance |any )?(?:notice|warning)|(?:in|at) (?:our|its|their) (?:sole|absolute|own)(?: and absolute)? discretion|without (?:any )?(?:liability|cause|explanation)|for any or no reason|without giving (?:you )?(?:a |any )?reason`

// negation is a word that turns a statement around.
const negation = `\b(?:not|never|no|don't|doesn't|won't|neither|nor|cannot|can't)\b`

// they is a clause's subject acting, as in "we may", "GitHub reserves the
// right to" or "the Company can": a pattern that starts with it takes
// notYou as its skip, so "you may" is not the company acting.
const they = `\b[\w&.'-]+,?\s+(?:(?:may|can|might|could|will|shall)(?:\s+(?:also|at any time|immediately|without notice),?)?|reserves? the right to|ha(?:s|ve) the right to|retains? the right to|(?:is|are) entitled to|(?:may|can) choose to)\b`

// notYou drops a match whose subject is the person, not the company.
var notYou = re(`^(?:you|users?|members?|customers?|subscribers?|account holders?|parents?|if you)\b`)

var catalog = []*topic{
	// Disputes.
	{
		id: "arbitration", group: Disputes, verdict: Against,
		title: "Disputes go to private arbitration",
		find: []rx{
			{re(`binding (?:individual )?arbitration|arbitration (?:shall|will) be (?:final and )?binding`), 2, nil},
			{re(near(`(?:resolved?|settled?|decided|determined|submitted|brought|conducted|handled)`, `arbitration`, 8)), 2, nil},
			{re(`\b(?:JAMS|American Arbitration Association|AAA|ICDR|LCIA|ICC Rules|Federal Arbitration Act|arbitral tribunal)\b`), 1, nil},
			{re(`\barbitrat(?:ion|or|ors|e)\b`), 1, nil},
		},
		need: 3,
		head: re(`arbitrat|dispute|resolution`),
		// An arbitration a person may choose, such as the Data Privacy
		// Framework's, takes nothing from them.
		not: re(`small claims? court only|\binvoke (?:binding )?arbitration|(?:possibility|option|right|entitled|may elect|may choose|can choose) (?:\w+ ){0,4}(?:to )?(?:invoke|pursue|seek|request) (?:binding )?arbitration|data privacy framework|\bDPF\b`),
		says: func(m *match) string {
			s := "If you have a dispute with them, you can't take it to court: a private arbitrator decides, and the decision is final and hard to appeal."
			if d := find(find(m.all(), re(either(`opt[ -]?out|reject (?:this|the) (?:arbitration|agreement to arbitrate)`, days, 40))), re(days)); d != "" {
				m.verdict = Watch
				s += " You can opt out within " + plainDays(d) + " of first agreeing, by telling them in writing."
			} else if re(`opt[ -]?out`).MatchString(m.all()) {
				m.verdict = Watch
				s += " They say you can opt out of it: the clause says how."
			}
			return s
		},
		ask:   re(`\b(?:sue|suing|sued|court|lawsuits?|arbitrat\w*|disputes?|judge|lawyers?|attorneys?|legal action|take (?:them|it) to|complain\w*|claims?)\b`),
		about: "how disputes are settled",
	},
	{
		id: "class-action", group: Disputes, verdict: Against,
		title: "No class actions",
		find: []rx{
			{re(`class[ -]action|class[ -]wide|class arbitration|collective action|representative (?:action|proceeding|capacity)|consolidated (?:action|proceeding|arbitration)|private attorney general`), 1.5, nil},
			{re(`individual basis|individual capacity|individually|only (?:on behalf of|for) (?:yourself|itself)`), 1, nil},
			{re(`\bwaive|waiver|may not|will not|shall not|cannot|no (?:class|collective)|not (?:as a |a )?(?:plaintiff|class member|participate|bring|join)`), 1, nil},
		},
		need: 3.5,
		head: re(`class action|waiver|dispute|arbitration`),
		says: func(m *match) string {
			return "You give up the right to join a class action: you'd have to bring any claim alone, even if many people were harmed the same way."
		},
		ask:   re(`\bclass[ -]?actions?|together|other (?:users|people|customers)|group (?:lawsuit|claim|action)\b`),
		about: "class actions",
	},
	{
		id: "jury", group: Disputes, verdict: Against,
		title: "No trial by jury",
		find: []rx{
			{re(either(`waiv(?:e|es|ed|er|ing)|give up|giving up|no right to|relinquish`, `jury trial|trial by jury|right to a jury|jury`, 10)), 3, nil},
		},
		need: 3,
		says: func(m *match) string {
			return "You give up a trial by jury: if a case ever goes to court, a judge alone decides it."
		},
		ask:   re(`\bjury\b`),
		about: "trial by jury",
	},
	{
		id: "small-claims", group: Disputes, verdict: For,
		title: "Small claims court stays open to you",
		find: []rx{
			{re(`small[ -]claims? (?:court|tribunal|action|matters?|proceeding)`), 2, nil},
		},
		need: 2,
		says: func(m *match) string {
			return "You can still take a small claim to a small-claims court instead of arbitration."
		},
		ask:   re(`\bsmall[ -]claims?\b`),
		about: "small claims",
	},
	{
		id: "claim-deadline", group: Disputes, verdict: Against,
		title: "A short deadline for claims",
		find: []rx{
			{re(near(`(?:within|no later than|not later than|more than|after|beyond)`, `(?:one|two|1|2|six|6|twelve|12|eighteen|18)(?:\s*\(\d+\))?\s*(?:years?|months?)`, 3)), 1.5, nil},
			{re(either(`claims?|cause of action|actions?|proceedings?|disputes?`, `(?:must|shall) be (?:filed|commenced|brought|initiated)|(?:is|are|be) (?:forever |permanently )?(?:barred|waived)|time[ -]barred`, 25)), 2, nil},
		},
		need: 3.5,
		says: func(m *match) string {
			if t := find(m.text, re(`\b(?:one|two|1|2|six|6|twelve|12|eighteen|18)(?:\s*\(\d+\))?\s*(?:years?|months?)\b`)); t != "" {
				return "You must bring any claim within " + plainSpan(t) + " of it arising, often much sooner than the law would otherwise allow, or lose it."
			}
			return "You must bring any claim within a short time of it arising, or lose it."
		},
		ask:   re(`\b(?:deadline|how long (?:do i have|to)|time limit|too late|statute of limitations?)\b`),
		about: "deadlines for claims",
	},
	{
		id: "law", group: Disputes, verdict: Know,
		title: "Whose law applies",
		find: []rx{
			{re(`governed by(?:,? and (?:shall be |will be )?(?:construed|interpreted|enforced)(?: in accordance with| under)?,?)? (?:the )?(?:federal |substantive |internal )?laws? of`), 3, nil},
			{re(`(?:governing|applicable|choice of) laws?\b`), 1, nil},
			{re(`laws? of (?:the )?(?:state|commonwealth|province|republic|kingdom) of`), 1, nil},
		},
		need: 3,
		head: re(`governing law|choice of law|applicable law|jurisdiction`),
		says: func(m *match) string {
			if p := lawPlace(m.text); p != "" {
				return "The laws of " + p + " apply to these terms, wherever you live; the laws where you live may still protect you."
			}
			return "These terms say which country's or state's laws apply to them; the laws where you live may still protect you."
		},
		ask:   re(`\b(?:which laws?|what laws?|governing law|jurisdiction|countr(?:y|ies)|state laws?|laws? appl)\w*`),
		about: "which law applies",
	},
	{
		id: "venue", group: Disputes, verdict: Watch,
		title: "Court cases happen where they choose",
		find: []rx{
			{re(`(?:exclusive|sole) (?:personal )?(?:jurisdiction|venue|forum)|exclusively in the courts|submit to the (?:personal |exclusive )?jurisdiction`), 2, nil},
			{re(`\bcourts? (?:located|sitting|situated|based) (?:in|within)`), 1.5, nil},
			{re(`\bexclusively\W+(?:[\w'-]+\W+){0,6}?courts?\b|\b(?:brought|filed|heard|litigated|resolved|commenced) (?:\w+ ){0,3}in (?:the )?(?:state (?:or|and) federal |federal (?:or|and) state )?courts?\b`), 1, nil},
			{re(`\bvenue\b`), 1, nil},
		},
		need: 2,
		head: re(`jurisdiction|venue|governing law|disputes?`),
		says: func(m *match) string {
			if p := courtPlace(m.text); p != "" {
				return "Any court case has to be in " + p + ", which may be far from where you live."
			}
			return "Any court case has to be in courts they choose, which may be far from where you live."
		},
		ask:   re(`\b(?:where (?:would|do|can|must) i (?:sue|file|go)|which courts?|venue|jurisdiction)\b`),
		about: "where court cases happen",
	},

	// Your account.
	{
		id: "termination", group: Account, verdict: Against,
		title: "They can close your account whenever they like",
		find: []rx{
			{re(near(near(they, `\b(?:suspend|terminate|disable|deactivate|discontinue|close|restrict|delete|cancel|revoke|block|ban|limit|remove)\b`, 6), `(?:accounts?|access|membership|use|right to use|registration|profiles?|licen[cs]e|subscriptions?)\b`, 8)), 1.5, notYou},
			{re(`\b(?:accounts?|access|membership|registration)\W+(?:[\w'-]+\W+){0,4}?(?:may|can|will|could) be (?:suspended|terminated|disabled|deactivated|closed|deleted|cancell?ed|revoked|restricted|limited|removed)`), 1.5, nil},
			{re(atWill), 1.5, nil},
		},
		need: 3,
		head: re(`terminat|suspen|account|cancel`),
		says: func(m *match) string {
			how := []string{}
			if re(`at any time`).MatchString(m.text) {
				how = append(how, "at any time")
			}
			if re(`without (?:prior |advance |any )?(?:notice|warning)|with or without notice`).MatchString(m.text) {
				how = append(how, "without warning")
			}
			if re(`for any reason|for no reason|any or no reason|without (?:any )?(?:cause|reason|explanation)|with or without cause|sole(?: and absolute)? discretion|without giving`).MatchString(m.text) {
				how = append(how, "for any reason")
			}
			s := "They can suspend or close your account"
			if len(how) > 0 {
				s += " " + list(how)
			}
			return s + ", and you may lose what's in it."
		},
		ask:   re(`\b(?:ban(?:ned)?|suspend\w*|terminat\w*|kick\w*|deactivat\w*|lose (?:my )?(?:access|account)|close my account|shut (?:down )?my|block(?:ed)? me)\b`),
		about: "when they can close your account",
	},
	{
		id: "you-can-leave", group: Account, verdict: For,
		title: "You can leave at any time",
		find: []rx{
			{re(`\byou (?:may|can|are free to) (?:terminate|cancel|close|delete|deactivate|stop using|end)\b`), 2, nil},
			{re(near(`(?:delete|close|cancel|deactivate|terminate) your (?:account|subscription|membership)`, `at any time|any time|by|through|via|in your|from your`, 4)), 2, nil},
			{re(`stop using the (?:services?|site|app|platform) at any time`), 2, nil},
		},
		need: 2,
		head: re(`terminat|cancel|account`),
		says: func(m *match) string {
			s := "You can close your account or stop using the service at any time"
			if h := find(m.all(), re(`(?:by|through|via|in|from) (?:visiting |going to |using |contacting |emailing |clicking |following )?(?:your |the |our )?(?:account settings|settings|account page|profile|help center|support|instructions)[^.;]{0,60}`)); h != "" {
				s += ", " + strings.TrimRight(h, " ,")
			}
			return s + "."
		},
		ask:   re(`\b(?:cancel\w*|close (?:my )?account|delete (?:my )?account|leave|quit|stop using|unsubscrib\w*|get out|end (?:my )?(?:subscription|account))\b`),
		about: "how to close your account",
		expect: func(d *Doc) bool {
			return d.Kind == KindTerms
		},
		missing: "They don't say how you can close your account.",
	},
	{
		id: "content-removal", group: Account, verdict: Watch,
		title: "They can remove what you post",
		find: []rx{
			{re(near(near(they, `\b(?:remove|delete|take down|block|refuse|reject|edit|modify|disable access to|screen|filter)\b`, 6), `(?:content|posts?|materials?|submissions?|listings?|comments?|uploads?|messages?|reviews?|videos?|photos?)\b`, 6)), 1.5, notYou},
			{re(`\b(?:content|posts?|materials?|submissions?|listings?|comments?|uploads?|reviews?|videos?|photos?)\W+(?:[\w'-]+\W+){0,4}?(?:may|can|will) be (?:removed|deleted|taken down|blocked|edited|disabled)`), 1.5, nil},
			{re(atWill), 1.5, nil},
		},
		need: 3,
		head: re(`content|removal|moderation|conduct`),
		says: func(m *match) string {
			s := "They can remove anything you post"
			if re(`without (?:prior |advance |any )?(?:notice|warning)`).MatchString(m.text) {
				s += ", without warning"
			}
			if re(`for any reason|sole(?: and absolute)? discretion|any or no reason|for no reason`).MatchString(m.text) {
				s += ", for any reason"
			}
			return s + "."
		},
		ask:   re(`\b(?:remov\w*|delet\w*|take down|censor\w*|moderat\w*)\b.*\b(?:posts?|content|comments?|videos?|photos?|stuff)\b`),
		about: "removing what you post",
	},
	{
		id: "inactive", group: Account, verdict: Watch,
		title: "Unused accounts may be closed",
		find: []rx{
			{re(`inactiv(?:e|ity)|dormant|not (?:logged|signed) in (?:for|to)|unused accounts?`), 1.5, nil},
			{re(`\b(?:delete|terminate|close|reclaim|deactivate|remove|suspend)\w*`), 1, nil},
		},
		need: 2.5,
		says: func(m *match) string {
			if t := find(m.text, re(`\b(?:\d{1,3}|one|two|three|six|twelve|eighteen|twenty[- ]four)(?:\s*\(\d+\))?\s*(?:years?|months?|days?)\b`)); t != "" {
				return "An account you don't use for " + plainSpan(t) + " may be closed, and what's in it deleted."
			}
			return "An account you don't use for a while may be closed, and what's in it deleted."
		},
		ask:   re(`\b(?:inactiv\w*|don't use|not using|unused|dormant)\b`),
		about: "unused accounts",
	},
	{
		id: "account-yours", group: Account, verdict: Know,
		title: "Everything done on your account is on you",
		find: []rx{
			{re(near(`(?:you are|you're|you will be|you shall be|you remain|you'll be) (?:solely |fully |entirely |wholly )?responsible for`, `(?:activit\w*|use|everything|anything|all (?:actions|conduct|use)|conduct)`, 4)), 1.5, nil},
			{re(`account|password|credentials|log[ -]?in|sign[ -]?in`), 1, nil},
			{re(near(`keep|maintain|protect`, `password|credentials|login|account`, 5)), 0.5, nil},
		},
		need: 2.5,
		head: re(`account|security|registration|password`),
		says: func(m *match) string {
			return "You're responsible for everything done with your account, so keep your password safe and tell them if it's stolen."
		},
		ask:   re(`\b(?:hack\w*|stolen|password|someone else (?:used|uses)|my account was|unauthori[sz]ed)\b`),
		about: "your account's security",
	},
	{
		id: "age", group: Account, verdict: Know,
		title: "A minimum age",
		find: []rx{
			{re(`\b(?:at least|must be|minimum age(?: of)?|over the age of|older than|age of|aged|under(?: the age of)?)\s+(?:\d{2}|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty[- ]one)\b`), 2, nil},
			{re(`\b(?:1[3-9]|21)\s*(?:\+|years? (?:of age|old)|or older)|age of (?:majority|consent|legal)`), 2, nil},
			{re(`\b(?:must be|you are|you're|at least|minimum age|not (?:permitted|allowed|intended|directed|designed)|(?:do not|don't) permit|may not (?:use|access|register|create)|old enough|eligib\w*|to use (?:the|our|this) (?:services?|site|app|platform))\b`), 1, nil},
			{re(`\b(?:children|minors?|parents?|guardians?)\b`), 0.5, nil},
		},
		need: 3,
		// A privacy policy's words on children's data aren't an age to
		// use the service.
		not:  re(`(?:sell|share|collect|process|disclose|use)\w*"?\W+(?:[\w"'-]+\W+){0,8}?(?:of|from|about) (?:known )?(?:children|minors|kids|users|individuals|consumers) under`),
		head: re(`eligib|age|children|minors?`),
		says: func(m *match) string {
			a := find(m.text, re(`\b(?:1[3-9]|21|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty[- ]one)\b`))
			if a == "" {
				return "You must be old enough to use it, as the terms say."
			}
			s := "You must be at least " + plainNumber(a) + " to use it"
			if re(`parent|guardian`).MatchString(m.all()) {
				s += "; younger people may need a parent's permission"
			}
			return s + "."
		},
		ask:   re(`\b(?:age|old enough|how old|kids?|child(?:ren)?|teens?|teenagers?|minors?|under 1\d|parents?)\b`),
		about: "how old you must be",
	},

	// What you post.
	{
		id: "license", group: Content, verdict: Watch,
		title: "You give them a licence to what you post",
		find: []rx{
			{re(near(`\b(?:grant\w*|give\w*|provid\w*|hereby)\b`, `licen[cs]e|right to (?:use|copy|reproduce|display|store|host|publish|distribute|archive|parse|share|perform|transmit|adapt|modify|translate)`, 14)), 1.5, nil},
			{re(`\bgrant\w*\W+(?:to\W+)?(?:us|we|company|the company)\b`), 1, nil},
			{re(`\b(?:content|materials?|submissions?|posts?|photos?|videos?|images?|anything you|information you|user content|your \w+ content|works?)\b`), 1, nil},
			{re(`worldwide|world-wide|royalty[ -]free|perpetual|irrevocable|sub-?licen[cs]able|transferable|fully[ -]paid`), 1, nil},
		},
		need: 3.5,
		head: re(`content|licen[cs]e|rights|intellectual property|your (?:content|materials)`),
		not:  re(`^we grant you|(?:we|company) grants? you|licen[cs]e to you|grant(?:s)? you a`),
		says: func(m *match) string {
			t := m.text
			var what []string
			add := func(p, s string) {
				if re(p).MatchString(t) {
					what = append(what, s)
				}
			}
			add(`worldwide|world-wide`, "anywhere in the world")
			add(`royalty[ -]free|fully[ -]paid|without (?:compensation|payment)`, "without paying you")
			add(`perpetual|in perpetuity`, "forever")
			add(`irrevocable`, "with no way for you to take it back")
			add(`sub-?licen[cs]able|sub-?licen[cs]e`, "letting others use it too")
			add(`transferable|assignable`, "handing it on to another company")
			add(`modify|adapt|derivative works|edit|translate`, "changing it")
			add(`commercial|any purpose|advertis|promot|market`, "for any purpose, including ads and making money")
			if re(`perpetual|irrevocable|in perpetuity`).MatchString(t) {
				m.verdict = Against
			}
			s := "You let them use, copy and show what you post"
			if len(what) > 0 {
				s += ": " + list(what)
			}
			return s + "."
		},
		ask:   re(`\b(?:own|owns|ownership|my (?:content|photos?|videos?|art|work|posts?|music|writing|stuff)|licen[cs]e|copyright|rights to|use my|upload\w*|post(?:ed|ing|s)?)\b`),
		about: "what they may do with what you post",
	},
	{
		id: "you-own", group: Content, verdict: For,
		title: "What you post stays yours",
		find: []rx{
			{re(near(`you (?:retain|keep|own|maintain|continue to own|remain the (?:sole )?owner|still own)`, `ownership|rights?|title|interest|copyright|intellectual property|content|it\b`, 8)), 2, nil},
			{re(`(?:content|it|materials?) (?:remains?|is|stays?) (?:solely )?(?:yours|your (?:property|own))|belongs to you`), 2, nil},
			{re(`we (?:do not|don't|will not|won't) (?:claim|take|own) (?:any )?ownership|does not (?:claim|transfer) (?:any )?ownership`), 2, nil},
		},
		need: 2,
		head: re(`content|ownership|intellectual property|rights`),
		says: func(m *match) string {
			return "You keep ownership of what you create or post."
		},
		ask:   re(`\b(?:own|owns|ownership|belongs?|mine|my (?:content|photos?|work))\b`),
		lead:  re(`\b(?:who owns|owns?|ownership|belongs?)\b`),
		about: "who owns what you post",
	},
	{
		id: "ai-training", group: Content, verdict: Watch,
		title: "Your content may train AI",
		find: []rx{
			{re(near(`\b(?:train\w*|develop\w*|improv\w*|fine-?tun\w*|build\w*)`, `machine[ -]learning|artificial intelligence|\bA\.?I\.?\b|(?:AI|ML|language|generative|foundation) models?|large language|neural networks?|\bLLMs?\b`, 10)), 2, nil},
			{re(`machine[ -]learning|artificial intelligence|generative AI|\bAI\b|large language models?|\bLLMs?\b`), 1, nil},
		},
		need: 3,
		head: re(`\bAI\b|artificial intelligence|machine learning|model|training`),
		neg:  re(near(negation, `(?:use|train|share)\w*`, 6) + `\W+(?:[\w'-]+\W+){0,10}?(?:train|model|machine|AI\b|artificial)`),
		flip: &variant{
			title: "They say your content won't train AI", verdict: For,
			says: func(m *match) string {
				return "They say they won't use your content to train AI models."
			},
		},
		says: func(m *match) string {
			return "They may use what you post, or data about you, to train AI or machine-learning models."
		},
		ask:   re(`\b(?:AI|A\.I\.|artificial intelligence|machine learning|train\w*|models?|LLMs?|chatgpt|gpt)\b`),
		about: "AI training",
	},
	{
		id: "feedback", group: Content, verdict: Know,
		title: "Your ideas become theirs to use",
		find: []rx{
			{re(`\b(?:feedback|suggestions?|ideas?|proposals?|recommendations?)\b`), 1, nil},
			{re(`without (?:any )?(?:obligation|compensation|attribution|payment|restriction)|for any purpose|free(?:ly)? to use|non-?confidential|(?:we|they) (?:may|can) use|(?:owned by|assign\w* to|property of) (?:us|the company)|royalty[ -]free`), 1.5, nil},
		},
		need: 2.5,
		head: re(`feedback|suggestion|ideas`),
		says: func(m *match) string {
			return "Ideas or feedback you send them are theirs to use for free, with no credit or payment to you."
		},
		ask:   re(`\b(?:feedback|ideas?|suggestions?)\b`),
		about: "feedback you send",
	},
	{
		id: "moral-rights", group: Content, verdict: Watch,
		title: "You give up moral rights",
		find: []rx{
			{re(either(`moral rights?`, `waiv\w*|give up|not (?:to )?assert|consent`, 12)), 3, nil},
		},
		need: 3,
		says: func(m *match) string {
			return "You give up your moral rights in what you post: to be named as its author, and to object to changes made to it."
		},
		ask:   re(`\b(?:moral rights?|credit|attribution|named as)\b`),
		about: "moral rights",
	},

	// Your data.
	{
		id: "collect", group: Data, verdict: Know,
		title: "What they collect",
		find: []rx{
			{re(near(`\b(?:we|they|company)\W+(?:may |also |automatically )*(?:collect|gather|obtain|receive|record|log|process)\w*`, `\b(?:information|data|details|content)\b`, 8)), 2, nil},
			{re(`personal (?:information|data)|information (?:we|they) collect`), 1, nil},
		},
		need: 3,
		head: re(`collect|information|data|privacy`),
		says: func(m *match) string {
			t := m.all()
			var what []string
			add := func(p, s string) {
				if re(p).MatchString(t) && len(what) < 7 {
					what = append(what, s)
				}
			}
			add(`\bnames?\b`, "your name")
			add(`e-?mail`, "email address")
			add(`phone`, "phone number")
			add(`precise (?:geo)?location|gps|exact location`, "precise location")
			add(`\blocation\b`, "location")
			add(`contacts|address book|phone ?book`, "contacts")
			add(`payment|credit card|billing|financial`, "payment details")
			add(`\b(?:ip address|device|browser|operating system)\b`, "device details")
			add(`usage|activity|interactions|how you use`, "what you do on it")
			add(`messages|communications`, "your messages")
			add(`biometric|face|voice|fingerprint`, "biometric data")
			add(`health`, "health data")
			if len(what) == 0 {
				return "They collect information about you and how you use the service."
			}
			return "They collect information about you, such as " + list(what) + "."
		},
		ask:   re(`\b(?:collect\w*|gather\w*|what (?:data|information) do|know about me|track(?:s|ing)? me|information about me)\b`),
		about: "what they collect",
	},
	{
		id: "sell-data", group: Data, verdict: Against,
		title: "They may sell your data",
		find: []rx{
			{re(near(`\bwe\s+(?:[\w'-]+\s+){0,3}?(?:sell|rent|trade)\b|\b[\w&.'-]+\s+(?:may|might|can|could|will|would|do|does|did)\s+(?:also\s+|not\s+|never\s+)*(?:sell|rent|trade)\b|\b(?:sells|rents|trades)\b`, `\b(?:data|information|details)\b`, 6)), 2.5, notYou},
			{re(`\bsale of (?:your |users' |user |customer )?personal (?:data|information)\b`), 2.5, nil},
			{re(`\bsell(?:s|ing)?\b|\bsale\b`), 0.5, nil},
		},
		need: 2.5,
		head: re(`sell|sale|share|disclos|privacy`),
		not:  re(`do not sell (?:or share )?my|"do not sell"|sale of (?:goods|products|our business|assets|the company)|point of sale`),
		neg:  re(near(negation, `\bsell\w*|\brent\w*|\btrade\b|\bsale\b`, 5)),
		flip: &variant{
			title: "They say they don't sell your data", verdict: For,
			says: func(m *match) string {
				return "They say they don't sell your personal information."
			},
		},
		says: func(m *match) string {
			return "They may sell your personal information to other companies."
		},
		ask:   re(`\b(?:sell\w*|sold|sale|brokers?|make money (?:from|off) my)\b`),
		about: "selling your data",
	},
	{
		id: "share-data", group: Data, verdict: Watch,
		title: "They share your data with other companies",
		find: []rx{
			{re(near(`(?:\bwe|`+they+`)\s+(?:also\s+)?(?:[\w'-]+\s+){0,2}?(?:share|disclose|provide|transfer|make available)\w*`, `\b(?:third[ -]part(?:y|ies)|partners?|affiliates?|advertisers?|vendors?|service providers?|other companies|business partners|sponsors)\b`, 12)), 2, notYou},
			{re(`personal (?:information|data)|your (?:information|data)|information about you`), 1, nil},
		},
		need: 3,
		head: re(`shar|disclos|third part|privacy`),
		neg:  re(`\b(?:do not|don't|never|will not|won't|does not)\W+(?:[\w'-]+\W+){0,3}?(?:share|disclose|provide|transfer)\b`),
		flip: &variant{
			title: "They say they don't share your data", verdict: For,
			says: func(m *match) string {
				return "They say they don't share your personal information with other companies, beyond what the service needs."
			},
		},
		says: func(m *match) string {
			t := m.all()
			var who []string
			add := func(p, s string) {
				if re(p).MatchString(t) {
					who = append(who, s)
				}
			}
			add(`advertis`, "advertisers")
			add(`partners?`, "partners")
			add(`affiliat`, "companies in their group")
			add(`service providers?|vendors?|processors?`, "companies that work for them")
			if len(who) == 0 {
				return "They share your information with other companies."
			}
			return "They share your information with other companies: " + list(who) + "."
		},
		ask:   re(`\b(?:shar\w*|disclos\w*|third[ -]part\w*|partners?|advertisers?|who (?:sees|gets|has access))\b`),
		about: "sharing your data",
	},
	{
		id: "ads", group: Data, verdict: Watch,
		title: "They track you for ads",
		find: []rx{
			{re(`(?:targeted|personali[sz]ed|interest[ -]based|behaviou?ral|tailored|relevant) (?:ads|advertising|advertisements|marketing)`), 2, nil},
			{re(`\b(?:cookies|web beacons|pixels?|tracking technologies|device identifiers|advertising (?:id|identifiers?)|tracking)\b`), 1, nil},
			{re(`\b(?:ads|advertis\w*)\b`), 1, nil},
		},
		need: 3,
		head: re(`advertis|cookies|tracking`),
		says: func(m *match) string {
			return "They track what you do, with cookies or tools like them, to show you ads picked for you."
		},
		ask:   re(`\b(?:ads|adverts?|advertis\w*|cookies?|track\w*|targeted)\b`),
		about: "ads and tracking",
	},
	{
		id: "monitor", group: Data, verdict: Watch,
		title: "They may look at what you send",
		find: []rx{
			{re(near(near(they, `\b(?:monitor|review|scan|screen|inspect|access|read)\b`, 4), `(?:content|communications|messages|activity|chats?|e-?mails?|files|uploads)\b`, 5)), 2, notYou},
			{re(`but (?:are|is) not obligated|ha(?:ve|s) no obligation|reserves? the right|\bwe may\b`), 1, nil},
		},
		need: 3,
		says: func(m *match) string {
			return "They may look at what you post or send, including your messages."
		},
		ask:   re(`\b(?:read (?:my )?(?:messages|chats|emails?)|private messages|monitor\w*|spy\w*|look at|see my)\b`),
		about: "looking at what you send",
	},
	{
		id: "delete-data", group: Data, verdict: For,
		title: "You can ask for your data to be deleted",
		find: []rx{
			{re(`\byou (?:may|can|have the right to|are entitled to) (?:also )?(?:request|ask)\W+(?:[\w'-]+\W+){0,4}?(?:delet\w*|eras\w*)`), 2, nil},
			{re(`\b(?:request|ask) (?:us )?(?:to )?(?:delete|erase|the deletion of|the erasure of|deletion of)\W+(?:[\w'-]+\W+){0,3}?(?:personal )?(?:data|information)`), 2, nil},
			{re(`\bright to (?:delete|deletion|erasure|be forgotten|request deletion)\b|\bdelete your (?:personal )?(?:data|information)\b`), 2, nil},
			{re(`(?:access|correct|export|download|port) (?:a copy of )?(?:your )?(?:personal )?(?:data|information)`), 1, nil},
		},
		need: 2,
		head: re(`your (?:rights|choices)|delet|privacy rights|access`),
		says: func(m *match) string {
			s := "You can ask them to delete your personal data"
			if re(`(?:access|export|download|copy of|port)`).MatchString(m.all()) {
				s += ", and to see or take a copy of it"
			}
			return s + "."
		},
		ask:   re(`\b(?:delete (?:my )?(?:data|information|everything)|eras\w*|forget me|remove my (?:data|information)|gdpr|ccpa|copy of my data|download my data|export)\b`),
		about: "deleting your data",
		expect: func(d *Doc) bool {
			return d.Kind == KindPrivacy
		},
		missing: "They don't say how to get your data deleted.",
	},
	{
		id: "data-kept", group: Data, verdict: Watch,
		title: "Your data may outlast your account",
		find: []rx{
			{re(near(`retain\w*|keep\w*|kept|stor\w*|preserv\w*|continue to (?:use|hold|store)`, `(?:after|following|even after|beyond|despite)\W+(?:[\w'-]+\W+){0,6}?(?:termination|deletion|cancellation|you (?:delete|close|cancel|terminate)|account (?:is |has been )?(?:closed|deleted|terminated))`, 10)), 2.5, nil},
			{re(`(?:backup|archived?|cached|residual) copies|(?:remain|persist) in (?:our )?(?:backups?|archives?|systems?)`), 1.5, nil},
		},
		need: 2.5,
		head: re(`retention|delet|terminat|storage`),
		says: func(m *match) string {
			return "They may keep your data, or copies of it, after you delete your account."
		},
		ask:   re(`\b(?:keep|kept|retain\w*|after i (?:delete|close|leave)|how long)\b`),
		about: "how long they keep your data",
	},
	{
		id: "company-sale", group: Data, verdict: Watch,
		title: "Your data goes with the company if it's sold",
		find: []rx{
			{re(`merger|acquisition|reorgani[sz]ation|bankruptcy|insolvency|receivership|change of control|sale of (?:all |some |part |a portion )?(?:or (?:substantially|a portion) )?(?:of )?(?:our|its|the company's|their) (?:assets|business|company|stock)`), 1.5, nil},
			{re(near(`\b(?:transfer\w*|assign\w*|sold|sell|disclos\w*|pass(?:ed|es)?|part of)\b`, `\b(?:data|information|accounts?|assets)\b`, 8)), 1.5, nil},
		},
		need: 3,
		says: func(m *match) string {
			return "If the company is sold, merged or goes bankrupt, your data can pass to whoever takes it over."
		},
		ask:   re(`\b(?:bought|acquired|acquisition|merg\w*|bankrupt\w*|goes under|sold to)\b`),
		about: "your data if the company is sold",
	},
	{
		id: "authorities", group: Data, verdict: Know,
		title: "They hand data to authorities when asked",
		find: []rx{
			{re(`\b(?:law enforcement|government (?:authorities|agencies|requests|officials)|subpoenas?|court orders?|legal process|search warrants?|public authorities)\b`), 1.5, nil},
			{re(`\b(?:disclos\w*|shar\w*|provid\w*|comply|respond\w*)\b`), 1, nil},
		},
		need: 2.5,
		says: func(m *match) string {
			return "They give your information to the police, courts or governments when the law requires it, and sometimes when they judge it necessary."
		},
		ask:   re(`\b(?:police|government|law enforcement|subpoenas?|warrants?|authorities|fbi)\b`),
		about: "requests from authorities",
	},
	{
		id: "abroad", group: Data, verdict: Know,
		title: "Your data may be kept abroad",
		find: []rx{
			{re(near(`transferr?\w*|stored?|process\w*|hosted|maintained`, `(?:to|in) (?:the )?(?:United States|U\.S\.|USA|other countries|countries other than|outside (?:of )?(?:your|the))`, 10)), 2, nil},
		},
		need: 2,
		says: func(m *match) string {
			if re(`United States|U\.S\.|USA`).MatchString(m.text) {
				return "Your data may be stored and used in the United States or other countries, under their laws."
			}
			return "Your data may be stored and used in other countries, under their laws."
		},
		ask:   re(`\b(?:where (?:is|are) (?:my )?(?:data|information)|stored|servers?|countr(?:y|ies)|abroad)\b`),
		about: "where your data is kept",
	},
	{
		id: "privacy-policy", group: Data, verdict: Know,
		title: "Your data is covered by a privacy policy",
		find: []rx{
			{re(`privacy (?:policy|notice|statement)`), 2, nil},
			{re(`\b(?:governs|describes|explains|incorporated|see|read|review|refer|learn|available at)\b|how we (?:collect|use)`), 1, nil},
		},
		need: 3,
		says: func(m *match) string {
			if m.doc != nil && m.doc.Privacy != "" {
				return "What they do with your data is in their privacy policy, a separate document: " + brand.Name + " can read it too."
			}
			return "What they do with your data is in their privacy policy, a separate document."
		},
		ask:   re(`\b(?:privacy|personal (?:data|information))\b`),
		about: "a privacy policy",
		expect: func(d *Doc) bool {
			return d.Kind == KindTerms && d.Privacy == ""
		},
		missing: "They don't point to a privacy policy.",
	},

	// Money.
	{
		id: "auto-renew", group: Money, verdict: Watch,
		title: "Subscriptions renew themselves",
		find: []rx{
			{re(`(?:automatic(?:ally)?|auto)[ -]?renew\w*|renew\w* automatically`), 2, nil},
			{re(`(?:recurring|periodic|repeat) (?:billing|charges?|payments?|fees?|subscriptions?)`), 1.5, nil},
			{re(`(?:until|unless) you cancel|continue to (?:charge|bill)|charged (?:each|every) (?:month|year)`), 1.5, nil},
		},
		need: 2,
		head: re(`subscription|renewal|billing|payment|fees`),
		says: func(m *match) string {
			s := "Paid plans renew and charge you automatically until you cancel"
			if d := find(m.all(), re(near(`cancel\w*`, `(?:\d+|one|two|seven|fourteen|thirty|twenty[- ]four|forty[- ]eight)(?:\s*\(\d+\))?\s*(?:hours?|days?)\s*(?:before|prior)`, 10))); d != "" {
				if t := find(d, re(`(?:\d+|one|two|seven|fourteen|thirty|twenty[- ]four|forty[- ]eight)(?:\s*\(\d+\))?\s*(?:hours?|days?)`)); t != "" {
					s += "; cancel at least " + plainSpan(t) + " before it renews"
				}
			}
			return s + "."
		},
		ask:   re(`\b(?:renew\w*|subscription|recurring|charged? (?:again|every|each)|billing|billed|monthly|yearly|annual)\b`),
		about: "renewing subscriptions",
	},
	{
		id: "no-refunds", group: Money, verdict: Against,
		title: "No refunds",
		find: []rx{
			{re(`\b(?:no|non)[ -]?refund(?:s|able)?\b`), 2, nil},
			{re(`(?:all )?(?:payments?|fees|sales|charges|purchases|amounts paid) (?:are |will be |shall be )?(?:final|non-?refundable|not refundable)`), 2, nil},
			{re(near(`not|no|never`, `(?:entitled|eligible) (?:to|for) (?:a |any )?refunds?|(?:provide|issue|offer|give|make) (?:any )?refunds?`, 4)), 2, nil},
		},
		need: 2,
		head: re(`refund|payment|fees|billing|cancel`),
		says: func(m *match) string {
			s := "Payments are final: they don't give refunds"
			if re(`except|unless|required by (?:applicable )?law|where (?:applicable )?law`).MatchString(m.text) {
				s += ", except where the law requires one"
			}
			return s + "."
		},
		ask:   re(`\b(?:refund\w*|money back|reimburs\w*|chargebacks?|get (?:my )?money|returns?)\b`),
		about: "refunds",
		expect: func(d *Doc) bool {
			return d.Kind == KindTerms && d.paid()
		},
		missing: "They take payments, but don't say whether you can get a refund.",
	},
	{
		id: "refunds", group: Money, verdict: For,
		title: "You can get a refund",
		find: []rx{
			{re(`(?:money[ -]back|satisfaction) guarantee`), 2, nil},
			{re(`you (?:may|can|will|are entitled to|are eligible (?:to|for)) (?:request |receive |get |be )?(?:a )?(?:full |partial |pro[ -]?rat(?:a|ed) )?refund`), 2, nil},
			{re(near(`refund\w*`, `within (?:\d+|seven|fourteen|thirty|sixty)(?:\s*\(\d+\))? days|if you cancel|upon request|on request`, 8)), 1.5, nil},
		},
		need: 2,
		head: re(`refund|cancel|returns?`),
		not:  re(`\b(?:no|non)[ -]?refund|not (?:entitled|eligible) (?:to|for) (?:a |any )?refund|(?:are|is) (?:final|non-?refundable)`),
		says: func(m *match) string {
			if d := find(m.text, re(days)); d != "" && re(`within`).MatchString(m.text) {
				return "You can get a refund within " + plainDays(d) + ", on the terms the clause sets."
			}
			return "You can get a refund in some cases, which the clause sets out."
		},
		ask:   re(`\b(?:refund\w*|money back|reimburs\w*|get (?:my )?money|returns?)\b`),
		about: "refunds",
	},
	{
		id: "price-change", group: Money, verdict: Watch,
		title: "Prices can go up",
		find: []rx{
			{re(near(`\b(?:change|increase|modify|adjust|raise|revise|update)\b`, `\b(?:prices?|pricing|fees?|rates?|charges?)\b`, 5)), 2, nil},
			{re(`(?:prices?|fees|pricing) (?:are |is |may be )?(?:subject to change|change from time to time)`), 2, nil},
		},
		need: 2,
		head: re(`price|pricing|fees|payment|subscription|billing`),
		not:  re(`exchange rates?|currency conversion|interest rates?|frame rates?|data rates?`),
		says: func(m *match) string {
			s := "They can raise prices"
			if d := find(m.all(), re(near(days, `notice`, 4)+`|`+near(`notice`, days, 8))); d != "" {
				if t := find(d, re(days)); t != "" {
					s += ", telling you " + plainDays(t) + " ahead"
				}
			} else if re(`without (?:prior )?notice`).MatchString(m.text) {
				s += ", without telling you first"
			}
			return s + "."
		},
		ask:   re(`\b(?:prices?|pricing|cost|fees?|raise|increase|more expensive)\b`),
		about: "price changes",
	},
	{
		id: "free-trial", group: Money, verdict: Watch,
		title: "Free trials turn into paid plans",
		find: []rx{
			{re(`free trials?|trial period|introductory (?:offer|period|price)`), 1.5, nil},
			{re(`charge|bill|convert|automatically|paid (?:subscription|plan|membership)|end of (?:the|your) (?:trial|period)|unless you cancel`), 1, nil},
		},
		need: 2.5,
		says: func(m *match) string {
			return "A free trial becomes a paid plan, and starts charging you, unless you cancel before it ends."
		},
		ask:   re(`\b(?:free trials?|trial)\b`),
		about: "free trials",
	},
	{
		id: "virtual-items", group: Money, verdict: Watch,
		title: "Coins and items you buy aren't really yours",
		find: []rx{
			{re(`virtual (?:currency|currencies|items?|goods|coins?|points)|in-(?:game|app) (?:currency|items?|purchases)|\b(?:tokens|credits|gems|coins)\b`), 1.5, nil},
			{re(`no (?:monetary |cash |real[ -]world )?value|not (?:redeemable|transferable|refundable)|limited licen[cs]e|forfeit\w*|(?:may|can) (?:be )?(?:modif|remov|revok)\w*|do not own|no ownership`), 1.5, nil},
		},
		need: 3,
		says: func(m *match) string {
			return "Coins, points or items you buy have no cash value, aren't yours to keep, and can be changed or taken away."
		},
		ask:   re(`\b(?:coins?|gems?|tokens?|credits?|virtual|in-game|in-app|items?)\b`),
		about: "virtual items and currency",
	},

	// If things go wrong.
	{
		id: "as-is", group: Wrong, verdict: Know,
		title: "No promises it works",
		find: []rx{
			{re(`(?:provided|offered|available|made available|on an?) (?:on an? )?"?as[ -]is"?|"as[ -]is"`), 2, nil},
			{re(`"?as[ -]available"?`), 1, nil},
			{re(near(`disclaim\w*`, `(?:all )?(?:warranties|warranty|representations|conditions|guarantees)`, 6)), 2, nil},
			{re(`without (?:any )?(?:warrant(?:y|ies)|guarantees?) of any kind|makes? no (?:warrant(?:y|ies)|representations|guarantees?)`), 2, nil},
		},
		need: 2,
		head: re(`warrant|disclaim`),
		says: func(m *match) string {
			return "The service comes with no promises: it's offered \"as is\", so they don't guarantee it works, is safe, or is right."
		},
		ask:   re(`\b(?:guarantee\w*|warrant\w*|promise\w*|works?|broken|bugs?|outages?|down|reliab\w*)\b`),
		about: "promises that it works",
	},
	{
		id: "liability-cap", group: Wrong, verdict: Watch,
		title: "A cap on what they'd ever pay you",
		find: []rx{
			{re(`(?:aggregate|total|entire|maximum|cumulative|overall) liability|liability (?:of|to you)\W+(?:[\w'-]+\W+){0,6}?(?:shall|will) not exceed|in no event (?:shall|will) (?:our|its|the company's)\W+(?:[\w'-]+\W+){0,4}?liability`), 2, nil},
			{re(`(?:shall|will|does|may) not exceed|(?:is |be )?limited to|(?:the )?(?:greater|lesser) of|in excess of|more than (?:the )?(?:amount|greater)`), 1.5, nil},
			{re(`\$\s?\d|\b\d+ (?:us )?dollars|amounts? (?:you )?(?:have )?paid|fees (?:you )?paid`), 0.5, nil},
		},
		need: 3.5,
		head: re(`limitation of liability|liability|limits?`),
		says: func(m *match) string {
			t := m.text
			amount := find(t, re(`(?:US\s?|USD\s?)?\$\s?\d[\d,]*(?:\.\d{2})?|\b\d[\d,]*(?:\.\d{2})?\s?(?:US |U\.S\. )?dollars|(?:EUR|GBP|€|£)\s?\d[\d,]*(?:\.\d{2})?`))
			paid := re(`amounts? (?:actually )?(?:you )?(?:have )?paid|fees (?:you )?(?:have )?paid|paid (?:by you|to us)`).MatchString(t)
			months := find(t, re(`\b(?:\d+|three|six|twelve)(?:\s*\(\d+\))?\s*months?\b`))
			switch {
			case paid && amount != "":
				s := "If they harm you, the most they'd ever pay is " + money(amount) + " or what you paid them"
				if months != "" {
					s += " in the " + plainSpan(months) + " before"
				}
				switch {
				case re(`greater of|higher of|whichever is (?:greater|higher|more)`).MatchString(t):
					return s + ", whichever is more."
				case re(`lesser of|lower of|whichever is (?:less|lower|smaller)`).MatchString(t):
					return s + ", whichever is less."
				}
				return s + "."
			case paid:
				s := "If they harm you, the most they'd ever pay is what you paid them"
				if months != "" {
					s += " in the " + plainSpan(months) + " before"
				}
				return s + "."
			case amount != "":
				return "If they harm you, the most they'd ever pay is " + money(amount) + "."
			}
			return "If they harm you, what they'd pay is capped, often at very little."
		},
		ask:   re(`\b(?:compensat\w*|damages?|liab\w*|responsible|pay (?:me|for)|harm\w*|lose|lost|losses?|breach\w*|leak\w*|hack\w*|sue (?:them )?for|how much)\b`),
		about: "what they'd pay if they harm you",
	},
	{
		id: "no-liability", group: Wrong, verdict: Watch,
		title: "They aren't responsible for most losses",
		find: []rx{
			{re(near(`not|in no event|under no circumstances|never|neither`, `liable|responsible|liability`, 12)), 1.5, nil},
			{re(`indirect|incidental|consequential|special|punitive|exemplary|lost profits|loss of (?:profits|data|revenue|goodwill|use|business)|damages`), 1.5, nil},
		},
		need: 3,
		head: re(`limitation of liability|liability|disclaim`),
		says: func(m *match) string {
			t := m.all()
			var what []string
			add := func(p, s string) {
				if re(p).MatchString(t) {
					what = append(what, s)
				}
			}
			add(`loss of data|lost data|data loss|corruption of data`, "lost data")
			add(`lost profits|loss of (?:profits|revenue|business)`, "lost money or business")
			add(`goodwill|reputation`, "harm to your reputation")
			add(`indirect|consequential|incidental`, "knock-on losses")
			if len(what) == 0 {
				return "They aren't responsible for most of the harm you might suffer from using it."
			}
			return "They aren't responsible for most of the harm you might suffer from using it, such as " + list(what) + "."
		},
		ask:   re(`\b(?:liab\w*|responsible|fault|lost (?:data|money|files)|data loss|damages?|blame)\b`),
		about: "their responsibility for losses",
	},
	{
		id: "indemnify", group: Wrong, verdict: Watch,
		title: "You cover their legal costs",
		find: []rx{
			{re(`\bindemnif(?:y|ies|ied|ication|ying)\b`), 2, nil},
			{re(near(`hold`, `harmless`, 8)), 1.5, nil},
			{re(`\byou (?:agree to |will |shall |must )?(?:defend,? )?(?:indemnify|hold)`), 1, nil},
		},
		need: 2,
		head: re(`indemn`),
		not:  re(`^(?:we|company) (?:will|shall|agrees? to) (?:defend|indemnify) you`),
		says: func(m *match) string {
			return "If your use of the service gets them sued or fined, you agree to pay their costs, lawyers' fees included."
		},
		ask:   re(`\b(?:indemn\w*|legal fees|their (?:costs|lawyers)|pay (?:for )?their|hold harmless)\b`),
		about: "covering their legal costs",
	},

	// Changes.
	{
		id: "changes", group: Changes, verdict: Watch,
		title: "They can change these terms",
		find: []rx{
			{re(near(`\b(?:may|can|might|will|reserves? the right to|right to)\b`, `(?:modify|change|amend|update|revise|alter|replace)\W+(?:[\w'-]+\W+){0,4}?(?:these|this|the|our|any|its) (?:terms|agreement|conditions|policy|policies|tos|eula)`, 6)), 2, nil},
			{re(`(?:changes?|modifications?|updates?|amendments?|revisions?) to (?:these|this|the|our) (?:terms|agreement|policy)`), 1.5, nil},
			{re(`(?:continued|continuing|continue to) (?:use|access)`), 0.5, nil},
		},
		need: 2,
		head: re(`changes?|modif|amend|updates?|revisions?`),
		says: func(m *match) string {
			t := m.all()
			s := "They can change these terms"
			if d := find(t, re(near(days, `(?:prior |advance |written )*notice|before (?:they|the changes?) (?:take|become)`, 6)+`|`+near(`notice|notify`, days, 10))); d != "" {
				if x := find(d, re(days)); x != "" {
					s += " and will tell you " + plainDays(x) + " ahead"
				}
			} else if re(`without (?:prior |advance |any )?notice|at any time and without`).MatchString(t) {
				m.verdict = Against
				s += " without telling you first"
			} else if re(near(`notify|notice|tell|inform|email|e-mail`, `you`, 4) + `|reasonable notice`).MatchString(t) {
				s += " and say they'll tell you"
			} else if re(`posting|post the|posted|updating the (?:date|"last updated")|last updated`).MatchString(t) {
				m.verdict = Against
				s += " just by posting a new version"
			}
			return s + "; using it afterwards counts as accepting the changes."
		},
		ask:   re(`\b(?:chang\w*|updat\w*|modif\w*|amend\w*|new (?:terms|version)|notif\w*|tell me)\b`),
		about: "changes to the terms",
	},
	{
		id: "service-changes", group: Changes, verdict: Watch,
		title: "They can change or end the service",
		find: []rx{
			{re(near(near(they, `\b(?:modify|change|suspend|discontinue|stop|remove|interrupt|terminate|cease|shut down|withdraw|limit)\b`, 4), `(?:services?|features?|functionality|any part|platform|website|site|apps?|products?|offerings?)\b`, 6)), 1.5, notYou},
			{re(atWill + `|without (?:any )?liability to you|we (?:are|will) not (?:be )?liable`), 1.5, nil},
		},
		need: 3,
		head: re(`changes?|modif|availability|services?|discontinu`),
		not:  re(near(`suspend|terminate|disable|close`, `your (?:account|access)`, 3)),
		says: func(m *match) string {
			s := "They can change the service, or shut it down, in whole or in part"
			if re(`at any time`).MatchString(m.text) {
				s += ", at any time"
			}
			if re(`without (?:prior |advance |any )?notice`).MatchString(m.text) {
				s += ", without notice"
			}
			return s + "."
		},
		ask:   re(`\b(?:shut (?:it )?down|discontinu\w*|go away|stop (?:offering|working)|remove features?|features?)\b`),
		about: "changes to the service",
	},
	{
		id: "by-using", group: Changes, verdict: Watch,
		title: "Using it means you agree",
		find: []rx{
			{re(near(`by (?:accessing|using|visiting|browsing|downloading|installing|continuing to use|registering|signing up|creating an account|clicking)`, `(?:you )?(?:agree|accept|consent|acknowledge|are bound|indicate your acceptance)`, 12)), 2, nil},
			{re(`(?:continued|continuing) (?:use|access)\W+(?:[\w'-]+\W+){0,12}?(?:constitutes|means|signifies|deemed|indicates|shall be|will be)\W+(?:[\w'-]+\W+){0,6}?(?:accept|agree|consent)`), 2, nil},
			{re(near(`if you do not agree`, `(?:do not|not|must not|may not|stop|discontinue) (?:use|access|using)`, 10)), 1, nil},
		},
		need: 2,
		head: re(`accept|agree|introduction|welcome|overview`),
		says: func(m *match) string {
			return "Just using the site or app counts as agreeing to all of this; you never have to click \"I agree\"."
		},
		ask:   re(`\b(?:agree\w*|accept\w*|bound|signed up|did i agree|never clicked)\b`),
		about: "how you agree to the terms",
	},
	{
		id: "assignment", group: Changes, verdict: Know,
		title: "They can hand the agreement to another company",
		find: []rx{
			{re(near(`\b(?:we|company|it|[\w&.'-]+) (?:may|can) (?:freely )?`, `(?:assign|transfer|delegate)\W+(?:[\w'-]+\W+){0,6}?(?:terms|agreement|rights|obligations)`, 3)), 2, nil},
			{re(near(`you (?:may not|cannot|shall not|can't|will not)`, `assign|transfer`, 3)), 1, nil},
		},
		need: 2,
		head: re(`assign|transfer|general|miscellaneous`),
		says: func(m *match) string {
			return "They can hand this agreement, and your account with it, to another company; you can't."
		},
		ask:   re(`\b(?:assign\w*|transfer (?:my )?account|another company)\b`),
		about: "handing the agreement on",
	},

	// Using it.
	{
		id: "rules", group: Using, verdict: Know,
		title: "Rules on what you may do",
		find: []rx{
			{re(near(`\byou (?:agree )?(?:not to|will not|may not|must not|shall not|won't|cannot)|(?:prohibited|not permitted|forbidden) (?:from|to)`, `\b(?:use|post|upload|share|access|copy|reverse[ -]engineer|scrape|harass|impersonate|resell|sell|distribute|interfere|circumvent|create|transmit|collect)`, 6)), 1.5, nil},
			{re(`acceptable use|prohibited (?:uses?|conduct|activities)|code of conduct|community (?:guidelines|standards)|house rules|restrictions on use|user conduct`), 1.5, nil},
		},
		need: 1.5,
		head: re(`acceptable use|prohibited|restrictions|conduct|community|rules|you (?:agree )?not`),
		says: func(m *match) string {
			t := m.all()
			var what []string
			add := func(p, s string) {
				if re(p).MatchString(t) && len(what) < 4 {
					what = append(what, s)
				}
			}
			add(`harass|abuse|threat|bully`, "harass people")
			add(`spam|unsolicited`, "send spam")
			add(`scrap|crawl|spider|robots?|bots?|automated (?:means|access)`, "use bots or scrape it")
			add(`reverse[ -]engineer|decompil|disassembl`, "take its software apart")
			add(`impersonat|false identity|misrepresent`, "pretend to be someone else")
			add(`resell|sublicense|commercial`, "resell it")
			add(`shar\w* (?:your )?(?:account|password|credentials)`, "share your account")
			add(`infring|intellectual property`, "post what isn't yours to post")
			if len(what) == 0 {
				return "There's a list of things you mustn't do; breaking it can cost you your account."
			}
			return "There's a list of things you mustn't do, such as " + list(what) + "; breaking it can cost you your account."
		},
		ask:   re(`\b(?:allowed|not allowed|can i (?:use|post|share|make|sell)|prohibit\w*|rules?|forbidden|banned for|bots?|scrap\w*|vpn|resell)\b`),
		about: "what you may and mayn't do",
	},
	{
		id: "messages", group: Using, verdict: Know,
		title: "They can send you messages",
		find: []rx{
			{re(near(`consent|agree|authori[sz]e|accept`, `(?:to )?(?:receive|send(?:ing)?)\W+(?:[\w'-]+\W+){0,6}?(?:communications|e-?mails?|messages|notifications|text messages|texts|sms|calls|marketing|newsletters|promotional|offers)`, 6)), 2, nil},
			{re(`electronic communications|marketing (?:communications|emails|messages)|opt[ -]?out of (?:marketing|promotional)`), 1, nil},
		},
		need: 2,
		says: func(m *match) string {
			s := "They can email or message you"
			if re(`marketing|promotional|offers|newsletters`).MatchString(m.all()) {
				s += ", including marketing, until you opt out"
			}
			return s + "."
		},
		ask:   re(`\b(?:e-?mails?|spam|newsletters?|marketing|texts?|sms|notifications?|calls?|contact me)\b`),
		about: "messages they send you",
	},
	{
		id: "third-party", group: Using, verdict: Know,
		title: "Other sites and services are on you",
		find: []rx{
			{re(`(?:third[ -]party|other) (?:websites?|sites|services|links|content|apps?|applications|products|resources|platforms)`), 1.5, nil},
			{re(`not responsible|no control|at your own risk|do not endorse|not liable|no responsibility`), 1.5, nil},
		},
		need: 3,
		head: re(`third[ -]part|links|other (?:sites|services)`),
		says: func(m *match) string {
			return "They aren't responsible for other sites or services they link to or work with."
		},
		ask:   re(`\b(?:third[ -]part\w*|links?|other (?:sites|apps|services)|integrations?)\b`),
		about: "other sites and services",
	},
}

// topicByID finds a topic.
func topicByID(id string) *topic {
	for _, t := range catalog {
		if t.id == id {
			return t
		}
	}
	return nil
}

// days is a number of days, in figures or words.
const days = `\b(?:\d{1,3}|one|two|three|five|seven|ten|fourteen|fifteen|twenty|thirty|forty[ -]five|sixty|ninety)\b(?:\s*\(\d{1,3}\))?\s*(?:calendar |business |working )?days?\b`

// find is the first text p matches in s, or "".
func find(s string, p *pattern) string { return p.FindString(s) }

// numberWords are the numbers terms write out.
var numberWords = map[string]string{
	"one": "1", "two": "2", "three": "3", "five": "5", "six": "6", "seven": "7", "ten": "10",
	"twelve": "12", "thirteen": "13", "fourteen": "14", "fifteen": "15", "sixteen": "16",
	"seventeen": "17", "eighteen": "18", "nineteen": "19", "twenty": "20", "twenty-one": "21",
	"twenty one": "21", "twenty-four": "24", "twenty four": "24", "thirty": "30",
	"forty-five": "45", "forty five": "45", "forty-eight": "48", "forty eight": "48",
	"sixty": "60", "ninety": "90",
}

var leadingNumber = re(`^(\d{1,3}|twenty[ -](?:one|four)|forty[ -](?:five|eight)|[a-z]+)`)

// plainNumber is a number in figures: "thirty" is 30.
func plainNumber(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, ok := numberWords[s]; ok {
		return n
	}
	return s
}

// plainSpan is a span of time in figures: "thirty (30) days" is 30 days,
// "one (1) year" is a year.
func plainSpan(s string) string {
	s = strings.ToLower(squash(s))
	m := leadingNumber.FindString(s)
	n := plainNumber(m)
	unit := ""
	for _, u := range []string{"business day", "working day", "calendar day", "day", "hour", "month", "year"} {
		if strings.Contains(s, u) {
			unit = u
			break
		}
	}
	if unit == "" {
		return s
	}
	if n == "1" {
		switch unit {
		case "hour":
			return "an hour"
		}
		return "a " + unit
	}
	return n + " " + unit + "s"
}

// plainDays is a number of days in figures.
func plainDays(s string) string { return plainSpan(s) }

// money tidies an amount: "US $100" is $100.
func money(s string) string {
	s = squash(s)
	s = strings.ReplaceAll(s, "$ ", "$")
	for _, p := range []string{"USD ", "US ", "USD"} {
		if strings.HasPrefix(s, p+"$") {
			s = strings.TrimPrefix(s, p)
		}
	}
	return s
}

// list joins words in a sentence: a, b and c.
func list(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

var (
	lawsOf   = re(`laws?\s+(?:in force\s+)?of\s+(?:the\s+)?((?:state|commonwealth|province|republic|kingdom|federal republic|grand duchy)\s+of\s+(?:the\s+)?)?([A-Za-z][A-Za-z.'-]*(?:\s+[A-Za-z][A-Za-z.'-]*){0,4})`)
	courtsIn = re(`\b(?:courts?|tribunals?)\b(?:\s+(?:of|for|located|sitting|situated|based|having jurisdiction))*\s+(?:in|within|of|for)\s+(?:the\s+)?((?:state|county|city|province|republic|commonwealth|district)\s+of\s+(?:the\s+)?)?([A-Za-z][A-Za-z.'-]*(?:,?\s+[A-Za-z][A-Za-z.'-]*){0,7})`)
	// stopPlace are words a place's name doesn't go on past.
	stopPlace = map[string]bool{
		"without": true, "excluding": true, "exclusive": true, "applicable": true, "regardless": true,
		"notwithstanding": true, "that": true, "which": true, "as": true, "in": true, "to": true,
		"except": true, "other": true, "its": true, "any": true, "shall": true, "will": true,
		"apply": true, "applies": true, "govern": true, "governs": true, "for": true, "and": true,
		"or": true, "with": true, "the": true, "you": true, "your": true, "we": true, "our": true,
		"if": true, "is": true, "are": true, "be": true, "by": true, "under": true, "unless": true,
		"including": true, "located": true, "having": true, "giving": true, "conflict": true,
		"conflicts": true, "choice": true, "principles": true, "rules": true, "provisions": true,
		"also": true, "both": true, "federal": true, "state": true, "courts": true, "court": true,
		"each": true, "all": true, "such": true, "from": true, "at": true, "on": true, "of": true,
	}
)

// lawPlace is the place whose laws a clause names, such as the State of
// California, or two: the United States and the State of California.
func lawPlace(s string) string {
	var places []string
	for _, m := range lawsOf.FindAllStringSubmatch(s, 3) {
		p := placeName(m[1], m[2])
		if p != "" && !slices.Contains(places, p) && len(places) < 2 {
			places = append(places, p)
		}
	}
	return strings.Join(places, " and ")
}

// courtPlace is where a clause says court cases happen.
func courtPlace(s string) string {
	m := courtsIn.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return placeName(m[1], m[2])
}

func placeName(kind, name string) string {
	fs := strings.Fields(name)
	var keep []string
	for i, f := range fs {
		w := strings.ToLower(strings.Trim(f, ",.;:'\""))
		prev := ""
		if i > 0 {
			prev = strings.ToLower(strings.Trim(fs[i-1], ",.;:'\""))
		}
		// England and Wales, the City and County of San Francisco: an "and"
		// between two names stays, as does "of" after a kind of place.
		if w == "and" && i > 0 && i+1 < len(fs) && isCap(fs[i+1]) && isCap(fs[i-1]) && !capitals(name) {
			keep = append(keep, "and")
			continue
		}
		if w == "of" && placeKinds[prev] && i+1 < len(fs) {
			keep = append(keep, "of")
			continue
		}
		if stopPlace[w] && !(w == "state" && prev == "and") {
			break
		}
		keep = append(keep, strings.TrimRight(f, ".;:"))
		// A full stop ends the sentence, and the place with it, unless it
		// is an initial's, as in D.C.
		if strings.HasSuffix(f, ".") && len(strings.Trim(f, ".")) > 1 && !strings.Contains(strings.Trim(f, "."), ".") {
			break
		}
		if strings.HasSuffix(f, ",") && (i+1 >= len(fs) || stopPlace[strings.ToLower(strings.Trim(fs[i+1], ",.;:'\""))]) {
			break
		}
	}
	place := strings.TrimRight(strings.Join(keep, " "), ", ")
	if place == "" {
		return ""
	}
	if capitals(place) {
		place = titleCase(place)
	}
	for _, p := range []string{"City and County of ", "County of ", "City of "} {
		place = strings.TrimPrefix(place, p)
	}
	if !isCap(place) {
		return ""
	}
	k := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(squash(kind)), " the"), " of")
	if k != "" && k != "city" && k != "county" {
		// The State of California; a city's or county's courts go by the
		// place's own name.
		place = "the " + titleCase(k) + " of " + place
	} else if theNames.MatchString(place) {
		place = "the " + place
	}
	return place
}

// placeKinds are words a place's name goes on past "of" after: the State
// of New York, the District of Columbia.
var placeKinds = map[string]bool{"state": true, "county": true, "city": true, "district": true, "province": true, "commonwealth": true, "republic": true, "kingdom": true, "isle": true, "duchy": true}

// theNames are places whose names take "the": the United States, the
// Netherlands.
var theNames = lazy(`^(?:United|Netherlands|Philippines|Bahamas|Czech Republic|Republic|Kingdom|Commonwealth|Federal|People's|Russian Federation|Dominican|Gambia|Maldives|Seychelles|Isle of|Grand Duchy)`)

func isCap(s string) bool {
	for _, r := range s {
		return r >= 'A' && r <= 'Z'
	}
	return false
}

// titleCase is s in title case: NEW YORK is New York.
func titleCase(s string) string {
	fs := strings.Fields(strings.ToLower(s))
	for i, f := range fs {
		if i > 0 && smallWords[f] {
			continue
		}
		fs[i] = strings.ToUpper(f[:1]) + f[1:]
	}
	return strings.Join(fs, " ")
}
