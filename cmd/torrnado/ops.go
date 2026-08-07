package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/format"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/player"
)

// withClient connects to (or spawns) the daemon, runs fn, and always
// closes the connection afterwards.
//
// Every subcommand needs those three steps, and forgetting the close
// leaks a connection on the daemon side, so they live here once.
func withClient(fn func(*ipc.Client) error) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := dialOrSpawn(cfg)
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

// Purge is its own verb rather than a flag on remove, because it is the
// opposite half of what remove does: remove keeps the files and drops the
// torrent, purge keeps the torrent and drops the files.
func newPurgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "purge <torrent-id>...",
		Short: "Delete a torrent's data, keeping the torrent in the list",
		Long: "Deletes the downloaded files and keeps the torrent, paused and at\n" +
			"zero, with its save path, rate limits and place in the list intact.\n" +
			"For freeing space without losing the entry -- resuming downloads it\n" +
			"again.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return eachTorrent(args, func(c *ipc.Client, id engine.TorrentID) error {
				return c.PurgeData(id)
			})
		},
	}
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

func newLimitCmd() *cobra.Command {
	var torrentID string

	cmd := &cobra.Command{
		Use:   "limit <up|down> <rate|unlimited>",
		Short: "Set a rate limit (global by default, or per-torrent with --torrent)",
		Long: "Sets an upload or download speed cap. Global limits are enforced\n" +
			"precisely by the torrent library.\n\n" +
			"Per-torrent limits (--torrent <id>) are a best-effort approximation:\n" +
			"the library throttles the whole client and offers no per-torrent\n" +
			"hook, so the daemon toggles the torrent off and on around the cap\n" +
			"each second. It averages out, but it is bursty.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			direction := strings.ToLower(args[0])
			if direction != "up" && direction != "down" {
				return fmt.Errorf("direction must be \"up\" or \"down\", got %q", args[0])
			}
			bps, err := config.ParseRate(args[1])
			if err != nil {
				return err
			}

			return withClient(func(c *ipc.Client) error {
				if torrentID != "" {
					// The per-torrent call sets both directions at once, so
					// -1 means "leave this one alone".
					up, down := int64(-1), int64(-1)
					if direction == "up" {
						up = bps
					} else {
						down = bps
					}
					return c.SetTorrentRateLimit(engine.TorrentID(torrentID), up, down)
				}
				if direction == "up" {
					return c.SetGlobalUploadLimit(bps)
				}
				return c.SetGlobalDownloadLimit(bps)
			})
		},
	}
	cmd.Flags().StringVar(&torrentID, "torrent", "", "limit only this torrent (approximate)")
	return cmd
}

func newMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "move <torrent-id> <new-directory>",
		Short: "Move a torrent's downloaded data to a new directory",
		Long: "Moves the files, then re-verifies them in place. The torrent is\n" +
			"re-added internally, which resets any custom per-file priorities.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(c *ipc.Client) error {
				return c.MoveStorage(engine.TorrentID(args[0]), args[1])
			})
		},
	}
}

func newPreviewCmd() *cobra.Command {
	var play bool

	cmd := &cobra.Command{
		Use:   "preview <torrent-id> <file-index>",
		Short: "Print a local streaming URL for a file (playable while downloading)",
		Long: "Prints a loopback HTTP URL serving one file's data. The URL is playable\n" +
			"immediately -- reads block until the pieces arrive, and watching drives\n" +
			"which pieces are fetched first -- so it works long before the torrent\n" +
			"finishes. Asking for it resumes the torrent and raises the file's\n" +
			"priority, since a paused or unwanted file cannot stream.\n\n" +
			"The URL carries a token and is only valid for the life of the daemon\n" +
			"that issued it.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			idx, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("file-index must be a number: %w", err)
			}
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}
			return withClient(func(c *ipc.Client) error {
				url, err := c.PreviewURL(engine.TorrentID(args[0]), idx)
				if err != nil {
					return err
				}
				if play {
					return player.Launch(cfg.Player, url)
				}
				fmt.Println(url)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&play, "play", false, "open the URL in the configured player instead of printing it")
	return cmd
}

func newListCmd() *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List torrents known to the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(c *ipc.Client) error {
				snaps, err := c.List()
				if err != nil {
					return err
				}
				if !watch {
					return writeTorrentTable(os.Stdout, snaps)
				}
				return watchTorrents(c, snaps)
			})
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false,
		"redraw as the daemon reports changes, until interrupted")
	return cmd
}

// writeTorrentTable renders the snapshot table used by both `list` and
// its --watch mode, so the two can't drift apart.
func writeTorrentTable(out io.Writer, snaps []engine.TorrentSnapshot) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATE\tPROGRESS\tDOWN\tUP\tRATIO\tPEERS")
	for _, s := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%.1f%%\t%s\t%s\t%s\t%d/%d\n",
			s.ID, s.Name, s.StatusText(), s.Progress*100,
			format.Rate(s.DownloadBPS), format.Rate(s.UploadBPS), format.Ratio(s.Ratio),
			s.NumSeeds, s.NumPeers)
	}
	return w.Flush()
}

// watchTorrents redraws the table until interrupted.
//
// It renders the daemon's pushed events rather than polling List on a
// timer: the daemon already broadcasts a snapshot every tick, so this
// costs no extra RPCs and updates exactly when the state actually
// changes. initial is the snapshot already fetched, drawn immediately so
// the first frame isn't a blank second.
func watchTorrents(c *ipc.Client, initial []engine.TorrentSnapshot) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Cursor-home-and-clear only makes sense on a terminal. Piped into a
	// file or a pager, escape codes would be garbage in the output, so
	// there the frames simply append.
	tty := isTerminal(os.Stdout)

	draw := func(snaps []engine.TorrentSnapshot, global engine.GlobalStats) error {
		var b bytes.Buffer
		if tty {
			b.WriteString("\033[H\033[2J")
		}
		if err := writeTorrentTable(&b, snaps); err != nil {
			return err
		}
		fmt.Fprintf(&b, "\n%s  |  %d torrents  |  ↓ %s  ↑ %s  |  ctrl-c to stop\n",
			time.Now().Format("15:04:05"), len(snaps),
			format.Rate(global.DownloadBPS), format.Rate(global.UploadBPS))
		_, err := os.Stdout.Write(b.Bytes())
		return err
	}

	// List returns snapshots without the daemon's aggregate stats, so the
	// first frame's totals are summed from the rows rather than shown as
	// zero until the first event lands.
	if err := draw(initial, sumRates(initial)); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-c.Events():
			if !ok {
				return fmt.Errorf("lost connection to daemon")
			}
			if err := draw(ev.Torrents, ev.Global); err != nil {
				return err
			}
		}
	}
}

func sumRates(snaps []engine.TorrentSnapshot) engine.GlobalStats {
	g := engine.GlobalStats{NumTorrents: len(snaps)}
	for _, s := range snaps {
		g.DownloadBPS += s.DownloadBPS
		g.UploadBPS += s.UploadBPS
	}
	return g
}

// isTerminal reports whether f is a character device, i.e. a terminal
// rather than a pipe or a file.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
