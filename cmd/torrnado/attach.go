package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/indexer"
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

	// Built only when there is something to point it at, and assigned
	// only on success: a typed nil in an interface is not nil, so
	// `finder = (*indexer.Client)(nil)` would make the model's "is one
	// configured" test quietly wrong. A bad [indexer] table has already
	// been rejected by config.Validate, so a failure here is a surprise
	// worth reporting rather than carrying on without the feature.
	var finder indexer.Searcher
	if cfg.Indexer.URL != "" {
		f, err := newSearcher(cfg)
		if err != nil {
			return err
		}
		finder = f
	}

	// WithAltScreen switches the terminal to its alternate buffer, so
	// quitting leaves the scrollback exactly as it was found.
	model := tui.New(tui.Options{
		Client:    client,
		Keys:      tui.DefaultKeyMap().WithOverrides(cfg.Keybinds),
		Theme:     th,
		ThemesDir: themesDir,
		Player:    cfg.Player,
		Opener:    cfg.Opener,
		Finder:    finder,
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
