package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/terminal"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type pdfRequest struct {
	scope, primary, explicit string
	page                     int
	graphics                 bool
}

type pdfStamp struct {
	path string
	size int64
	mod  time.Time
}

type pdfState struct {
	request     pdfRequest
	stamp       pdfStamp
	page, pages int
	pageImage   *terminal.Graphic
	text        string
	err         error
}

type pdfResult struct{ state pdfState }

// mainTeX prefers a configured root or a standalone TeX document in scope.
func (m Model) mainTeX() string {
	if file := m.cfg.Tools.MainTeX; file != "" && m.fileInScope(file) {
		return filepath.ToSlash(file)
	}
	var best string
	bestRank := 0
	for _, node := range m.snapshot.Files {
		if node.Kind != "file" || !m.fileInScope(node.File) || !strings.EqualFold(filepath.Ext(node.File), ".tex") {
			continue
		}
		rank := 0
		switch strings.ToLower(filepath.Base(node.File)) {
		case "main.tex":
			rank = 4
		case "master.tex", "root.tex":
			rank = 3
		default:
			if strings.Contains(node.Summary, "\\documentclass") {
				rank = 2
			}
		}
		if !strings.Contains(node.File, "/") && rank > 0 {
			rank += 4
		}
		if rank > bestRank {
			best, bestRank = node.File, rank
		}
	}
	return best
}

func (m Model) previewRequest() pdfRequest {
	explicit := m.cfg.Tools.PDF
	if !m.fileInScope(explicit) {
		explicit = ""
	}
	scope := ""
	if m.course != "" && m.course != rootCourse {
		scope = m.course
	}
	return pdfRequest{scope: scope, primary: m.mainTeX(), explicit: explicit, page: max(1, m.pdfPage),
		graphics: m.surface.Enabled() && !m.pdfText}
}

func (m *Model) probePDF() tea.Cmd {
	if m.pdfBusy || m.width < 28 || m.height < 12 {
		return nil
	}
	req := m.previewRequest()
	m.pdfBusy = true
	ctx, root, old := m.ctx, m.root, m.pdf
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return pdfResult{loadPDF(ctx, root, req, old)}
	}
}

func loadPDF(ctx context.Context, root string, request pdfRequest, previous pdfState) pdfState {
	state := pdfState{request: request, page: request.page}
	path := findPDF(root, request)
	if path == "" {
		return state
	}
	info, err := os.Stat(path)
	if err != nil {
		state.err = err
		return state
	}
	state.stamp = pdfStamp{path, info.Size(), info.ModTime()}
	if previous.stamp == state.stamp {
		if previous.request == request {
			return previous
		}
		state.pages = previous.pages
	} else {
		state.pages = workflow.PDFPages(ctx, path)
	}
	if state.pages > 0 {
		state.page = min(state.page, state.pages)
	}
	if request.graphics {
		picture, err := workflow.PDFPage(ctx, path, state.page)
		if err == nil {
			state.pageImage, err = terminal.PNG(picture)
		}
		if err == nil {
			return state
		}
		state.err = err
	}
	state.text, err = workflow.PDFText(ctx, path, state.page)
	if err != nil && state.err == nil {
		state.err = err
	}
	return state
}

func findPDF(root string, request pdfRequest) string {
	candidate := func(file string) string {
		if file == "" {
			return ""
		}
		file, _ = workflow.WorkspaceFile(root, file)
		return file
	}
	if request.explicit != "" {
		return candidate(request.explicit)
	}
	if request.primary != "" {
		base := strings.TrimSuffix(request.primary, filepath.Ext(request.primary)) + ".pdf"
		for _, dir := range []string{"", "build", "out"} {
			if file := candidate(filepath.Join(filepath.Dir(base), dir, filepath.Base(base))); file != "" {
				return file
			}
		}
	}
	for _, dir := range []string{"", "build", "out"} {
		path := filepath.Join(root, request.scope, dir)
		entries, _ := os.ReadDir(path)
		files := []string{}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(name), ".pdf") && !strings.EqualFold(name, "roadmap.pdf") {
				files = append(files, name)
			}
		}
		slices.Sort(files)
		if len(files) == 1 {
			if file := candidate(filepath.Join(request.scope, dir, files[0])); file != "" {
				return file
			}
		}
	}
	return ""
}

func (m Model) pdfRect() cellRect {
	l := m.layout()
	if l.split {
		return cellRect{l.left + 1, l.header + l.todo + 2, l.right - 2, l.pdf - 3}
	}
	return cellRect{1, l.header + l.body + l.todo + 2, l.left - 2, l.pdf - 3}
}

func (m Model) pdfCurrent() bool {
	r := m.previewRequest()
	return m.pdf.request.scope == r.scope && m.pdf.request.primary == r.primary && m.pdf.request.explicit == r.explicit
}

func (m Model) pdfPlacement() terminal.Placement {
	r := m.pdfRect()
	picture := m.pdf.pageImage
	if picture == nil || !m.pdfCurrent() || m.pdfText || r.cols < 4 || r.rows < 3 {
		return terminal.Placement{}
	}
	ratio := m.surface.CellRatio(m.width, m.height)
	aspect := float64(picture.Width) / float64(picture.Height)
	cols := min(r.cols, max(1, int(math.Round(float64(r.rows)*aspect/ratio))))
	rows := min(r.rows, max(1, int(math.Round(float64(cols)*ratio/aspect))))
	return terminal.Placement{Graphic: picture, X: r.x + (r.cols-cols)/2, Y: r.y, Cols: cols, Rows: rows}
}

func (m Model) pdfRows() []string {
	if !m.pdfCurrent() {
		return []string{muted.Render(" Caricamento…")}
	}
	if m.pdf.stamp.path == "" {
		hint := "b compila il documento"
		if m.mainTeX() == "" {
			hint = "Scegli un PDF con tools.pdf"
		}
		return []string{muted.Render(" PDF da compilare"), muted.Render(" " + hint)}
	}
	page := fmt.Sprintf(" Pag. %d", m.pdf.page)
	if m.pdf.pages > 0 {
		page += fmt.Sprintf("/%d", m.pdf.pages)
	}
	label := muted.Render(fit(page+" · "+clean(filepath.Base(m.pdf.stamp.path)), m.pdfRect().cols-5) + " ‹ › ")
	if m.pdfPlacement().Graphic != nil {
		return []string{label}
	}
	if m.pdf.request.graphics != m.previewRequest().graphics {
		return []string{label, muted.Render(" Caricamento…")}
	}
	if m.pdf.text != "" {
		text := strings.Split(m.pdf.text, "\n")
		offset := min(m.pdfOffset, len(text)-1)
		rows := []string{label}
		width := m.pdfRect().cols - 2
		blank := false
		for _, line := range text[offset:] {
			line = clean(strings.TrimSpace(line))
			if line == "" && blank {
				continue
			}
			blank = line == ""
			for _, wrapped := range strings.Split(ansi.Wrap(line, max(1, width), " "), "\n") {
				rows = append(rows, " "+wrapped)
			}
			if len(rows) > m.pdfRect().rows {
				break
			}
		}
		return rows
	}
	if m.pdf.err != nil {
		return []string{label, " " + clean(m.pdf.err.Error())}
	}
	return []string{label, muted.Render(" Pagina priva di testo estraibile")}
}

type compiled struct {
	file string
	err  error
}

func (m *Model) compile() tea.Cmd {
	if m.compiling {
		return nil
	}
	file := m.mainTeX()
	if file == "" {
		m.message = "Imposta tools.main_tex per scegliere il documento principale"
		return nil
	}
	m.compiling = true
	m.message = "Compilazione " + file + "…"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 90*time.Second)
		defer cancel()
		out, err := workflow.Compile(ctx, m.root, file, m.cfg.Tools.LatexEngine)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = errors.New("compilazione oltre 90 secondi")
		}
		return compiled{out, err}
	}
}
