package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/agent"
)

func activityTitle(job agent.Job) string {
	if job.Goal != "" && (job.Todo != nil || job.Kind == "" &&
		(!job.Write || job.Target.Title == "" || strings.EqualFold(job.Target.Title, "main"))) {
		return clean(job.Goal)
	}
	if job.Target.Title != "" {
		return clean(job.Target.Title)
	}
	if job.Target.File != "" {
		return clean(filepath.Base(job.Target.File))
	}
	return "Richiesta all’agente"
}

func activityKind(job agent.Job) string {
	switch {
	case job.Kind == agent.GraphActivity:
		return "Grafo"
	case job.Kind == agent.SummaryActivity:
		return "Riepilogo"
	case job.Todo != nil:
		return "TODO-AI"
	case job.Write:
		return "Delega"
	default:
		return "Domanda"
	}
}

func activityStatus(state string) (string, lipgloss.Style) {
	switch state {
	case agent.Completed, agent.Answered:
		return "Completato", accent
	case agent.Working:
		return "In corso", graphColors[2]
	case agent.Failed:
		return "Errore", pink
	default:
		return "In attesa", graphColors[3]
	}
}

func (m Model) activityState(job agent.Job) string {
	if job.State == agent.Ready && m.agents.applying == job.ID {
		return agent.Working
	}
	if job.State == agent.Completed && job.Todo != nil && len(job.Events) > 0 {
		latest := job.Events[len(job.Events)-1]
		if latest.Title == "Aggiornamento del PDF" {
			return latest.State
		}
	}
	return job.State
}

func activityTime(value time.Time, seconds bool) string {
	if value.IsZero() {
		return ""
	}
	pattern := "15:04"
	if seconds {
		pattern += ":05"
	}
	now, local := time.Now().Local(), value.Local()
	if now.Year() != local.Year() || now.YearDay() != local.YearDay() {
		pattern = "02/01 " + pattern
	}
	return local.Format(pattern)
}

func activityDuration(elapsed time.Duration) string {
	elapsed = max(0, elapsed).Round(time.Second)
	switch {
	case elapsed < time.Second:
		return "<1 s"
	case elapsed < time.Minute:
		return fmt.Sprintf("%d s", elapsed/time.Second)
	case elapsed < time.Hour:
		return fmt.Sprintf("%d min %02d s", elapsed/time.Minute, elapsed/time.Second%60)
	default:
		return fmt.Sprintf("%d h %02d min", elapsed/time.Hour, elapsed/time.Minute%60)
	}
}

func activityTokens(count int) string {
	digits := fmt.Sprint(count)
	first := (len(digits)-1)%3 + 1
	var text strings.Builder
	text.WriteString(digits[:first])
	for i := first; i < len(digits); i += 3 {
		text.WriteByte('.')
		text.WriteString(digits[i : i+3])
	}
	return text.String() + " token"
}

func (m Model) activityRows(width, height int) []string {
	jobs := m.agentTasks()
	if len(jobs) == 0 {
		return []string{muted.Render("  Nessuna attività dell’agente")}
	}
	var rows []string
	start := listStart(m.cursor, len(jobs), height)
	for i := start; i < min(start+height, len(jobs)); i++ {
		job := jobs[i]
		label, color := activityStatus(m.activityState(job))
		stamp := activityTime(job.StartedAt(), false)
		meta := muted.Render(stamp) + "  " + color.Bold(true).Render(fit(label, 10))
		if width >= 62 {
			meta = muted.Render(fit(activityKind(job), 9)) + "  " + meta
		}
		prefix, titleStyle := "  ", homeTitleStyle.Bold(true)
		if i == min(m.cursor, len(jobs)-1) {
			prefix, titleStyle = accent.Render("› "), accent.Bold(true)
		}
		available := width - lipgloss.Width(meta) - 6
		title := ansi.Truncate(activityTitle(job), max(0, available), "…")
		rows = append(rows, prefix+graphColors[2].Render("✦")+" "+titleStyle.Render(fit(title, available))+"  "+meta)
	}
	return rows
}

func activityDivider(title string, width int) string {
	line := " ── " + title + " "
	return accent.Bold(true).Render(line) + dim.Render(strings.Repeat("─", max(0, width-lipgloss.Width(line))))
}

func activityText(text string, width int) []string {
	var rows []string
	for line := range strings.SplitSeq(text, "\n") {
		rows = append(rows, foldLine("  "+clean(line), width)...)
	}
	return rows
}

func (m Model) activityOverview(job agent.Job, width int) []string {
	state, color := activityStatus(m.activityState(job))
	meta := muted.Render(activityKind(job)) + "  " + color.Bold(true).Render(state)
	if stamp := activityTime(job.StartedAt(), true); stamp != "" {
		meta += muted.Render("  ·  " + stamp)
	}
	var rows []string
	for _, title := range foldLine("  ✦ "+activityTitle(job), width) {
		rows = append(rows, homeTitleStyle.Bold(true).Render(title))
	}
	rows = append(rows, "  "+meta)
	if job.Error != "" {
		rows = append(rows, activityText(job.Error, width)...)
	}
	if job.ErrorHelp != "" {
		rows = append(rows, activityText(job.ErrorHelp, width)...)
	} else if job.State == agent.Failed && job.Kind == "" {
		rows = append(rows, muted.Render("  Invio riprova la richiesta."))
	}
	if job.PauseReason != "" {
		rows = append(rows, activityText(job.PauseReason, width)...)
	}
	if job.Goal != "" {
		rows = append(rows, "", activityDivider("Richiesta", width))
		rows = append(rows, activityText(job.Goal, width)...)
	}
	if job.Target.File != "" {
		rows = append(rows, "", activityDivider("Destinazione", width))
		if job.Target.Title != "" && activityTitle(job) != clean(job.Target.Title) {
			rows = append(rows, activityText(job.Target.Title, width)...)
		}
		rows = append(rows, activityText(fmt.Sprintf("%s · riga %d", job.Target.File, max(1, job.Target.Line, job.Applied)), width)...)
	}
	if len(job.Sources) == 0 {
		return rows
	}
	rows = append(rows, "", activityDivider(fmt.Sprintf("Fonti · %d", len(job.Sources)), width))
	for _, pdf := range []bool{true, false} {
		heading := "Sorgenti e note"
		if pdf {
			heading = "PDF"
		}
		for _, ref := range job.Sources {
			if isPDFSource(ref) != pdf {
				continue
			}
			if heading != "" {
				rows = append(rows, muted.Bold(true).Render("  "+heading))
				heading = ""
			}
			name := sourceLabel(ref)
			if ref.Heading != "" {
				name += " · sezione " + ref.Heading
			}
			rows = append(rows, activityText("  • "+name, width)...)
		}
	}
	return rows
}

func (m Model) activityHistory(job agent.Job, width int) []string {
	rows := []string{"", activityDivider("Cronologia", width)}
	if len(job.Events) == 0 {
		note := "Nessuna fase registrata."
		if stamp := activityTime(job.Updated, true); stamp != "" {
			note = "Ultimo aggiornamento · " + stamp
		}
		return append(rows, muted.Render("  "+note))
	}
	for _, event := range slices.Backward(job.Events) {
		state := event.State
		if state == agent.Working && job.State == agent.Waiting {
			state = agent.Waiting
		}
		label, color := activityStatus(state)
		meta := label
		switch {
		case !event.Finished.IsZero():
			meta += " · " + activityDuration(event.Finished.Sub(event.Started))
		case event.State == agent.Working:
			meta += " · " + activityDuration(time.Since(event.Started))
		}
		if event.Tokens != nil {
			meta += " · " + activityTokens(*event.Tokens)
		}
		date := activityTime(event.Started, true)
		if lipgloss.Width(date+event.Title+meta)+7 <= width {
			room := width - lipgloss.Width(meta) - 2
			rows = append(rows, fit("  "+muted.Render(date)+"  "+homeTitleStyle.Bold(true).Render(event.Title), room)+"  "+color.Render(meta))
		} else {
			rows = append(rows, "  "+homeTitleStyle.Bold(true).Render(event.Title))
			rows = append(rows, foldLine("    "+muted.Render(date)+"  "+color.Render(meta), width)...)
		}
		for _, line := range foldLine("    "+clean(event.Detail), width) {
			if event.Detail != "" {
				rows = append(rows, muted.Render(line))
			}
		}
	}
	return rows
}

func (m *Model) scrollActivity(job agent.Job, delta int) {
	l := m.layout()
	last := max(0, len(m.agentDetailLines(job, l.left-2))-max(1, l.body-2))
	m.agents.offset = min(last, max(0, m.agents.offset+delta))
}
