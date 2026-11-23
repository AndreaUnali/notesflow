package tui

import (
	"path/filepath"
	"strings"
	"testing"

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
