package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/agent"
)

func TestAgentNavigationShowsWorkBeforeAnOlderPause(t *testing.T) {
	book := &agent.Book{Jobs: []agent.Job{
		{ID: 1, State: agent.Working, Target: agent.Target{Title: "Successioni"}},
		{ID: 2, State: agent.Paused, Target: agent.Target{Title: "Limiti"}},
	}}
	model := Model{tab: editorTab, width: 120, height: 42, agents: agentUI{
		book: book, running: map[int]agentRun{1: {token: 4, preparing: true}},
	}}
	status, _ := model.agentButton()
	if status = ansi.Strip(status); !strings.Contains(status, "Legge le fonti · Successioni") {
		t.Fatal(status)
	}
	model, _ = model.update(agentPrepared{id: 1, token: 3})
	if !model.agents.running[1].preparing {
		t.Fatal("a stopped attempt changed the current phase")
	}
	model, _ = model.update(agentPrepared{id: 1, token: 4})
	if model.agents.running[1].preparing {
		t.Fatal("the current attempt remained in the source phase")
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyShiftTab})
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if model.tab != activityTab || model.agents.detail != 1 || model.commandMode {
		t.Fatal("the navigation shortcut did not open the active job")
	}

	model.tab = editorTab
	_, button := model.agentButton()
	model.mouse(tea.MouseEvent{X: button.x, Y: button.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if model.tab != activityTab || model.agents.detail != 1 {
		t.Fatal("clicking the agent status did not open the active job")
	}
}

func TestAgentPauseRequiresPAndResumeRestoresThePendingQuestion(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".notesflow"), 0700); err != nil {
		t.Fatal(err)
	}
	book, err := agent.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	question := &agent.Question{Text: "Quale esempio?"}
	if err := book.Store(agent.Job{State: agent.Waiting, Result: agent.Draft{Question: question}, Deadline: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	model := Model{ctx: t.Context(), tab: activityTab, agents: agentUI{book: book, detail: 1}}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if job, _ := book.Lookup(1); job.State != agent.Waiting {
		t.Fatal("Space paused a delegation")
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	book, err = agent.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := book.Lookup(1)
	if job.State != agent.Paused || job.PauseReason != agent.ManualPause || !job.Deadline.IsZero() {
		t.Fatalf("wrong saved pause: %+v", job)
	}
	model.agents.book = book
	model, cmd := model.key(tea.KeyMsg{Type: tea.KeyEnter})
	job, _ = model.agents.book.Lookup(1)
	if cmd != nil || job.State != agent.Waiting || job.PauseReason != "" || job.Result.Question == nil || job.Result.Question.Text != question.Text {
		t.Fatalf("resume should restore the pending question: %+v", job)
	}
}

func TestActivityShowsTypesStatesSourceGroupsAndLatestEventFirst(t *testing.T) {
	now := time.Now()
	tokens := 75_000
	delegation := agent.Job{
		ID: 5, Created: now, Target: agent.Target{File: "capitoli/pipeline.tex", Title: "Pipelining", Line: 17},
		Write: true, State: agent.Completed, Goal: "Spiega gli hazard.",
		Sources: []agent.Ref{{File: "roadmap.md"}, {File: "fonti/introduzione.pdf"}, {File: "fonti/esempi.pdf", First: 12, Last: 35}},
		Events: []agent.Event{
			{Title: "Lettura delle fonti", State: agent.Completed, Started: now.Add(-5 * time.Minute), Finished: now, Tokens: &tokens},
			{Title: "Applicazione agli appunti", State: agent.Completed, Started: now, Finished: now.Add(12 * time.Second)},
		},
	}
	book := &agent.Book{Jobs: []agent.Job{
		{ID: 1, Created: now, State: agent.Working, Kind: agent.GraphActivity, Target: agent.Target{Title: "Analisi del grafo"}},
		{ID: 2, Created: now, State: agent.Failed, Kind: agent.SummaryActivity, Target: agent.Target{Title: "Riepilogo TODO"}},
		{ID: 3, Created: now, State: agent.Waiting, Goal: "Come si calcola il CPI?"},
		{ID: 4, Created: now, State: agent.Paused, Goal: "Sposta la figura", Todo: &agent.TodoWork{}},
		delegation,
	}}
	model := Model{tab: activityTab, width: 120, height: 42, agents: agentUI{book: book}}
	items := model.activityRows(78, 12)
	if len(items) != 5 {
		t.Fatalf("got %d activities", len(items))
	}
	list := ansi.Strip(strings.Join(items, "\n"))
	for _, text := range []string{"✦", "Pipelining", "Riepilogo TODO", "Analisi del grafo", "Come si calcola il CPI?", "TODO-AI", "Completato", "In corso", "In attesa", "Errore", now.Format("15:04")} {
		if !strings.Contains(list, text) {
			t.Fatalf("missing %q in %s", text, list)
		}
	}
	detail := ansi.Strip(strings.Join(model.agentDetailLines(delegation, 76), "\n"))
	previous := -1
	for _, text := range []string{"Richiesta", "Spiega gli hazard.", "Destinazione", "capitoli/pipeline.tex · riga 17",
		"Fonti · 3", "introduzione.pdf · PDF intero", "esempi.pdf · pagine 12–35", "Sorgenti e note", "roadmap.md",
		"Cronologia", "Applicazione agli appunti", "12 s", "Lettura delle fonti", "5 min 00 s", "75.000 token"} {
		index := strings.Index(detail, text)
		if index <= previous {
			t.Fatalf("out-of-order or missing %q in %s", text, detail)
		}
		previous = index
	}
	narrow := ansi.Strip(strings.Join(model.activityRows(32, 12), "\n"))
	if !strings.Contains(narrow, "In attesa") || !strings.Contains(narrow, "Completato") {
		t.Fatal(narrow)
	}
}

func TestAgentResultKeepsBothPhasesWhenPreparedMessageArrivesLate(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".notesflow"), 0700); err != nil {
		t.Fatal(err)
	}
	book, err := agent.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	job := agent.Job{State: agent.Working}
	job.BeginEvent("Lettura delle fonti", now.Add(-time.Second))
	if err := book.Store(job); err != nil {
		t.Fatal(err)
	}
	sourceTokens, answerTokens := 720, 180
	model := Model{agents: agentUI{book: book, running: map[int]agentRun{1: {token: 2, preparing: true}}}}
	report := agentPrepared{id: 1, token: 2, at: now, tokens: &sourceTokens}
	model.finishAgent(agentResult{id: 1, token: 2, prepared: report, at: now.Add(time.Second),
		tokens: &answerTokens, result: agent.Draft{Summary: "Risposta"}})
	model.markAgentPrepared(report)
	reopened, err := agent.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	job, _ = reopened.Lookup(1)
	if job.State != agent.Answered || len(job.Events) != 2 {
		t.Fatalf("unexpected state or timeline: %+v", job)
	}
	for i, want := range []int{sourceTokens, answerTokens} {
		if event := job.Events[i]; event.State != agent.Completed || event.Tokens == nil ||
			*event.Tokens != want || event.Finished.Sub(event.Started) != time.Second {
			t.Fatalf("unexpected event: %+v", event)
		}
	}
}
