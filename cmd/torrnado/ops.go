package main

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
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

// eachTorrent runs fn for every id given, reporting failures as it goes
// but carrying on. One bad id should not stop the rest, though the exit
// status still has to reflect that something went wrong.
func eachTorrent(ids []string, fn func(*ipc.Client, engine.TorrentID) error) error {
	return withClient(func(c *ipc.Client) error {
		var failed int
		for _, id := range ids {
			if err := fn(c, engine.TorrentID(id)); err != nil {
				fmt.Printf("failed on %s: %v\n", id, err)
				failed++
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d of %d failed", failed, len(ids))
		}
		return nil
	})
}

func newRemoveCmd() *cobra.Command {
	var deleteData bool

	cmd := &cobra.Command{
		Use:   "remove <torrent-id>...",
		Short: "Remove one or more torrents",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachTorrent(args, func(c *ipc.Client, id engine.TorrentID) error {
				return c.Remove(id, deleteData)
			})
		},
	}
	cmd.Flags().BoolVar(&deleteData, "delete-data", false, "also delete the downloaded files")
	return cmd
}

// Pause and resume are separate commands rather than one that toggles.
// A script that says "pause" has to mean it, whatever state the torrent
// happens to be in.
func newPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <torrent-id>...",
		Short: "Pause one or more torrents",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachTorrent(args, func(c *ipc.Client, id engine.TorrentID) error {
				return c.SetPaused(id, true)
			})
		},
	}
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <torrent-id>...",
		Short: "Resume one or more torrents",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachTorrent(args, func(c *ipc.Client, id engine.TorrentID) error {
				return c.SetPaused(id, false)
			})
		},
	}
}

func newRecheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recheck <torrent-id>...",
		Short: "Re-verify downloaded data against the torrent's piece hashes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachTorrent(args, func(c *ipc.Client, id engine.TorrentID) error {
				return c.ForceRecheck(id)
			})
		},
	}
}

func newPriorityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "priority <torrent-id> <file-index> <none|low|normal|high|now>",
		Short: "Set a file's download priority",
		Long: "Sets how badly one file's data is wanted. File indexes are the\n" +
			"positions shown by the detail view, counting from zero.\n\n" +
			"Note that \"low\" is accepted but stored as \"normal\": the torrent\n" +
			"library has no level between \"not wanted\" and \"wanted\".",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("file-index must be a number: %w", err)
			}
			prio, ok := engine.ParsePriority(args[2])
			if !ok {
				return fmt.Errorf("unknown priority %q (want: none, low, normal, high, now)", args[2])
			}
			return withClient(func(c *ipc.Client) error {
				return c.SetFilePriority(engine.TorrentID(args[0]), idx, prio)
			})
		},
	}
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
				fmt.Fprintln(w, "ID\tNAME\tSTATE\tPROGRESS\tDOWN\tUP\tRATIO\tPEERS\tETA")
				for _, s := range snaps {
					fmt.Fprintf(w, "%s\t%s\t%s\t%.1f%%\t%s\t%s\t%s\t%d/%d\t%s\n",
						s.ID, s.Name, s.State, s.Progress*100,
						format.Rate(s.DownloadBPS), format.Rate(s.UploadBPS),
						format.Ratio(s.Ratio), s.NumSeeds, s.NumPeers, format.ETA(s.ETA))
				}
				return w.Flush()
			})
		},
	}
}
