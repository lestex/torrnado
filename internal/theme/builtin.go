package theme

func init() {
	register(Theme{
		Name:       "dracula",
		Background: "#282a36",
		Foreground: "#f8f8f2",
		Muted:      "#6272a4",
		Accent:     "#bd93f9",
		Success:    "#50fa7b",
		Warning:    "#f1fa8c",
		Error:      "#ff5555",
		Border:     "#44475a",
		SelectedBg: "#44475a",
		SelectedFg: "#f8f8f2",
	})

	register(Theme{
		Name:       "nord",
		Background: "#2e3440",
		Foreground: "#d8dee9",
		Muted:      "#4c566a",
		Accent:     "#88c0d0",
		Success:    "#a3be8c",
		Warning:    "#ebcb8b",
		Error:      "#bf616a",
		Border:     "#3b4252",
		SelectedBg: "#434c5e",
		SelectedFg: "#eceff4",
	})

	register(Theme{
		Name:       "gruvbox",
		Background: "#282828",
		Foreground: "#ebdbb2",
		Muted:      "#928374",
		Accent:     "#d3869b",
		Success:    "#b8bb26",
		Warning:    "#fabd2f",
		Error:      "#fb4934",
		Border:     "#3c3836",
		SelectedBg: "#3c3836",
		SelectedFg: "#fbf1c7",
	})

	register(Theme{
		Name:       "solarized-dark",
		Background: "#002b36",
		Foreground: "#839496",
		Muted:      "#586e75",
		Accent:     "#268bd2",
		Success:    "#859900",
		Warning:    "#b58900",
		Error:      "#dc322f",
		Border:     "#073642",
		SelectedBg: "#073642",
		SelectedFg: "#eee8d5",
	})

	register(Theme{
		Name:       "solarized-light",
		Background: "#fdf6e3",
		Foreground: "#657b83",
		Muted:      "#93a1a1",
		Accent:     "#268bd2",
		Success:    "#859900",
		Warning:    "#b58900",
		Error:      "#dc322f",
		Border:     "#eee8d5",
		SelectedBg: "#eee8d5",
		SelectedFg: "#586e75",
	})

	register(Theme{
		Name:       "catppuccin",
		Background: "#1e1e2e",
		Foreground: "#cdd6f4",
		Muted:      "#a6adc8",
		Accent:     "#cba6f7",
		Success:    "#a6e3a1",
		Warning:    "#f9e2af",
		Error:      "#f38ba8",
		Border:     "#313244",
		SelectedBg: "#45475a",
		SelectedFg: "#cdd6f4",
	})

	register(Theme{
		Name:       "tokyo-night",
		Background: "#1a1b26",
		Foreground: "#c0caf5",
		Muted:      "#565f89",
		Accent:     "#7aa2f7",
		Success:    "#9ece6a",
		Warning:    "#e0af68",
		Error:      "#f7768e",
		Border:     "#292e42",
		SelectedBg: "#292e42",
		SelectedFg: "#c0caf5",
	})

	// plain is a 16-color-safe fallback using bare ANSI codes instead of
	// hex, for terminals with no usable color profile at all.
	register(Theme{
		Name:       "plain",
		Background: "0",
		Foreground: "7",
		Muted:      "8",
		Accent:     "4",
		Success:    "2",
		Warning:    "3",
		Error:      "1",
		Border:     "8",
		SelectedBg: "4",
		SelectedFg: "15",
	})
}
