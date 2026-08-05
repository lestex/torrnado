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
	CursorRow   lipgloss.Style
	ColHeader   lipgloss.Style

	// The two halves of a progress underline: how far along, and the
	// track it runs on.
	ProgressFill  lipgloss.Style
	ProgressTrack lipgloss.Style
	Muted         lipgloss.Style
	Accent        lipgloss.Style
	Success       lipgloss.Style
	Warning       lipgloss.Style
	Error         lipgloss.Style
	Title         lipgloss.Style

	// Pane is the bordered box every part of the interface sits in.
	// PaneFocused differs only in border colour: which pane is
	// highlighted is the only cue about where keystrokes will go.
	Pane        lipgloss.Style
	PaneFocused lipgloss.Style

	TabActive   lipgloss.Style
	TabInactive lipgloss.Style

	SidebarTitle      lipgloss.Style
	SidebarItem       lipgloss.Style
	SidebarItemActive lipgloss.Style
}

func newStyles(t theme.Theme) styles {
	// The padding sits inside the width the pane is given (lipgloss wraps
	// at width minus horizontal padding), so it costs the text area, not
	// the frame. layout() subtracts the same panePadX from what the
	// render functions are told they have.
	pane := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Border).
		Padding(0, panePadX)

	return styles{
		theme: t,

		Base: lipgloss.NewStyle().Foreground(t.Foreground),

		// Foreground rather than Muted: the footer carries the transfer
		// totals and whatever just happened, which is worth reading
		// without leaning in.
		StatusBar: lipgloss.NewStyle().Foreground(t.Foreground),

		StatusErr: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),

		SelectedRow: lipgloss.NewStyle().
			Foreground(t.SelectedFg).
			Background(t.SelectedBg).
			Bold(true),

		Row: lipgloss.NewStyle().Foreground(t.Foreground),

		CursorRow: lipgloss.NewStyle().Foreground(t.Accent).Bold(true),

		// A table's own labels should not be the same colour as the
		// chrome around it. This is the list's column headings, the
		// sidebar's section titles and the detail pane's headers, so
		// lightening it here keeps the three panes speaking with one
		// voice.
		ColHeader: lipgloss.NewStyle().Foreground(t.Foreground).Bold(true),

		ProgressFill:  lipgloss.NewStyle().Foreground(t.Accent),
		ProgressTrack: lipgloss.NewStyle().Foreground(t.Border),

		Muted:   lipgloss.NewStyle().Foreground(t.Muted),
		Accent:  lipgloss.NewStyle().Foreground(t.Accent),
		Success: lipgloss.NewStyle().Foreground(t.Success),
		Warning: lipgloss.NewStyle().Foreground(t.Warning),
		Error:   lipgloss.NewStyle().Foreground(t.Error),

		Title: lipgloss.NewStyle().
			Foreground(t.Accent).
			Bold(true),

		Pane:        pane,
		PaneFocused: pane.BorderForeground(t.Accent),

		TabActive:   lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		TabInactive: lipgloss.NewStyle().Foreground(t.Muted),

		SidebarTitle:      lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		SidebarItem:       lipgloss.NewStyle().Foreground(t.Foreground),
		SidebarItemActive: lipgloss.NewStyle().Foreground(t.SelectedFg).Background(t.SelectedBg).Bold(true),
	}
}

// pane returns the border style for a pane, highlighted when it holds
// focus.
func (s styles) pane(focused bool) lipgloss.Style {
	if focused {
		return s.PaneFocused
	}
	return s.Pane
}
