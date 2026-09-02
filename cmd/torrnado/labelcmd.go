package main

import (
	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

// `torrnado label <name> <torrent-id>...` files torrents under a label.
//
// The label comes first so the ids can be a variadic list, which is what
// makes `torrnado label tv $(torrnado list ...)` work. --clear takes the
// label off instead, rather than asking anyone to pass an empty string
// that a shell would swallow.
func newLabelCmd() *cobra.Command {
	var clear bool

	cmd := &cobra.Command{
		Use:   "label <name> <torrent-id>...",
		Short: "File one or more torrents under a label",
		Long: "Files torrents under a label, which the interface can filter by.\n\n" +
			"There is no separate step to create a label: one exists while some\n" +
			"torrent carries it, and stops existing when the last torrent drops\n" +
			"it. So there is nothing to tidy up, and nothing to delete.\n\n" +
			"A torrent has one label at a time; setting another replaces it.",
		// --clear takes ids only, so it needs one argument where setting a
		// label needs two. Cobra parses flags before it validates args,
		// so this can read the flag it depends on.
		Args: func(cmd *cobra.Command, args []string) error {
			if clear {
				return cobra.MinimumNArgs(1)(cmd, args)
			}
			return cobra.MinimumNArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			label, ids := args[0], args[1:]
			if clear {
				// --clear means the first argument is an id like the rest,
				// not a label nobody is going to read.
				label, ids = "", args
			}
			return eachTorrent(ids, func(c *ipc.Client, id engine.TorrentID) error {
				return c.SetLabel(id, label)
			})
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false,
		"remove the label instead, taking only torrent ids")
	return cmd
}
