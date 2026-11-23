package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

type todoTabSpec struct {
	kind, label, short string
}

var todoTabs = [...]todoTabSpec{
	{kind: "", label: "Tutti", short: "Tutti"},
	{kind: "TODO", label: "Classici", short: "C"},
	{kind: "VISUAL", label: "Visual", short: "V"},
	{kind: parser.MaintenanceTask, label: "Manutenzione", short: "M"},
}

type sidebarButton struct {
	tab, start, end int
	label           string
}

func sidebarTabs(width int) []sidebarButton {
	var buttons []sidebarButton
	x := 0
	for i, tab := range todoTabs {
		label := tab.label
		switch {
		case width >= 43:
			label = " " + label + " "
		case width < 30:
			label = tab.short
		case i == len(todoTabs)-1 && width < 36:
			label = "Manut."
		}
		next := x + lipgloss.Width(label)
		buttons = append(buttons, sidebarButton{i, x, next, label})
		x = next + 1
	}
	return buttons
}

func (m Model) sidebarItems() []parser.Task {
	tasks := m.tasks(false)
	kind := todoTabs[m.sidebarTab].kind
	if kind == "" {
		return tasks
	}
	return slices.DeleteFunc(tasks, func(task parser.Task) bool { return task.Kind != kind })
}

func (m *Model) openSidebarList() tea.Cmd {
	m.taskKind = todoTabs[m.sidebarTab].kind
	m.showSummary = false
	return m.setTab(todoTab)
}

type sidebarEntry struct {
	label string
	task  int
}

func (m Model) sidebarTasks(width, height int) []sidebarEntry {
	if width < 5 || height <= 0 {
		return nil
	}
	tasks := m.sidebarItems()
	if len(tasks) == 0 {
		return []sidebarEntry{{muted.Render(" Nessun TODO in questa scheda"), -1}}
	}
	var rows []sidebarEntry
	for i, task := range tasks {
		first := accent.Render(" ◇ ")
		switch task.Kind {
		case "VISUAL":
			first = pink.Render(" ◆ ")
		case parser.MaintenanceTask:
			first = graphColors[3].Render(" ! ")
		}
		lines := strings.Split(ansi.Wrap(clean(task.Text), width-3, " "), "\n")
		if len(lines) > 2 {
			lines = lines[:2]
			lines[1] = ansi.Truncate(lines[1]+"…", width-3, "…")
		}
		if len(rows)+len(lines) > height {
			if len(rows) == height {
				rows = rows[:height-1]
			}
			rows = append(rows, sidebarEntry{muted.Render(" Apri la lista TODO"), -1})
			break
		}
		for j, text := range lines {
			prefix := "   "
			if j == 0 {
				prefix = first
			}
			rows = append(rows, sidebarEntry{prefix + strings.TrimSpace(text), i})
		}
	}
	return rows
}

func (m Model) sidebarRows(width, height int) []string {
	if height < 1 {
		return nil
	}
	var row strings.Builder
	for _, button := range sidebarTabs(width) {
		style := muted
		if button.tab == m.sidebarTab {
			style = selectedStyle
		}
		row.WriteString(style.Render(button.label))
		row.WriteByte(' ')
	}
	rows := []string{row.String()}
	for _, entry := range m.sidebarTasks(width, height-1) {
		rows = append(rows, entry.label)
	}
	return rows
}
