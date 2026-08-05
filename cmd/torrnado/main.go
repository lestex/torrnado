// Command torrnado is a terminal BitTorrent client. `torrnado daemon`
// runs the engine in the foreground; the other subcommands are scriptable
// passthroughs to a running daemon.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "torrnado",
		Short: "A terminal BitTorrent client",
		Long: "torrnado runs a torrent engine as a background daemon and attaches a\n" +
			"terminal UI (or scriptable CLI commands) to it over a local Unix socket.\n" +
			"Run with no arguments to open the TUI; it will spawn a daemon for you\n" +
			"if one isn't already running.",
		// Cobra prints usage after any error by default, which buries a
		// one-line failure under a wall of help text. Report errors
		// ourselves instead, in one place, below.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runAttach,
	}

	root.PersistentFlags().StringVar(&configPathFlag, "config", "",
		"path to config.toml (default: $XDG_CONFIG_HOME/torrnado/config.toml)")

	root.AddCommand(
		newDaemonCmd(),
		newAddCmd(),
		newRemoveCmd(),
		newPauseCmd(),
		newResumeCmd(),
		newRecheckCmd(),
		newPriorityCmd(),
		newLimitCmd(),
		newMoveCmd(),
		newListCmd(),
		newPreviewCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
