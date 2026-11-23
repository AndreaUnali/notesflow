package tui

import (
	"fmt"
	"math"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/palette"
	"github.com/AndreaUnali/notesflow/internal/parser"
)

type point struct{ x, y float64 }

type graphBounds struct{ left, right, top, bottom float64 }

type terminalGraph struct {
	revision  uint64
	scope     string
	nodes     []parser.Node
	edges     []graph.Edge
	positions map[string]point
	family    map[string]string
	overview  bool
	bounds    graphBounds
}

func (m Model) graphNodes() []parser.Node {
	var nodes []parser.Node
	query := strings.ToLower(strings.TrimSpace(m.query))
	if query != "" {
		seen := make(map[string]bool)
		for _, list := range [][]parser.Node{m.snapshot.Nodes, m.snapshot.Headings} {
			for _, node := range list {
				if !seen[node.ID] && m.fileInScope(node.File) &&
					strings.Contains(strings.ToLower(node.Title+" "+node.File), query) {
					nodes = append(nodes, node)
					seen[node.ID] = true
				}
			}
		}
		return nodes
	}
	if m.graphGroup != "" {
		group, ok := m.selectedGroup(m.graphGroup)
		if ok {
			members := make(map[string]bool, len(group.Members))
			for _, id := range group.Members {
				members[id] = true
			}
			for _, node := range m.snapshot.Nodes {
				if members[node.ID] {
					nodes = append(nodes, node)
				}
			}
			return nodes
		}
	}
	index := make(map[string]parser.Node, len(m.snapshot.Nodes))
	for _, node := range m.snapshot.Nodes {
		index[node.ID] = node
	}
	for _, group := range m.snapshot.Groups {
		if !m.fileInScope(group.File) {
			continue
		}
		nodes = append(nodes, group.Node)
		for _, id := range group.Topics {
			if node, ok := index[id]; ok {
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

func (m Model) selectedGroup(id string) (graph.Group, bool) {
	for _, group := range m.snapshot.Groups {
		if group.ID == id && m.fileInScope(group.File) {
			return group, true
		}
	}
	return graph.Group{}, false
}

func (m Model) graphEdges() []graph.Edge {
	if m.query == "" && m.graphGroup == "" {
		return m.snapshot.OverviewEdges
	}
	return m.snapshot.Edges
}

func (m *Model) enterGraph(id string) tea.Cmd {
	if group, ok := m.selectedGroup(id); ok {
		m.graphGroup, m.query, m.graphSearch, m.cursor = group.ID, "", false, 0
		m.graphHover, m.pan, m.zoom = "", point{}, 1
		m.prepareGraph()
		return nil
	}
	if node, ok := m.graphNode(); ok {
		return m.openEditor(source{node.File, node.Line}, false)
	}
	return nil
}

func (m *Model) backGraph() {
	if m.query != "" || m.graphSearch {
		m.query, m.graphSearch, m.cursor = "", false, 0
	} else if m.graphGroup != "" {
		group := m.graphGroup
		m.graphGroup = ""
		m.cursor = max(0, slices.IndexFunc(m.graphNodes(), func(n parser.Node) bool { return n.ID == group }))
	}
	m.graphHover, m.pan, m.zoom = "", point{}, 1
}

func (m Model) graphNode() (parser.Node, bool) {
	nodes := m.graphNodes()
	if len(nodes) == 0 {
		return parser.Node{}, false
	}
	return nodes[min(m.cursor, len(nodes)-1)], true
}

// Keep the chapter and its highlighted topics together when changing pages.
func (m Model) graphPage(nodes []parser.Node, index int) (int, int) {
	if m.query != "" || m.graphGroup != "" {
		from := index / graph.GraphPageSize * graph.GraphPageSize
		return from, min(len(nodes), from+graph.GraphPageSize)
	}
	from, groups := 0, 0
	for i, node := range nodes {
		if node.Kind != "group" {
			continue
		}
		if groups > 0 && groups%graph.MapChaptersPerPage == 0 {
			if index < i {
				return from, i
			}
			from = i
		}
		groups++
	}
	return from, len(nodes)
}

func (m *Model) stepGraphPage(direction int) {
	nodes := m.graphNodes()
	if len(nodes) == 0 {
		return
	}
	from, to := m.graphPage(nodes, min(m.cursor, len(nodes)-1))
	if direction > 0 && to < len(nodes) {
		m.cursor = to
	} else if direction < 0 && from > 0 {
		m.cursor, _ = m.graphPage(nodes, from-1)
	}
}

func (m *Model) prepareGraph() {
	if m.graphGroup != "" {
		if _, ok := m.selectedGroup(m.graphGroup); !ok {
			m.graphGroup, m.cursor = "", 0
		}
	}
	nodes := m.graphNodes()
	if len(nodes) == 0 {
		return
	}
	start, end := m.graphPage(nodes, min(m.cursor, len(nodes)-1))
	scope := fmt.Sprintf("%s\n%s\n%s\n%d", m.course, m.graphGroup, m.query, start)
	if m.canvas.revision == m.snapshot.Revision && m.canvas.scope == scope {
		return
	}
	m.canvas = layoutGraph(m.snapshot.Revision, m.graphEdges(), nodes[start:end], scope)
}

func layoutGraph(revision uint64, edges []graph.Edge, visible []parser.Node, scope string) terminalGraph {
	g := terminalGraph{revision: revision, scope: scope, nodes: visible, positions: make(map[string]point), family: make(map[string]string)}
	index := make(map[string]int)
	pos := make([]point, len(visible))
	for i, node := range visible {
		index[node.ID] = i
		if node.Kind == "group" {
			g.family[node.ID] = node.ID
			g.overview = true
		}
		angle := float64(i) * 2.399963
		spread := .45 * math.Sqrt(float64(i)+1)
		pos[i] = point{math.Cos(angle) * spread, math.Sin(angle) * spread}
	}
	for _, edge := range edges {
		_, a := index[edge.Source]
		_, b := index[edge.Target]
		if a && b {
			if g.family[edge.Source] == edge.Source && edge.Kind == "hierarchy" {
				g.family[edge.Target] = edge.Source
			}
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
				continue
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
	if len(pos) > 0 {
		g.bounds = graphBounds{pos[0].x, pos[0].x, pos[0].y, pos[0].y}
	}
	for i, node := range visible {
		p := pos[i]
		g.positions[node.ID] = p
		g.bounds.left, g.bounds.right = min(g.bounds.left, p.x), max(g.bounds.right, p.x)
		g.bounds.top, g.bounds.bottom = min(g.bounds.top, p.y), max(g.bounds.bottom, p.y)
	}
	return g
}

var graphColors = []lipgloss.Style{accent, pink,
	lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Purple)),
	lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Yellow))}

func (m Model) graphRows(width, height int) []string {
	node, ok := m.graphNode()
	nodes := m.graphNodes()
	total := len(nodes)
	first, last := m.graphPage(nodes, min(m.cursor, max(0, total-1)))
	var chapters int
	for _, node := range nodes[first:last] {
		if node.Kind == "group" {
			chapters++
		}
	}
	heading := fmt.Sprintf(" Panoramica · %d capitoli · %d concetti", chapters, last-first-chapters)
	if group, found := m.selectedGroup(m.graphGroup); found {
		heading = fmt.Sprintf(" %s · %d nodi", clean(group.Title), total)
	}
	if m.graphSearch || m.query != "" {
		heading = " / " + clean(m.query)
		if m.graphSearch {
			heading += "▏"
		}
		heading += fmt.Sprintf(" · %d risultati", total)
	}
	if last-first < total {
		heading += fmt.Sprintf(" · %d–%d di %d", first+1, last, total)
	}
	if !ok {
		return []string{heading, " Nessun nodo corrispondente"}
	}
	var latex, semantic, hierarchy int
	for _, edge := range m.graphEdges() {
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

	if node.Kind == "group" {
		details[1] = muted.Render(fmt.Sprintf("%s:%d · Invio entra nel capitolo", clean(node.File), node.Line))
	}
	details = append(details, muted.Render(clean(node.Summary)))
	if m.graphSurface.graphic != nil && height >= 6 {
		rows := make([]string, 1, height)
		rows[0] = heading
		rows = append(rows, make([]string, height-4)...)
		return append(rows, details...)
	}
	rows := []string{heading, ""}
	if m.canvas.overview {
		for _, visible := range m.canvas.nodes {
			if len(rows) >= height-3 {
				break
			}
			mark := "  "
			if visible.ID == node.ID {
				mark = "› "
			}
			if visible.Kind != "group" {
				mark += "  "
			}
			rows = append(rows, mark+clean(visible.Title))
		}
		return append(rows, details...)
	}
	for _, edge := range m.graphEdges() {
		neighbor := edge.Source
		if neighbor == node.ID {
			neighbor = edge.Target
		} else if edge.Target != node.ID {
			continue
		}
		if m.edgeFilter != 0 && m.edgeFilter != edgeKind(edge.Kind) {
			continue
		}
		i := slices.IndexFunc(m.snapshot.Nodes, func(n parser.Node) bool { return n.ID == neighbor })
		if i >= 0 && len(rows) < height-3 {
			rows = append(rows, graphColors[edgeKind(edge.Kind)-1].Render("  ├─ ")+clean(m.snapshot.Nodes[i].Title))
		}
	}
	return append(rows, details...)
}
