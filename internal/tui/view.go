package tui

// View renders the whole interface as one string.
//
// bubbletea diffs this against what is already on screen, so View must be
// a pure function of the Model -- no side effects, no I/O. Anything that
// needs either belongs in Update.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	// Nothing has told us how big the terminal is yet.
	if m.width == 0 {
		return "starting torrnado...\n"
	}
	return m.styles.Base.Render("torrnado") + "\n"
}
