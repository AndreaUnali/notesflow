package tui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type pdfRequest struct {
	scope, primary, explicit string
	page, width, height      int
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
	rows        []string
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
	for _, node := range m.snapshot.Nodes {
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
	areas := m.layout()
	width, height := areas.right-2, areas.pdf-3
	if !areas.split {
		width = areas.left - 2
	}
	explicit := m.cfg.Tools.PDF
	if !m.fileInScope(explicit) {
		explicit = ""
	}
	scope := ""
	if m.course != "" && m.course != rootCourse {
		scope = m.course
	}
	return pdfRequest{scope: scope, primary: m.mainTeX(), explicit: explicit, page: max(1, m.pdfPage),
		width: max(1, min(120, width)), height: max(1, min(70, height))}
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
	picture, err := workflow.PDFPage(ctx, path, state.page, max(request.width, request.height*2)*2)
	if err != nil {
		state.err = err
		return state
	}
	state.rows = terminalPage(picture, request.width, request.height)
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

func terminalPage(input image.Image, width, height int) []string {
	b := input.Bounds()
	if b.Empty() {
		return nil
	}
	scale := min(float64(width)/float64(b.Dx()), float64(height*2)/float64(b.Dy()))
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	type pair struct{ top, bottom color.RGBA }
	cache := make(map[pair]lipgloss.Style)
	sample := func(x, y int) color.RGBA {
		if y >= h {
			return color.RGBA{R: 9, G: 11, B: 16, A: 255}
		}
		// Bilinear interpolation retains page content at terminal cell sizes.
		xf := (float64(x)+.5)*float64(b.Dx())/float64(w) - .5
		yf := (float64(y)+.5)*float64(b.Dy())/float64(h) - .5
		x0, y0 := max(0, int(xf)), max(0, int(yf))
		x1, y1 := min(b.Dx()-1, x0+1), min(b.Dy()-1, y0+1)
		tx, ty := max(0, xf-float64(x0)), max(0, yf-float64(y0))
		colors := [4]color.RGBA{
			color.RGBAModel.Convert(input.At(b.Min.X+x0, b.Min.Y+y0)).(color.RGBA),
			color.RGBAModel.Convert(input.At(b.Min.X+x1, b.Min.Y+y0)).(color.RGBA),
			color.RGBAModel.Convert(input.At(b.Min.X+x0, b.Min.Y+y1)).(color.RGBA),
			color.RGBAModel.Convert(input.At(b.Min.X+x1, b.Min.Y+y1)).(color.RGBA),
		}
		mix := func(a, b, c, d uint8) uint8 {
			return uint8((float64(a)*(1-tx)+float64(b)*tx)*(1-ty) + (float64(c)*(1-tx)+float64(d)*tx)*ty)
		}
		return color.RGBA{R: mix(colors[0].R, colors[1].R, colors[2].R, colors[3].R),
			G: mix(colors[0].G, colors[1].G, colors[2].G, colors[3].G),
			B: mix(colors[0].B, colors[1].B, colors[2].B, colors[3].B), A: 255}
	}
	format := func(c color.RGBA) lipgloss.Color {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B))
	}
	var rows []string
	for y := 0; y < h; y += 2 {
		var row strings.Builder
		row.WriteString(strings.Repeat(" ", max(0, (width-w)/2)))
		for x := 0; x < w; x++ {
			key := pair{sample(x, y), sample(x, y+1)}
			style, ok := cache[key]
			if !ok {
				style = lipgloss.NewStyle().Foreground(format(key.top)).Background(format(key.bottom))
				cache[key] = style
			}
			row.WriteString(style.Render("▀"))
		}
		rows = append(rows, row.String())
	}
	return rows
}

func (m Model) pdfRows() []string {
	if m.pdf.request.scope != m.previewRequest().scope || m.pdf.request.primary != m.mainTeX() {
		return []string{muted.Render("Caricamento…")}
	}
	if m.pdf.err != nil {
		return []string{m.pdf.err.Error()}
	}
	if m.pdf.stamp.path == "" {
		hint := "F9 compila il progetto"
		if m.mainTeX() == "" {
			hint = "Imposta tools.main_tex o tools.pdf"
		}
		return []string{muted.Render("PDF non disponibile"), muted.Render(hint)}
	}
	page := fmt.Sprintf("Pag. %d", m.pdf.page)
	if m.pdf.pages > 0 {
		page += fmt.Sprintf("/%d", m.pdf.pages)
	}
	label := page + " · " + filepath.Base(m.pdf.stamp.path)
	return append([]string{muted.Render(label)}, m.pdf.rows...)
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
