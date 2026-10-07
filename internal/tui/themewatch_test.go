package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/lestex/torrnado/internal/theme"
)

const watchedTheme = `background  = "%s"
foreground  = "#c0caf5"
muted       = "#565f89"
accent      = "#7aa2f7"
success     = "#9ece6a"
warning     = "#e0af68"
error       = "#f7768e"
border      = "#565f89"
selected_bg = "#292e42"
selected_fg = "#c0caf5"
`

// writeTheme writes themesDir/<name>.toml with the given background, and
// moves its mtime on, so two writes inside one clock tick still differ.
func writeTheme(t *testing.T, themesDir, name, background string, age time.Duration) {
	t.Helper()
	path := filepath.Join(themesDir, name+".toml")
	data := strings.Replace(watchedTheme, "%s", background, 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// watchedModel is a model on the user theme "current" from themesDir.
func watchedModel(t *testing.T, themesDir string) Model {
	t.Helper()
	writeTheme(t, themesDir, "current", "#111111", -time.Hour)
	th, err := theme.Load("current", themesDir)
	if err != nil {
		t.Fatalf("load current: %v", err)
	}
	m := testModel("a")
	m.themesDir = themesDir
	m.width, m.height = 120, 40
	return m.applyTheme(th)
}

func TestARewrittenThemeFileIsAppliedLive(t *testing.T) {
	dir := t.TempDir()
	m := watchedModel(t, dir)

	writeTheme(t, dir, "current", "#222222", 0)
	m, cmd := m.checkThemeFile()

	if m.theme.Background != lipgloss.Color("#222222") {
		t.Errorf("background %q, want the rewritten file's #222222", m.theme.Background)
	}
	if cmd == nil {
		t.Error("no next check was scheduled")
	}
}

func TestAnUnchangedThemeFileIsNotReloaded(t *testing.T) {
	dir := t.TempDir()
	m := watchedModel(t, dir)
	m.theme.Background = lipgloss.Color("#abcdef") // would be undone by a reload

	m, _ = m.checkThemeFile()

	if m.theme.Background != lipgloss.Color("#abcdef") {
		t.Error("an unchanged file was loaded again")
	}
}

func TestAThemeLinkFollowsItsTarget(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	writeTheme(t, elsewhere, "generated", "#111111", -time.Hour)
	if err := os.Symlink(filepath.Join(elsewhere, "generated.toml"), filepath.Join(dir, "current.toml")); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("current", dir)
	if err != nil {
		t.Fatal(err)
	}
	m := testModel("a")
	m.themesDir = dir
	m = m.applyTheme(th)

	writeTheme(t, elsewhere, "generated", "#333333", 0)
	m, _ = m.checkThemeFile()

	if m.theme.Background != lipgloss.Color("#333333") {
		t.Errorf("background %q, want the link target's new #333333", m.theme.Background)
	}
}

func TestTheOpenPickerOwnsTheTheme(t *testing.T) {
	dir := t.TempDir()
	m := watchedModel(t, dir)
	next, _ := m.openThemePicker()
	m = next.(Model)

	writeTheme(t, dir, "current", "#222222", 0)
	m, _ = m.checkThemeFile()

	if m.theme.Background == lipgloss.Color("#222222") {
		t.Error("the file was applied while the picker was open")
	}
}

func TestABrokenThemeFileIsReportedOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	m := watchedModel(t, dir)

	path := filepath.Join(dir, "current.toml")
	if err := os.WriteFile(path, []byte("background = \"#222222\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.checkThemeFile()

	if m.theme.Background != lipgloss.Color("#111111") {
		t.Errorf("background %q, want the last good theme kept", m.theme.Background)
	}
	if !m.statusIsErr || !strings.Contains(m.status, "missing required color") {
		t.Errorf("status %q, want the load error", m.status)
	}

	seq := m.statusSeq
	m, _ = m.checkThemeFile()
	if m.statusSeq != seq {
		t.Error("the same broken file was reported again")
	}
}

func TestABuiltInThemeHasNothingToWatch(t *testing.T) {
	dir := t.TempDir()
	th, err := theme.Load("nord", dir)
	if err != nil {
		t.Fatal(err)
	}
	m := testModel("a")
	m.themesDir = dir
	m = m.applyTheme(th)

	m, cmd := m.checkThemeFile()

	if m.theme.Name != "nord" || cmd == nil {
		t.Error("a built-in theme should stay, with checks still scheduled")
	}
}
