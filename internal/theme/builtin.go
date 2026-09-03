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

	// alucard is Dracula's official light counterpart.
	register(Theme{
		Name:       "alucard",
		Background: "#fffbeb",
		Foreground: "#1f1f1f",
		Muted:      "#6c664b",
		Accent:     "#644ac9",
		Success:    "#14710a",
		Warning:    "#846e15",
		Error:      "#cb3a2a",
		Border:     "#cfcfde",
		SelectedBg: "#cfcfde",
		SelectedFg: "#1f1f1f",
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
	//
	// Accent is bright cyan (14), and the choice matters more here than
	// in any other theme: Accent marks the row under the cursor, the
	// focused pane's border and the active tab, and draws all of them as
	// a foreground colour with nothing behind it.
	//
	// It was ANSI 4, the normal blue, which against a black terminal is
	// 1.3:1 - the cursor row came out darker than the ordinary rows
	// around it, so the one row you were meant to be looking at was the
	// hardest to see. Bright blue (12) only reaches 2.4:1 on a terminal
	// using the literal xterm palette, still under the 3:1 wanted for
	// something you have to pick out. Cyan is 16.8:1.
	//
	// Cyan sits close to white in *luminance*, so it separates from the
	// ordinary rows by hue rather than brightness - which is enough here
	// because the cursor row is bold and already carries a ">" marker.
	// Being readable is the part that was missing.
	//
	// The other themes pick hex colours and never meet any of this.
	register(Theme{
		Name:       "plain",
		Background: "0",
		Foreground: "7",
		Muted:      "8",
		Accent:     "14",
		Success:    "2",
		Warning:    "3",
		Error:      "1",
		Border:     "8",
		SelectedBg: "4",
		SelectedFg: "15",
	})
}
