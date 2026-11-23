package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type inputForm struct {
	mode, course       string
	tab                int
	field              int
	job                int
	targets            []agent.Target
	target             int
	text               string
	query              string
	choices            []agent.Ref
	selected           []agent.Ref
	match              int
	error              string
	chapter            workflow.Chapter
	buffer, bufferFile string
}

func (m Model) formOpen() bool {
	return m.form.mode != "" && m.form.tab == m.tab && m.form.course == m.course
}

func (m *Model) showAgentForm(mode string, id int, file string, line int) {
	if m.agents.book == nil {
		m.message = "Registro dell’agente non disponibile"
		return
	}
	form := inputForm{mode: mode, job: id, tab: m.tab, course: m.course}
	if mode == "chapter" {
		m.form = form
		return
	}
	if mode == "source" {
		form.field = 2
	}
	if mode == "write" || mode == "followup" {
		form.field = 1
	}
	if mode == "ask" || mode == "delegate" {
		form.targets = m.targets(mode == "delegate")
		for index, target := range form.targets {
			if target.File == file && target.Line <= max(1, line) {
				form.target = index
			}
		}
		if mode == "ask" {
			form.field = 1
		}
	}
	form.choices = m.sourceChoices()
	m.form = form
}

func (m Model) targets(texOnly bool) []agent.Target {
	var targets []agent.Target
	for _, file := range m.snapshot.Files {
		if !m.fileInScope(file.File) || file.Kind != "file" ||
			(texOnly && !strings.EqualFold(filepath.Ext(file.File), ".tex")) ||
			(!texOnly && !agent.SourceFile(file.File)) {
			continue
		}
		targets = append(targets, agent.Target{File: file.File, Title: workflow.ChapterName(file.File, file.Summary), Line: 1})
		var headings []agent.Target
		for _, section := range m.snapshot.Headings {
			if section.File == file.File && section.Kind == "section" {
				headings = append(headings, agent.Target{File: file.File, Section: section.ID, Title: section.Title, Line: section.Line})
			}
		}
		slices.SortStableFunc(headings, func(a, b agent.Target) int { return a.Line - b.Line })
		targets = append(targets, headings...)
	}
	return targets
}

func (m Model) sourceChoices() []agent.Ref {
	sources := m.agents.book.RecentSources(m.course)
	_ = filepath.WalkDir(m.scopeDir(), func(path string, entry fs.DirEntry, err error) error {
		if len(sources) >= 700 {
			return fs.SkipAll
		}
		if err != nil {
			return nil
		}
		if entry.IsDir() && path != m.scopeDir() && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
			return fs.SkipDir
		}
		if !entry.IsDir() && agent.SourceFile(path) {
			relative, err := filepath.Rel(m.root, path)
			if err == nil {
				ref := agent.Ref{File: filepath.ToSlash(relative)}
				if !slices.Contains(sources, ref) {
					sources = append(sources, ref)
				}
			}
		}
		return nil
	})
	return sources
}

func (f inputForm) matches() []agent.Ref {
	query := strings.ToLower(strings.TrimSpace(f.query))
	if file, _, hasPages := strings.Cut(query, ":"); hasPages && strings.HasSuffix(file, ".pdf") {
		return nil
	}
	var references []agent.Ref
	for _, ref := range f.choices {
		if strings.Contains(strings.ToLower(ref.String()), query) && !slices.Contains(f.selected, ref) {
			references = append(references, ref)
		}
	}
	return references
}

func (m *Model) formKey(key tea.KeyMsg) tea.Cmd {
	form := &m.form
	value := key.String()
	switch value {
	case "esc", "ctrl+c":
		if !m.chapterBusy {
			m.form = inputForm{}
		}
		return nil
	}
	if form.mode == "chapter-review" {
		if value == "enter" {
			return m.createChapter()
		}
		return nil
	}
	form.error = ""
	if form.mode == "chapter" {
		if value == "enter" {
			plan, err := workflow.PlanChapter(m.root, m.mainTeX(), form.text)
			if err != nil {
				form.error = err.Error()
			} else {
				form.chapter, form.mode = plan, "chapter-review"
			}
			return nil
		}
		form.text = appendInput(form.text, key, 120)
		return nil
	}
	if value == "ctrl+s" {
		if form.field == 2 && strings.TrimSpace(form.query) != "" && !m.addSource() {
			return nil
		}
		return m.submitAgentForm()
	}
	if form.mode == "source" {
		if value == "enter" && m.addSource() {
			return m.submitAgentForm()
		}
		m.sourceInput(key)
		return nil
	}
	if value == "tab" {
		first := 0
		if form.job != 0 {
			first = 1
		}
		size := 3 - first
		form.field = first + (form.field-first+1)%size
		return nil
	}
	switch form.field {
	case 0:
		switch value {
		case "left", "up":
			form.target = max(0, form.target-1)
		case "right", "down":
			form.target = min(max(0, len(form.targets)-1), form.target+1)
		case "enter":
			form.field = 1
		}
	case 1:
		if value == "enter" {
			return m.submitAgentForm()
		}
		form.text = appendInput(form.text, key, 6000)
	case 2:
		if value == "enter" {
			m.addSource()
		} else {
			m.sourceInput(key)
		}
	}
	return nil
}

func (m *Model) sourceInput(key tea.KeyMsg) {
	form := &m.form
	switch key.String() {
	case "up":
		form.match = max(0, form.match-1)
	case "down":
		form.match = min(max(0, len(form.matches())-1), form.match+1)
	case "ctrl+u":
		if len(form.selected) > 0 {
			form.selected = form.selected[:len(form.selected)-1]
		}
	default:
		form.query = appendInput(form.query, key, 500)
		form.match = 0
	}
}

func appendInput(value string, key tea.KeyMsg, limit int) string {
	if key.Paste {
		runes := []rune(value + strings.ReplaceAll(string(key.Runes), "\r\n", "\n"))
		return string(runes[:min(limit, len(runes))])
	}
	if key.Type == tea.KeyBackspace {
		runes := []rune(value)
		return string(runes[:max(0, len(runes)-1)])
	}
	if (key.Type == tea.KeyRunes || key.Type == tea.KeySpace) && !key.Alt {
		runes := []rune(value + key.String())
		return string(runes[:min(limit, len(runes))])
	}
	return value
}

func (m *Model) addSource() bool {
	form := &m.form
	var ref agent.Ref
	var err error
	matches := form.matches()
	switch {
	case strings.TrimSpace(form.query) == "" && len(matches) == 0:
		return false
	case len(matches) > 0 && (form.query == "" || !slices.ContainsFunc(matches, func(ref agent.Ref) bool { return ref.File == form.query })):
		ref = matches[min(form.match, len(matches)-1)]
	default:
		ref, err = agent.ParseSource(form.query)
	}
	if err == nil {
		_, err = workflow.WorkspaceFile(m.root, ref.File)
	}
	if err != nil {
		form.error = "Fonte · " + err.Error()
		return false
	}
	if len(form.selected) >= 8 {
		form.error = "Puoi selezionare fino a otto fonti."
		return false
	}
	if !slices.Contains(form.selected, ref) {
		form.selected = append(form.selected, ref)
	}
	form.query, form.match = "", 0
	return true
}

func (m *Model) submitAgentForm() tea.Cmd {
	form := &m.form
	if form.mode != "source" && strings.TrimSpace(form.text) == "" {
		form.error = "Scrivi una richiesta per l’agente."
		return nil
	}
	job := agent.Job{Course: m.course, Write: form.mode == "delegate", Goal: strings.TrimSpace(form.text)}
	if form.job == 0 {
		if len(form.targets) == 0 {
			form.error = "Apri o crea un sorgente prima di usare l’agente."
			return nil
		}
		job.Target = form.targets[form.target]
		original, err := agent.ReadTarget(m.root, job.Target)
		if form.mode == "ask" && form.bufferFile == job.Target.File && form.buffer != "" {
			original, err = agent.Excerpt(job.Target, form.buffer)
		}
		if form.mode == "ask" {
			job.Compilation = m.lastCompile
		}
		if err != nil {
			form.error = err.Error()
			return nil
		}
		job.Original = original
	} else {
		old, ok := m.agents.book.Lookup(form.job)
		if !ok {
			return nil
		}
		job = old
		if m.agents.applying == job.ID {
			form.error = "L’agente sta applicando questa proposta."
			return nil
		}
		if form.mode == "write" || (form.mode == "followup" && (job.State == agent.Completed || job.State == agent.Review)) {
			job.ID, job.Decision, job.DecisionWhy, job.Automatic = 0, "", "", false
			job.Buffered, job.BufferedHash, job.Applied = false, "", 0
			job.Deadline = time.Time{}
			job.Write = (form.mode == "write" || (form.mode == "followup" && old.Write))
			job.Goal = strings.TrimSpace(form.text)
			original, err := agent.ReadTarget(m.root, job.Target)
			if err != nil {
				form.error = err.Error()
				return nil
			}
			job.Original = original
		}
		if form.mode == "followup" && old.State == agent.Waiting {
			job.Decision, job.DecisionWhy, job.Automatic = strings.TrimSpace(form.text), "Risposta dell’utente", false
			job.Deadline = time.Time{}
			if m.agents.notice.id == old.ID {
				m.agents.notice = agentNotice{}
			}
		}
	}
	job.Sources = slices.Clone(job.Sources)
	for _, reference := range form.selected {
		if !slices.Contains(job.Sources, reference) {
			job.Sources = append(job.Sources, reference)
		}
	}
	if len(job.Sources) > 8 {
		form.error = "Questa delega ha già otto fonti."
		return nil
	}
	if form.mode == "source" && len(form.selected) == 0 {
		form.error = "Seleziona una fonte."
		return nil
	}
	if form.mode == "source" {
		if job.State == agent.Completed || job.State == agent.Paused || job.State == agent.Review {
			if m.saveAgent(job) {
				m.form = inputForm{}
				m.message = "Fonte aggiunta · " + job.Target.Title
			}
			return nil
		}
		if job.State == agent.Waiting {
			job.Deadline = time.Time{}
			if m.agents.notice.id == job.ID {
				m.agents.notice = agentNotice{}
			}
		}
	}
	if value := strings.TrimSpace(form.text); value != "" {
		job.History = append(slices.Clone(job.History), agent.Turn{Role: "user", Text: value})
	}
	asking := !job.Write
	cmd := m.beginAgent(job)
	if cmd == nil {
		return nil
	}
	m.form = inputForm{}
	m.message = "Agente al lavoro · " + job.Target.Title
	if asking || m.tab == activityTab {
		if job.ID == 0 {
			job.ID = m.agents.book.Jobs[len(m.agents.book.Jobs)-1].ID
		}
		m.openAgentActivity(job.ID)
	}
	return cmd
}

func (m Model) formTitle() string {
	return map[string]string{
		"chapter": "Nuovo capitolo", "chapter-review": "Conferma il capitolo",
		"ask": "Chiedi all’agente", "write": "Chiedi di modificare", "delegate": "Delega all’agente",
		"followup": "Messaggio all’agente", "source": "Aggiungi una fonte",
	}[m.form.mode]
}

func (m Model) formRows(width int) []string {
	f := m.form
	var rows []string
	field := func(index int, title, value string) {
		mark := muted.Render("  " + title)
		if f.field == index {
			mark = accent.Render("› " + title)
		}
		rows = append(rows, mark)
		rows = append(rows, foldLine("  "+clean(value), width)...)
	}
	switch f.mode {
	case "chapter":
		field(0, "Titolo", f.text)
		rows = append(rows, "", " Invio continua · Esc torna")
	case "chapter-review":
		rows = append(rows, " "+clean(f.chapter.Title), "", " File   "+clean(f.chapter.File),
			fmt.Sprintf(" Indice %s · prima di \\end{document}, riga %d", clean(f.chapter.Main), f.chapter.Line), "",
			" Invio crea e apri · Esc annulla")
	default:
		if len(f.targets) > 0 {
			t := f.targets[f.target]
			field(0, "Destinazione  ←/→", fmt.Sprintf("%s · %s:%d", t.Title, t.File, t.Line))
		}
		if f.mode != "source" {
			field(1, "Richiesta", f.text)
		}
		field(2, "Fonti  ·  es. slide.pdf:12-14", f.query)
		if len(f.selected) > 0 {
			var names []string
			for _, reference := range f.selected {
				names = append(names, reference.String())
			}
			rows = append(rows, foldLine("  ✓ "+strings.Join(names, " · "), width)...)
		}
		if f.field == 2 {
			matches := f.matches()
			start := max(0, min(f.match-1, len(matches)-3))
			for index := start; index < min(start+3, len(matches)); index++ {
				name := "    " + clean(matches[index].String())
				if index == f.match {
					name = accent.Render("  › " + clean(matches[index].String()))
				}
				rows = append(rows, name)
			}
		}
		if f.mode == "source" {
			rows = append(rows, "", " ↑↓ Cerca · Invio aggiungi e riprendi · Esc torna")
		} else {
			rows = append(rows, "", " Tab Campo · Invio Aggiungi fonte / Invia richiesta · Ctrl+S Invia · Esc Torna")
		}
	}
	if f.error != "" {
		rows = append(foldLine(" "+clean(f.error), width), rows...)
	}
	return rows
}

func foldLine(text string, width int) []string {
	if width <= 1 {
		return nil
	}
	return strings.Split(ansi.Wrap(text, width, ""), "\n")
}
