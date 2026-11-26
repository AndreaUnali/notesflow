package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/palette"
)

func TestSetupCyclesNamedPalettes(t *testing.T) {
	current := setup{}
	names := []string{
		"Catppuccin Mocha", "Dracula", "Nord", "Gruvbox Dark", "Tokyo Night",
		"Solarized Dark", "One Dark", "Rosé Pine", "Kanagawa Wave", "GitHub Dark",
	}
	for _, want := range names {
		_, _, rows := current.content()
		if rows[0] != want {
			t.Fatalf("palette name = %q, want %q", rows[0], want)
		}
		next, _ := current.Update(tea.KeyMsg{Type: tea.KeyRight})
		current = next.(setup)
	}
	if current.settings.Theme != "catppuccin-mocha" {
		t.Fatal("the selector did not wrap around")
	}
	previous, _ := current.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if previous.(setup).settings.Theme != "github-dark" {
		t.Fatal("the selector did not wrap backwards")
	}
	current.settings.Theme = "contrasto"
	_, _, rows := current.content()
	if rows[0] != "Contrasto alto" {
		t.Fatalf("the saved older palette is no longer recognized: %q", rows[0])
	}
}

func TestPaletteSwatchesFitSmallAndLargeTerminals(t *testing.T) {
	theme := palette.ThemeFor("")
	for _, width := range []int{26, 40, 80} {
		view := setupView{width: width, height: 20}
		frame := view.draw(theme.ID, "Aspetto", "Scegli la palette.", "", []string{theme.Name, "Avanti"}, theme.Swatches())
		plain := ansi.Strip(frame)
		if !strings.Contains(plain, theme.Name) || strings.Count(plain, "■") != len(theme.Swatches()) {
			t.Fatalf("width %d: missing the palette name or color samples", width)
		}
		for _, line := range strings.Split(plain, "\n") {
			if lipgloss.Width(line) != width {
				t.Fatalf("width %d: found a line with width %d", width, lipgloss.Width(line))
			}
		}
	}
}

func TestApplyPaletteSavesWithoutReconfiguringModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := setup{setupView: setupView{cursor: 1}, settings: config.Settings{
		Theme: "nord", Profile: &config.Profile{ID: "api", Model: "previous-model"}, APIKey: "previous-key",
	}}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.(setup).saved || cmd == nil {
		t.Fatal("applying the palette did not finish the setup")
	}
	got, exists, err := config.LoadSettings()
	if err != nil || !exists || got.Theme != "nord" || got.Profile == nil || got.Profile.Model != "previous-model" || got.APIKey != "previous-key" {
		t.Fatal("palette change did not preserve the existing connection")
	}
}
