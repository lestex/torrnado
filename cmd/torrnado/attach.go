package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/theme"
	"github.com/lestex/torrnado/internal/tui"
)

// runAttach opens the TUI against a running daemon, spawning one first if
// there is none. It is the root command's action: `torrnado` on its own
// is the interface, and the subcommands are the scriptable way in.
func runAttach(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	client, err := dialOrSpawn(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	themesDir, err := config.DefaultThemesDir()
	if err != nil {
		return err
	}
	th, err := theme.Load(cfg.Theme, themesDir)
	if err != nil {
		return err
	}

	// The daemon does the searching, so the client only has to ask what
	// is configured. Asked once here rather than on every keystroke:
	// knowing the names is what lets the overlay say "nothing is set up"
	// without running a search to find out, and name the sources while
	// one is running.
	//
	// A daemon too old to know the method answers "unknown method". That
	// is not worth failing the whole TUI over - search simply reports
	// itself unconfigured, which is what it effectively is.
	sources, err := client.SearchSources()
	if err != nil {
		sources = nil
	}

	// WithAltScreen switches the terminal to its alternate buffer, so
	// quitting leaves the scrollback exactly as it was found.
	model := tui.New(tui.Options{
		Client:        client,
		Keys:          tui.DefaultKeyMap().WithOverrides(cfg.Keybinds),
		Theme:         th,
		ThemesDir:     themesDir,
		Player:        cfg.Player,
		Opener:        cfg.Opener,
		Finder:        client,
		FinderSources: sources,
	})
	// WithMouseCellMotion gives clicks, releases and the wheel; "all
	// motion" would add hover, which nothing here uses and which fewer
	// terminals support.
	//
	// The cost is that the terminal stops handling click-drag itself, so
	// selecting text to copy needs shift held down. That is the usual
	// bargain for a mouse-aware TUI, and it is said out loud in the docs
	// because the first time it happens it looks like the terminal broke.
	if _, err := tea.NewProgram(model,
		tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
