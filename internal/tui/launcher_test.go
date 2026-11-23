package tui

import (
	"math"
	"strings"
	"testing"

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
