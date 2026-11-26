package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/network"
)

func TestLauncherCentersEmptyFirstRunAndKeepsFolderAction(t *testing.T) {
	model := launcher{width: 280, height: 76, hover: -1}
	layout := model.layout()
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Open a folder to add your first note network.") ||
		!strings.Contains(view, "Open folder…  Ctrl+O") || strings.Contains(view, "Recent networks") {
		t.Fatalf("empty launcher should explain how to open a folder")
	}
	if math.Abs(float64(layout.search.x*2+layout.search.cols-model.width)) > 1 ||
		layout.search.y < model.height/3 || layout.search.y > 2*model.height/3 {
		t.Fatalf("search must be centered at fullscreen size, got %+v", layout.search)
	}
}

func TestLauncherQuitKeepsQAvailableInSearch(t *testing.T) {
	event := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	launcher := launcher{}
	_, cmd := launcher.update(event)
	if cmd == nil {
		t.Fatal("q should leave the launcher")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q should quit the app")
	}
	launcher.searchFocused = true
	updated, cmd := launcher.update(event)
	if cmd != nil || updated.query != "q" {
		t.Fatal("search should accept q as a character")
	}
}

func TestLauncherGraphBoxesLeaveSpaceForThePicker(t *testing.T) {
	for _, size := range [][2]int{{280, 76}, {112, 34}, {70, 30}, {52, 27}} {
		model := launcher{width: size[0], height: size[1], hover: -1}
		model.graphs = []launchGraph{{entry: network.Recent{Name: "Cloud"}}, {}, {}}
		for tick := range 80 {
			model.t = float64(tick) / 8
			l := model.layout()
			for _, b := range model.graphBoxes(l) {
				if b.x < 0 || b.y < 0 || b.x+b.cols > model.width || b.y+b.rows > model.height {
					t.Fatalf("thumbnail leaves the terminal at %v: %+v", size, b)
				}
				hitX := b.x < l.search.x+l.search.cols && b.x+b.cols > l.search.x
				hitY := b.y <= l.help && b.y+b.rows > l.brand.y
				if hitX && hitY {
					t.Fatalf("thumbnail overlaps picker at %v: %+v", size, b)
				}
			}
		}
	}
}

func TestLauncherDriftMovesByPixelsAndFreezesOnHover(t *testing.T) {
	model := launcher{width: 280, height: 76, cellW: 10, cellH: 23, hover: -1}
	model.graphs = []launchGraph{{entry: network.Recent{Name: "Cloud"}}}
	box := model.graphBoxes(model.layout())[0]
	first := model.graphPlacement(0, box)
	model.t = .2
	next := model.graphPlacement(0, box)
	if dx := (next.X-first.X)*model.cellW + next.OffsetX - first.OffsetX; dx <= 0 || dx >= model.cellW {
		t.Fatalf("motion should cover less than one cell, got %d pixels", dx)
	}
	model.hover, model.held = 0, model.t
	model.t = 1.4
	if frozen := model.graphPlacement(0, box); frozen != next {
		t.Fatalf("hover changed the placement: first %+v frozen %+v", next, frozen)
	}
	model.graphs[0].paused = model.t - model.held
	model.hover = -1
	if resumed := model.graphPlacement(0, box); resumed != next {
		t.Fatalf("leaving hover jumped to %+v from %+v", resumed, next)
	}
}
