// Package agent keeps the record of delegated changes and human decisions.
package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AndreaUnali/notesflow/internal/storage"
)

const (
	Working   = "In corso"
	Waiting   = "Domanda"
	Paused    = "In pausa"
	Ready     = "Da applicare"
	Review    = "Da verificare"
	Completed = "Completata"
	Answered  = "Risposta"
	Failed    = "Errore"

	ManualPause  = "Pausa richiesta da Attività."
	sessionEnded = "La sessione precedente si è chiusa durante la richiesta."
)

type Target struct {
	File    string
	Section string
	Title   string
	Line    int
}

type Ref struct {
	File    string `json:"file"`
	First   int    `json:"first_page,omitempty"`
	Last    int    `json:"last_page,omitempty"`
	Heading string `json:"heading,omitempty"`
}

func (r Ref) String() string {
	switch {
	case r.First == 0:
		return r.File
	case r.First == r.Last:
		return fmt.Sprintf("%s:%d", r.File, r.First)
	default:
		return fmt.Sprintf("%s:%d-%d", r.File, r.First, r.Last)
	}
}

type Material struct {
	Ref
	Text string `json:"text"`
}

type Question struct {
	Text        string   `json:"text"`
	Choices     []string `json:"choices"`
	Recommended int      `json:"recommended"`
	Why         string   `json:"why"`
}

type Visual struct {
	Location    string `json:"location"`
	Description string `json:"description"`
}

type Draft struct {
	Summary          string    `json:"summary"`
	Text             string    `json:"replacement"`
	Visuals          []Visual  `json:"visuals"`
	Question         *Question `json:"question"`
	References       []string  `json:"references"`
	QuestionFallback bool      `json:"question_fallback,omitempty"`
}

type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type Job struct {
	ID           int
	Kind         string    `json:",omitempty"`
	Created      time.Time `json:",omitzero"`
	Events       []Event   `json:",omitempty"`
	Course       string
	Target       Target
	Goal         string
	Write        bool
	Sources      []Ref
	History      []Turn
	Original     string
	Result       Draft
	State        string
	Error        string
	PauseReason  string `json:",omitempty"`
	ErrorHelp    string `json:",omitempty"`
	Deadline     time.Time
	Updated      time.Time
	Decision     string
	DecisionWhy  string
	Automatic    bool
	Applied      int
	Buffered     bool
	BufferedHash string
	Compilation  string
	Todo         *TodoWork `json:",omitempty"`
}

type Request struct {
	Target       Target     `json:"target"`
	Goal         string     `json:"goal"`
	Write        bool       `json:"write"`
	Original     string     `json:"original"`
	Sources      []Material `json:"sources"`
	History      []Turn     `json:"history"`
	Decision     string     `json:"decision,omitempty"`
	Compilation  string     `json:"compilation,omitempty"`
	CanAsk       bool       `json:"can_ask"`
	ReadingBytes int        `json:"reading_bytes,omitempty"`
	Todo         bool       `json:"todo_ai,omitempty"`
}

type Backend interface {
	Guide(context.Context, Request) (Draft, error)
}

type Book struct {
	path string
	Jobs []Job
}

func Open(root string) (*Book, error) {
	book := &Book{path: filepath.Join(root, ".notesflow", "agent.json")}
	if err := storage.Load(book.path, &book.Jobs); err != nil {
		return nil, err
	}
	for i := range book.Jobs {
		job := &book.Jobs[i]
		job.FinishEvent(Paused, time.Time{}, nil, sessionEnded)
		if job.State == Working {
			job.State = Paused
			job.PauseReason = sessionEnded
			job.Result.Question = nil
		}
		if job.State == Paused && job.PauseReason == "" {
			switch job.Error {
			case "Riprendi la richiesta da Attività.":
				job.PauseReason, job.Error = sessionEnded, ""
			case "":
				job.PauseReason = ManualPause
			}
		}
	}
	return book, nil
}

func (b *Book) Lookup(id int) (Job, bool) {
	for _, j := range b.Jobs {
		if j.ID == id {
			return j, true
		}
	}
	return Job{}, false
}

func (b *Book) Store(j Job) error {
	next := slices.Clone(b.Jobs)
	j.Updated = time.Now()
	i := slices.IndexFunc(next, func(other Job) bool { return other.ID == j.ID })
	if i < 0 {
		j.ID = 1
		if j.Created.IsZero() {
			j.Created = j.Updated
		}
		for _, other := range next {
			j.ID = max(j.ID, other.ID+1)
		}
		next = append(next, j)
	} else {
		next[i] = j
	}
	if err := storage.Save(b.path, next); err != nil {
		return err
	}
	b.Jobs = next
	return nil
}

func (b *Book) RecentSources(course string) []Ref {
	var refs []Ref
	for _, job := range slices.Backward(b.Jobs) {
		if job.Course == course {
			for _, ref := range job.Sources {
				if !slices.Contains(refs, ref) {
					refs = append(refs, ref)
				}
			}
		}
	}
	return refs
}

func (b *Book) ForCourse(course string) []Job {
	var jobs []Job
	for _, job := range b.Jobs {
		if course == "" || job.Course == course || job.Kind == GraphActivity {
			jobs = append(jobs, job)
		}
	}
	slices.SortStableFunc(jobs, func(a, b Job) int { return cmp.Compare(b.ID, a.ID) })
	return jobs
}

// SourceError identifies an error before any model request was sent.
type SourceError struct{ Err error }

func (e *SourceError) Error() string { return e.Err.Error() }
func (e *SourceError) Unwrap() error { return e.Err }

func Run(ctx context.Context, root string, backend Backend, job Job, prepared func()) (Draft, error) {
	materials, err := readMaterials(ctx, root, job.Sources)
	if err != nil {
		return Draft{}, &SourceError{err}
	}
	if err := condenseSources(ctx, backend, job, materials); err != nil {
		return Draft{}, err
	}
	request := Request{
		Target: job.Target, Goal: job.Goal, Write: job.Write, Original: job.Original,
		Sources: materials, History: job.History[max(0, len(job.History)-12):],
		Decision: job.Decision, Compilation: job.Compilation, CanAsk: job.Decision == "" && job.Write,
		Todo: job.Todo != nil,
	}
	if err := ctx.Err(); err != nil {
		return Draft{}, err
	}
	if prepared != nil {
		prepared()
	}
	draft, err := backend.Guide(ctx, request)
	if err != nil {
		return Draft{}, err
	}
	skippedQuestion := draft.Question != nil && !validQuestion(request, draft.Question)
	if job.Write && (draft.Question == nil || skippedQuestion) && !hasEdits(draft) {
		request.CanAsk = false
		draft, err = backend.Guide(ctx, request)
		if err != nil {
			return Draft{}, err
		}
		skippedQuestion = skippedQuestion || draft.Question != nil
	}
	if skippedQuestion {
		draft.Question = nil
	}
	draft.QuestionFallback = skippedQuestion && job.Write
	draft.References = slices.DeleteFunc(draft.References, func(ref string) bool {
		return !slices.ContainsFunc(job.Sources, func(source Ref) bool {
			return ref == source.String() || ref == source.File
		})
	})
	if strings.TrimSpace(draft.Summary) == "" || len(draft.Summary) > 12000 || len(draft.Text) > 64000 || len(draft.Visuals) > 12 {
		return Draft{}, errors.New("risposta dell’agente mancante o troppo lunga")
	}
	if job.Write && draft.Question == nil && !hasEdits(draft) {
		return Draft{}, errors.New("l’agente non ha prodotto una bozza applicabile dopo due tentativi")
	}
	return draft, nil
}

func validQuestion(request Request, q *Question) bool {
	return request.CanAsk && len(q.Choices) >= 2 && len(q.Choices) <= 3 && q.Recommended >= 0 && q.Recommended < len(q.Choices) &&
		strings.TrimSpace(q.Text) != "" && strings.TrimSpace(q.Why) != "" &&
		!slices.ContainsFunc(q.Choices, func(choice string) bool { return strings.TrimSpace(choice) == "" })
}

func hasEdits(draft Draft) bool {
	return strings.TrimSpace(draft.Text) != "" ||
		slices.ContainsFunc(draft.Visuals, func(visual Visual) bool { return strings.TrimSpace(visual.Description) != "" })
}
