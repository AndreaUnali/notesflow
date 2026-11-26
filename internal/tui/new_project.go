package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/terminal"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type newProject struct {
	setupView
	ctx           context.Context
	theme         string
	title         string
	status        string
	root          string
	compileDetail string
	busy          bool
}

type projectCreated struct {
	root   string
	create error
	build  error
}

func RunNewProject(ctx context.Context, settings config.Settings) (root, detail string, err error) {
	m := newProject{ctx: ctx, theme: settings.Theme}
	surface := terminal.New(os.Stdout, terminal.Enabled())
	result, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithOutput(surface), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return "", "", err
	}
	created := result.(newProject)
	return created.root, created.compileDetail, nil
}

func (m newProject) Init() tea.Cmd { return tea.SetWindowTitle("Nuovo progetto Notesflow") }

func (m newProject) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case projectCreated:
		m.busy = false
		if v.create != nil {
			m.status = v.create.Error()
			m.cursor = 0
			return m, nil
		}
		m.root = v.root
		m.compileDetail = "Progetto creato · PDF pronto"
		if v.build != nil {
			if errors.Is(v.build, context.DeadlineExceeded) {
				m.compileDetail = "Progetto creato · Prima compilazione oltre 60 secondi"
			} else {
				m.compileDetail = "Progetto creato · Prima compilazione: " + strings.SplitN(v.build.Error(), "\n", 2)[0]
			}
		}
		return m, tea.Quit
	case tea.MouseMsg:
		if i, ok := m.rowAt(v, 3); ok && !m.busy {
			m.cursor = i
			if i != 0 {
				cmd := m.activate()
				return m, cmd
			}
		}
	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil
		}
		if v.Paste {
			if m.cursor == 0 {
				m.title = setupInput(m.title, v, 120)
				m.status = ""
			}
			return m, nil
		}
		switch v.String() {
		case "esc":
			return m, tea.Quit
		case "enter":
			cmd := m.activate()
			return m, cmd
		}
		if !m.move(v.String(), 3) && m.cursor == 0 {
			m.title = setupInput(m.title, v, 120)
			m.status = ""
		}
	}
	return m, nil
}

func (m *newProject) activate() tea.Cmd {
	if m.cursor == 0 {
		m.cursor = 1
		return nil
	}
	m.busy, m.status = true, "Creazione del progetto e prima compilazione…"
	structure := workflow.SingleFile
	if m.cursor == 2 {
		structure = workflow.Chapters
	}
	title, parent := m.title, m.ctx
	return func() tea.Msg {
		root, err := workflow.CreateProject(title, structure)
		if err != nil {
			return projectCreated{create: err}
		}
		cfg, err := config.Load(root, "")
		if err != nil {
			return projectCreated{root: root, build: err}
		}
		if _, err := exec.LookPath(cfg.Tools.LatexEngine); err != nil {
			return projectCreated{root: root, build: errors.New("installa latexmk per generare il PDF")}
		}
		ctx, cancel := context.WithTimeout(parent, 60*time.Second)
		defer cancel()
		_, err = workflow.CompileIn(ctx, root, cfg.Tools.MainTeX, cfg.Tools.LatexEngine, cfg.Tools.BuildDir)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return projectCreated{root: root, build: err}
	}
}

func (m newProject) View() string {
	return m.draw(m.theme, "Nuovo progetto", "La cartella con il titolo del progetto sarà in Documenti/Notesflow.", m.status, []string{
		"Titolo · " + m.title,
		"File unico · Crea un progetto con main.tex",
		"A capitoli · Crea un progetto con capitoli separati",
	}, nil)
}
