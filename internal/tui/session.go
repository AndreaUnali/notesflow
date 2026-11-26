package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

// WithSession restores saved choices and prepares setup for ambiguous workspaces.
func (m Model) WithSession() Model {
	session, err := network.LoadSession(m.root)
	if err != nil {
		m.message = err.Error()
	}
	m.session = &session
	book, err := agent.Open(m.root)
	if err != nil {
		m.message = "Attività · " + err.Error()
	}
	m.agents.book = book
	m.setupWorkspace()
	return m
}

func (m Model) WithLaunchNotice(notice string) Model {
	if notice != "" {
		m.message = notice
	}
	return m
}

type workspaceChoice struct {
	kind      string
	options   []string
	selection int
}

func (m *Model) setupWorkspace() {
	if m.session == nil {
		return
	}
	mainFiles := network.MainFiles(m.snapshot.Files)
	if m.cfg.Tools.MainTeX == "" {
		main := m.session.MainTeX
		if _, err := workflow.WorkspaceFile(m.root, main); err != nil {
			main = ""
		}
		switch {
		case main != "":
			m.cfg.Tools.MainTeX = main
		case len(mainFiles) == 1:
			m.cfg.Tools.MainTeX, m.session.MainTeX = mainFiles[0], mainFiles[0]
		case len(mainFiles) > 1:
			m.choice = workspaceChoice{kind: "Documento principale", options: mainFiles}
			return
		}
	}
	if m.cfg.Tools.PDF == "" {
		pdf := m.session.PDF
		if _, err := workflow.WorkspaceFile(m.root, pdf); err != nil {
			pdf = ""
		}
		files := network.PDFs(m.root, mainFiles)
		stem := strings.TrimSuffix(filepath.Base(m.cfg.Tools.MainTeX), ".tex")
		index := slices.IndexFunc(files, func(file string) bool { return filepath.Base(file) == stem+".pdf" })
		switch {
		case pdf != "":
			m.cfg.Tools.PDF = pdf
		case len(files) == 1:
			m.cfg.Tools.PDF, m.session.PDF = files[0], files[0]
		case len(files) > 1:
			m.choice = workspaceChoice{kind: "PDF del progetto", options: files, selection: max(0, index)}
			return
		}
	}
	m.choice = workspaceChoice{}
	if m.course == "" && m.cfg.Tools.MainTeX != "" && !strings.Contains(m.cfg.Tools.MainTeX, "/") {
		m.course = rootCourse
	}
	m.saveSession()
}

func (m *Model) saveSession() {
	if m.session != nil {
		if err := network.SaveSession(m.root, *m.session); err != nil {
			m.message = "Sessione · " + err.Error()
		}
	}
}

func (m Model) workspacePickerOpen() bool {
	return m.tab == homeTab && len(m.choice.options) > 0
}

func (m *Model) workspaceKey(key string) tea.Cmd {
	choice := &m.choice
	switch key {
	case "up":
		choice.selection = max(0, choice.selection-1)
	case "down":
		choice.selection = min(len(choice.options)-1, choice.selection+1)
	case "enter":
		selected := choice.options[choice.selection]
		switch choice.kind {
		case "Documento principale":
			m.session.MainTeX = selected
		case "PDF del progetto":
			m.session.PDF = selected
		}
		m.setupWorkspace()
		m.pdfPage = 1
		return m.probePDF()
	case "esc":
		m.choice = workspaceChoice{}
	case "ctrl+c", "q":
		return m.quit(key == "q")
	}
	return nil
}

func (m Model) workspaceRows(height int) []string {
	rows := []string{muted.Render(" Scegli il file da usare per questa Note Network."), ""}
	for i := listStart(m.choice.selection, len(m.choice.options), max(1, height-2)); i < len(m.choice.options); i++ {
		item := fmt.Sprintf("   %s", clean(m.choice.options[i]))
		if i == m.choice.selection {
			item = accent.Render(" › " + clean(m.choice.options[i]))
		}
		rows = append(rows, item)
	}
	return rows
}
