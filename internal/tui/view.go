package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

type layout struct {
	left, right, body, todo, pdf, header, road int
	split                                      bool
}

func (m Model) layout() layout {
	l := layout{left: m.width, header: 3, road: 4}
	if m.width < 75 {
		l.header = 4
	}
	if m.width < 57 {
		l.header++
	}
	if m.roadCollapsed || m.height < 24 {
		l.road = 1
	}
	l.body = max(0, m.height-l.header-l.road-3)
	l.split = m.width >= 66 || l.body < 12
	if l.split {
		l.left = m.width * 2 / 3
		l.right = max(0, m.width-l.left)
		l.todo = min(9, max(3, l.body/3))
		l.pdf = max(0, l.body-l.todo)
	} else {
		l.todo = max(3, l.body/5)
		l.pdf = max(4, l.body/3)
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
		style = accent
	}
	count := max(0, width-2)
	title = " " + clean(title) + " "
	title = ansi.Truncate(title, count, "")
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
		return fit("Notesflow · F12 esci", m.width)
	}
	l := m.layout()
	lines := m.header(l)
	if l.body >= 2 {
		title := "Operazioni del corso"
		width, height := l.left-2, l.body-2
		var main []string
		switch m.tab {
		case editorTab:
			title = "Neovim"
			if mode := m.editorFrame.Mode(); mode != "" {
				title += " · " + strings.ToUpper(mode)
			}
			switch {
			case m.editorBusy && m.editor == nil:
				main = []string{" Avvio Neovim…"}
			case m.editor == nil:
				main = []string{" Invio · avvia Neovim"}
			default:
				main = m.editorFrame.Rows()
			}
		case graphTab:
			title = "Grafo del corso"
			main = m.graphRows(width, height)
		case todoTab:
			title = "TODO del corso"
			if m.showSummary {
				title = "Riepilogo TODO"
				main = m.summaryRows()
			} else {
				main = m.listRows(height)
			}
		case activityTab:
			title = "Attività"
			main = m.listRows(height)
		case apiTab:
			title = "Dashboard API"
			main = m.listRows(height)
		default:
			main = m.listRows(height)
		}
		left := panel(title, main, l.left, l.body, m.focus == mainFocus)
		rightWidth := l.right
		if !l.split {
			rightWidth = l.left
		}
		top := panel(fmt.Sprintf("TODO · %d", len(m.tasks(false))), m.sidebarTasks(l.todo-2), rightWidth, l.todo, false)
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
	for len(lines) < m.height-l.road-3 {
		lines = append(lines, "")
	}
	lines = append(lines, m.roadRows(l.road)...)
	lines = append(lines, muted.Render(fit(m.help(), m.width)), fit(clean(m.message), m.width), accent.Render(fit(m.status(), m.width)))
	return strings.Join(lines[:min(m.height, len(lines))], "\n")
}

func (m Model) header(l layout) []string {
	lines := []string{fit(accent.Render(" NOTESFLOW")+"  /  "+clean(m.scopeName()), m.width)}
	names := []string{"F1 Home", "F2 Editor", "F3 Grafo", "F4 TODO", "F5 Attività"}
	var tabs []string
	for i, label := range names {
		style := muted
		if i == m.tab && m.focus == mainFocus {
			style = selectedStyle
		}
		tabs = append(tabs, style.Render(" "+label+" "))
	}
	if m.width < 57 {
		lines = append(lines, fit(strings.Join(tabs[:3], " "), m.width), fit(strings.Join(tabs[3:], " "), m.width))
	} else {
		lines = append(lines, fit(strings.Join(tabs, " "), m.width))
	}
	if l.header == 3 {
		return append(lines, muted.Render(fit(" F6 Roadmap   F7 Collassa   F8 PDF   F9 Compila   F10 Corso   F12 Esci", m.width)))
	}
	return append(lines, muted.Render(fit(" F6 Roadmap   F7 Collassa   F8 PDF", m.width)), muted.Render(fit(" F9 Compila   F10 Corso   F12 Esci", m.width)))
}

func (m Model) sidebarTasks(height int) []string {
	if height <= 0 {
		return nil
	}
	tasks := m.tasks(false)
	if len(tasks) == 0 {
		return []string{muted.Render(" Nessun TODO nel corso")}
	}
	limit := height
	if len(tasks) > height && height > 1 {
		limit--
	}
	var rows []string
	for _, task := range tasks[:min(len(tasks), limit)] {
		mark := accent.Render(" ◇ ")
		if task.Kind == "VISUAL" {
			mark = pink.Render(" ◆ ")
		}
		rows = append(rows, mark+clean(task.Text))
	}
	if len(tasks) > limit && height > 1 {
		rows = append(rows, muted.Render(fmt.Sprintf(" F4 · altri %d", len(tasks)-limit)))
	}
	return rows
}

func (m Model) listRows(height int) []string {
	items := m.rows()
	if len(items) == 0 {
		return []string{muted.Render(" Nessun elemento")}
	}
	cursor := min(m.cursor, len(items)-1)
	start := max(0, min(cursor-height/2, len(items)-height))
	rows := []string{}
	for i := start; i < min(len(items), start+height); i++ {
		label := clean(items[i].text)
		if i == cursor && m.tab != activityTab && m.tab != apiTab {
			rows = append(rows, accent.Render(" › "+label))
		} else {
			rows = append(rows, "   "+label)
		}
	}
	return rows
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
	case m.focus == roadFocus:
		return " ←/→ Tappe · Invio Apri la fonte · Esc Torna"
	case m.focus == pdfFocus:
		return " ←/→ Pagina PDF · Esc Torna"
	case m.tab == editorTab:
		return " Tasti Neovim · :w Salva · F1 Home · F9 Compila dal disco"
	case m.tab == graphTab:
		return " ↑↓ Nodi · ←→ Pan · +/− Zoom · 0 Inquadra · / Cerca · f Filtra · Invio Apri"
	case m.tab == todoTab:
		return " ↑↓ Scorri · f Tipo · s Riepilogo · v Lista · Invio Apri"
	default:
		return " ↑↓ Scorri · Invio Seleziona · Tab Cambia vista · a API"
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

func (m Model) roadRows(height int) []string {
	items := m.milestones()
	done, total := 0, 0
	for _, item := range items {
		if item.Done != nil {
			total++
			if *item.Done {
				done++
			}
		}
	}
	progress := fmt.Sprintf("%d tappe", len(items))
	if total > 0 {
		progress = fmt.Sprintf("%d/%d completate", done, total)
	}
	title := " Roadmap · " + progress + "  [F6 seleziona · F7 collassa]"
	if height == 1 {
		if m.focus == roadFocus && len(items) > 0 {
			selected := min(m.roadCursor, len(items)-1)
			return []string{accent.Render(fit(fmt.Sprintf("← %d/%d  %s →", selected+1, len(items), clean(items[selected].Title)), m.width))}
		}
		shortcut := "  [F7 espandi]"
		if !m.roadCollapsed {
			shortcut = "  [F6 seleziona]"
		}
		return []string{accent.Render(fit("▸ Roadmap · "+progress+shortcut, m.width))}
	}
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
			style, mark = accent, "✓ "
		}
		if i == cursor && m.focus == roadFocus {
			style, mark = pink, "› "
		}
		dashes := strings.Repeat("─", boxWidth-2)
		blocks[0].WriteString(style.Render("╭" + dashes + "╮"))
		blocks[1].WriteString(style.Render("│" + fit(mark+clean(item.Title), boxWidth-2) + "│"))
		blocks[2].WriteString(style.Render("╰" + dashes + "╯"))
	}
	for i := range blocks {
		lines = append(lines, fit(blocks[i].String(), m.width))
	}
	return lines
}

func (m Model) status() string {
	state := m.manager.Snapshot()
	current := "provider in attesa"
	if len(state.Profiles) > 0 {
		profile := state.Profiles[0]
		for _, candidate := range state.Profiles {
			if candidate.ID == state.Active {
				profile = candidate
				break
			}
		}
		current = clean(profile.ID + "/" + profile.Model + " · " + profile.Status)
		if !profile.AvailableAt.IsZero() {
			current += " → " + profile.AvailableAt.Local().Format("15:04")
		} else if !profile.WindowEnd.IsZero() {
			current += " → " + profile.WindowEnd.Local().Format("15:04")
		}
	}
	return " " + clean(m.cfg.Project) + "  │  " + current
}
