package editor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type cell struct {
	text string
	attr int
}

type highlight struct {
	fg, bg                           int
	bold, italic, underline, reverse bool
}

type Frame struct {
	cells                           []cell
	width, height, cursorX, cursorY int
	mode                            string
	colors                          highlight
	attrs                           map[int]highlight
	cursorVisible                   bool
}

type grid struct{ Frame }

func number(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func (g *grid) update(batch [][]any) (Frame, bool) {
	flushed := false
	for _, event := range batch {
		if len(event) == 0 {
			continue
		}
		name, _ := event[0].(string)
		for _, entry := range event[1:] {
			args, ok := entry.([]any)
			if ok {
				g.event(name, args)
			}
		}
		flushed = flushed || name == "flush"
	}
	if !flushed {
		return Frame{}, false
	}
	f := g.Frame
	f.cells, f.attrs = slices.Clone(g.cells), maps.Clone(g.attrs)
	return f, true
}

func (g *grid) event(name string, args []any) {
	switch name {
	case "grid_resize":
		if len(args) < 3 || number(args[0]) != 1 {
			return
		}
		w, h := number(args[1]), number(args[2])
		if w < 1 || h < 1 || w > 1000 || h > 1000 {
			return
		}
		next := make([]cell, w*h)
		for y := 0; y < min(h, g.height); y++ {
			copy(next[y*w:y*w+min(w, g.width)], g.cells[y*g.width:])
		}
		g.width, g.height, g.cells = w, h, next
	case "grid_clear":
		clear(g.cells)
	case "grid_cursor_goto":
		if len(args) >= 3 && number(args[0]) == 1 {
			g.cursorY, g.cursorX = number(args[1]), number(args[2])
			g.cursorVisible = true
		}
	case "grid_line":
		if len(args) >= 4 && number(args[0]) == 1 {
			cells, _ := args[3].([]any)
			g.line(number(args[1]), number(args[2]), cells)
		}
	case "grid_scroll":
		if len(args) >= 7 && number(args[0]) == 1 {
			g.scroll(number(args[1]), number(args[2]), number(args[3]), number(args[4]), number(args[5]), number(args[6]))
		}
	case "mode_change":
		if len(args) > 0 {
			g.mode, _ = args[0].(string)
		}
	case "default_colors_set":
		if len(args) >= 2 {
			g.colors = highlight{fg: number(args[0]), bg: number(args[1])}
		}
	case "hl_attr_define":
		if len(args) >= 2 {
			g.highlight(number(args[0]), args[1])
		}
	}
}

func (g *grid) highlight(id int, raw any) {
	values, _ := raw.(map[string]any)
	color := func(key string) int {
		if value, ok := values[key]; ok {
			return number(value)
		}
		return -1
	}
	if g.attrs == nil {
		g.attrs = make(map[int]highlight)
	}
	g.attrs[id] = highlight{fg: color("foreground"), bg: color("background"),
		bold: values["bold"] == true, italic: values["italic"] == true, reverse: values["reverse"] == true,
		underline: values["underline"] == true || values["undercurl"] == true}
}

func (g *grid) line(row, col int, values []any) {
	if row < 0 || row >= g.height || col < 0 || col >= g.width {
		return
	}
	attr := 0
	for _, value := range values {
		arg, ok := value.([]any)
		if !ok || len(arg) == 0 {
			continue
		}
		text, _ := arg[0].(string)
		if len(arg) >= 2 {
			attr = number(arg[1])
		}
		count := 1
		if len(arg) >= 3 {
			count = number(arg[2])
		}
		for range min(count, g.width-col) {
			g.cells[row*g.width+col] = cell{text, attr}
			col++
		}
	}
}

func (g *grid) scroll(top, bottom, left, right, dy, dx int) {
	top, bottom = max(0, top), min(bottom, g.height)
	left, right = max(0, left), min(right, g.width)
	prev := slices.Clone(g.cells)
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			to := y*g.width + x
			g.cells[to] = cell{}
			if sy, sx := y+dy, x+dx; sy >= top && sy < bottom && sx >= left && sx < right {
				g.cells[to] = prev[sy*g.width+sx]
			}
		}
	}
}

func (f Frame) Mode() string { return f.mode }

func (f Frame) Rows() []string {
	cache := make(map[int]lipgloss.Style)
	var result []string
	for y := range f.height {
		var line, run strings.Builder
		last, priorCursor := 0, false
		flush := func() {
			if run.Len() == 0 {
				return
			}
			if priorCursor {
				line.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#090B10")).Background(lipgloss.Color("#00F5D4")).Render(run.String()))
			} else {
				style, ok := cache[last]
				if !ok {
					style = f.style(last)
					cache[last] = style
				}
				line.WriteString(style.Render(run.String()))
			}
			run.Reset()
		}
		for x := range f.width {
			c := f.cells[y*f.width+x]
			if c.text == "" && x > 0 && lipgloss.Width(f.cells[y*f.width+x-1].text) > 1 {
				continue
			}
			cursor := f.cursorVisible && x == f.cursorX && y == f.cursorY
			if c.attr != last || cursor != priorCursor {
				flush()
				last, priorCursor = c.attr, cursor
			}
			if c.text == "" {
				c.text = " "
			}
			run.WriteString(c.text)
		}
		flush()
		result = append(result, line.String())
	}
	return result
}

func (f Frame) style(id int) lipgloss.Style {
	attr, ok := f.attrs[id]
	if !ok || id == 0 {
		attr = highlight{fg: -1, bg: -1}
	}
	if attr.fg < 0 {
		attr.fg = f.colors.fg
	}
	if attr.bg < 0 {
		attr.bg = f.colors.bg
	}
	style := lipgloss.NewStyle().Bold(attr.bold).Italic(attr.italic).Underline(attr.underline).Reverse(attr.reverse)
	if attr.fg >= 0 {
		style = style.Foreground(lipgloss.Color(fmt.Sprintf("#%06x", attr.fg)))
	}
	if attr.bg >= 0 {
		style = style.Background(lipgloss.Color(fmt.Sprintf("#%06x", attr.bg)))
	}
	return style
}
