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

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/editor"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/network"
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

const visibleTabCount = activityTab + 1

const (
	mainFocus = iota
	roadFocus
	pdfFocus
	sidebarFocus
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
	navTab             int
	todoFraction       float64
	transition         transition

	taskKind, summary        string
	sidebarTab               int
	summarizing, showSummary bool
	summaryOffset            int

	editor                       *editor.Editor
	editorBusy                   bool
	editorFrame                  editor.Frame
	editorWidth, editorHeight    int
	lastFile, editorCourse       string
	quitting, returnToLauncher   bool
	savingNotes                  bool
	unsavedWarning, checkingSave bool
	positionBusy, chapterBusy    bool
	session                      *network.Session
	choice                       workspaceChoice
	form                         inputForm
	agents                       agentUI

	pdf                pdfState
	pdfPage, pdfOffset int
	pdfText            bool
	pdfBusy, compiling bool
	lastCompile        string

	canvas           terminalGraph
	graphSearch      bool
	graphGroup       string
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
	course   string
	started  bool
}
type editorFrame struct {
	instance *editor.Editor
	frame    editor.Frame
	closed   bool
}
type quitCheck struct {
	unsaved  bool
	err      error
	position positionResult
}

type notesSaved struct {
	instance *editor.Editor
	err      error
}

type saveWarningChecked struct {
	instance *editor.Editor
	clean    bool
}

const unsavedNotesWarning = "Modifiche non salvate · clicca Salva in alto"

func refresh() tea.Cmd {
	return tea.Tick(850*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return tick(time.Now()) }, m.titleCmd())
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
		if m.tab == editorTab && m.course == next.course {
			cmd = tea.Batch(cmd, next.capturePosition())
		}
	}
	if next.course != m.course {
		cmd = tea.Batch(cmd, next.titleCmd())
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
		agentCmd := m.advanceAgent(time.Time(v))
		var position tea.Cmd
		if m.tab == editorTab {
			position = m.capturePosition()
		}
		saveCheck := m.checkSaveWarning()
		return m, tea.Batch(refresh(), cmd, agentCmd, position, saveCheck)
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
		m.lastCompile = ""
		m.message = "PDF aggiornato · " + filepath.Base(v.file)
		if v.err != nil {
			m.message = "Compilazione fallita · " + strings.SplitN(v.err.Error(), "\n", 2)[0] + " · vedi 5 Attività"
			m.store.Log(v.err.Error())
			log := []rune(strings.ReplaceAll(v.err.Error(), m.root+string(filepath.Separator), ""))
			m.lastCompile = string(log[:min(3500, len(log))])
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
			m.lastFile, m.editorCourse = v.file, v.course
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
			m.clearSaveWarning("Sessione di scrittura chiusa")
			if m.tab == editorTab && !m.editorBusy {
				m.message = "Sessione di scrittura chiusa · Invio per riaprire"
			}
		} else {
			m.editorFrame = v.frame
			return m, waitEditor(m.ctx, v.instance)
		}
	case positionResult:
		m.positionBusy = false
		m.savePosition(v)
	case chapterResult:
		m.chapterBusy = false
		if v.err != nil {
			m.form.error = v.err.Error()
			break
		}
		m.form = inputForm{}
		cmd := m.openEditor(source{v.plan.File, 1})
		return m, cmd
	case agentContext:
		file, _ := filepath.Rel(m.root, v.position)
		m.showAgentForm("ask", 0, filepath.ToSlash(file), v.line)
		m.form.buffer, m.form.bufferFile = v.buffer, filepath.ToSlash(file)
	case agentPrepared:
		if run, ok := m.agents.running[v.id]; ok && run.token == v.token {
			run.preparing = false
			m.agents.running[v.id] = run
		}
	case agentResult:
		m.finishAgent(v)
		cmd := m.advanceAgent(time.Now())
		return m, cmd
	case appliedAgent:
		m.appliedAgent(v)
		if job, ok := m.agents.book.Lookup(v.id); ok && job.Todo != nil && job.State == agent.Completed && !job.Buffered {
			return m, m.compileFile(job.Todo.MainTeX)
		}
		cmd := m.advanceAgent(time.Now())
		return m, cmd
	case notesSaved:
		m.savingNotes = false
		if v.instance == m.editor {
			if v.err != nil {
				m.message = "Salvataggio fallito · " + strings.SplitN(v.err.Error(), "\n", 2)[0]
			} else {
				m.unsavedWarning = false
				m.message = "Appunti salvati"
			}
		}
	case saveWarningChecked:
		m.checkingSave = false
		if v.instance == m.editor && v.clean && !m.savingNotes && !m.quitting {
			m.clearSaveWarning("Appunti salvati")
		}
	case quitCheck:
		m.quitting = false
		m.savePosition(v.position)
		if v.err != nil {
			m.returnToLauncher = false
			m.focusNavigation()
			m.message = "Uscita sospesa · Neovim: " + v.err.Error() + " · q Riprova"
		} else if v.unsaved {
			m.returnToLauncher = false
			m.focusNavigation()
			m.unsavedWarning, m.message = true, unsavedNotesWarning
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
	if m.quitting {
		return m, nil
	}
	if !key.Paste && key.Type == tea.KeyShiftTab {
		m.focusNavigation()
		return m, nil
	}
	if m.commandMode {
		if !key.Paste {
			cmd := m.navigate(s)
			return m, cmd
		}
		m.commandMode = false
	}
	if m.formOpen() {
		cmd := m.formKey(key)
		return m, cmd
	}
	if m.workspacePickerOpen() && !key.Paste {
		cmd := m.workspaceKey(s)
		return m, cmd
	}
	if !key.Paste && (m.tab != editorTab || m.focus != mainFocus) && !m.graphSearch &&
		(s == "q" || s == "ctrl+c") {
		cmd := m.quit(s == "q")
		return m, cmd
	}
	if !key.Paste && m.focus == mainFocus && m.tab != editorTab && !m.graphSearch {
		if cmd, ok := m.agentDetailKey(s); ok {
			return m, cmd
		}
		if cmd, ok := m.command(s); ok {
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
			cmd := m.openEditorAt(m.savedPosition())
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
	case "down", "j", "up", "k", "pgdown", "pgup":
		delta := 1
		if s == "pgdown" || s == "pgup" {
			delta = max(1, m.layout().body-3)
			if m.tab == graphTab {
				if s == "pgup" {
					m.stepGraphPage(-1)
				} else {
					m.stepGraphPage(1)
				}
				m.prepareGraph()
				return m, nil
			}
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
			current := slices.IndexFunc(todoTabs[:], func(tab todoTabSpec) bool { return tab.kind == m.taskKind })
			next := todoTabs[(current+1)%len(todoTabs)]
			m.taskKind, m.cursor, m.showSummary = next.kind, 0, false
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
	case "esc", "backspace", "+", "=", "-", "0", "left", "right":
		if m.tab == graphTab {
			switch s {
			case "esc", "backspace":
				m.backGraph()
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

func (m *Model) focusNavigation() {
	if !m.commandMode {
		m.commandMode, m.navTab = true, min(m.tab, activityTab)
	}
}

// navigate moves within the tab bar without sending keys to the active pane.
func (m *Model) navigate(key string) tea.Cmd {
	const count = visibleTabCount
	switch key {
	case "left", "h":
		m.navTab = (m.navTab + count - 1) % count
	case "right", "l":
		m.navTab = (m.navTab + 1) % count
	case "enter":
		return m.setTab(m.navTab)
	case "tab", "esc":
		m.commandMode = false
	case "s":
		m.commandMode = false
		return m.saveNotes()
	default:
		if cmd, ok := m.command(key); ok {
			m.commandMode = false
			return cmd
		}
	}
	return nil
}

func (m *Model) command(key string) (tea.Cmd, bool) {
	switch key {
	case "1", "2", "3", "4", "5":
		if key == "5" && m.agents.notice.id > 0 {
			return m.openAgentActivity(m.agents.notice.id), true
		}
		return m.setTab(int(key[0] - '1')), true
	case "g":
		return m.openCurrentAgent(), true
	case "?":
		return m.startAgentContext(), true
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
	case "t":
		if m.tab == editorTab || m.layout().todo < 3 {
			return m.openSidebarList(), true
		}
		m.focus = sidebarFocus
		return nil, true
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
		position := m.capturePosition()
		m.nextCourse()
		return tea.Batch(position, m.probePDF()), true
	case "q":
		return m.quit(true), true
	case "a":
		return m.setTab(apiTab), true
	}
	return nil, false
}

func (m *Model) setTab(tab int) tea.Cmd {
	m.tab, m.cursor, m.focus, m.graphSearch = tab, 0, mainFocus, false
	m.commandMode = false
	m.agents.detail, m.agents.offset = 0, 0
	m.graphDrag, m.graphHover = dragState{}, ""
	switch tab {
	case editorTab:
		if m.editor == nil || m.editorCourse != m.course {
			pos := m.savedPosition()
			if pos.File == "" {
				pos.File = m.suggestedFile()
			}
			return m.openEditorAt(pos)
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
	if m.focus == sidebarFocus {
		switch s {
		case "left", "h":
			m.sidebarTab = (m.sidebarTab + len(todoTabs) - 1) % len(todoTabs)
		case "right", "l":
			m.sidebarTab = (m.sidebarTab + 1) % len(todoTabs)
		case "enter":
			return m.openSidebarList()
		case "tab", "t":
			m.focus = mainFocus
		}
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
	case "e":
		if len(items) > 0 {
			if evidence := items[min(m.roadCursor, len(items)-1)].Evidence; evidence != nil {
				return m.openEditor(source{evidence.File, evidence.Line})
			}
		}
	case "enter":
		if len(items) > 0 {
			item := items[min(m.roadCursor, len(items)-1)]
			if item.editable {
				return m.openEditor(item.source)
			}
		}
	}
	return nil
}

func (m *Model) activate() tea.Cmd {
	if m.tab == graphTab {
		if node, ok := m.graphNode(); ok {
			return m.enterGraph(node.ID)
		}
		return nil
	}
	if m.tab == homeTab && m.cursor < m.homeActions() {
		action := homeAction(m.cursor)
		if action == createChapter && m.cfg.Structure == workflow.SingleFile {
			action = delegateNotes
		}
		switch action {
		case resumeNotes:
			return m.setTab(editorTab)
		case createChapter:
			m.form = inputForm{mode: "chapter", tab: m.tab, course: m.course}
		case delegateNotes:
			pos := m.savedPosition()
			m.showAgentForm("delegate", 0, m.suggestedFile(), pos.Line)
		}
		return nil
	}
	if m.tab == activityTab {
		jobs := m.agentTasks()
		if m.cursor < len(jobs) {
			return m.openAgentActivity(jobs[m.cursor].ID)
		}
	}
	rows := m.rows()
	if len(rows) == 0 || (m.tab == todoTab && m.showSummary) {
		return nil
	}
	target := rows[min(m.cursor, len(rows)-1)].source
	if target.file != "" {
		return m.openEditor(target)
	}
	return nil
}

func (m *Model) openEditor(target source) tea.Cmd {
	return m.openEditorAt(network.Position{File: target.file, Line: target.line})
}

func (m *Model) openEditorAt(target network.Position) tea.Cmd {
	if m.editorBusy {
		return nil
	}
	m.tab, m.focus = editorTab, mainFocus
	m.editorBusy = true
	m.message = ""
	width, height := m.editorSize()
	m.editorWidth, m.editorHeight = width, height
	current, ctx, root, course := m.editor, m.ctx, m.root, m.course
	colors := palette.Current()
	return func() tea.Msg {
		var path string
		var err error
		e := current
		started := false
		if target.File != "" {
			path, err = workflow.WorkspaceFile(root, target.File)
			if err != nil {
				return opened{instance: e, err: err}
			}
		}
		if e == nil {
			e, err = editor.Start(ctx, root, width, height, colors)
			if err != nil {
				return opened{err: err}
			}
			started = true
		}
		if path != "" {
			err = e.OpenAt(ctx, path, target)
		}
		return opened{instance: e, err: err, file: target.File, course: course, started: started}
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

func (m *Model) quit(returnToLauncher bool) tea.Cmd {
	if m.quitting || m.savingNotes {
		return nil
	}
	m.quitting = true
	m.returnToLauncher = returnToLauncher
	if m.editor == nil {
		return tea.Quit
	}
	m.message = "Controllo delle modifiche in Neovim…"
	e, ctx, course := m.editor, m.ctx, m.editorCourse
	return func() tea.Msg {
		state, err := e.ExitState(ctx)
		select {
		case <-e.Done():
			return quitCheck{}
		default:
		}
		return quitCheck{unsaved: state.Unsaved, err: err, position: positionResult{editor: e, course: course, pos: state.Position}}
	}
}

func (m *Model) saveNotes() tea.Cmd {
	if m.savingNotes || m.quitting || m.editor == nil {
		return nil
	}
	m.savingNotes, m.message = true, "Salvataggio degli appunti…"
	e, ctx := m.editor, m.ctx
	return func() tea.Msg { return notesSaved{instance: e, err: e.SaveAll(ctx)} }
}

func (m *Model) checkSaveWarning() tea.Cmd {
	if !m.unsavedWarning || m.editor == nil || m.checkingSave || m.savingNotes || m.quitting {
		return nil
	}
	m.checkingSave = true
	e, ctx := m.editor, m.ctx
	return func() tea.Msg {
		state, err := e.ExitState(ctx)
		return saveWarningChecked{instance: e, clean: err == nil && !state.Unsaved}
	}
}

func (m *Model) clearSaveWarning(message string) {
	m.unsavedWarning = false
	if m.message == unsavedNotesWarning {
		m.message = message
	}
}

func (m Model) ReturnToLauncher() bool { return m.returnToLauncher }

func (m Model) EditorDone() <-chan struct{} {
	if m.editor == nil {
		return nil
	}
	return m.editor.Done()
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
	for _, node := range m.snapshot.Files {
		if node.Kind == "file" && !strings.Contains(node.File, "/") &&
			(strings.EqualFold(node.File, "main.tex") || strings.Contains(node.Summary, "\\documentclass")) {
			return true
		}
	}
	return false
}

func (m *Model) nextCourse() {
	set := map[string]bool{}
	for _, node := range m.snapshot.Files {
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
	m.graphGroup, m.query, m.graphSearch = "", "", false
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
		if m.cfg.Project != "" {
			return m.cfg.Project
		}
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
	matches := func(task parser.Task) bool {
		return m.fileInScope(task.File) && (!filtered || m.taskKind == "" || m.taskKind == task.Kind)
	}
	for _, task := range m.snapshot.Tasks {
		if matches(task) {
			tasks = append(tasks, task)
		}
	}
	for _, job := range m.agentTasks() {
		needsReview := job.State == agent.Review && job.Automatic ||
			job.Todo != nil && (job.State == agent.Review || job.State == agent.Failed)
		if !needsReview && !(job.State == agent.Completed && job.Buffered) {
			continue
		}
		for _, task := range agent.Tasks(job) {
			if needsReview && task.Kind != parser.MaintenanceTask {
				continue
			}
			if matches(task) && !slices.ContainsFunc(tasks, func(item parser.Task) bool {
				return item.File == task.File && item.Kind == task.Kind && item.Text == task.Text
			}) {
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

func (m Model) files() []source {
	var files []source
	for _, node := range m.snapshot.Files {
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
		return m.homeRows()
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
		for _, job := range m.agentTasks() {
			label := "Chiedi"
			if job.Write {
				label = "Delega"
			}
			rows = append(rows, row{text: fmt.Sprintf("%s · %s · %s", label, job.Target.Title, m.agentPhase(job))})
		}
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

func (m Model) titleCmd() tea.Cmd {
	name := m.cfg.Project
	if m.course != "" && m.course != rootCourse {
		name = m.course
	}
	return tea.SetWindowTitle("Notesflow · " + clean(name))
}
