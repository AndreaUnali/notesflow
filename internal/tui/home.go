package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/editor"
	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type homeAction int

const (
	resumeNotes homeAction = iota
	createChapter
	delegateNotes
	homeActionCount
)

type homeItem struct {
	title, description string
	source             source
}

type homeHit struct {
	box   cellRect
	index int
}

var (
	homeTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.Text))
	homeCardStyle     = lipgloss.NewStyle().Background(lipgloss.Color(palette.Surface)).Foreground(lipgloss.Color(palette.Text))
	homeSelectedStyle = homeCardStyle.Background(lipgloss.Color("#10282D"))
)

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

func (m Model) homeItems() []homeItem {
	pos := m.savedPosition()
	resume := "Apri il documento principale"
	switch {
	case pos.File != "":
		resume = fmt.Sprintf("%s · riga %d", filepath.Base(pos.File), max(1, pos.Line))
	case m.suggestedFile() != "":
		resume = "Apri " + filepath.Base(m.suggestedFile())
	}
	items := []homeItem{
		{title: "Riprendi", description: resume},
		{title: "Nuovo capitolo", description: "Crea un file .tex e aggiungilo all’indice del corso"},
		{title: "Delega", description: "Scegli una sezione, le fonti e il risultato atteso"},
	}
	titles := map[string]string{}
	for _, group := range m.snapshot.Groups {
		if group.Title != "" && titles[group.File] == "" {
			titles[group.File] = group.Title
		}
	}
	for _, node := range m.snapshot.Files {
		if node.Kind != "file" || !m.fileInScope(node.File) || !strings.EqualFold(filepath.Ext(node.File), ".tex") {
			continue
		}
		title := titles[node.File]
		if title == "" {
			title = workflow.ChapterName(node.File, node.Summary)
		}
		state := m.fileAgentStatus(node.File)
		if state == "" && node.File == pos.File {
			state = "Ultimo aperto"
		}
		items = append(items, homeItem{title: title, description: state, source: source{node.File, node.Line}})
	}
	return items
}

func (m Model) homeRows() []row {
	items := m.homeItems()
	rows := make([]row, 0, len(items))
	for _, item := range items {
		rows = append(rows, row{text: item.title, source: item.source})
	}
	return rows
}

// homeContent lays out the visual actions and their mouse hit areas together.
func (m Model) homeContent(width, height int) ([]string, []homeHit) {
	items := m.homeItems()
	if width < 35 || height < 20 {
		return m.compactHome(items, width, height)
	}
	const inset = 2
	const cardRows = 4
	contentWidth := width - inset*2
	padding := strings.Repeat(" ", inset)
	lines := []string{padding + homeTitleStyle.Render(clean(m.scopeName())), ""}
	var hits []homeHit
	for i, item := range items[:homeActionCount] {
		hits = append(hits, homeHit{cellRect{inset, len(lines), contentWidth, cardRows}, i})
		for _, line := range homeCard(item, contentWidth, m.cursor == i && m.focus == mainFocus) {
			lines = append(lines, padding+line)
		}
		if i+1 < int(homeActionCount) {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "", padding+muted.Bold(true).Render("Capitoli"), padding+dim.Render(strings.Repeat("─", contentWidth)))
	chapters := items[homeActionCount:]
	if len(chapters) == 0 {
		return append(lines, padding+muted.Render("Nessun capitolo")), hits
	}
	room := max(0, height-len(lines))
	start := listStart(m.cursor-int(homeActionCount), len(chapters), room)
	for i := start; i < min(len(chapters), start+room); i++ {
		item := chapters[i]
		index := int(homeActionCount) + i
		hits = append(hits, homeHit{cellRect{inset, len(lines), contentWidth, 1}, index})
		label := homeChapter(item, i, contentWidth, m.cursor == index && m.focus == mainFocus)
		lines = append(lines, padding+label)
	}
	return lines, hits
}

func (m Model) compactHome(items []homeItem, width, height int) ([]string, []homeHit) {
	var lines []string
	var hits []homeHit
	start := listStart(m.cursor, len(items), height)
	for i := start; i < min(len(items), start+height); i++ {
		item := items[i]
		label := "   " + clean(item.title)
		if i == m.cursor && m.focus == mainFocus {
			label = accent.Render(" › " + clean(item.title))
		}
		hits = append(hits, homeHit{cellRect{0, len(lines), width, 1}, i})
		lines = append(lines, label)
	}
	return lines, hits
}

func homeCard(item homeItem, width int, selected bool) []string {
	border, fill := dim, homeCardStyle
	marker := "  "
	if selected {
		border, fill, marker = accent.Bold(false), homeSelectedStyle, "› "
	}
	dashes := strings.Repeat("─", width-2)
	label := " " + marker + clean(item.title)
	interior := width - 2
	key := ""
	if selected {
		key = " Invio ↵ "
	}
	firstLine := fill.Bold(true).Render(fit(label, interior-lipgloss.Width(key)))
	if key != "" {
		firstLine += fill.Foreground(lipgloss.Color(palette.Cyan)).Render(key)
	}
	detail := fill.Foreground(lipgloss.Color(palette.Muted)).Render(fit("   "+clean(item.description), interior))
	return []string{border.Render("╭" + dashes + "╮"),
		border.Render("│") + firstLine + border.Render("│"),
		border.Render("│") + detail + border.Render("│"),
		border.Render("╰" + dashes + "╯")}
}

func homeChapter(item homeItem, index, width int, selected bool) string {
	prefix := fmt.Sprintf("  %02d  ", index+1)
	label := clean(item.title)
	if selected {
		prefix = fmt.Sprintf("› %02d  ", index+1)
		label = accent.Render(label)
	}
	status := clean(item.description)
	if status == "" || lipgloss.Width(status) > width/2 {
		return ansi.Truncate(prefix+label, width, "…")
	}
	statusStyle := muted
	if strings.HasPrefix(status, "●") || strings.HasPrefix(status, "◆") {
		statusStyle = accent
	}
	lead := width - lipgloss.Width(status) - 1
	return fit(ansi.Truncate(prefix+label, lead-1, "…"), lead) + statusStyle.Render(status) + " "
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
