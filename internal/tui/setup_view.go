package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AndreaUnali/notesflow/internal/palette"
)

type setupView struct {
	width, height, cursor int
}

const setupRowsStart = 8

func (v setupView) position(count int) (left, width, first, visible int) {
	width = min(80, max(1, v.width-4))
	left = max(0, (v.width-width)/2)
	visible = min(count, max(1, v.height-12))
	first = listStart(v.cursor, count, visible)
	return
}

func (v setupView) rowAt(msg tea.MouseMsg, count int) (int, bool) {
	event := tea.MouseEvent(msg)
	left, width, first, visible := v.position(count)
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress &&
		event.X >= left && event.X < left+width && event.Y >= setupRowsStart && event.Y < setupRowsStart+visible {
		return first + event.Y - setupRowsStart, true
	}
	return 0, false
}

func (v *setupView) move(key string, count int) bool {
	switch key {
	case "up", "shift+tab":
		v.cursor = (v.cursor + count - 1) % count
	case "down", "tab":
		v.cursor = (v.cursor + 1) % count
	default:
		return false
	}
	return true
}

func (v setupView) draw(theme, title, detail, status string, rows []string, swatches []string) string {
	if v.width < 26 || v.height < 12 {
		return "Allarga il terminale per continuare."
	}
	colors := palette.Preset(theme)
	left, width, first, visible := v.position(len(rows))
	base := lipgloss.NewStyle().Background(lipgloss.Color(colors.Background)).Foreground(lipgloss.Color(colors.Text))
	mark := base.Foreground(lipgloss.Color(colors.Cyan)).Bold(true)
	quiet := base.Foreground(lipgloss.Color(colors.Muted))
	active := base.Background(lipgloss.Color(colors.Selection)).Bold(true)
	padding := base.Render(strings.Repeat(" ", left))
	blank := base.Render(strings.Repeat(" ", v.width))
	lines := []string{blank}
	addRendered := func(value string) {
		lines = append(lines, padding+value+base.Render(strings.Repeat(" ", max(0, v.width-width-left))))
	}
	add := func(style lipgloss.Style, value string) {
		addRendered(style.Render(fit(value, width)))
	}
	sample := func(style lipgloss.Style) string {
		var strip strings.Builder
		frame := style.Background(lipgloss.Color(colors.Line))
		for _, color := range swatches {
			strip.WriteString(frame.Foreground(lipgloss.Color(color)).Render("■"))
			strip.WriteString(style.Render(" "))
		}
		return strip.String()
	}
	wrap := func(style lipgloss.Style, value string) {
		parts := foldLine(clean(value), width)
		for i := range 2 {
			if i < len(parts) {
				add(style, parts[i])
			} else {
				add(style, "")
			}
		}
	}
	add(mark, "NOTESFLOW")
	add(base.Bold(true), title)
	wrap(quiet, detail)
	add(base, "")
	sampleWidth := len(swatches) * 2
	const sampleGap = "   "
	showInline := len(swatches) > 0 && lipgloss.Width(rows[0])+2+len(sampleGap)+sampleWidth <= width
	if len(swatches) > 0 && !showInline {
		addRendered(sample(base) + base.Render(strings.Repeat(" ", width-sampleWidth)))
		add(mark, clean(status))
	} else {
		wrap(mark, status)
	}
	for i := first; i < first+visible; i++ {
		style := base
		prefix := "  "
		if i == v.cursor {
			style, prefix = active, "› "
		}
		label := prefix + rows[i]
		if i == 0 && showInline {
			label += sampleGap
			trailing := strings.Repeat(" ", width-lipgloss.Width(label)-sampleWidth)
			addRendered(style.Render(label) + sample(style) + style.Render(trailing))
		} else {
			add(style, label)
		}
	}
	add(quiet, "")
	add(quiet, "↑↓ o Tab Seleziona   Invio Conferma   Esc Torna")
	if visible < len(rows) {
		add(quiet, "Scorri per vedere le altre opzioni.")
	}
	for len(lines) < v.height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

func setupInput(text string, key tea.KeyMsg, limit int) string {
	if key.Paste {
		key.Runes = []rune(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, string(key.Runes)))
	}
	return appendInput(text, key, limit)
}
