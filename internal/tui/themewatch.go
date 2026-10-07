package tui

import (
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lestex/torrnado/internal/theme"
)

// themeWatchInterval is how often the active theme's file is checked for
// changes. A stat of one small file a second costs nothing measurable.
const themeWatchInterval = time.Second

// themeCheckMsg asks Update to look at the active theme's file again.
type themeCheckMsg struct{}

// themeWatchCmd schedules the next check. Split out, as expireStatusCmd
// is, so a test can drive checks without sitting through the delay.
func themeWatchCmd(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg {
		return themeCheckMsg{}
	})
}

// fileStamp is what a file looks like from the outside: enough to notice
// that it was rewritten without reading it every second.
type fileStamp struct {
	modTime time.Time
	size    int64
}

func (s fileStamp) equal(o fileStamp) bool {
	return s.size == o.size && s.modTime.Equal(o.modTime)
}

// stampOf stats path, following a symlink - a theme file is often a link
// into a directory another program (a theme switcher) rewrites, and it is
// the target's change that matters, not the link's.
func stampOf(path string) (fileStamp, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return fileStamp{}, false
	}
	return fileStamp{modTime: fi.ModTime(), size: fi.Size()}, true
}

// themeFile is where the active theme would live if it is a user's own.
// A built-in has no file, and nothing is ever found there.
func (m Model) themeFile() string {
	if m.themesDir == "" || m.theme.Name == "" {
		return ""
	}
	return filepath.Join(m.themesDir, m.theme.Name+".toml")
}

// checkThemeFile re-applies the active theme when its file has changed
// since it was applied, so an edit - by hand, or by a theme switcher
// that regenerates the file - shows without restarting the TUI.
//
// It follows the theme in use, not the one in config.toml: after
// `:theme nord` it is nord's file that counts. Nothing is reloaded while
// the picker is open, because the picker owns the theme until enter or
// esc, and a file that vanishes leaves the theme as it is. A file that
// fails to load is reported once, then left until it changes again - a
// half-written file is usually complete by the next check.
func (m Model) checkThemeFile() (Model, tea.Cmd) {
	next := themeWatchCmd(themeWatchInterval)
	if m.themePicker {
		return m, next
	}
	path := m.themeFile()
	if path == "" {
		return m, next
	}
	stamp, ok := stampOf(path)
	if !ok || stamp.equal(m.themeStamp) {
		return m, next
	}
	m.themeStamp = stamp

	th, err := theme.Load(m.theme.Name, m.themesDir)
	if err != nil {
		return m, tea.Batch(next, m.setStatus(errStatus(err)))
	}
	m = m.applyTheme(th)
	return m, next
}
