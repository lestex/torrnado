package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/format"
	"github.com/lestex/torrnado/internal/ipc"
)

// withClient connects to (or spawns) the daemon, runs fn, and always
// closes the connection afterwards.
//
// Every subcommand needs those three steps, and forgetting the close
// leaks a connection on the daemon side, so they live here once.
func withClient(fn func(*ipc.Client) error) error {
	client, err := dialOrSpawn()
	if err != nil {
		return err
	}
	defer client.Close()
	return fn(client)
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List torrents known to the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(c *ipc.Client) error {
				snaps, err := c.List()
				if err != nil {
					return err
				}
				// tabwriter lines columns up by padding them once it has
				// seen every row, so the widest name still fits.
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tNAME\tSTATE\tPROGRESS\tDOWN\tETA")
				for _, s := range snaps {
					fmt.Fprintf(w, "%s\t%s\t%s\t%.1f%%\t%s\t%s\n",
						s.ID, s.Name, s.State, s.Progress*100,
						format.Rate(s.DownloadBPS), format.ETA(s.ETA))
				}
				return w.Flush()
			})
		},
	}
}
