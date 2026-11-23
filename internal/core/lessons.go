package core

// Lessons from what agents fail at most (DESIGN §4.11, Raphael), Blitz's
// harness learning made Irie's: the evidence is plain code over the
// journal and the transcripts of the sessions Irie ran, a local model
// drafts at most five one-sentence lessons from it, the user keeps or
// turns down each in Raffi's row of the Arch Agents tab or their own
// terminal, and every session hears the kept ones as it starts. Nothing is
// kept without the user's word, which an agent can't give.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/lessons"
	"github.com/irie-people/irie/internal/localmodel"
)

// lessonsState is the user's lessons. It has a lock of its own, taken
// after c.mu and never while waiting on a model.
type lessonsState struct {
	mu       sync.Mutex
	book     lessons.Book
	drafting bool
	// room says whether the model may be loaded now (modelRoom); a test
	// sets its own.
	room func(context.Context, localmodel.ChatModel) error
}

const (
	// lessonsDays is the stretch a draft reads by default, and
	// lessonsMostDays the longest it may.
	lessonsDays     = 14
	lessonsMostDays = 90
	// lessonsTimeout bounds a draft's model: a few minutes on a laptop.
	lessonsTimeout = 15 * time.Minute
)

// lessonsFile keeps the lessons, for the core after this one.
func (c *Core) lessonsFile() string { return filepath.Join(c.paths.State, "lessons.json") }

// loadLessons reads the lessons a core before this one kept.
func (c *Core) loadLessons() {
	b, err := os.ReadFile(c.lessonsFile())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("lessons", "err", err)
		}
		return
	}
	var book lessons.Book
	if err := json.Unmarshal(b, &book); err != nil {
		c.log.Warn("lessons", "err", err)
		return
	}
	c.lessons.mu.Lock()
	c.lessons.book = book
	c.lessons.mu.Unlock()
}

// saveLessons writes the lessons. The caller holds no lock.
func (c *Core) saveLessons() error {
	c.lessons.mu.Lock()
	b, err := json.Marshal(c.lessons.book)
	c.lessons.mu.Unlock()
	if err == nil {
		err = os.MkdirAll(c.paths.State, 0o700)
	}
	if err == nil {
		err = filetx.WriteFile(c.lessonsFile(), b, 0o600)
	}
	if err != nil {
		c.log.Warn("lessons", "err", err)
	}
	return err
}

// startRoom is the most characters Claude Code passes on whole from a
// SessionStart hook: past it, it saves the reply to a file and passes a
// 2,000-character preview (DESIGN §4.2, Preferences).
const startRoom = 10000

// withLessons is reply, what a session hears as it starts, then the
// lessons the user kept, oldest first, as many as fit within startRoom, so
// that they never push what comes before out of it. The caller may hold
// c.mu.
func (c *Core) withLessons(reply string) string {
	c.lessons.mu.Lock()
	kept := slices.Clone(c.lessons.book.Kept)
	c.lessons.mu.Unlock()
	for n := len(kept); n > 0; n-- {
		if s := joinNotes(reply, lessons.Note(kept[:n])); utf8.RuneCountInString(s) <= startRoom {
			if n < len(kept) {
				c.log.Warn("lessons: not every kept lesson fits in what a session hears as it starts", "kept", len(kept), "told", n)
			}
			return s
		}
	}
	return reply
}

// lessonsReport is the lessons as they stand.
func (c *Core) lessonsReport() ipc.LessonsResult {
	c.lessons.mu.Lock()
	defer c.lessons.mu.Unlock()
	var book lessons.Book
	if b, err := json.Marshal(c.lessons.book); err == nil {
		json.Unmarshal(b, &book)
	}
	return ipc.LessonsResult{Book: book, Drafting: c.lessons.drafting, Note: lessons.Note(book.Kept)}
}

// lessonsOp is the lessons op (OpLessons) from the process peer.
func (c *Core) lessonsOp(peer int, a ipc.LessonsArgs) (ipc.LessonsResult, error) {
	switch a.Do {
	case ipc.LessonsReport:
		return c.lessonsReport(), nil
	case ipc.LessonsEvidence, ipc.LessonsDraft:
		days := a.Days
		if days == 0 {
			days = lessonsDays
		}
		if days < 1 || days > lessonsMostDays {
			return ipc.LessonsResult{}, ipc.Errorf(ipc.CodeBadArgs, "Lessons read from 1 to %d days of what failed, not %d.", lessonsMostDays, days)
		}
		if a.Do == ipc.LessonsDraft {
			if !a.OK {
				return ipc.LessonsResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s Say OK to it first.", draftAsk(days))
			}
			asked, _ := c.agentOf(peer)
			if err := c.draftLessons(days, a.Model, a.Journal, asked); err != nil {
				return ipc.LessonsResult{}, err
			}
			return c.lessonsReport(), nil
		}
		now := time.Now()
		ev, err := lessons.Gather(c.paths.Journal(), now.Add(-time.Duration(days)*24*time.Hour), now, !a.Journal)
		if err != nil {
			return ipc.LessonsResult{}, err
		}
		res := c.lessonsReport()
		res.Evidence = &ev
		return res, nil
	case ipc.LessonsKeep, ipc.LessonsNo, ipc.LessonsDrop:
		if err := c.lessonsWord(peer); err != nil {
			return ipc.LessonsResult{}, err
		}
		st := &c.lessons
		st.mu.Lock()
		var err error
		switch a.Do {
		case ipc.LessonsKeep:
			if err = st.book.Pending(a.Lessons, a.Of); err == nil && !a.OK {
				err = errors.New(keepAsk(st.book, a.Lessons) + "Say OK to it first.")
				break
			}
			if err == nil {
				err = st.book.Keep(a.Lessons, a.Of, time.Now())
			}
		case ipc.LessonsNo:
			err = st.book.No(a.Lessons, a.Of, time.Now())
		default:
			err = st.book.Drop(a.Lessons)
		}
		st.mu.Unlock()
		if err != nil {
			code := ipc.CodeBadArgs
			switch {
			case errors.Is(err, lessons.ErrNotFound):
				code = ipc.CodeNotFound
			case errors.Is(err, lessons.ErrStale):
				code = ipc.CodeStale
			}
			return ipc.LessonsResult{}, ipc.Errorf(code, "%s", asSentence(err.Error()))
		}
		if err := c.saveLessons(); err != nil {
			return ipc.LessonsResult{}, err
		}
		c.log.Info("lessons: the user's word", "do", a.Do, "lessons", a.Lessons)
		return c.lessonsReport(), nil
	}
	return ipc.LessonsResult{}, ipc.Errorf(ipc.CodeBadArgs, "The lessons op does %s, not %q.", strings.Join([]string{ipc.LessonsEvidence, ipc.LessonsDraft, ipc.LessonsKeep, ipc.LessonsNo, ipc.LessonsDrop}, ", "), a.Do)
}

// asSentence is an error as a sentence: a capital first and a stop at the
// end.
func asSentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// lessonsWord says whether the process peer may give the user's word on
// the lessons: the user, outside any agent, or the session that asked for
// the latest draft within arch.gabriel.word of a message the user typed
// there, as a council's session may relay theirs on its plans.
func (c *Core) lessonsWord(peer int) error {
	sess, inAgent := c.agentOf(peer)
	if !inAgent {
		return nil
	}
	how := "with `" + brand.Slug + " ctl lessons keep N`, `no N` or `drop N` in their own terminal, or in Raffi's row of the Arch Agents tab"
	c.lessons.mu.Lock()
	asked := c.lessons.book.Asked
	c.lessons.mu.Unlock()
	if sess == "" || sess != asked {
		return ipc.Errorf(ipc.CodeBadArgs, "An agent can't pick lessons: they are your user's to pick, %s.", how)
	}
	word := c.cfg().Arch.Gabriel.Word
	c.mu.Lock()
	var said time.Time
	if x := c.sessions[sess]; x != nil {
		said = x.userWord
	}
	c.mu.Unlock()
	if said.IsZero() || time.Since(said) >= word {
		return ipc.Errorf(ipc.CodeBadArgs, "Only your user's own word picks a lesson, and they have not messaged you here in the last %s: ask them, and they answer %s.", spokenFor(word), how)
	}
	return nil
}

// draftAsk is what the person reads before a draft starts.
func draftAsk(days int) string {
	return fmt.Sprintf("A local model on this computer reads what your agents' calls failed at over the last %d days and drafts up to %d lessons, for you to keep or turn down. It costs no tokens, and takes a few minutes and a few GB of memory.", days, lessons.MostDrafted)
}

// keepAsk is what the person reads before they keep lessons: their words,
// as every session will hear them.
func keepAsk(b lessons.Book, ns []int) string {
	var texts []string
	for _, n := range ns {
		if n >= 1 && n <= len(b.Drafts) {
			texts = append(texts, "- "+b.Drafts[n-1].Text)
		}
	}
	return "Every session hears this as it starts, until you drop it:\n\n" + strings.Join(texts, "\n") + "\n\n"
}

// draftLessons has a local model draft lessons from the evidence of the
// last days, in the background, and keeps them waiting for the user's
// word; asked is the session that asked for them, "" for the user. One
// draft runs at a time.
func (c *Core) draftLessons(days int, model string, journalOnly bool, asked string) error {
	st := &c.lessons
	st.mu.Lock()
	if st.drafting {
		st.mu.Unlock()
		return ipc.Errorf(ipc.CodeBusy, "A draft is under way: its lessons show in `%s ctl lessons` and Raffi's row when it is done.", brand.Slug)
	}
	st.drafting = true
	kept := st.book.Texts()
	st.mu.Unlock()
	cfg := c.cfg()
	go func() {
		now := time.Now()
		ev, err := lessons.Gather(c.paths.Journal(), now.Add(-time.Duration(days)*24*time.Hour), now, !journalOnly)
		read := err == nil
		var drafts []lessons.Lesson
		var by string
		if err == nil && len(ev.Kinds) == 0 {
			err = lessons.ErrNothing
		}
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), lessonsTimeout)
			var m localmodel.ChatModel
			if m, err = localmodel.FindChatModel(ctx, localmodel.ChatServers(cfg.Local.URL, cfg.Local.Port), model); err == nil {
				by = m.Name
				room := st.room
				if room == nil {
					room = modelRoom
				}
				if err = room(ctx, m); err == nil {
					drafts, err = lessons.Draft(ctx, m, ev, kept, now)
				}
			}
			cancel()
		}
		st.mu.Lock()
		switch {
		case err == nil:
			st.book.Take(drafts, ev, by, asked, now)
		case read:
			st.book.Watch(ev)
			fallthrough
		default:
			st.book.Problem = asSentence(err.Error())
		}
		st.drafting = false
		st.mu.Unlock()
		c.saveLessons()
		c.log.Info("lessons: drafted", "lessons", len(drafts), "by", by, "kinds", len(ev.Kinds), "transcripts", ev.Transcripts, "err", err)
	}()
	return nil
}

// modelRoom says whether the local model m may be loaded now to draft:
// it is loaded already, or the memory free now holds it with room to spare
// and the disk has room for swap (localmodel.RoomToLoad). A 9B model loaded
// into a laptop short of both brought it down with a kernel panic, every
// session with it.
func modelRoom(ctx context.Context, m localmodel.ChatModel) error {
	o := localmodel.Ollama{URL: m.URL}
	if running, err := o.Running(ctx); err == nil {
		for _, l := range running {
			if localmodel.SameModel(l.Name, m.Name) {
				return nil
			}
		}
	}
	models, err := o.Models(ctx)
	if err != nil {
		return err
	}
	var size int64
	for _, x := range models {
		if localmodel.SameModel(x.Name, m.Name) {
			size = x.Size
		}
	}
	info, err := o.Show(ctx, m.Name)
	if err != nil {
		return err
	}
	mac, err := localmodel.ReadMachine()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	free, err := localmodel.FreeDisk(home)
	if err != nil {
		return err
	}
	if err := localmodel.RoomToLoad(localmodel.Estimate(size, info, lessons.Context, "f16", 1), mac, free); err != nil {
		return fmt.Errorf("%w, so %s was not loaded: draft again once there is room", err, m.Name)
	}
	return nil
}

func init() {
	// The lessons are Raffi's: he heals what breaks.
	e := archTable["raphael"]
	look := e.now
	e.now = func(c *Core) archNow {
		n := look(c)
		c.lessonsNow(&n)
		return n
	}
	archTable["raphael"] = e
}

// lessonsNow adds the lessons to Raffi's row in the arch op: Draft
// lessons, an item for each draft waiting for the user's word, with Keep
// and No, and one for each kept lesson, with what came of it and Drop.
func (c *Core) lessonsNow(n *archNow) {
	r := c.lessonsReport()
	b := r.Book
	waiting := b.Waiting()
	if r.Drafting {
		n.items = append(n.items, ipc.ArchItem{Text: "Drafting lessons from what your agents failed at…", Level: ipc.ArchNote})
	} else {
		draft, _ := json.Marshal(ipc.LessonsArgs{Do: ipc.LessonsDraft})
		n.actions = append(n.actions, ipc.ArchAction{Label: "Draft lessons", Op: ipc.OpLessons, Args: draft, Ask: draftAsk(lessonsDays),
			Done: "Raffi is drafting lessons: they show here when they are done."})
	}
	if b.Problem != "" && !r.Drafting {
		n.items = append(n.items, ipc.ArchItem{Text: "The last draft of lessons brought none", Detail: b.Problem, Level: ipc.ArchWarn})
	}
	for _, l := range waiting {
		keep, _ := json.Marshal(ipc.LessonsArgs{Do: ipc.LessonsKeep, Lessons: []int{l.N}, Of: b.Drafted})
		no, _ := json.Marshal(ipc.LessonsArgs{Do: ipc.LessonsNo, Lessons: []int{l.N}, Of: b.Drafted})
		n.items = append(n.items, ipc.ArchItem{
			Text:   fmt.Sprintf("Lesson %d: %s", l.N, l.Text),
			Detail: lessons.Evidenced(l),
			Level:  ipc.ArchNote,
			Actions: []ipc.ArchAction{
				{Label: "Keep", Op: ipc.OpLessons, Args: keep, Ask: keepAsk(b, []int{l.N}), Done: "Every session hears it from its next start."},
				{Label: "No", Op: ipc.OpLessons, Args: no, Done: "Raffi lets it go."},
			},
		})
	}
	for _, l := range b.Kept {
		drop, _ := json.Marshal(ipc.LessonsArgs{Do: ipc.LessonsDrop, Lessons: []int{l.N}})
		n.items = append(n.items, ipc.ArchItem{
			Text:    "Every session hears: " + l.Text,
			Detail:  lessons.CameOf(l),
			Level:   ipc.ArchOK,
			Actions: []ipc.ArchAction{{Label: "Drop", Op: ipc.OpLessons, Args: drop, Done: "Sessions stop hearing it from their next start."}},
		})
	}
}
