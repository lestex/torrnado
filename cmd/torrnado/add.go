package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

func newAddCmd() *cobra.Command {
	var savePath string
	var paused bool

	cmd := &cobra.Command{
		Use:   "add <magnet|torrent-file>...",
		Short: "Add one or more torrents",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := engine.AddOpts{SavePath: savePath, Paused: paused}
			return withClient(func(c *ipc.Client) error {
				var failed int
				for _, src := range args {
					id, err := addSource(c, src, opts)
					if err != nil {
						fmt.Printf("failed %s: %v\n", src, err)
						failed++
						continue
					}
					fmt.Printf("added: %s\n", id)
				}
				// One bad source should not stop the others, but the exit
				// status still has to say something went wrong -- scripts
				// read that, not the printed lines.
				if failed > 0 {
					return fmt.Errorf("%d of %d sources failed", failed, len(args))
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&savePath, "save-path", "", "download into this directory instead of the default")
	cmd.Flags().BoolVar(&paused, "paused", false, "add the torrent but leave it paused")
	return cmd
}

// addSource picks the right call for one source. A magnet is a URI with a
// known scheme; anything else is treated as a path to a .torrent file.
func addSource(c *ipc.Client, source string, opts engine.AddOpts) (engine.TorrentID, error) {
	if isMagnet(source) {
		return c.AddMagnet(source, opts)
	}
	return c.AddTorrentFile(source, opts)
}

func isMagnet(source string) bool {
	return strings.HasPrefix(strings.ToLower(source), "magnet:")
}
