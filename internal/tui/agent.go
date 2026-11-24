package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/editor"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type agentUI struct {
	book     *agent.Book
	running  map[int]agentRun
	next     int
	applying int
	detail   int
	offset   int
	notice   agentNotice
	queued   []agentNotice
}

type agentRun struct {
	token     int
	cancel    context.CancelFunc
	preparing bool
}

type agentPrepared struct{ id, token int }

type agentResult struct {
	id, token int
	result    agent.Draft
	err       error
}

type appliedAgent struct {
	id, line int
	inBuffer bool
	err      error
}

type agentContext struct {
	position string
	line     int
	buffer   string
}

func (m *Model) startAgentContext() tea.Cmd {
	if m.editor == nil {
		m.showAgentForm("ask", 0, "", 0)
		return nil
	}
	e, ctx, root := m.editor, m.ctx, m.root
	return func() tea.Msg {
		pos, _ := e.Position(ctx)
		result := agentContext{position: pos.File, line: pos.Line}
		if file, err := filepath.Rel(root, pos.File); err == nil {
			if path, err := workflow.WorkspaceFile(root, file); err == nil {
				if buffer, err := e.Buffer(ctx, path); err == nil && buffer.ID > 0 {
					result.buffer = buffer.Text()
				}
			}
		}
		return result
	}
}

func (m *Model) beginAgent(job agent.Job) tea.Cmd {
	job.State, job.Error, job.PauseReason = agent.Working, "", ""
	job.Result.Question, job.Deadline = nil, time.Time{}
	if !m.saveAgent(job) {
		return nil
	}
	if job.ID == 0 {
		job.ID = m.agents.book.Jobs[len(m.agents.book.Jobs)-1].ID
	}
	if m.agents.notice.id == job.ID {
		m.agents.notice = agentNotice{}
	}
	m.agents.queued = slices.DeleteFunc(m.agents.queued, func(notice agentNotice) bool { return notice.id == job.ID })
	if m.agents.running == nil {
		m.agents.running = make(map[int]agentRun)
	}
	if previous, exists := m.agents.running[job.ID]; exists {
		previous.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.agents.next++
	token := m.agents.next
	preparing := len(job.Sources) > 0
	m.agents.running[job.ID] = agentRun{token: token, cancel: cancel, preparing: preparing}
	root, backend := m.root, m.manager
	prepared := make(chan struct{}, 1)
	work := func() tea.Msg {
		defer cancel()
		defer close(prepared)
		result, err := agent.Run(ctx, root, backend, job, func() { prepared <- struct{}{} })
		return agentResult{id: job.ID, token: token, result: result, err: err}
	}
	if !preparing {
		return work
	}
	return tea.Batch(work, func() tea.Msg {
		if _, ok := <-prepared; ok {
			return agentPrepared{job.ID, token}
		}
		return nil
	})
}

func (m *Model) saveAgent(job agent.Job) bool {
	if m.agents.book == nil {
		m.message = "Registro dell’agente non disponibile"
		return false
	}
	if err := m.agents.book.Store(job); err != nil {
		m.message = "Agente · " + err.Error()
		return false
	}
	return true
}

func (m *Model) finishAgent(result agentResult) {
	run, exists := m.agents.running[result.id]
	if !exists || run.token != result.token {
		return
	}
	delete(m.agents.running, result.id)
	job, ok := m.agents.book.Lookup(result.id)
	if !ok {
		return
	}
	if result.err != nil {
		job.State, job.Error = agent.Failed, result.err.Error()
		if m.saveAgent(job) {
			m.toastAgent(job.ID, "Agente · richiesta interrotta", job.Error, false)
		}
		return
	}
	job.Result = result.result
	job.History = append(slices.Clone(job.History), agent.Turn{Role: "assistant", Text: job.Result.Summary})
	switch {
	case job.Result.Question != nil && job.Write:
		job.State, job.Deadline = agent.Waiting, time.Time{}
	case job.Write:
		job.State = agent.Ready
	default:
		job.State = agent.Answered
	}
	if m.saveAgent(job) && job.State == agent.Answered {
		m.toastAgent(job.ID, "Agente · risposta pronta", job.Result.Summary, false)
	}
}

func (m *Model) advanceAgent(now time.Time) tea.Cmd {
	if m.agents.book == nil {
		return nil
	}
	notice := m.agents.notice
	if notice.id > 0 && !notice.question && now.After(notice.until) {
		m.agents.notice = agentNotice{}
	}
	if m.agents.notice.id == 0 && len(m.agents.queued) > 0 {
		m.agents.notice = m.agents.queued[0]
		m.agents.notice.until = now.Add(6 * time.Second)
		m.agents.queued = m.agents.queued[1:]
	}
	var commands []tea.Cmd
	var hashes map[string]string
	for _, job := range m.agents.book.Jobs {
		if job.Buffered && job.BufferedHash != "" {
			if hashes == nil {
				hashes = m.store.Hashes()
			}
			if current := hashes[job.Target.File]; current != "" && current != job.BufferedHash {
				job.Buffered, job.BufferedHash, job.Error = false, "", ""
				m.saveAgent(job)
			}
		}
		if job.State == agent.Ready && m.agents.applying == 0 && !m.chapterBusy {
			commands = append(commands, m.applyAgent(job))
		}
		if job.State != agent.Waiting || job.Result.Question == nil {
			continue
		}
		if job.Deadline.IsZero() && m.agents.notice.id == 0 && (m.course == "" || job.Course == m.course) {
			job.Deadline = now.Add(m.questionDuration())
			if m.saveAgent(job) {
				m.toastAgent(job.ID, "Agente · domanda", job.Result.Question.Text, true)
			}
		}
		if !job.Deadline.IsZero() {
			if !job.Deadline.After(now) {
				commands = append(commands, m.answerAgent(job, job.Result.Question.Recommended, true))
			} else if m.agents.notice.id == 0 {
				m.toastAgent(job.ID, "Agente · domanda", job.Result.Question.Text, true)
			}
		}
	}
	m.renderNotice(now)
	return tea.Batch(commands...)
}

func (m Model) questionDuration() time.Duration {
	seconds := m.cfg.Agent.QuestionSeconds
	if seconds <= 0 {
		seconds = 90
	}
	return time.Duration(seconds) * time.Second
}

func (m *Model) answerAgent(job agent.Job, index int, automatic bool) tea.Cmd {
	question := job.Result.Question
	if question == nil || index < 0 || index >= len(question.Choices) {
		return nil
	}
	job.Decision, job.DecisionWhy, job.Automatic = question.Choices[index], question.Why, automatic
	job.History = append(slices.Clone(job.History), agent.Turn{Role: "user", Text: job.Decision})
	job.Deadline = time.Time{}
	job.Result.Question = nil
	if m.agents.notice.id == job.ID {
		m.agents.notice = agentNotice{}
	}
	if index == question.Recommended && (strings.TrimSpace(job.Result.Text) != "" || len(job.Result.Visuals) > 0) {
		job.State = agent.Ready
		m.saveAgent(job)
		return nil
	}
	return m.beginAgent(job)
}

func (m *Model) applyAgent(job agent.Job) tea.Cmd {
	m.agents.applying = job.ID
	e, ctx, root := m.editor, m.ctx, m.root
	return func() tea.Msg {
		line, buffer, err := applyAgentToNotes(ctx, root, e, job)
		return appliedAgent{job.ID, line, buffer, err}
	}
}

func applyAgentToNotes(ctx context.Context, root string, e *editor.Editor, job agent.Job) (int, bool, error) {
	if e == nil {
		line, err := agent.Apply(root, job)
		return line, false, err
	}
	path, err := workflow.WorkspaceFile(root, job.Target.File)
	if err != nil {
		return 0, false, err
	}
	for range 3 {
		buffer, err := e.Buffer(ctx, path)
		if err != nil {
			return 0, false, err
		}
		if buffer.ID == 0 {
			line, err := agent.Apply(root, job)
			return line, false, err
		}
		next, line, err := agent.Update(job, buffer.Text())
		if err != nil {
			return 0, false, err
		}
		applied, saved, err := e.ReplaceBuffer(ctx, path, buffer, next)
		if err != nil || applied {
			return line, applied && !saved, err
		}
	}
	return 0, false, agent.ErrConflict
}

func (m *Model) appliedAgent(result appliedAgent) {
	m.agents.applying = 0
	job, ok := m.agents.book.Lookup(result.id)
	if !ok {
		return
	}
	if result.err != nil {
		job.State, job.Error = agent.Review, result.err.Error()
		if m.saveAgent(job) {
			m.toastAgent(job.ID, "Agente · proposta da rivedere", job.Target.Title, false)
		}
		return
	}
	job.State, job.Applied = agent.Completed, result.line
	if result.inBuffer {
		job.Buffered = true
		job.BufferedHash = m.store.Hashes()[job.Target.File]
		job.Error = "Modifiche integrate nel buffer di Neovim. Usa :w per salvarle insieme alle tue."
	}
	if m.saveAgent(job) {
		extra := ""
		if count := len(job.Result.Visuals); count > 0 {
			extra += fmt.Sprintf(" · %d TODO Visual", count)
		}
		if job.Automatic {
			extra += " · 1 TODO Manutenzione"
		}
		m.toastAgent(job.ID, "Agente · delega completata", job.Target.Title+extra, false)
		m.store.Log("Delega completata · " + job.Target.File)
	}
}

func (m *Model) pauseAgent(job agent.Job) {
	if job.State != agent.Working && job.State != agent.Waiting {
		return
	}
	job.State, job.Deadline = agent.Paused, time.Time{}
	job.PauseReason = agent.ManualPause
	if !m.saveAgent(job) {
		return
	}
	if running, ok := m.agents.running[job.ID]; ok {
		running.cancel()
		delete(m.agents.running, job.ID)
	}
	if m.agents.notice.id == job.ID {
		m.agents.notice = agentNotice{}
	}
}

func (m *Model) resumeAgent(job agent.Job) tea.Cmd {
	if job.State == agent.Paused && job.Result.Question != nil && job.Decision == "" {
		job.State, job.PauseReason = agent.Waiting, ""
		m.saveAgent(job)
		return nil
	}
	if job.State == agent.Paused || job.State == agent.Failed {
		return m.beginAgent(job)
	}
	return nil
}

func (m Model) agentTasks() []agent.Job {
	if m.agents.book == nil {
		return nil
	}
	return m.agents.book.ForCourse(m.course)
}

func (m Model) fileAgentStatus(file string) string {
	for _, job := range m.agentTasks() {
		if job.Write && job.Target.File == file {
			switch job.State {
			case agent.Working, agent.Ready:
				return "● Agente al lavoro"
			case agent.Waiting:
				return "◆ Domanda dell’agente"
			case agent.Paused:
				return "◌ Delega in pausa"
			case agent.Review:
				return "! Proposta da verificare"
			}
		}
	}
	return ""
}

// Give questions and work in progress precedence over older paused jobs.
func (m Model) currentAgentJob() agent.Job {
	var current agent.Job
	best := -1
	for _, job := range m.agentTasks() {
		priority := 0
		switch job.State {
		case agent.Waiting:
			priority = 3
		case agent.Working, agent.Ready:
			priority = 2
		case agent.Paused, agent.Failed, agent.Review:
			priority = 1
		}
		if priority > best {
			best, current = priority, job
		}
	}
	return current
}

func (m Model) agentPhase(job agent.Job) string {
	switch job.State {
	case agent.Working:
		if run, ok := m.agents.running[job.ID]; ok {
			if run.preparing {
				return "Legge le fonti"
			}
			return "Attende il modello"
		}
	case agent.Ready:
		if m.agents.applying == job.ID {
			return "Applica agli appunti"
		}
	case agent.Waiting:
		return "Attende una risposta"
	}
	return job.State
}

func (m *Model) openCurrentAgent() tea.Cmd {
	if job := m.currentAgentJob(); job.ID > 0 {
		return m.openAgentActivity(job.ID)
	}
	return m.setTab(activityTab)
}

func (m *Model) openAgentActivity(id int) tea.Cmd {
	cmd := m.setTab(activityTab)
	m.agents.detail, m.agents.offset = id, 0
	if m.agents.notice.id == id && !m.agents.notice.question {
		m.agents.notice = agentNotice{}
	}
	return cmd
}

func (m *Model) agentDetailKey(key string) (tea.Cmd, bool) {
	if m.agents.book == nil || m.tab != activityTab || m.agents.detail == 0 {
		return nil, false
	}
	job, ok := m.agents.book.Lookup(m.agents.detail)
	if !ok {
		return nil, false
	}
	switch key {
	case "esc":
		m.agents.detail, m.agents.offset = 0, 0
	case "up":
		m.agents.offset = max(0, m.agents.offset-1)
	case "down":
		m.agents.offset = min(m.agents.offset+1, max(0, len(m.agentDetailLines(job, m.layout().left-2))-1))
	case "pgup":
		m.agents.offset = max(0, m.agents.offset-max(1, m.layout().body-4))
	case "pgdown":
		m.agents.offset = min(m.agents.offset+max(1, m.layout().body-4), max(0, len(m.agentDetailLines(job, m.layout().left-2))-1))
	case "a", "b", "c":
		if job.State == agent.Waiting {
			return m.answerAgent(job, int(key[0]-'a'), false), true
		}
		return nil, false
	case "P":
		m.pauseAgent(job)
	case "enter":
		// A detail page must not forward Enter to the row behind it.
		return m.resumeAgent(job), true
	case "f":
		m.showAgentForm("followup", job.ID, job.Target.File, job.Target.Line)
	case "s":
		m.showAgentForm("source", job.ID, job.Target.File, job.Target.Line)
	case "e":
		return m.openEditor(source{job.Target.File, max(job.Target.Line, job.Applied)}), true
	case "w":
		if !job.Write && job.State == agent.Answered {
			m.showAgentForm("write", job.ID, job.Target.File, job.Target.Line)
			return nil, true
		}
		return nil, false
	default:
		return nil, false
	}
	return nil, true
}

func (m Model) agentDetailLines(job agent.Job, width int) []string {
	var lines []string
	add := func(text string) {
		for row := range strings.SplitSeq(text, "\n") {
			lines = append(lines, foldLine(clean(row), width)...)
		}
	}
	phase := m.agentPhase(job)
	if !job.Updated.IsZero() {
		last := "Aggiornata alle " + job.Updated.Local().Format("15:04:05")
		if job.State == agent.Working {
			last = "Da " + max(0, time.Since(job.Updated)).Truncate(time.Second).String()
		}
		phase += " · " + last
	}
	add(phase)
	if job.State == agent.Paused {
		if job.PauseReason != "" {
			add(job.PauseReason)
		}
		add("Invio riprende la delega.")
	}
	add(fmt.Sprintf("%s:%d · %s", job.Target.File, max(job.Target.Line, job.Applied), job.Target.Title))
	add(job.Goal)
	if len(job.Sources) > 0 {
		add("Fonti")
		for _, source := range job.Sources {
			add("  " + source.String())
		}
	}
	if job.Decision != "" {
		mark := "Risposta"
		if job.Automatic {
			mark = "Scelta automatica"
		}
		add(mark + " · " + job.Decision + " · " + job.DecisionWhy)
	}
	if job.Error != "" {
		add(job.Error)
	}
	if job.State == agent.Waiting && job.Result.Question != nil {
		q := job.Result.Question
		add("")
		add(q.Text)
		for i, choice := range q.Choices {
			label := fmt.Sprintf("  %c  %s", 'a'+i, choice)
			if i == q.Recommended {
				label += " · consigliata"
			}
			add(label)
		}
		add(q.Why)
	}
	if job.Result.Summary != "" {
		add("")
		add(job.Result.Summary)
	}
	for _, ref := range job.Result.References {
		add("  ↳ " + ref)
	}
	for _, visual := range job.Result.Visuals {
		add("◇ TODO Visual · " + visual.Location + " · " + visual.Description)
	}
	if job.Write && (job.State == agent.Review || job.State == agent.Completed) {
		replacement, err := agent.Replacement(job)
		if err != nil {
			add(err.Error())
			replacement = job.Result.Text
		}
		add("")
		if job.State == agent.Review {
			add("Originale all’avvio")
			add(job.Original)
			if current, err := agent.ReadTarget(m.root, job.Target); err == nil && current != job.Original {
				add("")
				add("Testo attuale")
				add(current)
			}
		}
		add("")
		add("Modifiche proposte")
		add(replacement)
	}
	return lines
}

func (m Model) foldAgentHelp() string {
	job, ok := m.agents.book.Lookup(m.agents.detail)
	if !ok {
		return ""
	}
	switch job.State {
	case agent.Waiting:
		return " a–c Rispondi · P Pausa · f Messaggio · s Fonte · Esc Lista"
	case agent.Answered:
		return " f Continua · w Chiedi di applicare · s Fonte · e Appunti · Esc Lista"
	case agent.Working:
		return " P Pausa · f Messaggio · s Fonte · e Appunti · Esc Lista"
	case agent.Paused:
		return " Invio Riprendi · f Messaggio · s Fonte · e Appunti · Esc Lista"
	case agent.Failed:
		return " Invio Riprova · f Messaggio · s Fonte · e Appunti · Esc Lista"
	default:
		return " ↑↓ Leggi · f Messaggio · s Fonte · e Appunti · Esc Lista"
	}
}

func (m Model) selectedAgentJob() (agent.Job, bool) {
	if m.agents.book == nil || m.agents.detail == 0 {
		return agent.Job{}, false
	}
	return m.agents.book.Lookup(m.agents.detail)
}
