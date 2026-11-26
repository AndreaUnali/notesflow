package tui

import (
	"image/color"
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/palette"
)

// UseAppearance applies user preferences before starting the workspace UI.
func UseAppearance(settings config.Settings) {
	colors := palette.Preset(settings.Theme)
	palette.Use(colors)
	accent = accent.Foreground(lipgloss.Color(colors.Cyan))
	selectedStyle = selectedStyle.Foreground(lipgloss.Color(colors.Background)).Background(lipgloss.Color(colors.Cyan))
	muted = muted.Foreground(lipgloss.Color(colors.Muted))
	dim = dim.Foreground(lipgloss.Color(colors.Line))
	pink = pink.Foreground(lipgloss.Color(colors.Pink))
	homeTitleStyle = homeTitleStyle.Foreground(lipgloss.Color(colors.Text))
	homeCardStyle = homeCardStyle.Foreground(lipgloss.Color(colors.Text)).Background(lipgloss.Color(colors.Surface))
	homeSelectedStyle = homeCardStyle.Background(lipgloss.Color(colors.Selection))
	helpFooterStyle = muted.Background(lipgloss.Color(colors.Surface))
	roadFooterStyle = accent.Background(lipgloss.Color(colors.Selection))
	messageFooterStyle = messageFooterStyle.Foreground(lipgloss.Color(colors.Yellow)).Background(lipgloss.Color(colors.Surface))
	statusFooterStyle = statusFooterStyle.Foreground(lipgloss.Color(colors.Purple)).Background(lipgloss.Color(colors.Surface))
	graphColors = []lipgloss.Style{
		accent, pink,
		lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Purple)),
		lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Yellow)),
	}
	launchNodeStyle = launchNodeStyle.Foreground(lipgloss.Color(colors.Cyan))
	surfaceRGB = rgb(colors.Background)
	textRGB = rgb(colors.Text)
	courseRGB = []color.RGBA{rgb(colors.Cyan), rgb(colors.Pink), rgb(colors.Purple), rgb(colors.Yellow)}
}

func rgb(value string) color.RGBA {
	hex, _ := strconv.ParseUint(value[1:], 16, 24)
	return hexColor(uint32(hex))
}
