// Package palette contains the colors shared by the terminal interface and editor.
package palette

type Colors struct {
	Background, Surface, Line, Text, Muted string
	Cyan, Pink, Purple, Yellow, Selection  string
}

type Theme struct {
	ID, Name string
	Colors
}

var themes = [...]Theme{
	{"catppuccin-mocha", "Catppuccin Mocha", Colors{"#1e1e2e", "#313244", "#585b70", "#cdd6f4", "#a6adc8", "#89dceb", "#f5c2e7", "#cba6f7", "#f9e2af", "#45475a"}},
	{"dracula", "Dracula", Colors{"#282a36", "#21222c", "#6272a4", "#f8f8f2", "#bfbfbf", "#8be9fd", "#ff79c6", "#bd93f9", "#f1fa8c", "#44475a"}},
	{"nord", "Nord", Colors{"#2e3440", "#3b4252", "#4c566a", "#eceff4", "#d8dee9", "#88c0d0", "#bf616a", "#b48ead", "#ebcb8b", "#434c5e"}},
	{"gruvbox-dark", "Gruvbox Dark", Colors{"#282828", "#3c3836", "#665c54", "#ebdbb2", "#bdae93", "#8ec07c", "#fb4934", "#d3869b", "#fabd2f", "#504945"}},
	{"tokyo-night", "Tokyo Night", Colors{"#1a1b26", "#16161e", "#414868", "#c0caf5", "#a9b1d6", "#7dcfff", "#f7768e", "#bb9af7", "#e0af68", "#28344a"}},
	{"solarized-dark", "Solarized Dark", Colors{"#002b36", "#073642", "#586e75", "#eee8d5", "#93a1a1", "#2aa198", "#d33682", "#6c71c4", "#b58900", "#164554"}},
	{"one-dark", "One Dark", Colors{"#282c34", "#21252b", "#4b5263", "#abb2bf", "#9da5b4", "#56b6c2", "#e06c75", "#c678dd", "#e5c07b", "#3e4451"}},
	{"rose-pine", "Rosé Pine", Colors{"#191724", "#1f1d2e", "#524f67", "#e0def4", "#908caa", "#9ccfd8", "#eb6f92", "#c4a7e7", "#f6c177", "#26233a"}},
	{"kanagawa-wave", "Kanagawa Wave", Colors{"#1f1f28", "#2a2a37", "#54546d", "#dcd7ba", "#c8c093", "#7fb4ca", "#e46876", "#957fb8", "#e6c384", "#2d4f67"}},
	{"github-dark", "GitHub Dark", Colors{"#0d1117", "#161b22", "#30363d", "#c9d1d9", "#8b949e", "#58a6ff", "#ff7b72", "#d2a8ff", "#e3b341", "#1f3048"}},
}

func ThemeFor(id string) Theme {
	for _, theme := range themes {
		if theme.ID == id {
			return theme
		}
	}
	// Keep saved preferences from before the palette picker.
	switch id {
	case "classico":
		return Theme{id, "Classico", Colors{"#090B10", "#101722", "#34465F", "#E3ECFA", "#A4B3CA", "#00F5D4", "#FF2A85", "#B693FF", "#FEE440", "#10282D"}}
	case "sobrio":
		return Theme{id, "Sobrio", Colors{"#171719", "#222228", "#625F70", "#F0EDF1", "#C0BAC8", "#AABCFB", "#F49CB9", "#C3A9E9", "#F7D79D", "#30384C"}}
	case "contrasto":
		return Theme{id, "Contrasto alto", Colors{"#000000", "#151515", "#828282", "#FFFFFF", "#EBEBEB", "#55F8E3", "#FF83B7", "#D4BFFF", "#FFEB70", "#263E3B"}}
	default:
		return themes[0]
	}
}

func Preset(id string) Colors { return ThemeFor(id).Colors }

func Adjacent(id string, offset int) string {
	current := ThemeFor(id).ID
	for i, theme := range themes {
		if theme.ID == current {
			return themes[(i+offset%len(themes)+len(themes))%len(themes)].ID
		}
	}
	if offset < 0 {
		return themes[len(themes)-1].ID
	}
	return themes[0].ID
}

func (c Colors) Swatches() []string {
	return []string{c.Background, c.Surface, c.Text, c.Cyan, c.Pink, c.Purple, c.Yellow}
}

var active = Preset("")

var (
	Background, Surface, Line, Text, Muted = active.Background, active.Surface, active.Line, active.Text, active.Muted
	Cyan, Pink, Purple, Yellow             = active.Cyan, active.Pink, active.Purple, active.Yellow
)

func Current() Colors { return active }

// Use sets the colors before the launcher or editor starts.
func Use(colors Colors) {
	active = colors
	Background, Surface, Line, Text, Muted = colors.Background, colors.Surface, colors.Line, colors.Text, colors.Muted
	Cyan, Pink, Purple, Yellow = colors.Cyan, colors.Pink, colors.Purple, colors.Yellow
}
