package tui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

const transitionDuration = 145 * time.Millisecond

type transition struct {
	start    time.Time
	from, to float64
}

type transitionFrame struct {
	start, now time.Time
}

func (m *Model) beginTransition() tea.Cmd {
	target := 1.0
	if m.tab == editorTab {
		target = 0
	}
	if m.todoFraction == target {
		return nil
	}
	m.transition = transition{start: time.Now(), from: m.todoFraction, to: target}
	return nextTransition(m.transition.start)
}

func nextTransition(start time.Time) tea.Cmd {
	return tea.Tick(24*time.Millisecond, func(now time.Time) tea.Msg { return transitionFrame{start, now} })
}

func (m *Model) animate(frame transitionFrame) tea.Cmd {
	if frame.start != m.transition.start {
		return nil
	}
	progress := min(1.0, float64(frame.now.Sub(frame.start))/float64(transitionDuration))
	remaining := 1 - progress
	eased := 1 - remaining*remaining
	m.todoFraction = m.transition.from + (m.transition.to-m.transition.from)*eased
	m.resizeEditor()
	if progress < 1 {
		return nextTransition(frame.start)
	}
	return nil
}

type dragState struct {
	active, moved bool
	node          string
	x, y          int
	clicked       string
	clickedAt     time.Time
}

func (m *Model) mouse(event tea.MouseEvent) tea.Cmd {
	click := event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress
	if click {
		m.commandMode = false
		for _, tab := range m.tabs() {
			if tab.hit.contains(event.X, event.Y) {
				return m.setTab(tab.tab)
			}
		}
	}
	if m.tab == graphTab && (m.graphRect().contains(event.X, event.Y) || m.graphDrag.active) && m.graphSurface.graphic != nil {
		return m.graphMouse(event, click)
	}
	if m.tab == graphTab && event.Action == tea.MouseActionMotion {
		m.graphHover = ""
	}
	l := m.layout()
	main := cellRect{1, l.header + 1, l.left - 2, l.body - 2}
	if m.tab == editorTab && m.editor != nil && main.contains(event.X, event.Y) {
		if click {
			m.focus = mainFocus
		}
		m.editorMouse(event, main)
		return nil
	}
	pdf := m.pdfRect()
	pdfBox := cellRect{pdf.x - 1, pdf.y - 2, pdf.cols + 2, l.pdf}
	if pdfBox.contains(event.X, event.Y) {
		return m.pdfMouse(event, pdf, click)
	}
	if !click {
		return nil
	}
	if event.Y == m.height-l.road-3 {
		return m.toggleRoad()
	}
	if l.todo >= 3 {
		side := cellRect{l.left, l.header, l.right, l.todo}
		if !l.split {
			side = cellRect{0, l.header + l.body, l.left, l.todo}
		}
		if side.contains(event.X, event.Y) {
			row, col := event.Y-side.y-1, event.X-side.x-1
			if row <= 0 {
				m.focus = sidebarFocus
				if row == 0 {
					for _, button := range sidebarTabs(side.cols - 2) {
						if col >= button.start && col < button.end {
							m.sidebarTab = button.tab
							break
						}
					}
				}
				return nil
			}
			rows := m.sidebarTasks(side.cols-2, l.todo-3)
			if i := row - 1; i < len(rows) && rows[i].task >= 0 {
				task := m.sidebarItems()[rows[i].task]
				return m.openEditor(source{task.File, task.Line}, false)
			}
			return m.openSidebarList()
		}
	}
	if main.contains(event.X, event.Y) && m.tab != editorTab && m.tab != graphTab && !(m.tab == todoTab && m.showSummary) {
		m.focus = mainFocus
		items := m.rows()
		i := listStart(m.cursor, len(items), main.rows) + event.Y - main.y
		if i < len(items) {
			m.cursor = i
			return m.activate()
		}
	}
	return nil
}

func (m *Model) toggleRoad() tea.Cmd {
	cmd, _ := m.command("R")
	return cmd
}

func (m *Model) graphMouse(event tea.MouseEvent, click bool) tea.Cmd {
	d := &m.graphDrag
	target := m.graphHit(event.X, event.Y)
	switch {
	case event.Button == tea.MouseButtonWheelUp:
		m.zoom = min(4, m.zoom*1.15)
	case event.Button == tea.MouseButtonWheelDown:
		m.zoom = max(.3, m.zoom/1.15)
	case click:
		m.focus = mainFocus
		d.active, d.moved, d.node, d.x, d.y = true, false, target, event.X, event.Y
	case event.Action == tea.MouseActionMotion:
		if d.active && (event.X != d.x || event.Y != d.y) {
			d.moved = true
			dx, dy := event.X-d.x, event.Y-d.y
			if d.node != "" {
				m.moveGraphNode(d.node, dx, dy)
			} else {
				m.pan.x = max(-260, min(260, m.pan.x+float64(dx)))
				m.pan.y = max(-160, min(160, m.pan.y+float64(dy)))
			}
			d.x, d.y = event.X, event.Y
		}
		m.graphHover = target
	case event.Action == tea.MouseActionRelease && d.active:
		d.active = false
		if d.moved || d.node == "" {
			return nil
		}
		nodes := m.graphNodes()
		i := slices.IndexFunc(nodes, func(node parser.Node) bool { return node.ID == d.node })
		if i < 0 {
			return nil
		}
		m.cursor = i
		m.prepareGraph()
		if d.clicked == d.node && time.Since(d.clickedAt) < 350*time.Millisecond {
			return m.enterGraph(nodes[i].ID)
		}
		d.clicked, d.clickedAt = d.node, time.Now()
	}
	return nil
}

func (m *Model) pdfMouse(event tea.MouseEvent, r cellRect, click bool) tea.Cmd {
	switch event.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		m.focus = pdfFocus
		key := "left"
		if event.Button == tea.MouseButtonWheelDown {
			key = "right"
		}
		if m.pdf.pageImage == nil {
			key = "up"
			if event.Button == tea.MouseButtonWheelDown {
				key = "down"
			}
		}
		return m.sideKey(key)
	}
	if click {
		m.focus = pdfFocus
		if event.Y == r.y-1 && event.X >= r.x+r.cols-5 {
			if event.X >= r.x+r.cols-2 {
				return m.sideKey("right")
			}
			return m.sideKey("left")
		}
	}
	return nil
}

func (m *Model) editorMouse(event tea.MouseEvent, main cellRect) {
	button, action := "", ""
	switch event.Button {
	case tea.MouseButtonLeft:
		button = "left"
	case tea.MouseButtonMiddle:
		button = "middle"
	case tea.MouseButtonRight:
		button = "right"
	case tea.MouseButtonWheelUp:
		button, action = "wheel", "up"
	case tea.MouseButtonWheelDown:
		button, action = "wheel", "down"
	}
	if button == "" && event.Action == tea.MouseActionRelease {
		button = "left"
	}
	if button == "" {
		return
	}
	if action == "" {
		switch event.Action {
		case tea.MouseActionPress:
			action = "press"
		case tea.MouseActionMotion:
			action = "drag"
		case tea.MouseActionRelease:
			action = "release"
		}
	}
	if action == "" {
		return
	}
	var mods strings.Builder
	if event.Shift {
		mods.WriteByte('S')
	}
	if event.Ctrl {
		mods.WriteByte('C')
	}
	if event.Alt {
		mods.WriteByte('A')
	}
	m.editor.Mouse(button, action, mods.String(), event.Y-main.y, event.X-main.x)
}
