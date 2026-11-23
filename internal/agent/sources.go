package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/workflow"
)

const (
	maxSourceBytes = 48_000
	maxSources     = 8
)

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
		if err != nil || last < first || last-first >= 24 {
			return Ref{}, errors.New("indica un intervallo crescente di al massimo 24 pagine")
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
			first, last := ref.First, ref.Last
			if first == 0 {
				first, last = 1, workflow.PDFPages(ctx, path)
				if last < 1 || last > 24 {
					return nil, fmt.Errorf("%s: indica pagine precise, ad es. %s:12-14", ref, ref.File)
				}
			}
			var excerpt strings.Builder
			for page := first; page <= last; page++ {
				content, err := workflow.PDFText(ctx, path, page)
				if err != nil {
					return nil, fmt.Errorf("%s, pagina %d: %w", ref, page, err)
				}
				fmt.Fprintf(&excerpt, "\n[Pagina %d]\n%s", page, content)
			}
			text = excerpt.String()
		} else {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
			f.Close()
			if err != nil {
				return nil, err
			}
			text = string(data)
		}
		if len(text) > limit {
			return nil, fmt.Errorf("la fonte %s supera il limite di contesto: seleziona meno pagine o un estratto più breve", ref)
		}
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("fonte %s senza testo estraibile", ref)
		}
		sources = append(sources, Material{Ref: ref, Text: text})
	}
	return sources, nil
}
