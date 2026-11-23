package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/providers"
)

func TestCourseHomeKeepsPreviewRoadmapAndShortcutsInFrame(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	const tex = "\\documentclass{article}\n\\section{Prima}\n% TODO: Esercizi"
	store, err := graph.Open(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Apply(map[string]parser.Document{
		"main.tex":           parser.Parse("main.tex", []byte(tex)),
		"capitoli/serie.tex": parser.Parse("capitoli/serie.tex", []byte("% TODO: Serie")),
		"roadmap.md":         parser.Parse("roadmap.md", []byte("# Corso\n- [x] Una\n- [ ] Due")),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := providers.Open(filepath.Join(root, "providers.json"), cfg.Providers)
	if err != nil {
		t.Fatal(err)
	}
	model := New(t.Context(), root, "", cfg, store, manager)
	if model.course != rootCourse || model.mainTeX() != "main.tex" || len(model.tasks(false)) != 2 {
		t.Fatalf("scope = %q, main = %q, TODO = %v", model.course, model.mainTeX(), model.tasks(false))
	}
	model.width, model.height = 120, 42
	view := ansi.Strip(model.View())
	for _, marker := range []string{"1 Home", "2 Scrittura", "3 Grafo", "R Mostra/nascondi", "TODO · 2", "Anteprima PDF", "Roadmap · 1/2", "─▶"} {
		if !strings.Contains(view, marker) {
			t.Errorf("view does not contain %q", marker)
		}
	}
	if model.layout().left != 80 {
		t.Fatalf("left pane = %d", model.layout().left)
	}
	if lines := len(strings.Split(view, "\n")); lines != model.height {
		t.Fatalf("view has %d rows", lines)
	}
	model.roadCollapsed = true
	if view := ansi.Strip(model.View()); !strings.Contains(view, "R espandi") || strings.Contains(view, "─▶") {
		t.Fatal("the roadmap did not collapse")
	}
	model.width = 48
	if view := ansi.Strip(model.View()); !strings.Contains(view, "5 Attività") || !strings.Contains(view, "Anteprima PDF") {
		t.Fatal("shortcuts and preview must be visible in a narrow terminal")
	}
}

func TestShiftTabFocusesNavigationWithoutChangingEditorTab(t *testing.T) {
	model := Model{tab: editorTab}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !model.commandMode || model.navTab != editorTab || model.tab != editorTab {
		t.Fatalf("expected focus on the editor tab: %+v", model)
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyRight})
	if model.navTab != graphTab || model.tab != editorTab {
		t.Fatalf("expected graph highlighted without switching tabs")
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyTab})
	if model.commandMode || model.tab != editorTab {
		t.Fatal("Tab should return to the editor")
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if model.tab != editorTab {
		t.Fatal("plain digits belong to the editor")
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyShiftTab})
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyLeft})
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyEnter})
	if model.commandMode || model.tab != homeTab {
		t.Fatal("Enter should open the highlighted Home tab")
	}
}
