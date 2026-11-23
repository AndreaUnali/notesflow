package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

type launchGraph struct {
	entry         network.Recent
	graph         terminalGraph
	image, dimmed *terminal.Graphic
}

type launcher struct {
	entries       []network.Recent
	graphs        []launchGraph
	surface       *terminal.Surface
	width, height int
	query, notice string
	chosen        string
	cursor        int
	t, held       float64
	hover         int
	pathOnly      bool
}

type drift time.Time

func ChooseNetwork(ctx context.Context, surface *terminal.Surface) (string, error) {
	entries, err := network.List()
	m := launcher{entries: entries, surface: surface, hover: -1}
	if err != nil {
		m.notice = err.Error()
	}
	for _, entry := range entries[:min(3, len(entries))] {
		nodes, edges := entry.Preview()
		if len(nodes) > 0 {
			m.graphs = append(m.graphs, launchGraph{entry: entry, graph: layoutGraph(0, edges, nodes, entry.Root)})
		}
	}
	result, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithOutput(surface), tea.WithMouseAllMotion(), tea.WithFPS(30)).Run()
	if err != nil {
		return "", err
	}
	return result.(launcher).chosen, nil
}

func (m launcher) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("Notesflow"), driftTick())
}

func driftTick() tea.Cmd {
	return tea.Tick(160*time.Millisecond, func(now time.Time) tea.Msg { return drift(now) })
}

func (m launcher) results() []network.Recent {
	if m.pathOnly {
		return nil
	}
	return network.Search(m.entries, m.query)
}

func (m *launcher) pick(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if dir, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(dir, strings.TrimPrefix(path, "~"))
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		m.notice = "Folder unavailable · enter a local directory"
		return nil
	}
	m.chosen = path
	return tea.Quit
}

func (m launcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case drift:
		m.t += .16
		if len(m.graphs) > 0 {
			return m, driftTick()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.renderGraphs()
	case tea.MouseMsg:
		event := tea.MouseEvent(v)
		previous := m.hover
		boxes := m.graphBoxes()
		m.hover = -1
		for i, box := range boxes {
			if m.graphVisible(i) && box.contains(event.X, event.Y) {
				m.hover = i
				if previous != i {
					m.held = m.t
				}
				if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress {
					cmd := m.pick(m.graphs[i].entry.Root)
					return m, cmd
				}
				break
			}
		}
	case tea.KeyMsg:
		key := v.String()
		if v.Paste {
			m.query += strings.ReplaceAll(strings.ReplaceAll(string(v.Runes), "\r", ""), "\n", "")
			m.cursor = 0
			return m, nil
		}
		switch key {
		case "ctrl+c", "esc":
			if m.pathOnly && key == "esc" {
				m.pathOnly, m.cursor, m.query, m.notice = false, 0, "", ""
				break
			}
			return m, tea.Quit
		case "ctrl+o":
			m.pathOnly, m.cursor, m.query, m.notice = true, 0, "", ""
		case "up":
			m.cursor = max(0, m.cursor-1)
		case "down":
			m.cursor = min(m.cursor+1, len(m.results()))
		case "enter":
			results := m.results()
			switch {
			case len(results) > 0 && m.cursor < len(results):
				cmd := m.pick(results[m.cursor].Root)
				return m, cmd
			case m.query != "":
				cmd := m.pick(m.query)
				return m, cmd
			default:
				m.pathOnly = true
			}
		case "backspace":
			runes := []rune(m.query)
			m.query, m.cursor = string(runes[:max(0, len(runes)-1)]), 0
		default:
			if v.Type == tea.KeyRunes || v.Type == tea.KeySpace {
				m.query, m.cursor = m.query+key, 0
			}
		}
	}
	return m, nil
}

func (m launcher) graphBoxes() []cellRect {
	if m.width < 50 || m.height < 28 {
		return nil
	}
	cols, rows := min(32, (m.width-12)/3), min(10, m.height/4)
	y := max(m.height*2/3, m.height/2+1)
	var boxes []cellRect
	for index := range m.graphs {
		center := (index + 1) * m.width / (len(m.graphs) + 1)
		t := m.t
		if index == m.hover {
			t = m.held
		}
		x := center - cols/2 + int(3*math.Sin(t*.75+float64(index)*2))
		top := y + int(2*math.Cos(t*.58+float64(index)*2))
		boxes = append(boxes, cellRect{max(0, min(m.width-cols, x)), max(0, min(m.height-rows-3, top)), cols, rows})
	}
	return boxes
}

func (m *launcher) renderGraphs() {
	graphs := append([]launchGraph(nil), m.graphs...)
	for i, box := range m.graphBoxes() {
		if m.surface.Enabled() {
			key := graphRenderKey{cols: box.cols, rows: box.rows, ratio: m.surface.CellRatio(m.width, m.height), zoom: 1}
			graphs[i].image = renderGraph(graphs[i].graph, key).graphic
			key.ambience = true
			graphs[i].dimmed = renderGraph(graphs[i].graph, key).graphic
		}
	}
	m.graphs = graphs
}

func (m launcher) View() string {
	if m.width < 20 || m.height < 8 {
		return "Notesflow"
	}
	rows := make([]string, m.height)
	if len(m.graphs) > 0 {
		dots := dim.Render(strings.Repeat(" ·    ", m.width/6))
		for i := 1; i < len(rows); i += 3 {
			rows[i] = dots
		}
	}
	width := min(m.width-4, 64)
	left := (m.width - width) / 2
	put := func(row int, value string) {
		if row >= 0 && row < len(rows) {
			rows[row] = overlayLine(rows[row], left, width, value)
		}
	}
	top := max(0, m.height/7)
	put(top, accent.Render("◉  NOTESFLOW"))
	prompt := "Which note network do you want to open?"
	if m.pathOnly {
		prompt = "Open folder…"
	}
	put(top+2, prompt)
	query := clean(m.query)
	if query == "" {
		query = muted.Render("Search by name or path…")
	}
	put(top+3, selectedStyle.Render(fit(" › "+query, width)))
	put(top+5, muted.Render("Recent networks"))
	results := m.results()
	end := m.height - 2
	if boxes := m.graphBoxes(); len(boxes) > 0 {
		for _, box := range boxes {
			end = min(end, box.y-2)
		}
	}
	limit := max(0, min(5, end-top-7))
	start := max(0, min(m.cursor-limit+1, len(results)-limit))
	for i, item := range results[start:min(len(results), start+limit)] {
		label := fmt.Sprintf("  %s  %s", clean(item.Name), muted.Render(clean(item.Root)))
		if start+i == m.cursor {
			label = accent.Render("› ") + label
		}
		put(top+6+i, label)
	}
	if m.notice != "" {
		put(top+6+limit, pink.Render(clean(m.notice)))
	}
	put(top+7+limit, muted.Render("↑↓   Enter Open   Ctrl+O Open folder…   Esc Back"))
	graphs := []terminal.Placement{}
	for i, box := range m.graphBoxes() {
		graph := m.graphs[i]
		active := m.hover == i || (m.query != "" && !m.pathOnly && len(results) > m.cursor && results[m.cursor].Root == graph.entry.Root)
		if m.pathOnly {
			continue
		}
		name := clean(graph.entry.Name)
		if active {
			name = accent.Render(name)
		} else {
			name = muted.Render(name)
		}
		graphic := graph.image
		if m.query != "" && !active {
			graphic, name = graph.dimmed, ""
		}
		if graphic != nil {
			graphs = append(graphs, terminal.Placement{Graphic: graphic, X: box.x, Y: box.y, Cols: box.cols, Rows: box.rows - 1})
		}
		rows[box.y+box.rows-1] = overlayLine(rows[box.y+box.rows-1], box.x, box.cols, name)
	}
	for i := range rows {
		rows[i] = background(fit(rows[i], m.width))
	}
	return m.surface.Marker(graphs) + strings.Join(rows, "\n")
}

// overlayLine replaces a range of terminal columns while preserving ANSI styles.
func overlayLine(base string, start, width int, overlay string) string {
	base = fit(base, start+max(width, ansi.StringWidth(base)-start))
	return ansi.Cut(base, 0, start) + fit(overlay, width) + ansi.Cut(base, start+width, ansi.StringWidth(base))
}

func (m launcher) graphVisible(index int) bool {
	if m.pathOnly {
		return false
	}
	if m.query == "" {
		return true
	}
	results := m.results()
	return m.cursor < len(results) && results[m.cursor].Root == m.graphs[index].entry.Root
}
