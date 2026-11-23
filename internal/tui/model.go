// Package tui provides the persistent Notesflow terminal workspace.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/editor"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/terminal"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

const (
	homeTab = iota
	editorTab
	graphTab
	todoTab
	activityTab
	apiTab
)

const (
	mainFocus = iota
	roadFocus
	pdfFocus
)

const rootCourse = "."

var (
	accent        = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Cyan)).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Background)).Background(lipgloss.Color(palette.Cyan)).Bold(true)
	muted         = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Muted))
	dim           = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Line))
	pink          = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Pink))
)

type Model struct {
	ctx       context.Context
	root, url string
	cfg       config.Config
	store     *graph.Store
	manager   *providers.Manager
	snapshot  graph.Snapshot
	surface   *terminal.Surface

	tab, cursor, focus int
	width, height      int
	course, message    string
	roadCollapsed      bool
	roadCursor         int
	commandMode        bool
	todoFraction       float64
	transition         transition

	taskKind, summary        string
	summarizing, showSummary bool
	summaryOffset            int

	editor                    *editor.Editor
	editorBusy                bool
	editorFrame               editor.Frame
	editorWidth, editorHeight int
	lastFile                  string
	quitting                  bool

	pdf                pdfState
	pdfPage, pdfOffset int
	pdfText            bool
	pdfBusy, compiling bool

	canvas           terminalGraph
	graphSearch      bool
	query            string
	edgeFilter       int
	zoom             float64
	pan              point
	graphHover       string
	graphSurface     renderedGraph
	graphDrag        dragState
	graphDragVersion uint64
}

func New(ctx context.Context, root, url string, cfg config.Config, store *graph.Store, manager *providers.Manager) Model {
	snapshot, _ := store.Snapshot()
	m := Model{ctx: ctx, root: root, url: url, cfg: cfg, store: store, manager: manager, snapshot: snapshot, width: 100, height: 32, pdfPage: 1, zoom: 1, todoFraction: 1}
	if m.isCourseRoot() {
		m.course = rootCourse
	}
	return m
}

func (m Model) WithSurface(surface *terminal.Surface) Model {
	m.surface = surface
	return m
}

type tick time.Time
type summarized struct {
	text string
	err  error
}
type source struct {
	file string
	line int
}
type row struct {
	text   string
	source source
}
type opened struct {
	instance *editor.Editor
	err      error
	file     string
	started  bool
}
type editorFrame struct {
	instance *editor.Editor
	frame    editor.Frame
	closed   bool
}
type quitCheck struct {
	unsaved bool
	err     error
}

func refresh() tea.Cmd {
	return tea.Tick(850*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return tick(time.Now()) }
}

func waitEditor(ctx context.Context, e *editor.Editor) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case <-e.Done():
			return editorFrame{instance: e, closed: true}
		case frame := <-e.Frames():
			return editorFrame{instance: e, frame: frame}
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if (m.tab == editorTab) != (next.tab == editorTab) {
		cmd = tea.Batch(cmd, next.beginTransition())
	}
	if next.tab == graphTab {
		next.prepareGraphic()
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch v := msg.(type) {
	case tick:
		m.snapshot, _ = m.store.Snapshot()
		if m.tab == graphTab {
			m.prepareGraph()
		}
		cmd := m.probePDF()
		return m, tea.Batch(refresh(), cmd)
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.resizeEditor()
		cmd := m.probePDF()
		return m, cmd
	case pdfResult:
		m.pdfBusy = false
		m.pdf = v.state
		if v.state.request == m.previewRequest() {
			m.pdfPage = max(1, v.state.page)
		}
	case compiled:
		m.compiling = false
		m.message = "PDF aggiornato · " + filepath.Base(v.file)
		if v.err != nil {
			m.message = "Compilazione fallita · " + strings.SplitN(v.err.Error(), "\n", 2)[0] + " · vedi 5 Attività"
			m.store.Log(v.err.Error())
		}
		cmd := m.probePDF()
		return m, cmd
	case summarized:
		m.summarizing = false
		m.summary, m.summaryOffset = v.text, 0
		m.message = "Riepilogo aggiornato"
		if v.err != nil {
			m.message = v.err.Error()
		}
	case opened:
		m.editorBusy = false
		m.editor = v.instance
		if v.err != nil {
			m.message = "Neovim · " + v.err.Error()
		} else if v.file != "" {
			m.lastFile = v.file
		}
		if v.started && m.editor != nil {
			m.resizeEditor()
			return m, waitEditor(m.ctx, m.editor)
		}
	case editorFrame:
		if v.instance != m.editor {
			break
		}
		if v.closed {
			m.editor, m.editorFrame = nil, editor.Frame{}
			m.editorWidth, m.editorHeight = 0, 0
			if m.tab == editorTab && !m.editorBusy {
				m.message = "Sessione di scrittura chiusa · Invio per riaprire"
			}
		} else {
			m.editorFrame = v.frame
			return m, waitEditor(m.ctx, v.instance)
		}
	case quitCheck:
		m.quitting = false
		if v.err != nil {
			m.message = v.err.Error()
		} else if v.unsaved {
			m.message = "Modifiche non salvate · 2 torna alla scrittura · :wall salva"
		} else {
			return m, tea.Quit
		}
	case transitionFrame:
		cmd := m.animate(v)
		return m, cmd
	case tea.MouseMsg:
		cmd := m.mouse(tea.MouseEvent(v))
		return m, cmd
	case tea.KeyMsg:
		return m.key(v)
	}
	return m, nil
}

func (m Model) key(key tea.KeyMsg) (Model, tea.Cmd) {
	s := key.String()
	if !key.Paste && s == "ctrl+g" {
		if m.commandMode && m.tab == editorTab && m.focus == mainFocus && m.editor != nil {
			m.editor.Key(key)
			m.commandMode = false
		} else {
			m.commandMode = !m.commandMode
		}
		return m, nil
	}
	if !key.Paste && (m.commandMode || (m.focus == mainFocus && m.tab != editorTab && !m.graphSearch)) {
		prefixed := m.commandMode
		m.commandMode = false
		cmd, ok := m.command(s)
		if ok || prefixed {
			return m, cmd
		}
	}
	if m.focus != mainFocus {
		cmd := m.sideKey(s)
		return m, cmd
	}
	if m.tab == editorTab {
		if m.editor != nil {
			if !m.editor.Key(key) {
				m.message = "Neovim impegnato"
			}
			return m, nil
		}
		if s == "enter" && !m.editorBusy {
			cmd := m.openEditor(source{m.lastFile, 1}, false)
			return m, cmd
		}
		return m, nil
	}
	if m.tab == graphTab && !m.graphSearch && !key.Paste && key.Type == tea.KeyRunes &&
		strings.HasPrefix(string(key.Runes), "/") {
		m.graphSearch = true
		m.query = strings.TrimPrefix(string(key.Runes), "/")
		m.cursor = 0
		m.prepareGraph()
		return m, nil
	}
	if m.tab == graphTab && m.graphSearch {
		switch s {
		case "esc":
			m.graphSearch, m.query = false, ""
		case "enter":
			m.graphSearch = false
		case "backspace":
			runes := []rune(m.query)
			if len(runes) > 0 {
				m.query = string(runes[:len(runes)-1])
			}
		default:
			if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
				m.query += string(key.Runes)
			}
		}
		m.cursor = 0
		m.prepareGraph()
		return m, nil
	}
	switch s {
	case "ctrl+c":
		cmd := m.quit()
		return m, cmd
	case "tab", "shift+tab":
		step := 1
		if s == "shift+tab" {
			step = 4
		}
		tab := (m.tab + step) % 5
		cmd := m.setTab(tab)
		return m, cmd
	case "down", "j", "up", "k", "pgdown", "pgup":
		delta := 1
		if s == "pgdown" || s == "pgup" {
			delta = max(1, m.layout().body-3)
		}
		if s == "up" || s == "k" || s == "pgup" {
			delta = -delta
		}
		if m.tab == todoTab && m.showSummary {
			m.summaryOffset = max(0, min(m.summaryOffset+delta, strings.Count(m.summary, "\n")))
		} else {
			m.cursor = max(0, min(m.cursor+delta, len(m.rows())-1))
		}
		m.prepareGraph()
	case "enter":
		cmd := m.activate()
		return m, cmd
	case "f":
		if m.tab == graphTab {
			m.edgeFilter = (m.edgeFilter + 1) % 4
			m.message = []string{"Tutti gli archi", "Collegamenti LaTeX", "Collegamenti semantici", "Gerarchia"}[m.edgeFilter]
		}
		if m.tab == todoTab {
			next := map[string]string{"": "TODO", "TODO": "VISUAL", "VISUAL": ""}
			m.taskKind, m.cursor, m.showSummary = next[m.taskKind], 0, false
		}
	case "s":
		if m.tab == todoTab && !m.summarizing {
			tasks := m.tasks(true)
			m.summarizing, m.showSummary, m.summary = true, true, ""
			return m, func() tea.Msg {
				report, err := m.manager.Summarize(m.ctx, tasks)
				return summarized{report, err}
			}
		}
	case "v":
		if m.tab == todoTab {
			m.showSummary = !m.showSummary
		}
	case "esc", "+", "=", "-", "0", "left", "right":
		if m.tab == graphTab {
			switch s {
			case "esc":
				m.query, m.cursor = "", 0
			case "+", "=":
				m.zoom = min(4, m.zoom*1.2)
			case "-":
				m.zoom = max(.3, m.zoom/1.2)
			case "0":
				m.pan, m.zoom = point{}, 1
			case "left":
				m.pan.x = min(220, m.pan.x+4)
			case "right":
				m.pan.x = max(-220, m.pan.x-4)
			}
			m.prepareGraph()
		}
	}
	return m, nil
}

func (m *Model) command(key string) (tea.Cmd, bool) {
	switch key {
	case "1", "2", "3", "4", "5":
		return m.setTab(int(key[0] - '1')), true
	case "r":
		if m.focus == roadFocus {
			m.focus = mainFocus
		} else {
			m.focus, m.roadCollapsed = roadFocus, false
		}
		m.resizeEditor()
		return m.probePDF(), true
	case "R":
		m.roadCollapsed = !m.roadCollapsed
		if m.roadCollapsed && m.focus == roadFocus {
			m.focus = mainFocus
		}
		m.resizeEditor()
		return m.probePDF(), true
	case "p":
		if m.focus == pdfFocus {
			m.focus = mainFocus
		} else {
			m.focus = pdfFocus
		}
		return m.probePDF(), true
	case "b":
		return m.compile(), true
	case "c":
		m.nextCourse()
		return m.probePDF(), true
	case "q":
		return m.quit(), true
	case "a":
		return m.setTab(apiTab), true
	}
	return nil, false
}

func (m *Model) setTab(tab int) tea.Cmd {
	m.tab, m.cursor, m.focus, m.graphSearch = tab, 0, mainFocus, false
	m.graphDrag, m.graphHover = dragState{}, ""
	switch tab {
	case editorTab:
		if m.editor == nil {
			file := m.lastFile
			if file == "" {
				file = m.mainTeX()
			}
			if file == "" {
				files := m.files()
				if len(files) > 0 {
					file = files[0].file
				}
			}
			return m.openEditor(source{file, 1}, false)
		}
		m.resizeEditor()
	case graphTab:
		m.prepareGraph()
	}
	return nil
}

func (m *Model) sideKey(s string) tea.Cmd {
	if s == "esc" {
		m.focus = mainFocus
		return nil
	}
	if m.focus == pdfFocus {
		page := m.pdfPage
		switch s {
		case "left", "pgup":
			page--
		case "right", "pgdown":
			page++
		}
		switch s {
		case "p":
			m.focus = mainFocus
		case "t":
			m.pdfText = !m.pdfText
			m.pdfOffset = 0
		case "up", "k":
			m.pdfOffset = max(0, m.pdfOffset-1)
		case "down", "j":
			m.pdfOffset = min(max(0, strings.Count(m.pdf.text, "\n")), m.pdfOffset+1)
		}
		if m.pdf.pages > 0 {
			page = min(m.pdf.pages, page)
		}
		if m.pdfPage != max(1, page) {
			m.pdfOffset = 0
		}
		m.pdfPage = max(1, page)
		return m.probePDF()
	}
	items := m.milestones()
	switch s {
	case "left", "up", "h", "k":
		m.roadCursor = max(0, m.roadCursor-1)
	case "right", "down", "l", "j":
		m.roadCursor = max(0, min(len(items)-1, m.roadCursor+1))
	case "enter":
		if len(items) > 0 {
			item := items[min(m.roadCursor, len(items)-1)]
			if item.editable {
				return m.openEditor(item.source, false)
			}
		}
	}
	return nil
}

func (m *Model) activate() tea.Cmd {
	if m.tab == homeTab && m.cursor < len(homeActions) {
		switch m.cursor {
		case 0:
			return m.setTab(editorTab)
		case 1:
			return m.compile()
		case 2:
			return m.setTab(graphTab)
		case 3:
			return m.setTab(todoTab)
		case 4:
			return m.openEditor(source{}, true)
		case 5:
			return m.setTab(apiTab)
		case 6:
			return m.setTab(activityTab)
		}
	}
	rows := m.rows()
	if len(rows) == 0 || (m.tab == todoTab && m.showSummary) {
		return nil
	}
	target := rows[min(m.cursor, len(rows)-1)].source
	if target.file != "" {
		return m.openEditor(target, false)
	}
	return nil
}

func (m *Model) openEditor(target source, agent bool) tea.Cmd {
	if m.editorBusy {
		return nil
	}
	m.tab, m.focus = editorTab, mainFocus
	m.editorBusy = true
	m.message = ""
	width, height := m.editorSize()
	m.editorWidth, m.editorHeight = width, height
	current, ctx, root, dir := m.editor, m.ctx, m.root, m.scopeDir()
	return func() tea.Msg {
		var path string
		var err error
		e := current
		started := false
		if target.file != "" {
			path, err = workflow.WorkspaceFile(root, target.file)
			if err != nil {
				return opened{instance: e, err: err}
			}
		}
		if e == nil {
			e, err = editor.Start(ctx, root, width, height)
			if err != nil {
				return opened{err: err}
			}
			started = true
		}
		switch {
		case agent:
			err = e.Agent(ctx, dir)
		case path != "":
			err = e.Open(ctx, path, target.line)
		}
		return opened{instance: e, err: err, file: target.file, started: started}
	}
}

func (m *Model) resizeEditor() {
	if m.editor != nil && m.tab == editorTab {
		w, h := m.editorSize()
		if w != m.editorWidth || h != m.editorHeight {
			m.editor.Resize(w, h)
			m.editorWidth, m.editorHeight = w, h
		}
	}
}

func (m *Model) quit() tea.Cmd {
	if m.quitting {
		return nil
	}
	if m.editor == nil {
		return tea.Quit
	}
	m.quitting = true
	e, ctx := m.editor, m.ctx
	return func() tea.Msg {
		select {
		case <-e.Done():
			return quitCheck{}
		default:
		}
		changed, err := e.Unsaved(ctx)
		return quitCheck{changed, err}
	}
}

func (m Model) isCourseRoot() bool {
	if m.cfg.Tools.MainTeX != "" || m.cfg.Tools.PDF != "" {
		return true
	}
	for _, road := range m.snapshot.Roadmaps {
		if !strings.Contains(road.File, "/") {
			return true
		}
	}
	for _, node := range m.snapshot.Nodes {
		if node.Kind == "file" && !strings.Contains(node.File, "/") &&
			(strings.EqualFold(node.File, "main.tex") || strings.Contains(node.Summary, "\\documentclass")) {
			return true
		}
	}
	return false
}

func (m *Model) nextCourse() {
	set := map[string]bool{}
	for _, node := range m.snapshot.Nodes {
		if node.Course != "Workspace" {
			set[node.Course] = true
		}
	}
	for _, road := range m.snapshot.Roadmaps {
		if road.Course != "Workspace" {
			set[road.Course] = true
		}
	}
	courses := []string{""}
	if m.isCourseRoot() {
		courses = append(courses, rootCourse)
	}
	var nested []string
	for course := range set {
		nested = append(nested, course)
	}
	slices.Sort(nested)
	courses = append(courses, nested...)
	m.course = courses[(slices.Index(courses, m.course)+1)%len(courses)]
	m.cursor, m.roadCursor, m.pdfPage, m.pdfOffset, m.showSummary = 0, 0, 1, 0, false
	m.tab, m.focus = homeTab, mainFocus
}

func (m Model) scopeDir() string {
	if m.course == "" || m.course == rootCourse {
		return m.root
	}
	return filepath.Join(m.root, m.course)
}

func (m Model) scopeName() string {
	switch m.course {
	case "":
		return "Tutti i corsi"
	case rootCourse:
		return filepath.Base(m.root)
	default:
		return m.course
	}
}

func (m Model) fileInScope(file string) bool {
	return file != "" && (m.course == "" || m.course == rootCourse || parser.Course(filepath.ToSlash(file)) == m.course)
}

func (m Model) tasks(filtered bool) []parser.Task {
	var tasks []parser.Task
	for _, task := range m.snapshot.Tasks {
		if m.fileInScope(task.File) && (!filtered || m.taskKind == "" || m.taskKind == task.Kind) {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

var homeActions = []string{
	"2   Scrivi appunti", "b   Compila il PDF",
	"3   Esplora il grafo", "4   Consulta e riassumi i TODO",
	"     Avvia OpenCode nel corso", " a   Dashboard API e quote",
	"5   Consulta l’attività",
}

func (m Model) files() []source {
	var files []source
	for _, node := range m.snapshot.Nodes {
		if node.Kind == "file" && m.fileInScope(node.File) {
			files = append(files, source{node.File, node.Line})
		}
	}
	return files
}

func (m Model) rows() []row {
	var rows []row
	switch m.tab {
	case homeTab:
		for _, action := range homeActions {
			rows = append(rows, row{text: action})
		}
		for i, file := range m.files() {
			text := file.file
			if i == 0 {
				text = "File · " + text
			}
			rows = append(rows, row{text: text, source: file})
		}
	case apiTab:
		status := m.manager.Snapshot()
		rows = append(rows, row{text: "Modalità " + status.Mode})
		if m.url != "" {
			rows = append(rows, row{text: "Grafo web · " + m.url})
		}
		rows = append(rows, row{text: fmt.Sprintf("%d nodi  ·  %d collegamenti  ·  %d TODO", len(m.snapshot.Nodes), len(m.snapshot.Edges), len(m.snapshot.Tasks))})
		semantic := fmt.Sprintf("%d delta in coda", m.snapshot.Pending)
		if !m.cfg.Semantic.Enabled {
			semantic = "Analisi semantica disattivata"
		}
		rows = append(rows, row{text: semantic}, row{text: ""}, row{text: "PROFILI"})
		for _, profile := range status.Profiles {
			line := fmt.Sprintf("%s · %s/%s · %s", profile.ID, profile.Driver, profile.Model, profile.Status)
			if profile.Remaining >= 0 {
				line += fmt.Sprintf(" · %d richieste residue stimate", profile.Remaining)
			}
			if !profile.WindowEnd.IsZero() {
				line += " · reset " + profile.WindowEnd.Local().Format("15:04")
			}
			if !profile.AvailableAt.IsZero() {
				line += " · disponibile " + profile.AvailableAt.Local().Format("02/01 15:04")
			}
			rows = append(rows, row{text: line})
		}
		if len(status.History) > 0 {
			rows = append(rows, row{text: ""}, row{text: "CAMBI MODELLO"})
			for i := len(status.History) - 1; i >= max(0, len(status.History)-12); i-- {
				change := status.History[i]
				rows = append(rows, row{text: change.Time.Local().Format("02/01 15:04") + " · " + change.Profile + " / " + change.Model})
			}
		}
	case graphTab:
		for _, node := range m.graphNodes() {
			rows = append(rows, row{text: node.Title, source: source{node.File, node.Line}})
		}
	case todoTab:
		for _, task := range m.tasks(true) {
			rows = append(rows, row{fmt.Sprintf("%s · %s  (%s:%d)", task.Kind, task.Text, task.File, task.Line), source{task.File, task.Line}})
		}
	case activityTab:
		events := m.store.Activity()
		for i := len(events) - 1; i >= 0; i-- {
			event := events[i]
			for _, line := range strings.Split(event.Text, "\n") {
				rows = append(rows, row{text: event.Time.Local().Format("15:04:05") + " · " + line})
			}
		}
	}
	return rows
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, text)
}
