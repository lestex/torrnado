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

	// WithAltScreen switches the terminal to its alternate buffer, so
	// quitting leaves the scrollback exactly as it was found.
	model := tui.New(tui.Options{
		Client:    client,
		Keys:      tui.DefaultKeyMap().WithOverrides(cfg.Keybinds),
		Theme:     th,
		ThemesDir: themesDir,
		Player:    cfg.Player,
	})
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}
