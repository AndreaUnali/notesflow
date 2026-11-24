package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/palette"
)

var (
	roadFooterStyle    = accent.Background(lipgloss.Color("#102D2B"))
	helpFooterStyle    = muted.Background(lipgloss.Color(palette.Surface))
	messageFooterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFDBA0")).Background(lipgloss.Color("#292218"))
	statusFooterStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4C6FF")).Background(lipgloss.Color("#211B31"))
)

type footerSegment struct {
	text, brief string
	style       lipgloss.Style
	minimum     int
	width       int
}

func (m Model) footerRows(l layout) []string {
	if l.road > 0 {
		status, _ := m.status()
		return append(m.roadRows(), muted.Render(fit(m.help(), m.width)), fit(clean(m.message), m.width), accent.Render(fit(status, m.width)))
	}
	segments := m.footerSegments()
	var row strings.Builder
	for index, segment := range segments {
		if index > 0 {
			row.WriteByte(' ')
		}
		row.WriteString(segment.render())
	}
	return []string{row.String()}
}

func (m Model) roadButton(l layout) cellRect {
	if l.road > 0 {
		return cellRect{0, m.height - l.footer - l.road, m.width, 1}
	}
	return cellRect{0, m.height - l.footer, m.footerSegments()[0].width, 1}
}

func (m Model) footerSegments() []footerSegment {
	items := m.milestones()
	progress, shortProgress := milestoneProgress(items)
	action, key := "R espandi", "R"
	if !m.roadCollapsed {
		action, key = "r seleziona", "r"
	}
	if (m.tab == editorTab || m.focus != mainFocus) && !m.commandMode {
		action, key = "Shift+Tab, poi "+action, "⇧Tab "+key
	}
	road := footerSegment{
		text:  "▸ Roadmap · " + progress + "  [" + action + "]",
		brief: "▸ " + shortProgress + " · " + key,
		style: roadFooterStyle, minimum: 16,
	}
	if m.focus == roadFocus && len(items) > 0 {
		index := min(m.roadCursor, len(items)-1)
		road.text = fmt.Sprintf("← %d/%d  %s →", index+1, len(items), clean(items[index].Title))
		road.brief = fmt.Sprintf("← %d/%d →", index+1, len(items))
	}
	segments := []footerSegment{
		road,
		{text: strings.TrimSpace(m.help()), brief: m.briefHelp(), style: helpFooterStyle, minimum: 14},
	}
	if message := strings.TrimSpace(clean(m.message)); message != "" {
		segments = append(segments, footerSegment{text: message, brief: message, style: messageFooterStyle, minimum: 15})
	}
	full, concise := m.status()
	if value := strings.TrimSpace(full); value != "" {
		segments = append(segments, footerSegment{text: value, brief: concise, style: statusFooterStyle, minimum: 14})
	}
	sizeFooterSegments(segments, m.width)
	return segments
}

// Give every section a share of the row, including when the window is too narrow
// for its preferred width. Surplus fills the complete row after labels fit.
func sizeFooterSegments(segments []footerSegment, width int) {
	available := max(0, width-len(segments)+1)
	targets := make([][2]int, len(segments))
	for i, segment := range segments {
		wanted := lipgloss.Width(segment.text) + 2
		targets[i] = [2]int{min(segment.minimum, wanted), wanted}
	}
	for pass := range 2 {
		for available > 0 {
			grew := false
			for i := range segments {
				if available > 0 && segments[i].width < targets[i][pass] {
					segments[i].width++
					available--
					grew = true
				}
			}
			if !grew {
				break
			}
		}
	}
	for i := range segments {
		share := available / (len(segments) - i)
		segments[i].width += share
		available -= share
	}
}

func (s footerSegment) render() string {
	width := max(0, s.width-2)
	text := s.text
	if s.brief != "" && lipgloss.Width(text) > width {
		text = s.brief
	}
	text = ansi.Truncate(text, width, "…")
	return s.style.Render(fit(" "+text, s.width))
}

func milestoneProgress(items []milestone) (full, short string) {
	done, total, reviews := 0, 0, 0
	for _, item := range items {
		if item.Done == nil {
			continue
		}
		total++
		if *item.Done {
			done++
		}
		if item.Evidence != nil && item.Evidence.Review != "" {
			reviews++
		}
	}
	full = fmt.Sprintf("%d tappe", len(items))
	short = full
	if total > 0 {
		short = fmt.Sprintf("%d/%d", done, total)
		full = short + " completate"
	}
	if reviews > 0 {
		full += fmt.Sprintf(" · %d da verificare", reviews)
		short += fmt.Sprintf(" · !%d", reviews)
	}
	return full, short
}

func (m Model) briefHelp() string {
	switch {
	case m.workspacePickerOpen():
		return "↑↓ · Invio · Esc"
	case m.formOpen():
		return "Esc Torna"
	case m.focus == roadFocus, m.focus == sidebarFocus:
		return "←/→ · Invio · Esc"
	case m.focus == pdfFocus:
		return "←/→ PDF · t · Esc"
	case m.tab == editorTab:
		return "Shift+Tab · :w"
	case m.tab == graphTab:
		return "/ Cerca · ↑↓ · Invio"
	case m.tab == activityTab && m.agents.detail != 0 && m.agents.book != nil:
		if job, ok := m.selectedAgentJob(); ok {
			switch job.State {
			case agent.Paused:
				return "Invio Riprendi"
			case agent.Failed:
				return "Invio Riprova"
			}
		}
		return "↑↓ Leggi · Esc Lista"
	default:
		return "↑↓ · Invio · Tab"
	}
}
