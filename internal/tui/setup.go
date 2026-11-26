package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

type setupStep int

const (
	setupAppearance setupStep = iota
	setupConnection
	setupAPI
	setupCatalog
	setupManual
	setupTest
)

type setup struct {
	setupView
	ctx        context.Context
	surface    *terminal.Surface
	settings   config.Settings
	step       setupStep
	connection config.Profile
	models     []string
	filter     string
	status     string
	pending    bool
	verified   bool
	saved      bool
}

type catalogResult struct {
	models []string
	err    error
}
type connectionTested struct{ err error }
type openCodeLogin struct{ err error }

func RunSetup(ctx context.Context, settings config.Settings) (config.Settings, bool, error) {
	surface := terminal.New(os.Stdout, terminal.Enabled())
	current := setup{ctx: ctx, surface: surface, settings: settings}
	result, err := tea.NewProgram(current, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithOutput(surface), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return settings, false, err
	}
	configured := result.(setup)
	return configured.settings, configured.saved, nil
}

func (m setup) Init() tea.Cmd { return tea.SetWindowTitle("Configurazione di Notesflow") }

func (m setup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case catalogResult:
		m.pending, m.models = false, v.models
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.status = "Catalogo ricevuto. Scegli un modello da provare."
		}
	case connectionTested:
		m.pending, m.verified = false, v.err == nil
		if v.err != nil {
			m.status = v.err.Error()
		} else {
			m.status = "Collegato · il modello ha risposto al test."
			m.cursor = 1
		}
	case openCodeLogin:
		m.pending = false
		if v.err != nil {
			m.status = "Accesso a OpenCode interrotto. Puoi riprovare."
			break
		}
		cmd := m.listModels()
		return m, cmd
	case tea.MouseMsg:
		if !m.pending {
			_, _, rows := m.content()
			if i, ok := m.rowAt(v, len(rows)); ok {
				m.cursor = i
				if !m.editing() {
					cmd := m.activate()
					return m, cmd
				}
			}
		}
	case tea.KeyMsg:
		if v.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.pending {
			return m, nil
		}
		if v.Paste || (m.editing() && v.String() != "enter" && v.String() != "esc") {
			_, _, rows := m.content()
			if v.Paste || !m.move(v.String(), len(rows)) {
				m.edit(v)
			}
			return m, nil
		}
		if m.step == setupAppearance && m.cursor == 0 {
			switch v.Type {
			case tea.KeyLeft:
				m.settings.Theme = palette.Adjacent(m.settings.Theme, -1)
				return m, nil
			case tea.KeyRight:
				m.settings.Theme = palette.Adjacent(m.settings.Theme, 1)
				return m, nil
			}
		}
		switch v.String() {
		case "esc":
			cmd := m.back()
			return m, cmd
		case "enter":
			cmd := m.activate()
			return m, cmd
		}
		_, _, rows := m.content()
		m.move(v.String(), len(rows))
	}
	return m, nil
}

func (m *setup) goTo(step setupStep) {
	m.step, m.cursor, m.status = step, 0, ""
}

func (m *setup) back() tea.Cmd {
	switch m.step {
	case setupAppearance:
		return tea.Quit
	case setupConnection:
		m.goTo(setupAppearance)
	case setupAPI:
		m.goTo(setupConnection)
	case setupCatalog:
		if m.connection.Driver == "opencode" {
			m.goTo(setupConnection)
		} else {
			m.goTo(setupAPI)
		}
	case setupManual, setupTest:
		m.goTo(setupCatalog)
	}
	return nil
}

func (m setup) editing() bool {
	return m.step == setupAPI && m.cursor < 2 ||
		m.cursor == 0 && (m.step == setupManual || m.step == setupCatalog)
}

func (m *setup) edit(key tea.KeyMsg) {
	if !m.editing() {
		return
	}
	switch {
	case m.step == setupCatalog:
		m.filter = setupInput(m.filter, key, 100)
	case m.step == setupManual:
		m.connection.Model = setupInput(m.connection.Model, key, 300)
	case m.cursor == 0:
		m.connection.BaseURL = setupInput(m.connection.BaseURL, key, 400)
	default:
		m.connection.LocalKey = setupInput(m.connection.LocalKey, key, 1024)
	}
}

func (m *setup) activate() tea.Cmd {
	switch m.step {
	case setupAppearance:
		switch m.cursor {
		case 0:
			m.settings.Theme = palette.Adjacent(m.settings.Theme, 1)
		case 1:
			return m.save()
		case 2:
			m.goTo(setupConnection)
		}
	case setupConnection:
		switch m.cursor {
		case 0:
			m.connection = config.Profile{ID: "opencode", Driver: "opencode"}
			m.filter = ""
			m.goTo(setupCatalog)
			return m.listModels()
		case 1:
			m.connection = config.Profile{ID: "api", Driver: "openai-compatible"}
			if m.settings.Profile != nil && m.settings.Profile.Driver == m.connection.Driver {
				m.connection = *m.settings.Profile
				m.connection.LocalKey = m.settings.APIKey
			}
			m.goTo(setupAPI)
		case 2:
			return m.save()
		}
	case setupAPI:
		if m.cursor < 2 {
			m.cursor++
			break
		}
		m.connection.BaseURL = strings.TrimRight(strings.TrimSpace(m.connection.BaseURL), "/")
		m.connection.LocalKey = strings.TrimSpace(m.connection.LocalKey)
		m.filter = ""
		m.goTo(setupCatalog)
		return m.listModels()
	case setupCatalog:
		switch m.cursor {
		case 0:
			firstModel := 3
			if m.connection.Driver == "opencode" {
				firstModel++
			}
			if len(m.matchingModels()) != 0 {
				m.cursor = firstModel
			}
		case 1:
			return m.listModels()
		case 2:
			m.goTo(setupManual)
		default:
			index := m.cursor - 3
			if m.connection.Driver == "opencode" {
				if index == 0 {
					return m.loginOpenCode()
				}
				index--
			}
			m.connection.Model = m.matchingModels()[index]
			m.selectModel()
		}
	case setupManual:
		m.connection.Model = strings.TrimSpace(m.connection.Model)
		if m.connection.Model == "" {
			m.status = "Indica il nome del modello."
			break
		}
		m.selectModel()
	case setupTest:
		if m.cursor == 0 {
			m.pending, m.verified, m.status = true, false, "Invio del messaggio di prova…"
			ctx, connection := m.ctx, m.connection
			return func() tea.Msg { return connectionTested{providers.TestModel(ctx, connection)} }
		}
		if m.verified {
			m.settings.Profile = &m.connection
			m.settings.APIKey = m.connection.LocalKey
			return m.save()
		}
	}
	return nil
}

func (m *setup) selectModel() {
	m.goTo(setupTest)
	m.verified = false
}

func (m *setup) listModels() tea.Cmd {
	m.pending, m.models, m.status, m.cursor = true, nil, "Verifica del collegamento…", 0
	ctx, profile := m.ctx, m.connection
	return func() tea.Msg {
		models, err := providers.Models(ctx, profile)
		return catalogResult{models, err}
	}
}

func (m setup) matchingModels() []string {
	filter := strings.ToLower(strings.TrimSpace(m.filter))
	if filter == "" {
		return m.models
	}
	var matches []string
	for _, model := range m.models {
		if strings.Contains(strings.ToLower(model), filter) {
			matches = append(matches, model)
		}
	}
	return matches
}

func (m *setup) loginOpenCode() tea.Cmd {
	cmd, err := providers.OpenCodeLogin(m.ctx)
	if err != nil {
		m.status = "Installa OpenCode, poi torna qui e ricarica il catalogo."
		return nil
	}
	m.pending = true
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return openCodeLogin{err}
	})
}

func (m *setup) save() tea.Cmd {
	m.settings.Theme = palette.ThemeFor(m.settings.Theme).ID
	if err := m.settings.Save(); err != nil {
		m.status = "Salvataggio delle impostazioni fallito. " + clean(err.Error())
		return nil
	}
	m.saved = true
	return tea.Quit
}

func (m setup) content() (title, detail string, rows []string) {
	switch m.step {
	case setupAppearance:
		return "Aspetto", "←/→ o Invio cambia palette. Il font del testo si regola nel terminale.", []string{
			palette.ThemeFor(m.settings.Theme).Name,
			"Applica palette e torna",
			"Collegamento ai modelli",
		}
	case setupConnection:
		first, later := "OpenCode · controlla l’harness e i modelli", "Configura più tardi"
		if _, err := exec.LookPath("opencode"); err != nil {
			first = "OpenCode · da installare"
		}
		if m.settings.Profile != nil {
			later = "Tieni il modello configurato · " + m.settings.Profile.Model
		}
		return "Collegamento ai modelli", "Scegli OpenCode oppure collega direttamente un provider API.", []string{first, "Provider compatibile con OpenAI · inserisci la chiave", later}
	case setupAPI:
		key := strings.Repeat("•", min(len(m.connection.LocalKey), 16))
		return "Provider API", "La chiave viene salvata nel file privato delle impostazioni utente.", []string{
			"URL base del provider · " + m.connection.BaseURL,
			"Chiave API · " + key,
			"Controlla il catalogo",
		}
	case setupCatalog:
		rows := []string{"Filtra i modelli · " + m.filter, "Ricarica catalogo", "Inserisci un modello manualmente"}
		if m.connection.Driver == "opencode" {
			rows = append(rows, "Accedi in OpenCode")
		}
		rows = append(rows, m.matchingModels()...)
		return "Modelli disponibili", "Scegli un modello dal catalogo o inserisci il suo identificativo.", rows
	case setupManual:
		return "Modello manuale", "Per OpenCode usa il formato provider/modello.", []string{
			"Identificativo del modello · " + m.connection.Model,
			"Avanti · Prova del modello",
		}
	case setupTest:
		rows := []string{"Invia un messaggio di prova"}
		if m.verified {
			rows = append(rows, "Usa questo modello e salva")
		}
		return "Prova del modello", m.connection.Model, rows
	default:
		return "", "", nil
	}
}

func (m setup) View() string {
	title, detail, rows := m.content()
	var swatches []string
	colors := palette.Preset(m.settings.Theme)
	if m.step == setupAppearance {
		swatches = colors.Swatches()
	}
	frame := m.draw(m.settings.Theme, title, detail, m.status, rows, swatches)
	var images []terminal.Placement
	if m.step == setupAppearance && m.surface.Enabled() && m.height >= 19 && m.width >= 46 {
		cellW, cellH := m.surface.CellPixels()
		if cellH > 0 {
			logo := renderWordmark(24, 3, float64(cellW)/float64(cellH), colors.Cyan, colors.Purple)
			if logo != nil {
				images = append(images, terminal.Placement{Graphic: logo, X: (m.width - 24) / 2, Y: 15, Cols: 24, Rows: 3})
			}
		}
	}
	return m.surface.Marker(images) + frame
}
