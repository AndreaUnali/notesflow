package tui

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

type layout struct {
	left, right, body, todo, pdf, header, road, footer int
	split                                              bool
}

func (m Model) layout() layout {
	l := layout{left: m.width, header: 3, road: 4, footer: 3}
	if m.width < 75 {
		l.header = 4
	}
	if m.width < 57 {
		l.header++
	}
	if m.roadCollapsed || m.height < 24 {
		l.road, l.footer = 0, 1
	}
	l.body = max(0, m.height-l.header-l.road-l.footer)
	l.split = m.width >= 66 || l.body < 12
	if l.split {
		l.left = m.width * 2 / 3
		l.right = max(0, m.width-l.left)
		l.todo = int(math.Ceil(float64(min(9, max(3, l.body/3))) * m.todoFraction))
		l.pdf = max(0, l.body-l.todo)
	} else {
		baseTODO := max(3, l.body/5)
		l.todo = int(math.Ceil(float64(baseTODO) * m.todoFraction))
		l.pdf = max(4, l.body/3) + baseTODO - l.todo
		l.body = max(0, l.body-l.todo-l.pdf)
	}
	return l
}

func (m Model) editorSize() (int, int) {
	l := m.layout()
	return min(1000, max(10, l.left-2)), min(1000, max(3, l.body-2))
}

func fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = ansi.Truncate(text, width, "")
	return text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
}

func panel(title string, rows []string, width, height int, active bool) []string {
	if height <= 0 || width <= 1 {
		return nil
	}
	style := dim
	if active {
		style = accent.Bold(false)
	}
	count := max(0, width-2)
	title = clean(title)
	if title != "" {
		title = ansi.Truncate(" "+title+" ", count, "")
	}
	out := []string{style.Render("╭" + title + strings.Repeat("─", max(0, count-lipgloss.Width(title))) + "╮")}
	for i := 0; i < height-2; i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		out = append(out, style.Render("│")+fit(row, count)+style.Render("│"))
	}
	if height >= 2 {
		out = append(out, style.Render("╰"+strings.Repeat("─", count)+"╯"))
	}
	return out
}

func (m Model) View() string {
	if m.width < 24 || m.height < 9 {
		return m.surface.Marker(nil) + fit("Notesflow · Shift+Tab naviga", m.width)
	}
	l := m.layout()
	lines := m.header(l)
	if l.body >= 2 {
		title := "Operazioni del corso"
		width, height := l.left-2, l.body-2
		var main []string
		switch {
		case m.workspacePickerOpen():
			title = m.choice.kind
			main = m.workspaceRows(height)
		case m.formOpen():
			title = m.formTitle()
			main = m.formRows(width)
		case m.tab == editorTab:
			title = "Scrittura"
			if mode := m.editorFrame.Mode(); mode != "" {
				title += " · " + editorMode(mode)
				if m.lastFile != "" {
					title += "  /  " + m.lastFile
				}
			}
			switch {
			case m.editorBusy && m.editor == nil:
				main = []string{" Avvio Neovim…"}
			case m.editor == nil:
				main = []string{" Invio · avvia Neovim"}
			default:
				main = m.editorFrame.Rows()
			}
		case m.tab == graphTab:
			title = "Grafo del corso"
			main = m.graphRows(width, height)
		case m.tab == todoTab:
			title = "TODO del corso"
			if m.taskKind != "" {
				title = "TODO · " + m.taskKind
			}
			if m.showSummary {
				title = "Riepilogo TODO"
				main = m.summaryRows()
			} else {
				main = m.listRows(height)
			}
		case m.tab == activityTab:
			title = "Attività"
			if job, ok := m.selectedAgentJob(); ok {
				title += " · " + job.Target.Title
				lines := m.agentDetailLines(job, width)
				main = lines[min(m.agents.offset, max(0, len(lines)-1)):]
			} else {
				main = m.listRows(height)
			}
		case m.tab == apiTab:
			title = "Dashboard API"
			main = m.listRows(height)
		case m.tab == homeTab:
			title = "Home"
			main, _ = m.homeContent(width, height)
		default:
			main = m.listRows(height)
		}
		left := panel(title, main, l.left, l.body, m.focus == mainFocus)
		rightWidth := l.right
		if !l.split {
			rightWidth = l.left
		}
		top := panel(fmt.Sprintf("TODO · %d", len(m.sidebarItems())), m.sidebarRows(rightWidth-2, l.todo-2), rightWidth, l.todo, m.focus == sidebarFocus)
		preview := panel("Anteprima PDF", m.pdfRows(), rightWidth, l.pdf, m.focus == pdfFocus)
		aside := append(top, preview...)
		if l.split {
			for i, row := range left {
				if i < len(aside) {
					row += aside[i]
				}
				lines = append(lines, row)
			}
		} else {
			lines = append(lines, left...)
			lines = append(lines, aside...)
		}
	}
	for len(lines) < m.height-l.road-l.footer {
		lines = append(lines, "")
	}
	lines = append(lines, m.footerRows(l)...)
	var images []terminal.Placement
	if p := m.pdfPlacement(); p.Graphic != nil {
		images = append(images, p)
	}
	if m.tab == graphTab && m.graphSurface.graphic != nil {
		r := m.graphRect()
		images = append(images, terminal.Placement{Graphic: m.graphSurface.graphic, X: r.x, Y: r.y, Cols: r.cols, Rows: r.rows})
	}
	lines, images = m.overlayNotice(lines, images)
	for i, line := range lines {
		lines[i] = background(fit(line, m.width))
	}
	lines[0] = m.surface.Marker(images) + lines[0]
	return strings.Join(lines[:min(m.height, len(lines))], "\n")
}

type tabControl struct {
	label string
	tab   int
	hit   cellRect
}

func (m Model) tabs() []tabControl {
	names := []string{"1 Home", "2 Scrittura", "3 Grafo", "4 TODO", "5 Attività"}
	x, y := 0, 1
	tabs := make([]tabControl, 0, len(names))
	for i, label := range names {
		if m.width < 57 && i == 3 {
			x, y = 0, 2
		}
		w := lipgloss.Width(label) + 4
		tabs = append(tabs, tabControl{label, i, cellRect{x, y, w, 1}})
		x += w + 1
	}
	return tabs
}

func (m Model) header(l layout) []string {
	lines := []string{fit(accent.Render(" ◉  NOTESFLOW")+"  /  "+clean(m.scopeName()), m.width)}
	tabRows := 1
	if m.width < 57 {
		tabRows = 2
	}
	for range tabRows {
		lines = append(lines, "")
	}
	for _, tab := range m.tabs() {
		style := muted.Background(lipgloss.Color(palette.Surface))
		if (m.commandMode && tab.tab == m.navTab) || (!m.commandMode && tab.tab == m.tab && m.focus == mainFocus) {
			style = selectedStyle
		}
		lines[tab.hit.y] += style.Render("  "+tab.label+"  ") + " "
	}
	first := m.navigationHint()
	second := " ? Agente · r/R Roadmap · p PDF · t TODO · b Compila · c Corso · a API"
	if l.header == 3 {
		return append(lines, muted.Render(fit(strings.TrimRight(first+"  "+strings.TrimSpace(second), " "), m.width)))
	}
	return append(lines, muted.Render(fit(first, m.width)), muted.Render(fit(second, m.width)))
}

func (m Model) navigationHint() string {
	if m.commandMode {
		return " q Esci · 1–5 Schede · ←/→ · Invio · Tab Torna"
	}
	return " Shift+Tab → q Esci · 1–5 Schede"
}

func (m Model) quitButton(l layout) cellRect {
	hint := m.navigationHint()
	before, _, _ := strings.Cut(hint, "q Esci")
	x := lipgloss.Width(before)
	row := l.header - 1
	if m.width < 75 {
		row--
	}
	return cellRect{x, row, min(6, max(0, m.width-x)), 1}
}

func background(text string) string {
	if os.Getenv("NO_COLOR") != "" {
		return text
	}
	const dark = "\x1b[48;2;9;11;16m"
	return dark + strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+dark) + "\x1b[0m"
}

func editorMode(mode string) string {
	switch mode {
	case "normal":
		return "NORMALE"
	case "insert":
		return "INSERIMENTO"
	case "visual", "visual_line", "visual_block":
		return "SELEZIONE"
	case "cmdline_normal", "cmdline_insert":
		return "COMANDO"
	case "terminal":
		return "TERMINALE"
	default:
		return strings.ToUpper(mode)
	}
}

func (m Model) listRows(height int) []string {
	items := m.rows()
	if len(items) == 0 {
		return []string{muted.Render(" Nessun elemento")}
	}
	cursor := min(m.cursor, len(items)-1)
	start := listStart(cursor, len(items), height)
	rows := []string{}
	for i := start; i < min(len(items), start+height); i++ {
		label := clean(items[i].text)
		if i == cursor && m.tab != apiTab {
			rows = append(rows, accent.Render(" › "+label))
		} else {
			rows = append(rows, "   "+label)
		}
	}
	return rows
}

func listStart(cursor, length, height int) int {
	return max(0, min(min(cursor, length-1)-height/2, length-height))
}

func (m Model) summaryRows() []string {
	if m.summarizing {
		return []string{" Riepilogo in elaborazione…"}
	}
	if m.summary == "" {
		return []string{" Premi s per generare il riepilogo"}
	}
	lines := strings.Split(m.summary, "\n")
	lines = lines[min(m.summaryOffset, len(lines)-1):]
	for i, line := range lines {
		lines[i] = clean(line)
	}
	return lines
}

func (m Model) help() string {
	switch {
	case m.workspacePickerOpen():
		return " ↑↓ Seleziona · Invio Conferma · Esc Salta"
	case m.formOpen():
		return " Esc Torna"
	case m.focus == roadFocus:
		items := m.milestones()
		if len(items) > 0 {
			item := items[min(m.roadCursor, len(items)-1)]
			if source := item.Evidence; source != nil {
				return fmt.Sprintf(" ←/→ Tappe · Invio Roadmap · e Appunti · %s:%d", clean(source.File), source.Line)
			}
		}
		return " ←/→ Tappe · Invio Apri la fonte · Esc Torna"
	case m.focus == sidebarFocus:
		return " ←/→ Filtri TODO · Invio Apri la lista · Esc Torna"
	case m.focus == pdfFocus:
		return " ←/→ pagina · ↑/↓ scorri testo · t testo/pagina · Esc torna"
	case m.tab == editorTab:
		return " :w Salva · Shift+Tab poi ? Agente · Shift+Tab poi b Compila"
	case m.tab == graphTab:
		return " / Cerca · ↑↓ Nodi · Invio entra/apri · Esc torna · Pg↑/Pg↓ pagina · f Filtra · +/− zoom"
	case m.tab == todoTab:
		return " ↑↓ Scorri · f Tipo · s Riepilogo · v Lista · Invio Apri"
	case m.tab == activityTab && m.agents.detail != 0 && m.agents.book != nil:
		return m.foldAgentHelp()
	default:
		return " ↑↓ Scorri · Invio Seleziona · Shift+Tab Naviga"
	}
}

type milestone struct {
	parser.Milestone
	source   source
	editable bool
}

func (m Model) milestones() []milestone {
	var items []milestone
	for _, road := range m.snapshot.Roadmaps {
		if !m.fileInScope(road.File) {
			continue
		}
		if strings.HasSuffix(strings.ToLower(road.File), ".pdf") {
			for _, item := range road.Items {
				items = append(items, milestone{Milestone: item})
			}
			continue
		}
		var checklist []parser.Milestone
		for _, item := range road.Items {
			if item.Done != nil {
				checklist = append(checklist, item)
			}
		}
		if len(checklist) == 0 {
			checklist = road.Items
		}
		for _, item := range checklist {
			items = append(items, milestone{item, source{road.File, item.Line}, true})
		}
	}
	return items
}

func (m Model) roadRows() []string {
	items := m.milestones()
	progress, _ := milestoneProgress(items)
	keys := "r seleziona · R comprimi"
	if (m.tab == editorTab || m.focus != mainFocus) && !m.commandMode {
		keys = "Shift+Tab, poi r/R"
	}
	title := " Roadmap · " + progress + "  [" + keys + "]"
	lines := []string{accent.Render(fit(title, m.width))}
	if len(items) == 0 {
		return append(lines, muted.Render(fit(" Aggiungi una roadmap nel corso", m.width)), "", "")
	}
	boxWidth := max(14, min(29, (m.width-8)/3))
	visible := max(1, (m.width-1)/(boxWidth+3))
	cursor := min(m.roadCursor, len(items)-1)
	start := max(0, min(cursor-visible/2, len(items)-visible))
	blocks := [3]strings.Builder{}
	for i := start; i < min(len(items), start+visible); i++ {
		if i > start {
			blocks[0].WriteString("   ")
			blocks[1].WriteString(dim.Render("─▶ "))
			blocks[2].WriteString("   ")
		}
		item := items[i]
		style, mark := graphColors[2], "○ "
		if item.Done != nil && *item.Done {
			style, mark = accent.Bold(false), "✓ "
		}
		if item.Evidence != nil && item.Evidence.Review != "" {
			style, mark = graphColors[3], "! "
		}
		if i == cursor && m.focus == roadFocus {
			style = pink
			if mark != "! " {
				mark = "› "
			}
		}
		dashes := strings.Repeat("─", boxWidth-2)
		blocks[0].WriteString(style.Render("╭" + dashes + "╮"))
		blocks[1].WriteString(style.Render("│" + fit(ansi.Truncate(mark+clean(item.Title), boxWidth-2, "…"), boxWidth-2) + "│"))
		blocks[2].WriteString(style.Render("╰" + dashes + "╯"))
	}
	for i := range blocks {
		lines = append(lines, fit(blocks[i].String(), m.width))
	}
	return lines
}

func (m Model) status() (full, concise string) {
	parts := []string{clean(m.cfg.Project)}
	if status := m.agentStatus(); status != "" {
		parts = append(parts, status)
		concise = status
	}
	if m.manager != nil {
		state := m.manager.Snapshot()
		current, shortProvider := "provider in attesa", "provider in attesa"
		if len(state.Profiles) > 0 {
			profile := state.Profiles[0]
			for _, candidate := range state.Profiles {
				if candidate.ID == state.Active {
					profile = candidate
					break
				}
			}
			current = clean(profile.ID + "/" + profile.Model + " · " + profile.Status)
			shortProvider = clean(profile.ID + " · " + profile.Status)
			if !profile.AvailableAt.IsZero() {
				current += " → " + profile.AvailableAt.Local().Format("15:04")
			} else if !profile.WindowEnd.IsZero() {
				current += " → " + profile.WindowEnd.Local().Format("15:04")
			}
		}
		parts = append(parts, current)
		if concise == "" {
			concise = shortProvider
		}
	}
	if concise == "" {
		concise = parts[0]
	}
	return " " + strings.Join(parts, "  │  "), concise
}
