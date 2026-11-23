package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/editor"
	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

var homeActions = []string{"Riprendi", "Nuovo capitolo", "Delega", "Capitoli"}

func (m Model) suggestedFile() string {
	if pos := m.savedPosition(); pos.File != "" {
		return pos.File
	}
	if file := m.mainTeX(); file != "" {
		return file
	}
	for _, file := range m.files() {
		if strings.EqualFold(filepath.Ext(file.file), ".tex") {
			return file.file
		}
	}
	return ""
}

func (m Model) savedPosition() network.Position {
	if m.session == nil {
		return network.Position{}
	}
	return m.session.Position[m.course]
}

func (m Model) homeRows() []row {
	actions := append([]string(nil), homeActions...)
	pos := m.savedPosition()
	switch {
	case pos.File != "":
		actions[0] += fmt.Sprintf("  ·  %s:%d", filepath.Base(pos.File), pos.Line)
	case m.suggestedFile() != "":
		actions[0] = "Apri gli appunti  ·  " + filepath.Base(m.suggestedFile())
	default:
		actions[0] = "Apri gli appunti"
	}
	rows := make([]row, 0, len(actions)+len(m.snapshot.Files))
	for _, action := range actions {
		rows = append(rows, row{text: action})
	}
	for _, node := range m.snapshot.Files {
		if node.Kind == "file" && m.fileInScope(node.File) && strings.EqualFold(filepath.Ext(node.File), ".tex") {
			label := workflow.ChapterName(node.File, node.Summary)
			if state := m.fileAgentStatus(node.File); state != "" {
				label += "  ·  " + state
			} else if node.File == pos.File {
				label += "  ·  Ultimo aperto"
			}
			rows = append(rows, row{text: label, source: source{node.File, node.Line}})
		}
	}
	return rows
}

type positionResult struct {
	editor *editor.Editor
	course string
	pos    network.Position
}

func (m *Model) capturePosition() tea.Cmd {
	if m.editor == nil || m.positionBusy || m.session == nil {
		return nil
	}
	m.positionBusy = true
	e, ctx, course := m.editor, m.ctx, m.editorCourse
	return func() tea.Msg {
		position, _ := e.Position(ctx)
		return positionResult{editor: e, course: course, pos: position}
	}
}

func (m *Model) savePosition(result positionResult) {
	if result.editor != m.editor || result.pos.File == "" || m.session == nil {
		return
	}
	file, err := filepath.Rel(m.root, result.pos.File)
	if err != nil {
		return
	}
	if _, err := workflow.WorkspaceFile(m.root, file); err != nil {
		return
	}
	position := result.pos
	position.File = filepath.ToSlash(file)
	if m.session.Position[result.course] != position {
		m.session.Position[result.course] = position
		m.saveSession()
	}
	if result.course == m.course {
		m.lastFile = position.File
	}
}

type chapterResult struct {
	plan workflow.Chapter
	err  error
}

func (m *Model) createChapter() tea.Cmd {
	plan, root, e, ctx := m.form.chapter, m.root, m.editor, m.ctx
	if m.chapterBusy {
		return nil
	}
	m.chapterBusy = true
	return func() tea.Msg {
		if e != nil {
			main, err := workflow.WorkspaceFile(root, plan.Main)
			if err != nil {
				return chapterResult{plan, err}
			}
			modified, err := e.Modified(ctx, main)
			if err != nil {
				return chapterResult{plan, err}
			}
			if modified {
				return chapterResult{plan, fmt.Errorf("salva %s prima di aggiornarne l’indice", plan.Main)}
			}
			if err := workflow.CreateChapter(root, plan); err != nil {
				return chapterResult{plan, err}
			}
			_ = e.Refresh(ctx, main)
			return chapterResult{plan, nil}
		}
		return chapterResult{plan, workflow.CreateChapter(root, plan)}
	}
}
