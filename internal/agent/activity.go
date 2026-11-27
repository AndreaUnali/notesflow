package agent

import (
	"slices"
	"time"
)

const (
	SummaryActivity = "todo_summary"
	GraphActivity   = "graph"
)

type Event struct {
	Title    string
	State    string
	Started  time.Time
	Finished time.Time
	Tokens   *int   `json:",omitempty"`
	Detail   string `json:",omitempty"`
}

func (job *Job) BeginEvent(title string, now time.Time) {
	job.Events = append(slices.Clone(job.Events), Event{Title: title, State: Working, Started: now})
}

func (job *Job) FinishEvent(state string, now time.Time, tokens *int, detail string) {
	i := len(job.Events) - 1
	if i < 0 || job.Events[i].State != Working {
		return
	}
	job.Events = slices.Clone(job.Events)
	job.Events[i].State = state
	job.Events[i].Finished = now
	job.Events[i].Tokens = tokens
	job.Events[i].Detail = detail
}

func (job Job) StartedAt() time.Time {
	if !job.Created.IsZero() {
		return job.Created
	}
	return job.Updated
}
