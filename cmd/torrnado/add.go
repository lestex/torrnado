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
	var files []string
	var paused bool

	cmd := &cobra.Command{
		Use:   "add <magnet|file|url|dir|glob|magnet-list>...",
		Short: "Add one or more torrents",
		Long: "Adds torrents from anything that names them: a magnet URI, a\n" +
			".torrent file, an http(s) URL to one, a directory of them, a glob\n" +
			"pattern, or a text file listing one magnet per line.\n\n" +
			"--files picks which files inside the torrent to download, so a\n" +
			"season pack does not start pulling the extras before you can say\n" +
			"otherwise. A pattern with a slash in it is matched against a file's\n" +
			"whole path inside the torrent, one without against its base name -\n" +
			"so '*.mkv' finds videos at any depth, and 'Season 1/*' finds a\n" +
			"folder.\n\n" +
			"The choice is applied when the torrent's file list arrives, which\n" +
			"for a magnet means after a peer supplies it. If nothing matches,\n" +
			"nothing is downloaded and the daemon log says so - widening it back\n" +
			"to everything would defeat the point of asking.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Arguments are resolved here rather than in the daemon so
			// that relative paths and globs are interpreted against the
			// directory the user is standing in, not the daemon's.
			sources, err := batch.Expand(args)
			if err != nil {
				return err
			}

			opts := engine.AddOpts{SavePath: savePath, Paused: paused, Files: files}
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
	cmd.Flags().StringSliceVar(&files, "files", nil,
		"download only files matching these glob patterns, e.g. '*.mkv' (repeatable, or comma-separated)")
	return cmd
}
