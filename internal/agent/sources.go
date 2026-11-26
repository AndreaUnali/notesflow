package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/AndreaUnali/notesflow/internal/workflow"
)

const (
	maxSourceBytes = 48_000
	maxSources     = 8
	maxRoadmapRead = 1 << 20
)

var roadmapMention = regexp.MustCompile(`(?i)\broadmap\b`)
var roadmapChapter = regexp.MustCompile(`(?i)^capitolo\s+[0-9]+\s*[:.]\s*(.+)$`)

// SourcesFor includes the matching roadmap section when the request names the roadmap.
func SourcesFor(root string, job Job) []Ref {
	if job.Target.Title == "" || len(job.Sources) >= maxSources || !roadmapMention.MatchString(job.Goal) ||
		slices.ContainsFunc(job.Sources, func(ref Ref) bool {
			return strings.EqualFold(filepath.Base(ref.File), "roadmap.md") || strings.EqualFold(filepath.Base(ref.File), "roadmap.pdf")
		}) {
		return job.Sources
	}
	const roadmap = "roadmap.md"
	path, err := workflow.WorkspaceFile(root, roadmap)
	if err != nil {
		return job.Sources
	}
	text, err := readTextSource(path, maxRoadmapRead)
	if err != nil || len(text) > maxRoadmapRead || roadmapExcerpt(text, job.Target.Title) == "" {
		return job.Sources
	}
	return append(slices.Clone(job.Sources), Ref{File: roadmap, Heading: job.Target.Title})
}

func roadmapExcerpt(text, title string) string {
	lines := strings.Split(text, "\n")
	start, depth := -1, 0
	for i, line := range lines {
		heading := strings.TrimSpace(line)
		markdownDepth := len(heading) - len(strings.TrimLeft(heading, "#"))
		chapterDepth := 0
		if markdownDepth > 0 && markdownDepth <= 6 && len(heading) > markdownDepth && heading[markdownDepth] == ' ' {
			heading = strings.TrimSpace(strings.Trim(heading[markdownDepth:], "#"))
			chapterDepth = markdownDepth
		}
		if match := roadmapChapter.FindStringSubmatch(heading); match != nil {
			heading = match[1]
			chapterDepth = 1
		}
		if chapterDepth == 0 {
			continue
		}
		if start >= 0 && chapterDepth <= depth {
			return strings.TrimSpace(strings.Join(lines[start:i], "\n"))
		}
		if start < 0 && strings.EqualFold(strings.Join(strings.Fields(heading), " "), strings.Join(strings.Fields(title), " ")) {
			start, depth = i, chapterDepth
		}
	}
	if start < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n"))
}

func readTextSource(path string, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	return string(data), err
}

func SourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tex", ".md", ".txt", ".pdf":
		return true
	}
	return false
}

func ParseSource(raw string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	ref := Ref{File: raw}
	path, pages, split := strings.Cut(raw, ":")
	if split && strings.EqualFold(filepath.Ext(path), ".pdf") {
		a, b, rangePages := strings.Cut(pages, "-")
		first, err := strconv.Atoi(strings.TrimSpace(a))
		if err != nil || first < 1 {
			return Ref{}, errors.New("indica una pagina PDF positiva")
		}
		last := first
		if rangePages {
			last, err = strconv.Atoi(strings.TrimSpace(b))
		}
		if err != nil || last < first {
			return Ref{}, errors.New("indica un intervallo crescente, per esempio 12-28")
		}
		ref = Ref{File: strings.TrimSpace(path), First: first, Last: last}
	}
	if !SourceFile(ref.File) || filepath.IsAbs(ref.File) || ref.File == "" || strings.ContainsAny(ref.File, "\r\n\x00") {
		return Ref{}, errors.New("fonte relativa al progetto, in .tex, .md, .txt o .pdf")
	}
	ref.File = filepath.ToSlash(filepath.Clean(ref.File))
	return ref, nil
}

func readMaterials(ctx context.Context, root string, refs []Ref) ([]Material, error) {
	if len(refs) > maxSources {
		return nil, fmt.Errorf("seleziona al massimo %d fonti", maxSources)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	var sources []Material
	limit := maxSourceBytes / len(refs)
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := workflow.WorkspaceFile(root, ref.File)
		if err != nil {
			return nil, fmt.Errorf("fonte %s: %w", ref, err)
		}
		var text string
		if strings.EqualFold(filepath.Ext(path), ".pdf") {
			text, err = readPDFSource(ctx, path, ref)
		} else {
			readLimit := limit
			if ref.Heading != "" {
				readLimit = maxRoadmapRead
			}
			text, err = readTextSource(path, readLimit)
			if err == nil && ref.Heading != "" && len(text) <= readLimit {
				text = roadmapExcerpt(text, ref.Heading)
				if text == "" {
					err = fmt.Errorf("sezione %q non trovata", ref.Heading)
				}
			}
			if err == nil && len(text) > limit {
				err = errors.New("il testo supera il limite di contesto. Seleziona un estratto più breve")
			}
		}
		if err != nil {
			return nil, fmt.Errorf("fonte %s: %w", ref, err)
		}
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("fonte %s senza testo estraibile", ref)
		}
		sources = append(sources, Material{Ref: ref, Text: text})
	}
	return sources, nil
}

func readPDFSource(ctx context.Context, path string, ref Ref) (string, error) {
	count := workflow.PDFPages(ctx, path)
	if count < 1 {
		return "", errors.New("impossibile leggere il numero di pagine del PDF. Verifica il file e Poppler")
	}
	first, last := ref.First, ref.Last
	if first == 0 {
		first, last = 1, count
	}
	if first < 1 || last < first || last > count {
		return "", fmt.Errorf("il PDF ha %d pagine. Scegli un intervallo tra 1 e %d", count, count)
	}
	pages, err := workflow.PDFTextPages(ctx, path, first, last)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for i, page := range pages {
		if page = strings.TrimSpace(page); page != "" {
			fmt.Fprintf(&text, "[Pagina %d]\n%s\n\n", first+i, page)
		}
	}
	return text.String(), nil
}

func condenseSources(ctx context.Context, backend Backend, job Job, materials []Material) error {
	if len(materials) == 0 {
		return nil
	}
	limit := maxSourceBytes / len(materials)
	for i, source := range materials {
		if len(source.Text) <= limit {
			continue
		}
		chunks := sourceChunks(source.Text)
		var notes strings.Builder
		partLimit := (limit - len(chunks) + 1) / len(chunks)
		for _, chunk := range chunks {
			draft, err := backend.Guide(ctx, Request{
				Target: job.Target, Goal: job.Goal, Decision: job.Decision, ReadingBytes: partLimit,
				Sources: []Material{{Ref: source.Ref, Text: chunk}},
			})
			if err != nil {
				return fmt.Errorf("lettura di %s: %w", source.Ref, err)
			}
			summary := strings.TrimSpace(draft.Summary)
			if summary == "" {
				return fmt.Errorf("nessun appunto restituito durante la lettura di %s", source.Ref)
			}
			if notes.Len() > 0 {
				notes.WriteByte('\n')
			}
			notes.WriteString(summary[:utf8Boundary(summary, partLimit)])
		}
		materials[i].Text = notes.String()
	}
	return nil
}

// sourceChunks favors page boundaries and carries the page label across a split page.
func sourceChunks(text string) []string {
	const (
		chunkSize = 24_000
		pageLabel = "[Pagina "
	)
	var chunks []string
	prefix := ""
	for len(prefix)+len(text) > chunkSize {
		end := utf8Boundary(text, chunkSize-len(prefix))
		if next := strings.LastIndex(text[:end], "\n"+pageLabel); next >= end/2 {
			end = next + 1
		}
		part := text[:end]
		chunks = append(chunks, prefix+part)
		page := strings.LastIndex(part, "\n"+pageLabel) + 1
		if strings.HasPrefix(part[page:], pageLabel) {
			if line, _, found := strings.Cut(part[page:], "\n"); found && len(line) < 40 {
				prefix = line + "\n"
			}
		}
		text = text[end:]
		if strings.HasPrefix(text, pageLabel) {
			prefix = ""
		}
	}
	return append(chunks, prefix+text)
}

func utf8Boundary(text string, limit int) int {
	end := min(len(text), limit)
	for end > 0 && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	return end
}
