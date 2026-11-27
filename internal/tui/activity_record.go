package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/watcher"
)

func (m *Model) summarizeTasks() tea.Cmd {
	tasks := m.tasks(true)
	m.summarizing, m.showSummary, m.summary = true, true, ""
	title := "Riepilogo TODO"
	filter := "Tutti"
	for _, tab := range todoTabs {
		if tab.kind == m.taskKind {
			filter = tab.label
			break
		}
	}
	job := agent.Job{Course: m.course, Kind: agent.SummaryActivity, State: agent.Working,
		Target: agent.Target{Title: title},
		Goal:   fmt.Sprintf("Riepiloga %d TODO · filtro %s", len(tasks), filter)}
	job.BeginEvent("Elaborazione del riepilogo", time.Now())
	if m.agents.book != nil && m.saveAgent(job) {
		job.ID = m.agents.book.Jobs[len(m.agents.book.Jobs)-1].ID
	}
	return func() tea.Msg {
		ctx, meter := agent.MeasureTokens(m.ctx)
		report, err := m.manager.Summarize(ctx, tasks)
		return summarized{id: job.ID, text: report, err: err, tokens: meter.Take(), at: time.Now()}
	}
}

func (m *Model) finishSummaryActivity(result summarized) {
	if result.id == 0 {
		return
	}
	job, ok := m.agents.book.Lookup(result.id)
	if !ok {
		return
	}
	job.State, job.Result.Summary = agent.Completed, result.text
	if result.err != nil {
		job.State = agent.Failed
		job.Error, job.ErrorHelp = providers.Explain(result.err)
	}
	job.FinishEvent(job.State, result.at, result.tokens, job.Error)
	m.saveAgent(job)
}

func (m *Model) recordGraphActivity(progress watcher.AnalysisActivity) {
	if m.agents.book == nil {
		return
	}
	if !progress.Finished {
		job, exists := m.agents.book.Lookup(m.agents.graphJob)
		if !exists {
			for _, previous := range slices.Backward(m.agents.book.Jobs) {
				if previous.Kind == agent.GraphActivity {
					if previous.State == agent.Failed || previous.State == agent.Paused {
						job = previous
					}
					break
				}
			}
		}
		job.Kind, job.State = agent.GraphActivity, agent.Working
		job.Target.Title = "Analisi del grafo"
		job.Goal = fmt.Sprintf("Aggiorna i collegamenti concettuali delle %d sezioni in coda.", progress.Total)
		job.Error, job.ErrorHelp, job.PauseReason = "", "", ""
		job.BeginEvent("Analisi delle sezioni", progress.At)
		if m.saveAgent(job) {
			if job.ID != 0 {
				m.agents.graphJob = job.ID
			} else {
				m.agents.graphJob = m.agents.book.Jobs[len(m.agents.book.Jobs)-1].ID
				if m.tab == activityTab && m.agents.detail == 0 && !m.agents.log && m.cursor > 0 {
					m.cursor++
				}
			}
		}
		return
	}
	job, ok := m.agents.book.Lookup(m.agents.graphJob)
	if !ok {
		return
	}
	detail := fmt.Sprintf("%d di %d sezioni analizzate", progress.Resolved, progress.Total)
	job.State = agent.Completed
	switch {
	case errors.Is(progress.Err, context.Canceled):
		job.State = agent.Paused
		job.PauseReason = "L’analisi riprenderà quando l’agente sarà libero."
		detail += " · " + job.PauseReason
	case progress.Err != nil:
		job.State = agent.Failed
		job.Error, job.ErrorHelp = providers.Explain(progress.Err)
		detail += " · " + job.Error
	}
	job.FinishEvent(job.State, progress.At, progress.Tokens, detail)
	m.saveAgent(job)
	if job.State == agent.Completed {
		m.agents.graphJob = 0
	}
}

func (m *Model) finishAgentPDF(result compiled) {
	job, ok := m.agents.book.Lookup(result.job)
	if !ok {
		return
	}
	state := agent.Completed
	detail := "Anteprima aggiornata"
	if result.err != nil {
		state = agent.Failed
		detail = strings.SplitN(result.err.Error(), "\n", 2)[0]
	}
	job.FinishEvent(state, time.Now(), nil, detail)
	m.saveAgent(job)
}
