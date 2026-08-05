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
		Long: "torrnado runs a torrent engine as a background daemon and talks to it\n" +
			"over a local Unix socket, so downloads keep going after the command\n" +
			"that started them exits.",
		// Cobra prints usage after any error by default, which buries a
		// one-line failure under a wall of help text. Report errors
		// ourselves instead, in one place, below.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newDaemonCmd(),
		newListCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
