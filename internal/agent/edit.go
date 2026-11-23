package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

var ErrConflict = errors.New("la destinazione è cambiata durante la delega")
var ending = regexp.MustCompile(`^[\t ]*\\end\s*\{document\}`)
var guardedLine = regexp.MustCompile(`(?i)\\(includegraphics|includepdf|input|include|graphicspath|write18|directlua|openout|usepackage|documentclass)\b|\\(begin|end)\s*\{\s*(figure\*?|tikzpicture|tikzcd|circuitikz|mermaid|picture|pgfpicture|filecontents\*?|document)\s*\}`)

type Span struct {
	Start, End int
	Lines      []string
}

func ReadTarget(root string, target Target) (string, error) {
	path, err := workflow.WorkspaceFile(root, target.File)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > 2<<20 {
		return "", errors.New("il file supera il limite di 2 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Excerpt(target, string(data))
}

func Excerpt(target Target, content string) (string, error) {
	span, err := Locate(target, content)
	if err != nil {
		return "", err
	}
	text := strings.Join(span.Lines[span.Start:span.End], "\n")
	if len(text) > 24000 {
		return "", errors.New("destinazione troppo lunga: scegli una sezione")
	}
	return text, nil
}

func Locate(target Target, content string) (Span, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	span := Span{Start: 0, End: len(lines), Lines: lines}
	if target.Section == "" {
		return span, nil
	}
	sections := parser.Parse(target.File, []byte(content)).Sections
	index := slices.IndexFunc(sections, func(section parser.Section) bool { return section.ID == target.Section })
	if index <= 0 {
		return Span{}, ErrConflict
	}
	section := sections[index]
	span.Start = section.Line - 1
	for _, next := range sections[index+1:] {
		if next.Level <= section.Level {
			span.End = next.Line - 1
			break
		}
	}
	for i := span.Start; i < span.End; i++ {
		if ending.MatchString(lines[i]) {
			span.End = i
			break
		}
	}
	if span.Start >= span.End {
		return Span{}, ErrConflict
	}
	return span, nil
}

func Replacement(job Job) (string, error) {
	text := strings.TrimRight(strings.ReplaceAll(job.Result.Text, "\r\n", "\n"), "\n")
	if text == "" {
		text = strings.TrimRight(job.Original, "\n")
	}
	guarded := func(source string) []string {
		var result []string
		for line := range strings.SplitSeq(source, "\n") {
			if guardedLine.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "%") {
				result = append(result, strings.TrimSpace(line))
			}
		}
		return result
	}
	if !slices.Equal(guarded(job.Original), guarded(text)) {
		return "", errors.New("la proposta cambia immagini, inclusioni o comandi protetti")
	}
	if job.Target.Section != "" {
		before, _, _ := strings.Cut(job.Original, "\n")
		after, _, _ := strings.Cut(text, "\n")
		if before != after {
			return "", errors.New("la proposta cambia l’intestazione della sezione")
		}
	}
	var tasks []string
	for _, task := range Tasks(job) {
		tasks = append(tasks, "% TODO "+task.Kind+": "+task.Text)
	}
	if len(tasks) > 0 {
		at := len(text)
		if job.Target.Section == "" {
			for _, line := range strings.Split(text, "\n") {
				if ending.MatchString(line) {
					at = strings.LastIndex(text, line)
					break
				}
			}
		}
		text = strings.TrimRight(text[:at], "\n") + "\n" + strings.Join(tasks, "\n") + "\n" + text[at:]
	}
	return strings.TrimRight(text, "\n"), nil
}

func Tasks(job Job) []parser.Task {
	var tasks []parser.Task
	add := func(kind, text string) {
		tasks = append(tasks, parser.Task{NodeID: job.Target.Section, File: job.Target.File, Line: max(job.Target.Line, job.Applied), Section: job.Target.Title, Kind: kind, Text: text})
	}
	for _, visual := range job.Result.Visuals {
		description := oneLine(visual.Description, 360)
		if description == "" {
			continue
		}
		if location := oneLine(visual.Location, 240); location != "" {
			description = location + " · " + description
		}
		add("VISUAL", description)
	}
	if job.Automatic {
		reason := job.Decision + " · " + job.DecisionWhy
		add(parser.MaintenanceTask, "Verifica la scelta automatica «"+oneLine(reason, 350)+"»")
	}
	return tasks
}

func oneLine(text string, maxRunes int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	return string(runes[:min(len(runes), maxRunes)])
}

func Update(job Job, current string) (string, int, error) {
	span, err := Locate(job.Target, current)
	if err != nil {
		return "", 0, err
	}
	before := strings.Join(span.Lines[span.Start:span.End], "\n")
	if before != job.Original {
		return "", 0, ErrConflict
	}
	replacement, err := Replacement(job)
	if err != nil {
		return "", 0, err
	}
	eol := "\n"
	if strings.Contains(current, "\r\n") {
		eol = "\r\n"
	}
	rows := strings.Split(replacement, "\n")
	if span.End < len(span.Lines) && strings.HasSuffix(before, "\n") {
		rows = append(rows, "")
	}
	if span.End == len(span.Lines) && strings.HasSuffix(current, "\n") {
		rows = append(rows, "")
	}
	lines := slices.Replace(span.Lines, span.Start, span.End, rows...)
	return strings.Join(lines, eol), span.Start + 1, nil
}

func Apply(root string, job Job) (int, error) {
	file, err := workflow.WorkspaceFile(root, job.Target.File)
	if err != nil {
		return 0, err
	}
	if !strings.EqualFold(filepath.Ext(file), ".tex") {
		return 0, errors.New("la delega può modificare soltanto sorgenti .tex")
	}
	original, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	after, line, err := Update(job, string(original))
	if err != nil {
		return 0, err
	}
	if err := workflow.ReplaceSource(file, original, []byte(after)); err != nil {
		if errors.Is(err, workflow.ErrSourceChanged) {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("salvataggio della proposta: %w", err)
	}
	return line, nil
}
