// Package tui is the terminal entry point for workspace navigation.
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
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/providers"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

var tabs = []string{"Dashboard/API", "Roadmap", "Grafo", "TODO", "Attività"}

const (
	dashboardTab = iota
	roadmapTab
	graphTab
	todoTab
	activityTab
)

var (
	accent        = lipgloss.NewStyle().Foreground(lipgloss.Color("#00F5D4")).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#090B10")).Background(lipgloss.Color("#00F5D4")).Bold(true)
	muted         = lipgloss.NewStyle().Foreground(lipgloss.Color("#A5B2C8"))
	pink          = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF79B5"))
)

type Model struct {
	ctx           context.Context
	root, url     string
	cfg           config.Config
	store         *graph.Store
	manager       *providers.Manager
	snapshot      graph.Snapshot
	tab, cursor   int
	width, height int
	course        string
	taskKind      string
	message       string
	summary       string
	summarizing   bool
	showSummary   bool
	summaryOffset int
}

func New(ctx context.Context, root, url string, cfg config.Config, store *graph.Store, manager *providers.Manager) Model {
	snapshot, _ := store.Snapshot()
	return Model{ctx: ctx, root: root, url: url, cfg: cfg, store: store, manager: manager, snapshot: snapshot, width: 90, height: 27}
}

type tick time.Time
type done struct {
	err error
	ok  string
}
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

func refresh() tea.Cmd {
	return tea.Tick(450*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}

func (m Model) Init() tea.Cmd { return refresh() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tick:
		m.snapshot, _ = m.store.Snapshot()
		return m, refresh()
	case tea.WindowSizeMsg:
		m.width, m.height = value.Width, value.Height
	case done:
		m.message = value.ok
		if value.err != nil {
			m.message = value.err.Error()
		}
	case summarized:
		m.summarizing = false
		m.summary = value.text
		m.summaryOffset = 0
		m.message = "Riepilogo aggiornato"
		if value.err != nil {
			m.message = value.err.Error()
		}
	case tea.KeyMsg:
		return m.key(value.String())
	}
	return m, nil
}

func (m Model) key(key string) (tea.Model, tea.Cmd) {
	length := len(m.rows())
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % len(tabs)
		m.cursor = 0
	case "shift+tab", "left", "h":
		m.tab = (m.tab + len(tabs) - 1) % len(tabs)
		m.cursor = 0
	case "1", "2", "3", "4", "5":
		m.tab, m.cursor = int(key[0]-'1'), 0
	case "down", "j", "up", "k", "pgdown", "pgup":
		step := 1
		if key == "pgdown" || key == "pgup" {
			step = max(1, m.height-8)
		}
		if key == "up" || key == "k" || key == "pgup" {
			step = -step
		}
		if m.tab == todoTab && m.showSummary {
			m.summaryOffset = max(0, min(m.summaryOffset+step, strings.Count(m.summary, "\n")))
		} else {
			m.cursor = max(0, min(m.cursor+step, length-1))
		}
	case "c":
		m.nextCourse()
	case "g":
		return m, func() tea.Msg {
			return done{err: workflow.Open(m.ctx, m.url, ""), ok: "Grafo aperto nel browser"}
		}
	case "f":
		if m.tab == todoTab {
			m.cursor, m.showSummary = 0, false
			switch m.taskKind {
			case "":
				m.taskKind = "TODO"
			case "TODO":
				m.taskKind = "VISUAL"
			default:
				m.taskKind = ""
			}
		}
	case "s":
		if m.tab == todoTab && !m.summarizing {
			tasks := m.tasks()
			m.summarizing, m.showSummary, m.summary = true, true, ""
			return m, func() tea.Msg {
				text, err := m.manager.Summarize(m.ctx, tasks)
				return summarized{text, err}
			}
		}
	case "v":
		if m.tab == todoTab {
			m.showSummary = !m.showSummary
		}
	case "enter":
		rows := m.rows()
		if len(rows) == 0 || (m.tab == todoTab && m.showSummary) {
			return m, nil
		}
		target := rows[min(m.cursor, len(rows)-1)].source
		if target.file == "" {
			return m, nil
		}
		if strings.EqualFold(filepath.Ext(target.file), ".pdf") {
			return m, func() tea.Msg {
				file, err := workflow.WorkspaceFile(m.root, target.file)
				if err == nil {
					err = workflow.Open(m.ctx, file, m.cfg.Tools.PDFViewer)
				}
				return done{err: err, ok: "PDF aperto"}
			}
		}
		if m.cfg.Tools.NvimServer != "" {
			return m, func() tea.Msg {
				return done{err: workflow.RemoteEdit(m.ctx, m.root, m.cfg.Tools.NvimServer, target.file, target.line), ok: "Aperto in Neovim"}
			}
		}
		cmd, err := workflow.Edit(m.ctx, m.root, target.file, target.line)
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return done{err: err, ok: "Sessione Neovim terminata"} })
	}
	return m, nil
}

func (m *Model) nextCourse() {
	set := map[string]bool{}
	for _, node := range m.snapshot.Nodes {
		set[node.Course] = true
	}
	for _, road := range m.snapshot.Roadmaps {
		set[road.Course] = true
	}
	courses := make([]string, 1, len(set)+1)
	for course := range set {
		courses = append(courses, course)
	}
	slices.Sort(courses)
	current := slices.Index(courses, m.course)
	m.course = courses[(current+1)%len(courses)]
	m.cursor, m.showSummary = 0, false
}

func (m Model) matches(course string) bool { return m.course == "" || m.course == course }

func (m Model) tasks() []parser.Task {
	var tasks []parser.Task
	for _, task := range m.snapshot.Tasks {
		if m.matches(parser.Course(task.File)) && (m.taskKind == "" || m.taskKind == task.Kind) {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func (m Model) rows() []row {
	var rows []row
	switch m.tab {
	case dashboardTab:
		status := m.manager.Snapshot()
		rows = append(rows, row{text: "Modalità " + status.Mode + "  ·  " + m.url})
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
	case roadmapTab:
		for _, road := range m.snapshot.Roadmaps {
			if !m.matches(road.Course) {
				continue
			}
			done, total := road.Progress()
			title := road.File
			if total > 0 {
				title += fmt.Sprintf("  ·  %d/%d", done, total)
			}
			rows = append(rows, row{title, source{road.File, 1}})
			for _, item := range road.Items {
				mark := ""
				if item.Done != nil {
					mark = "○ "
					if *item.Done {
						mark = "✓ "
					}
				}
				rows = append(rows, row{"  " + strings.Repeat("  ", max(0, min(8, item.Depth))) + mark + item.Title, source{road.File, item.Line}})
			}
			if road.Note != "" {
				rows = append(rows, row{"  " + road.Note, source{road.File, 1}})
			}
		}
	case graphTab:
		for _, node := range m.snapshot.Nodes {
			if m.matches(node.Course) {
				rows = append(rows, row{fmt.Sprintf("%s · %s  %s:%d", node.Kind, node.Title, node.File, node.Line), source{node.File, node.Line}})
			}
		}
	case todoTab:
		for _, task := range m.tasks() {
			rows = append(rows, row{fmt.Sprintf("%-6s  %s · %s  %s:%d", task.Kind, task.Text, task.Section, task.File, task.Line), source{task.File, task.Line}})
		}
	case activityTab:
		events := m.store.Activity()
		for i := len(events) - 1; i >= 0; i-- {
			rows = append(rows, row{text: events[i].Time.Local().Format("15:04:05") + "  " + events[i].Text})
		}
	}
	return rows
}

func (m Model) ego() []string {
	var visible []parser.Node
	nodes := make(map[string]parser.Node)
	for _, node := range m.snapshot.Nodes {
		nodes[node.ID] = node
		if m.matches(node.Course) {
			visible = append(visible, node)
		}
	}
	if len(visible) == 0 {
		return nil
	}
	node := visible[min(m.cursor, len(visible)-1)]
	var adjacent []string
	counts := map[string]int{}
	for _, edge := range m.snapshot.Edges {
		other := ""
		if edge.Source == node.ID {
			other = edge.Target
		} else if edge.Target == node.ID {
			other = edge.Source
		}
		if other != "" {
			counts[edge.Kind]++
			adjacent = append(adjacent, edge.Kind+" · "+nodes[other].Title)
		}
	}
	explicit := counts["ref"] + counts["eqref"] + counts["input"] + counts["include"]
	lines := []string{fmt.Sprintf("%s · %d LaTeX / %d semantici / %d gerarchici", node.Title, explicit, counts["semantic"], counts["hierarchy"])}
	return append(lines, adjacent...)
}

func (m Model) status() string {
	state := m.manager.Snapshot()
	current := "in attesa"
	if len(state.Profiles) > 0 {
		profile := state.Profiles[0]
		for _, candidate := range state.Profiles {
			if candidate.ID == state.Active {
				profile = candidate
				break
			}
		}
		current = profile.ID + "/" + profile.Model + " · " + profile.Status
		if !profile.AvailableAt.IsZero() {
			current += " → " + profile.AvailableAt.Local().Format("15:04")
		} else if !profile.WindowEnd.IsZero() {
			current += " → finestra " + profile.WindowEnd.Local().Format("15:04")
		}
	}
	course := m.course
	if course == "" {
		course = "Tutti i corsi"
	}
	progress := "—"
	done, total := 0, 0
	for _, road := range m.snapshot.Roadmaps {
		if m.matches(road.Course) {
			d, t := road.Progress()
			done, total = done+d, total+t
		}
	}
	if total > 0 {
		progress = fmt.Sprintf("%d/%d", done, total)
	}
	return fmt.Sprintf("%s / %s · Roadmap %s  │  %s", m.cfg.Project, course, progress, current)
}

func (m Model) line(text string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, text)
	return lipgloss.NewStyle().MaxWidth(max(1, m.width)).Render(clean)
}

func (m Model) View() string {
	title := accent.Render(" NOTESFLOW ")
	var nav []string
	for index, tab := range tabs {
		label := " " + tab + " "
		if m.width < 65 {
			label = fmt.Sprintf(" %d:%s ", index+1, strings.Split(tab, "/")[0])
		}
		if index == m.tab {
			nav = append(nav, selectedStyle.Render(label))
		} else {
			nav = append(nav, muted.Render(label))
		}
	}
	tabbar := lipgloss.NewStyle().MaxWidth(max(1, m.width)).Render(strings.Join(nav, ""))
	lines := []string{title, tabbar, ""}
	available := max(0, m.height-6)
	help := "Tab cambia vista · ↑↓ scorri · c corso · g grafo web · q esci"
	if m.tab == todoTab {
		filter := m.taskKind
		if filter == "" {
			filter = "tutti"
		}
		help = "f tipo (" + filter + ") · s riepilogo · v lista/riepilogo · Invio Neovim · q esci"
	}
	if m.tab == roadmapTab || m.tab == graphTab {
		help = "↑↓ scorri · Invio apri · c corso · g grafo web · Tab viste · q esci"
	}
	var content []string
	if m.tab == todoTab && m.showSummary {
		switch {
		case m.summarizing:
			content = append(content, muted.Render("Riepilogo in elaborazione…"))
		case m.summary != "":
			summary := strings.Split(m.summary, "\n")
			for _, line := range summary[min(m.summaryOffset, len(summary)-1):] {
				content = append(content, m.line(line))
			}
		default:
			content = append(content, muted.Render("Premi s per generare un riepilogo."))
		}
	} else {
		rows := m.rows()
		var detail []string
		if m.tab == graphTab && available > 7 {
			detail = m.ego()
			if len(detail) > 0 {
				detail = detail[:min(5, len(detail))]
			}
		}
		listHeight := max(1, available-len(detail)-1)
		cursor := min(m.cursor, len(rows)-1)
		start := max(0, min(cursor-listHeight/2, len(rows)-listHeight))
		for i := start; i < min(len(rows), start+listHeight); i++ {
			if cursor == i && m.tab != dashboardTab && m.tab != activityTab {
				content = append(content, accent.Render(m.line("› "+rows[i].text)))
			} else {
				content = append(content, m.line("  "+rows[i].text))
			}
		}
		if len(rows) == 0 {
			content = append(content, muted.Render("  Nessun elemento per questa vista."))
		}
		if len(detail) > 0 {
			content = append(content, "")
			for _, line := range detail {
				content = append(content, pink.Render(m.line("  "+line)))
			}
		}
	}
	lines = append(lines, content[:min(available, len(content))]...)
	for len(lines) < m.height-3 {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(m.line(help)), m.line(m.message), accent.Render(m.line(m.status())))
	if len(lines) > m.height {
		lines = lines[len(lines)-m.height:]
	}
	return strings.Join(lines, "\n")
}
