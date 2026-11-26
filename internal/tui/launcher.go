package tui

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/terminal"
)

type launchGraph struct {
	entry         network.Recent
	graph         terminalGraph
	image, dimmed *terminal.Graphic
	text          []string
	asset         graphRenderKey
	paused        float64
}

type launcher struct {
	ctx           context.Context
	entries       []network.Recent
	discovery     <-chan network.Recent
	graphs        []launchGraph
	surface       *terminal.Surface
	wordmark      *terminal.Graphic
	brandKey      graphRenderKey
	frame         string
	matches       []network.Recent
	cellW, cellH  int
	started       time.Time
	width, height int
	query, notice string
	chosen        string
	cursor        int
	t, held       float64
	hover         int
	pathOnly      bool
	searchFocused bool
	action        LaunchAction
	searching     bool
}

type launchLayout struct {
	brand, search, items, action cellRect
	prompt, label, help          int
	graphRows                    int
	sideGraphs                   bool
}

type drift time.Time
type foundNetworks struct {
	entries []network.Recent
	done    bool
}

type LaunchAction int

const (
	LaunchOpen LaunchAction = iota
	LaunchNew
	LaunchSettings
)

func ChooseNetwork(ctx context.Context, surface *terminal.Surface) (string, LaunchAction, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := network.List()
	m := launcher{ctx: ctx, entries: entries, surface: surface, hover: -1, searching: true, started: time.Now()}
	m.discovery = network.Discover(ctx, network.DefaultSearchRoots(entries))
	if err != nil {
		m.notice = err.Error()
	}
	m.prepareGraphs()
	result, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithOutput(surface), tea.WithMouseAllMotion(), tea.WithFPS(45)).Run()
	if err != nil {
		return "", LaunchOpen, err
	}
	chosen := result.(launcher)
	return chosen.chosen, chosen.action, nil
}

func (m launcher) Init() tea.Cmd {
	var animation tea.Cmd
	if len(m.graphs) > 0 {
		animation = driftTick()
	}
	return tea.Batch(tea.SetWindowTitle("Notesflow"), animation, m.waitNetworks())
}

func (m launcher) waitNetworks() tea.Cmd {
	return func() tea.Msg {
		var batch foundNetworks
		select {
		case <-m.ctx.Done():
			return nil
		case entry, ok := <-m.discovery:
			if !ok {
				return foundNetworks{done: true}
			}
			batch.entries = append(batch.entries, entry)
		}
		for len(batch.entries) < 32 {
			select {
			case entry, ok := <-m.discovery:
				if !ok {
					batch.done = true
					return batch
				}
				batch.entries = append(batch.entries, entry)
			default:
				return batch
			}
		}
		return batch
	}
}

func driftTick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(now time.Time) tea.Msg { return drift(now) })
}

func (m launcher) results() []network.Recent {
	if m.pathOnly {
		return nil
	}
	return network.Search(m.entries, m.query)
}

func (m *launcher) prepareGraphs() {
	previous := m.graphs
	m.graphs = nil
	for _, item := range m.entries {
		if len(m.graphs) == 3 {
			break
		}
		if index := slices.IndexFunc(previous, func(g launchGraph) bool { return g.entry.Root == item.Root }); index >= 0 {
			m.graphs = append(m.graphs, previous[index])
			continue
		}
		if item.Opened.IsZero() && !item.Indexed {
			continue
		}
		nodes, edges := item.Preview()
		if len(nodes) > 1 && len(edges) > 0 {
			m.graphs = append(m.graphs, launchGraph{entry: item, graph: layoutGraph(0, edges, nodes, item.Root)})
		}
	}
}

func (m *launcher) mergeDiscovered(found []network.Recent) {
	results := m.results()
	var selected string
	if m.cursor < len(results) {
		selected = results[m.cursor].Root
	}
	known := make(map[string]bool, len(m.entries))
	m.entries = slices.Clone(m.entries)
	for _, entry := range m.entries {
		known[entry.Root] = true
	}
	refresh := false
	for _, entry := range found {
		if !known[entry.Root] {
			m.entries = append(m.entries, entry)
			known[entry.Root] = true
			refresh = refresh || entry.Indexed
		}
	}
	if selected != "" {
		m.cursor = max(0, slices.IndexFunc(m.results(), func(item network.Recent) bool { return item.Root == selected }))
	} else {
		m.cursor = 0
	}
	if refresh {
		m.prepareGraphs()
		m.renderGraphics()
	}
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
		m.notice = "Folder unavailable · enter an existing directory"
		return nil
	}
	m.chosen = path
	return tea.Quit
}

func (m *launcher) openFolder() {
	m.pathOnly, m.cursor, m.notice = true, 0, ""
	m.query = ""
}

func (m launcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	switch v := msg.(type) {
	case drift:
		// The dot field, text and graph labels stay fixed during motion.
	case tea.MouseMsg:
		if tea.MouseEvent(v).Action != tea.MouseActionMotion || next.hover != m.hover {
			next.matches = next.results()
			next.frame = next.renderText(next.matches)
		}
	default:
		next.matches = next.results()
		next.frame = next.renderText(next.matches)
	}
	return next, cmd
}

func (m launcher) update(msg tea.Msg) (launcher, tea.Cmd) {
	switch v := msg.(type) {
	case foundNetworks:
		hadGraphs := len(m.graphs) > 0
		m.mergeDiscovered(v.entries)
		m.searching = !v.done
		var drift, next tea.Cmd
		if !hadGraphs && len(m.graphs) > 0 {
			drift = driftTick()
		}
		if !v.done {
			next = m.waitNetworks()
		}
		return m, tea.Batch(drift, next)
	case drift:
		m.t = time.Time(v).Sub(m.started).Seconds()
		if len(m.graphs) > 0 {
			return m, driftTick()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.cellW, m.cellH = m.surface.CellPixels()
		m.renderGraphics()
	case tea.MouseMsg:
		event := tea.MouseEvent(v)
		click := event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress
		layout := m.layout()
		if click && layout.search.contains(event.X, event.Y) {
			m.searchFocused = true
			return m, nil
		}
		if click && !m.pathOnly && event.Y == layout.action.y+1 &&
			event.X >= layout.action.x && event.X < layout.action.x+layout.action.cols {
			m.action = LaunchNew
			if event.X >= layout.action.x+layout.action.cols/2 {
				m.action = LaunchSettings
			}
			return m, tea.Quit
		}
		if click && layout.action.contains(event.X, event.Y) {
			if m.pathOnly && m.query != "" {
				cmd := m.pick(m.query)
				return m, cmd
			}
			m.openFolder()
			return m, nil
		}
		if click && layout.items.contains(event.X, event.Y) {
			results, start := m.visible(layout)
			index := start + event.Y - layout.items.y
			if index < len(results) {
				cmd := m.pick(results[index].Root)
				return m, cmd
			}
		}
		hover := -1
		for i, box := range m.graphBoxes(layout) {
			moving := m.graphPlacement(i, box)
			graphicHit := cellRect{moving.X, moving.Y, moving.Cols, moving.Rows}
			labelHit := cellRect{box.x, box.y + box.rows - 1, box.cols, 1}
			if !m.pathOnly && (graphicHit.contains(event.X, event.Y) || labelHit.contains(event.X, event.Y)) {
				hover = i
				break
			}
		}
		if m.hover != hover {
			if m.hover >= 0 && m.hover < len(m.graphs) {
				m.graphs = slices.Clone(m.graphs)
				m.graphs[m.hover].paused += m.t - m.held
			}
			m.hover, m.held = hover, m.t
		}
		if click && hover >= 0 {
			cmd := m.pick(m.graphs[hover].entry.Root)
			return m, cmd
		}
	case tea.KeyMsg:
		key := v.String()
		if !v.Paste && key == "q" && m.query == "" && !m.pathOnly && !m.searchFocused {
			return m, tea.Quit
		}
		if v.Paste {
			m.query += strings.ReplaceAll(strings.ReplaceAll(string(v.Runes), "\r", ""), "\n", "")
			m.cursor, m.notice = 0, ""
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
			m.openFolder()
		case "ctrl+n":
			m.action = LaunchNew
			return m, tea.Quit
		case "f2":
			m.action = LaunchSettings
			return m, tea.Quit
		case "up":
			m.cursor = max(0, m.cursor-1)
		case "down":
			m.cursor = min(m.cursor+1, len(m.results()))
		case "enter":
			results := m.results()
			switch {
			case m.pathOnly:
				if m.query != "" {
					cmd := m.pick(m.query)
					return m, cmd
				}
			case m.cursor < len(results):
				cmd := m.pick(results[m.cursor].Root)
				return m, cmd
			case strings.ContainsRune(m.query, filepath.Separator) || m.query == ".":
				cmd := m.pick(m.query)
				return m, cmd
			default:
				m.openFolder()
			}
		case "backspace":
			runes := []rune(m.query)
			m.query, m.cursor, m.notice = string(runes[:max(0, len(runes)-1)]), 0, ""
		default:
			if v.Type == tea.KeyRunes || v.Type == tea.KeySpace {
				m.query, m.cursor, m.notice = m.query+key, 0, ""
			}
		}
	}
	return m, nil
}

func (m launcher) layout() launchLayout {
	width := min(82, max(1, m.width-6), max(48, m.width*34/100))
	left := (m.width - width) / 2
	brandRows := 1
	if m.surface.Enabled() && m.width >= 45 && m.height >= 24 {
		brandRows = 5
	}
	promptRows := 1
	if width < 42 {
		promptRows = 2
	}
	graphRows, available := 0, m.height
	sideGraphs := (m.width-width)/2 >= 28 && m.height >= 30
	if len(m.graphs) > 0 && !sideGraphs && m.height >= 28 && m.width >= 49 {
		graphRows = min(16, max(8, m.height/3))
		available -= graphRows + 2
	}
	slots := min(5, max(1, len(m.entries)), max(1, available-brandRows-promptRows-11))
	height := brandRows + promptRows + slots + 10
	top := max(0, (available-height)/2)
	prompt := top + brandRows + 1
	search := prompt + promptRows + 1
	label := search + 4
	items := label + 1
	action := items + slots + 1
	return launchLayout{
		brand: cellRect{left, top, width, brandRows}, search: cellRect{left, search, width, 3},
		items: cellRect{left, items, width, slots}, action: cellRect{left, action, width, 1},
		prompt: prompt, label: label, help: action + 2, graphRows: graphRows, sideGraphs: sideGraphs,
	}
}

func (m launcher) graphBoxes(l launchLayout) []cellRect {
	if !l.sideGraphs && l.graphRows == 0 {
		return nil
	}
	var boxes []cellRect
	free := l.search.x
	for index := range m.graphs {
		if l.sideGraphs {
			cols, rows := min(69, free-8), min(25, m.height/3)
			center, y := free/2, (m.height-rows)/2
			if index == 1 || len(m.graphs) == 1 {
				center = m.width - free/2
			} else if len(m.graphs) >= 3 {
				y = m.height/8 + index/2*m.height/2 - rows/5
			}
			boxes = append(boxes, cellRect{center - cols/2, max(0, y), cols, rows})
		} else {
			lane := m.width / len(m.graphs)
			cols := min(48, lane-8)
			boxes = append(boxes, cellRect{lane*index + (lane-cols)/2, m.height - l.graphRows - 2, cols, l.graphRows})
		}
	}
	return boxes
}

func (m launcher) graphPlacement(index int, box cellRect) terminal.Placement {
	p := terminal.Placement{X: box.x, Y: box.y, Cols: box.cols, Rows: box.rows - 1, BehindText: true}
	if m.cellW <= 0 || m.cellH <= 0 {
		return p
	}
	t := m.t
	if index == m.hover {
		t = m.held
	}
	t -= m.graphs[index].paused
	phase := float64(index) * 2
	dx := int(math.Round(float64(m.cellW) * 1.7 * math.Sin(t*.55+phase)))
	dy := int(math.Round(float64(m.cellH) * .8 * math.Cos(t*.44+phase)))
	x := max(0, box.x*m.cellW+dx)
	y := max(0, box.y*m.cellH+dy)
	p.X, p.OffsetX = x/m.cellW, x%m.cellW
	p.Y, p.OffsetY = y/m.cellH, y%m.cellH
	return p
}

func (m *launcher) renderGraphics() {
	l := m.layout()
	ratio := m.surface.CellRatio(m.width, m.height)
	brand := graphRenderKey{cols: l.brand.cols, rows: l.brand.rows, ratio: ratio}
	if m.brandKey != brand {
		m.brandKey, m.wordmark = brand, nil
		if l.brand.rows > 1 {
			m.wordmark = launchWordmark(l.brand.cols, l.brand.rows, ratio)
		}
	}
	graphs := slices.Clone(m.graphs)
	for i, box := range m.graphBoxes(l) {
		key := graphRenderKey{cols: box.cols, rows: box.rows - 1, ratio: ratio, zoom: 1, transparent: true}
		if graphs[i].asset == key {
			continue
		}
		graphs[i].asset = key
		if m.surface.Enabled() {
			result := renderGraph(graphs[i].graph, key)
			graphs[i].image, graphs[i].dimmed = result.graphic, result.dimmed
		} else {
			graphs[i].text = launchTextGraph(graphs[i].graph, box.cols, box.rows-1)
		}
	}
	m.graphs = graphs
}

func (m launcher) visible(l launchLayout) ([]network.Recent, int) {
	results := m.results()
	return results, visibleStart(m.cursor, len(results), l.items.rows)
}

func visibleStart(cursor, count, rows int) int {
	return max(0, min(cursor-rows+1, count-rows))
}

func (m launcher) View() string {
	frame, matches := m.frame, m.matches
	if frame == "" {
		matches = m.results()
		frame = m.renderText(matches)
	}
	l := m.layout()
	var images []terminal.Placement
	if m.wordmark != nil {
		images = append(images, terminal.Placement{Graphic: m.wordmark, X: l.brand.x, Y: l.brand.y, Cols: l.brand.cols, Rows: l.brand.rows})
	}
	if !m.pathOnly {
		for i, box := range m.graphBoxes(l) {
			graph := m.graphs[i]
			thumbnail := graph.image
			if m.query != "" && !m.activeGraph(i, matches) {
				thumbnail = graph.dimmed
			}
			if thumbnail != nil {
				placement := m.graphPlacement(i, box)
				placement.Graphic = thumbnail
				images = append(images, placement)
			}
		}
	}
	return m.surface.Marker(images) + frame
}

func (m launcher) activeGraph(i int, matches []network.Recent) bool {
	return m.hover == i || (m.query != "" && m.cursor < len(matches) && matches[m.cursor].Root == m.graphs[i].entry.Root)
}

func (m launcher) renderText(results []network.Recent) string {
	if m.width < 18 || m.height < 11 {
		return "Notesflow · enlarge terminal"
	}
	l := m.layout()
	rows := make([]string, m.height)
	for y := 1; y < len(rows); y += 3 {
		offset := (y / 3) % 2 * 3
		rows[y] = strings.Repeat(" ", offset) + dim.Render(strings.Repeat("·     ", max(0, (m.width-offset)/6)))
	}
	put := func(row, col, width int, value string) {
		if row >= 0 && row < len(rows) {
			rows[row] = overlayLine(rows[row], col, width, value)
		}
	}
	center := func(y int, text string) {
		put(y, l.search.x, l.search.cols, centered(text, l.search.cols))
	}
	if m.wordmark == nil {
		center(l.brand.y+l.brand.rows/2, accent.Render("Notesflow"))
	}
	prompt := "Which note network do you want to open?"
	placeholder := "Search by name or path…"
	if m.pathOnly {
		prompt, placeholder = "Open folder…", "Paste the path to a LaTeX project…"
	}
	for i, line := range foldLine(prompt, l.search.cols) {
		center(l.prompt+i, line)
	}
	query := clean(m.query)
	if query == "" {
		query = muted.Render(placeholder)
	}
	input := panel("", []string{" " + accent.Render("›") + " " + query}, l.search.cols, l.search.rows, false)
	for i, value := range input {
		put(l.search.y+i, l.search.x, l.search.cols, value)
	}
	start := visibleStart(m.cursor, len(results), l.items.rows)
	for i, item := range results[start:min(len(results), start+l.items.rows)] {
		name := clean(item.Name)
		path := clean(shortPath(item.Root))
		prefix := "   "
		if start+i == m.cursor {
			name, prefix = accent.Render(name), accent.Render(" › ")
		}
		line := prefix + name
		if l.items.cols-ansi.StringWidth(line) > 12 {
			line += muted.Render("  " + path)
		}
		put(l.items.y+i, l.items.x, l.items.cols, line)
	}
	switch {
	case m.notice != "":
		center(l.label, pink.Render(clean(m.notice)))
	case m.pathOnly:
		center(l.label, muted.Render("Choose a directory containing your notes."))
	case len(results) > 0:
		label := " Note networks"
		if m.query == "" && !results[0].Opened.IsZero() {
			label = " Recent networks"
		}
		put(l.label, l.items.x, l.items.cols, muted.Render(label))
	case m.searching:
		put(l.label, l.items.x, l.items.cols, muted.Render(" Searching for projects…"))
	case m.query != "":
		center(l.items.y, muted.Render("No matching note networks"))
	default:
		center(l.items.y, muted.Render("Open a folder to add your first note network."))
	}
	action := "Open folder…  Ctrl+O"
	if m.pathOnly {
		action = "Open folder  Enter"
	}
	if m.cursor >= len(results) {
		action = accent.Render("› " + action)
	} else {
		action = muted.Render(action)
	}
	center(l.action.y, action)
	if !m.pathOnly {
		half := l.action.cols / 2
		put(l.action.y+1, l.action.x, half, centered(accent.Render("Nuovo progetto  Ctrl+N"), half))
		put(l.action.y+1, l.action.x+half, l.action.cols-half, centered(muted.Render("Impostazioni  F2"), l.action.cols-half))
	}
	hint := "↑↓ Seleziona  ·  Invio Apri  ·  q Esci"
	if m.pathOnly {
		hint = "Esc Torna"
	} else if m.query != "" || m.searchFocused {
		hint = "↑↓ Seleziona  ·  Invio Apri  ·  Esc Esci"
	}
	center(l.help, muted.Render(hint))
	for i, box := range m.graphBoxes(l) {
		if m.pathOnly {
			break
		}
		graph := m.graphs[i]
		name := muted.Render(clean(graph.entry.Name))
		if m.activeGraph(i, results) {
			name = accent.Render("◉ " + clean(graph.entry.Name))
		}
		if graph.image == nil {
			for y, line := range graph.text {
				put(box.y+y, box.x, box.cols, line)
			}
		}
		put(box.y+box.rows-1, box.x, box.cols, centered(name, box.cols))
	}
	for i := range rows {
		rows[i] = background(fit(rows[i], m.width))
	}
	return strings.Join(rows, "\n")
}

func centered(s string, width int) string {
	return strings.Repeat(" ", max(0, (width-ansi.StringWidth(s))/2)) + s
}

func shortPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// overlayLine replaces a range of terminal columns while preserving ANSI styles.
func overlayLine(base string, start, width int, overlay string) string {
	base = fit(base, start+max(width, ansi.StringWidth(base)-start))
	return ansi.Cut(base, 0, start) + fit(overlay, width) + ansi.Cut(base, start+width, ansi.StringWidth(base))
}

var launchNodeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Cyan))

func launchTextGraph(g terminalGraph, cols, rows int) []string {
	points := make(map[string][2]int, len(g.positions))
	for id, p := range g.positions {
		x := (p.x-g.bounds.left)/max(.01, g.bounds.right-g.bounds.left) - .5
		y := (p.y-g.bounds.top)/max(.01, g.bounds.bottom-g.bounds.top) - .5
		points[id] = [2]int{cols/2 + int(x*float64(cols-6)), rows/2 + int(y*float64(rows-3))}
	}
	cells := make([][]rune, rows)
	for i := range cells {
		cells[i] = []rune(strings.Repeat(" ", cols))
	}
	for _, edge := range g.edges {
		a, b := points[edge.Source], points[edge.Target]
		count := max(1, abs(b[0]-a[0]), abs(b[1]-a[1]))
		for i := range count {
			y, x := a[1]+(b[1]-a[1])*i/count, a[0]+(b[0]-a[0])*i/count
			cells[y][x] = '·'
		}
	}
	for _, p := range points {
		cells[p[1]][p[0]] = '●'
	}
	lines := make([]string, rows)
	for y, row := range cells {
		lines[y] = launchNodeStyle.Render(string(row))
	}
	return lines
}

func abs(n int) int { return max(n, -n) }
