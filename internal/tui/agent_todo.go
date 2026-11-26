package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/editor"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

func (m *Model) startAITodo() tea.Cmd {
	if m.manager == nil || m.agents.applying != 0 || len(m.agents.running) != 0 ||
		m.compiling || m.chapterBusy || m.savingNotes || m.quitting {
		return nil
	}
	for _, job := range m.agents.book.Jobs {
		if job.State == agent.Waiting || job.State == agent.Ready {
			return nil
		}
	}
	main := m.mainTeX()
	if main == "" {
		return nil
	}
	for _, task := range m.snapshot.Tasks {
		if task.Kind != parser.AITask || !m.fileInScope(task.File) || !strings.EqualFold(filepath.Ext(task.File), ".tex") {
			continue
		}
		var restart int
		skip := false
		for _, job := range m.agents.book.Jobs {
			if job.Todo == nil || job.Target.File != task.File || job.Todo.Line != task.Line || job.Goal != task.Text {
				continue
			}
			switch {
			case job.State == agent.Completed && !job.Buffered:
			case job.State == agent.Paused && job.PauseReason != agent.ManualPause:
				restart = job.ID
			default:
				skip = true
			}
		}
		if skip {
			continue
		}
		if !m.manager.CanGuide() {
			return nil
		}
		job, err := agent.PrepareTodo(m.root, task, main)
		if errors.Is(err, agent.ErrTodoMoved) {
			continue
		}
		job.Course, job.ID = m.course, restart
		if err != nil {
			job.State, job.Error = agent.Review, err.Error()
			m.saveAgent(job)
			return nil
		}
		return m.beginAgent(job)
	}
	return nil
}

func applyAITodo(ctx context.Context, root, engine string, e *editor.Editor, job agent.Job) (int, bool, error) {
	path, err := workflow.WorkspaceFile(root, job.Target.File)
	if err != nil {
		return 0, false, err
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return 0, false, err
	}
	next, line, err := agent.Update(job, string(before))
	if err != nil {
		return 0, false, err
	}
	check, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := workflow.CheckTeXEdit(check, root, job.Todo.MainTeX, job.Target.File, engine, next); err != nil {
		for _, entry := range strings.Split(err.Error(), "\n") {
			if reason, ok := strings.CutPrefix(entry, "! "); ok {
				return 0, false, errors.New("LaTeX · " + reason)
			}
		}
		return 0, false, errors.New("Verifica del PDF · " + strings.SplitN(err.Error(), "\n", 2)[0])
	}
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if e != nil {
		buffer, err := e.Buffer(ctx, path)
		if err != nil {
			return 0, false, err
		}
		if buffer.ID != 0 {
			if buffer.Modified || buffer.Text() != strings.ReplaceAll(string(before), "\r\n", "\n") {
				return 0, false, agent.ErrConflict
			}
			applied, saved, err := e.ReplaceBuffer(ctx, path, buffer, next)
			if err != nil || !applied {
				if err == nil {
					err = agent.ErrConflict
				}
				return 0, false, err
			}
			return line, !saved, nil
		}
	}
	if err := workflow.ReplaceSource(path, before, []byte(next)); err != nil {
		return 0, false, err
	}
	return line, false, nil
}
