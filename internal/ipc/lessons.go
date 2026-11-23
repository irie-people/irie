package ipc

import (
	"time"

	"github.com/irie-people/irie/internal/lessons"
)

// Lessons from what agents fail at most (DESIGN §4.11, Raphael): evidence
// with no model call, a local model's draft of at most five one-sentence
// lessons from it, and the user's pick, which every session hears as it
// starts.
const (
	// OpLessons is the user's lessons: those kept, the latest draft's
	// waiting for their word, and whether a draft is under way. With Do it
	// reads the evidence, drafts lessons, or takes the user's word to
	// keep, turn down or drop lessons. It answers a LessonsResult.
	OpLessons = "lessons"
)

// What OpLessons does, LessonsArgs' Do.
const (
	LessonsReport   = ""
	LessonsEvidence = "evidence"
	LessonsDraft    = "draft"
	LessonsKeep     = "keep"
	LessonsNo       = "no"
	LessonsDrop     = "drop"
)

// LessonsArgs asks for the user's lessons, or with Do for more: evidence
// reads what failed over Days, and draft, with OK, since it loads a model,
// has a local model, Model or the best for chat on this computer, draft
// lessons from it in the background; with Journal both read the journal
// alone, without the transcripts. keep, with OK, keeps the drafts Lessons
// numbers, no turns them down, and drop stops every session hearing the
// kept lessons Lessons numbers: each the user's word alone.
type LessonsArgs struct {
	Do      string `json:"do,omitempty"`
	Days    int    `json:"days,omitempty"`    // evidence, draft: the stretch; 14 when 0
	Journal bool   `json:"journal,omitempty"` // evidence, draft: the journal alone
	Model   string `json:"model,omitempty"`   // draft: a local model, by its name
	Lessons []int  `json:"lessons,omitempty"` // keep, no: drafts by their numbers; drop: kept lessons by theirs
	// Of is when the draft the numbers come from was made, which keep and
	// no refuse once a newer one replaced it; zero for the latest.
	Of time.Time `json:"of,omitzero"`
	OK bool      `json:"ok,omitempty"` // draft, keep: the person said OK, having read what it does
}

// LessonsResult is the user's lessons (OpLessons), with what Do did.
type LessonsResult struct {
	Book     lessons.Book      `json:"book"`
	Drafting bool              `json:"drafting"`           // a draft is under way
	Evidence *lessons.Evidence `json:"evidence,omitempty"` // evidence: what it read
	Note     string            `json:"note,omitempty"`     // what every session hears of the kept lessons as it starts
}
