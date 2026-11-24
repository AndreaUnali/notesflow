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
