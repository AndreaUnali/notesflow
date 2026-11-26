package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/editor"
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
	for _, marker := range []string{"1 Home", "2 Scrittura", "3 Grafo", "Riprendi", "Nuovo capitolo", "Delega", "Capitoli", "Shift+Tab → q Progetti", "TODO · 2", "Anteprima PDF", "Roadmap · 1/2", "─▶"} {
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

func TestSidebarTabsFilterIndependentlyAndAreClickable(t *testing.T) {
	model := Model{width: 132, height: 42, todoFraction: 1, snapshot: graph.Snapshot{
		Tasks: []parser.Task{
			{File: "classico.tex", Kind: "TODO", Text: "Un esercizio"},
			{File: "schema.tex", Kind: "VISUAL", Text: "Un grafico"},
			{File: "roadmap.md", Kind: parser.MaintenanceTask, Text: "Verifica una decisione"},
		},
	}}
	if len(model.sidebarItems()) != 3 {
		t.Fatal("Tutti should be the initial tab")
	}
	model.command("t")
	model.sideKey("right")
	if entries := model.sidebarItems(); len(entries) != 1 || entries[0].Kind != "TODO" || model.taskKind != "" {
		t.Fatalf("unexpected sidebar or full TODO filter: %v, %q", entries, model.taskKind)
	}
	l := model.layout()
	for i, want := range []string{"", "TODO", "VISUAL", parser.MaintenanceTask} {
		button := sidebarTabs(l.right - 2)[i]
		model.mouse(tea.MouseEvent{X: l.left + 1 + button.start, Y: l.header + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if model.sidebarTab != i || todoTabs[model.sidebarTab].kind != want {
			t.Fatalf("click did not select tab %d", i)
		}
	}
	model.sideKey("enter")
	if model.tab != todoTab || model.taskKind != parser.MaintenanceTask {
		t.Fatal("Enter should open the corresponding full TODO list")
	}
}

func TestCourseHomeCardUsesItsWholeHitArea(t *testing.T) {
	model := Model{width: 132, height: 42, todoFraction: 1, course: rootCourse}
	l := model.layout()
	_, hits := model.homeContent(l.left-2, l.body-2)
	if len(hits) != int(homeActionCount) {
		t.Fatalf("got %d actionable regions", len(hits))
	}
	card := hits[createChapter].box
	model.mouse(tea.MouseEvent{
		X:      1 + card.x + card.cols/2,
		Y:      l.header + 1 + card.y + card.rows - 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if model.form.mode != "chapter" || model.cursor != int(createChapter) {
		t.Fatalf("click in the description did not open the chapter form (mode %q, cursor %d)", model.form.mode, model.cursor)
	}
}

func TestCollapsedRoadmapSharesOneFullWidthRow(t *testing.T) {
	done := true
	model := Model{width: 240, height: 45, todoFraction: 0, tab: editorTab, ctx: t.Context(),
		cfg: config.Config{Project: "Cloud Computing"}, message: "PDF aggiornato",
		snapshot: graph.Snapshot{Roadmaps: []parser.Roadmap{
			{File: "roadmap.md", Items: []parser.Milestone{{Title: "Grafi", Done: &done}}},
		}},
	}
	expanded := model.layout()
	pdfRows := model.pdfRect().rows
	model.roadCollapsed = true
	collapsed := model.layout()
	if collapsed.road != 0 || collapsed.footer != 1 || collapsed.body-expanded.body != 6 || model.pdfRect().rows-pdfRows != 6 {
		t.Fatalf("collapsed layout did not give reclaimed rows to the editor and PDF: %+v to %+v", expanded, collapsed)
	}
	for _, width := range []int{240, 100, 48, 24} {
		model.width = width
		lines := strings.Split(ansi.Strip(model.View()), "\n")
		if len(lines) != model.height || ansi.StringWidth(lines[len(lines)-1]) != width {
			t.Fatalf("footer must fill a single row at width %d", width)
		}
		if width == 240 {
			for _, marker := range []string{"Roadmap", "1/1", "Shift+Tab", "PDF aggiornato", "Cloud Computing"} {
				if !strings.Contains(lines[len(lines)-1], marker) {
					t.Errorf("%q missing from %q", marker, lines[len(lines)-1])
				}
			}
		}
	}
}

func TestCompactFooterOnlyRoadmapSegmentTogglesRoadmap(t *testing.T) {
	model := Model{width: 132, height: 44, todoFraction: 1, roadCollapsed: true, ctx: t.Context(),
		cfg: config.Config{Project: "Cloud Computing"}, message: "PDF aggiornato"}
	button := model.roadButton(model.layout())
	click := tea.MouseEvent{Y: button.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	click.X = button.cols + 2
	model.mouse(click)
	if !model.roadCollapsed {
		t.Fatal("clicking an adjacent segment expanded the roadmap")
	}
	click.X = 2
	model.mouse(click)
	if model.roadCollapsed {
		t.Fatal("clicking the roadmap segment did not expand it")
	}
}

func TestShiftTabAndQuitAreAvailableAcrossPanes(t *testing.T) {
	cases := map[string]Model{
		"home":             {tab: homeTab},
		"scrittura":        {tab: editorTab},
		"pdf":              {tab: editorTab, focus: pdfFocus},
		"todo laterali":    {tab: homeTab, focus: sidebarFocus},
		"roadmap":          {tab: homeTab, focus: roadFocus},
		"ricerca grafo":    {tab: graphTab, graphSearch: true, query: "sequenze"},
		"modulo delega":    {tab: homeTab, form: inputForm{mode: "delegate", text: "Una bozza"}},
		"scelta documento": {tab: homeTab, choice: workspaceChoice{options: []string{"main.tex"}}},
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			event := tea.KeyMsg{Type: tea.KeyShiftTab}
			model, _ := original.key(event)
			model, _ = model.key(event)
			if !model.commandMode || model.tab != original.tab || model.navTab != original.tab {
				t.Fatal("Shift+Tab should focus navigation without switching tabs")
			}
			model, _ = model.key(tea.KeyMsg{Type: tea.KeyTab})
			if model.commandMode || model.focus != original.focus || model.form.text != original.form.text || model.query != original.query {
				t.Fatal("Tab should return to the same pane and text")
			}
			model, _ = model.key(event)
			model, cmd := model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
			if cmd == nil || !model.quitting || !model.ReturnToLauncher() {
				t.Fatal("q should return to the launcher from global navigation")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("a workspace without an editor should exit")
			}
		})
	}
}

func TestUnsavedBuffersCancelReturningToLauncher(t *testing.T) {
	model := Model{quitting: true, returnToLauncher: true}
	result, cmd := model.update(quitCheck{unsaved: true})
	if result.ReturnToLauncher() || result.quitting || cmd != nil || !strings.Contains(result.message, "Modifiche non salvate") {
		t.Fatal("unsaved buffers did not keep the project open")
	}
}

func TestSaveButtonWorksFromHome(t *testing.T) {
	model := Model{width: 70, height: 30, editor: &editor.Editor{}, tab: homeTab}
	_, agent := model.agentButton()
	label, button := model.saveButton(agent.x)
	if !strings.Contains(label, "Salva") {
		t.Fatal("Save is missing from the Home header")
	}
	cmd := model.mouse(tea.MouseEvent{X: button.x + 1, Y: button.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil || !model.savingNotes || model.tab != homeTab {
		t.Fatal("the button did not start a save in Home")
	}
}

func TestNavigationPreservesDraftWhenSwitchingTabs(t *testing.T) {
	model := Model{tab: homeTab, course: rootCourse, form: inputForm{
		mode: "delegate", tab: homeTab, course: rootCourse, field: 1, text: "Spiega i container",
	}}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyShiftTab})
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if model.tab != activityTab || model.formOpen() {
		t.Fatal("navigation should open Activity in its own pane")
	}
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyShiftTab})
	model, _ = model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if !model.formOpen() || model.form.text != "Spiega i container" || model.form.field != 1 {
		t.Fatal("returning from Activity should restore the draft")
	}
}

func TestDelegationAddsWholePDFAndOffersOptionalPageRange(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pdf := agent.Ref{File: "pacchetto.pdf"}
	other := agent.Ref{File: "secondo pacchetto.pdf"}
	for _, ref := range []agent.Ref{pdf, other} {
		if err := os.WriteFile(filepath.Join(root, ref.File), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := Model{root: root, form: inputForm{mode: "delegate", field: 2, choices: []agent.Ref{pdf, other}}}
	m.formKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.form.selected) != 1 || m.form.selected[0] != pdf {
		t.Fatalf("enter must add the whole PDF: %+v", m.form.selected)
	}
	view := ansi.Strip(strings.Join(m.formRows(80), "\n"))
	if !strings.Contains(view, "pacchetto.pdf · PDF intero") || !strings.Contains(view, "→ Scegli pagine") {
		t.Fatalf("missing source instructions: %s", view)
	}
	m.formKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.form.pdfRange != other || !strings.Contains(strings.Join(m.formRows(80), "\n"), "12-28") {
		t.Fatal("right arrow must show a page range example")
	}
	m.formKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.form.mode != "delegate" || m.form.pdfRange.File != "" {
		t.Fatal("escape must reopen the source list")
	}
	m.formKey(tea.KeyMsg{Type: tea.KeyRight})
	m.formKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("12-40"), Paste: true})
	m.formKey(tea.KeyMsg{Type: tea.KeyEnter})
	want := agent.Ref{File: other.File, First: 12, Last: 40}
	if len(m.form.selected) != 2 || m.form.selected[1] != want || m.form.pdfRange.File != "" {
		t.Fatalf("enter must add the selected range: %+v", m.form)
	}
}
