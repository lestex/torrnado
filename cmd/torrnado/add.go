package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/batch"
	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

func newAddCmd() *cobra.Command {
	var savePath string
	var paused bool

	cmd := &cobra.Command{
		Use:   "add <magnet|file|url|dir|glob|magnet-list>...",
		Short: "Add one or more torrents",
		Long: "Adds torrents from anything that names them: a magnet URI, a\n" +
			".torrent file, an http(s) URL to one, a directory of them, a glob\n" +
			"pattern, or a text file listing one magnet per line.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Arguments are resolved here rather than in the daemon so
			// that relative paths and globs are interpreted against the
			// directory the user is standing in, not the daemon's.
			sources, err := batch.Expand(args)
			if err != nil {
				return err
			}

			opts := engine.AddOpts{SavePath: savePath, Paused: paused}
			return withClient(func(c *ipc.Client) error {
				ids, failures, err := c.AddBatch(sources, opts)
				if err != nil {
					return err
				}
				for _, id := range ids {
					fmt.Printf("added: %s\n", id)
				}
				for _, f := range failures {
					fmt.Printf("failed %s\n", f)
				}
				if len(failures) > 0 {
					return fmt.Errorf("%d of %d sources failed", len(failures), len(sources))
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&savePath, "save-path", "", "download into this directory instead of the default")
	cmd.Flags().BoolVar(&paused, "paused", false, "add the torrents but leave them paused")
	return cmd
}
