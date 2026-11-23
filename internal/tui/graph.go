package tui

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
)

type point struct{ x, y float64 }

type terminalGraph struct {
	revision  uint64
	scope     string
	center    string
	nodes     []parser.Node
	edges     []graph.Edge
	positions map[string]point
}

func (m Model) graphNodes() []parser.Node {
	var nodes []parser.Node
	query := strings.ToLower(strings.TrimSpace(m.query))
	for _, node := range m.snapshot.Nodes {
		if m.fileInScope(node.File) &&
			strings.Contains(strings.ToLower(node.Title+" "+node.Course+" "+node.File), query) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (m Model) graphNode() (parser.Node, bool) {
	nodes := m.graphNodes()
	if len(nodes) == 0 {
		return parser.Node{}, false
	}
	return nodes[min(m.cursor, len(nodes)-1)], true
}

func (m *Model) prepareGraph() {
	selection, ok := m.graphNode()
	if !ok {
		return
	}
	visible := m.graphNodes()
	center := ""
	if len(visible) > 70 {
		center = selection.ID
	}
	scope := m.course + "\n" + m.query
	if m.canvas.revision == m.snapshot.Revision && m.canvas.scope == scope && m.canvas.center == center {
		return
	}
	m.canvas = layoutGraph(m.snapshot, visible, scope, center, selection.ID)
}

func layoutGraph(snapshot graph.Snapshot, visible []parser.Node, scope, center, selection string) terminalGraph {
	if len(visible) > 70 {
		neighbors := map[string]bool{selection: true}
		for _, edge := range snapshot.Edges {
			if edge.Source == selection {
				neighbors[edge.Target] = true
			} else if edge.Target == selection {
				neighbors[edge.Source] = true
			}
		}
		visible = slices.Clone(visible)
		slices.SortStableFunc(visible, func(a, b parser.Node) int {
			switch {
			case a.ID == selection:
				return -1
			case b.ID == selection:
				return 1
			case neighbors[a.ID] != neighbors[b.ID]:
				if neighbors[a.ID] {
					return -1
				}
				return 1
			default:
				return 0
			}
		})
		visible = visible[:70]
	}
	g := terminalGraph{revision: snapshot.Revision, scope: scope, center: center, nodes: visible, positions: make(map[string]point)}
	index := make(map[string]int)
	pos := make([]point, len(visible))
	for i, node := range visible {
		index[node.ID] = i
		angle := float64(i) * 2.399963
		spread := .45 * math.Sqrt(float64(i)+1)
		pos[i] = point{math.Cos(angle) * spread, math.Sin(angle) * spread}
	}
	for _, edge := range snapshot.Edges {
		_, a := index[edge.Source]
		_, b := index[edge.Target]
		if a && b {
			g.edges = append(g.edges, edge)
		}
	}
	for step := range 55 {
		forces := make([]point, len(pos))
		for i, a := range pos {
			for j := i + 1; j < len(pos); j++ {
				dx, dy := a.x-pos[j].x, a.y-pos[j].y
				dist := max(.02, math.Hypot(dx, dy))
				push := min(.4, .046/(dist*dist))
				forces[i].x += dx / dist * push
				forces[i].y += dy / dist * push
				forces[j].x -= dx / dist * push
				forces[j].y -= dy / dist * push
			}
		}
		for _, edge := range g.edges {
			a, b := index[edge.Source], index[edge.Target]
			dx, dy := pos[b].x-pos[a].x, pos[b].y-pos[a].y
			dist := max(.02, math.Hypot(dx, dy))
			strength := (dist - .75) * .18
			if edge.Kind == "semantic" {
				strength *= .35
			}
			forces[a].x += dx / dist * strength
			forces[a].y += dy / dist * strength
			forces[b].x -= dx / dist * strength
			forces[b].y -= dy / dist * strength
		}
		cooling := .35 * (1 - float64(step)/65)
		for i, p := range pos {
			pos[i] = point{p.x + (forces[i].x-p.x*.032)*cooling, p.y + (forces[i].y-p.y*.032)*cooling}
		}
	}
	for i, node := range visible {
		g.positions[node.ID] = pos[i]
	}
	return g
}

var graphColors = []lipgloss.Style{accent, pink,
	lipgloss.NewStyle().Foreground(lipgloss.Color("#B693FF")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#FEE440"))}

func (m Model) graphRows(width, height int) []string {
	node, ok := m.graphNode()
	if !ok {
		return []string{"Nessun nodo corrispondente"}
	}
	var heading string
	if m.graphSearch || m.query != "" {
		heading = "/ " + m.query
		if m.graphSearch {
			heading += "▏"
		}
	} else {
		heading = "• LaTeX  · Semantico  ─ Gerarchia"
	}
	var latex, semantic, hierarchy int
	for _, edge := range m.snapshot.Edges {
		if edge.Source != node.ID && edge.Target != node.ID {
			continue
		}
		switch edge.Kind {
		case "semantic":
			semantic++
		case "hierarchy":
			hierarchy++
		default:
			latex++
		}
	}
	details := []string{
		accent.Render(clean(node.Title)),
		muted.Render(fmt.Sprintf("%s:%d · %d LaTeX / %d sem. / %d ger.", clean(node.File), node.Line, latex, semantic, hierarchy)),
	}
	if node.Summary != "" {
		details = append(details, clean(node.Summary))
	}
	if height < 7 || width < 20 {
		return append([]string{heading}, details...)
	}
	canvasHeight := height - len(details) - 1
	lines := m.canvas.draw(width, canvasHeight, node.ID, m.edgeFilter, m.zoom, m.pan)
	result := append([]string{heading}, lines...)
	return append(result, details...)
}

func (g terminalGraph) draw(width, height int, selected string, filter int, zoom float64, pan point) []string {
	if height <= 0 || width <= 0 {
		return nil
	}
	type pixel struct {
		glyph rune
		tone  int
	}
	pixels := make([]pixel, width*height)
	maxX, maxY := .6, .6
	for _, p := range g.positions {
		maxX, maxY = max(maxX, math.Abs(p.x)), max(maxY, math.Abs(p.y))
	}
	zoom = max(.3, zoom)
	scale := min(float64(width-5)/(maxX*2), float64(height-1)/maxY) * zoom
	project := func(p point) (int, int) {
		return int(math.Round(float64(width-1)/2 + p.x*scale + pan.x)), int(math.Round(float64(height-1)/2 + p.y*scale/2 + pan.y))
	}
	put := func(x, y int, glyph rune, tone int) {
		if x >= 0 && x < width && y >= 0 && y < height {
			pixels[y*width+x] = pixel{glyph, tone}
		}
	}
	neighbors := map[string]bool{selected: true}
	for _, edge := range g.edges {
		kind := 1
		glyph := '•'
		switch edge.Kind {
		case "semantic":
			kind, glyph = 2, '·'
		case "hierarchy":
			kind, glyph = 3, '─'
		}
		if filter != 0 && filter != kind {
			continue
		}
		emphasized := edge.Source == selected || edge.Target == selected
		tone := 4
		if emphasized {
			tone = kind - 1
			neighbors[edge.Source], neighbors[edge.Target] = true, true
		}
		ax, ay := project(g.positions[edge.Source])
		bx, by := project(g.positions[edge.Target])
		n := max(abs(bx-ax), abs(by-ay))
		for i := 1; i < n; i++ {
			if kind != 2 || i%2 == 0 {
				x := ax + int(math.Round(float64(i*(bx-ax))/float64(n)))
				y := ay + int(math.Round(float64(i*(by-ay))/float64(n)))
				if kind == 3 && y != ay {
					glyph = '┊'
				}
				put(x, y, glyph, tone)
			}
		}
	}
	courses := map[string]int{}
	for _, node := range g.nodes {
		if _, ok := courses[node.Course]; !ok {
			courses[node.Course] = len(courses)
		}
		x, y := project(g.positions[node.ID])
		tone, glyph := 4, '○'
		if neighbors[node.ID] {
			tone, glyph = courses[node.Course]%len(graphColors), '●'
		}
		put(x, y, glyph, tone)
	}
	if p, ok := g.positions[selected]; ok {
		x, y := project(p)
		put(x, y, '◆', 3)
	}
	rows := make([]string, height)
	for y := range height {
		var row strings.Builder
		for _, p := range pixels[y*width : (y+1)*width] {
			switch {
			case p.glyph == 0:
				row.WriteByte(' ')
			case p.tone == 4:
				row.WriteString(dim.Render(string(p.glyph)))
			default:
				row.WriteString(graphColors[p.tone].Render(string(p.glyph)))
			}
		}
		rows[y] = row.String()
	}
	return rows
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
