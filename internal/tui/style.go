package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/theme"
)

// styles bundles every lipgloss.Style derived from a Theme.
//
// Built once when the theme is chosen rather than on every render: View
// runs on each keystroke and each state push, and rebuilding a few dozen
// styles that often is work for nothing.
type styles struct {
	theme theme.Theme

	Base        lipgloss.Style
	StatusBar   lipgloss.Style
	StatusErr   lipgloss.Style
	SelectedRow lipgloss.Style
	Row         lipgloss.Style
	Muted       lipgloss.Style
	Accent      lipgloss.Style
	Success     lipgloss.Style
	Warning     lipgloss.Style
	Error       lipgloss.Style
	Title       lipgloss.Style
}

func newStyles(t theme.Theme) styles {
	return styles{
		theme: t,

		Base: lipgloss.NewStyle().Foreground(t.Foreground),

		StatusBar: lipgloss.NewStyle().Foreground(t.Muted),

		StatusErr: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),

		SelectedRow: lipgloss.NewStyle().
			Foreground(t.SelectedFg).
			Background(t.SelectedBg).
			Bold(true),

		Row: lipgloss.NewStyle().Foreground(t.Foreground),

		Muted:   lipgloss.NewStyle().Foreground(t.Muted),
		Accent:  lipgloss.NewStyle().Foreground(t.Accent),
		Success: lipgloss.NewStyle().Foreground(t.Success),
		Warning: lipgloss.NewStyle().Foreground(t.Warning),
		Error:   lipgloss.NewStyle().Foreground(t.Error),

		Title: lipgloss.NewStyle().
			Foreground(t.Accent).
			Bold(true),
	}
}
