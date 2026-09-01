package main

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/format"
	"github.com/lestex/torrnado/internal/ipc"
)

// statusWait is how long to wait for the daemon's first pushed event.
// The engine broadcasts on a one-second tick, so this is a couple of
// ticks' grace and not a guess.
const statusWait = 2500 * time.Millisecond

// `torrnado status` answers "is a daemon running, and which one".
//
// Nothing else could answer it. Every other subcommand dials the socket
// and *starts* a daemon when nothing replies, which is exactly the wrong
// behaviour for a question about whether one is running - asking would
// make the answer yes. This is the one command that never spawns.
//
// It also separates two states that look identical from outside and are
// not: a daemon that is running and answering, and one that is running
// but too busy to accept a connection. The latter is real - a daemon
// hash-checking a large torrent can miss any timeout you pick - and
// reporting it as "not running" is how someone ends up starting a second
// daemon on the same data directory.
func newStatusCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report whether a daemon is running, and what it is doing",
		Long: "Reports whether a daemon owns the configured socket, which process it\n" +
			"is, and - when it answers - its build, uptime and current transfers.\n\n" +
			"Unlike every other subcommand this never starts a daemon: asking\n" +
			"whether one is running must not be what makes one run.\n\n" +
			"With --quiet it prints nothing and reports the answer as its exit\n" +
			"status: 0 if a daemon is running, 1 if not.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}
			info, err := ipc.DaemonInfo(cfg.DaemonSocket)
			if err != nil {
				return err
			}
			if quiet {
				if !info.Running {
					// Silent, so a shell can branch on this without
					// having to discard output.
					return errSilentNotRunning
				}
				return nil
			}
			return writeStatusReport(cmd.OutOrStdout(), cfg, info)
		},
	}
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false,
		"print nothing; exit 0 if a daemon is running, 1 if not")
	return cmd
}

// errSilentNotRunning carries a non-zero exit without a message, for
// --quiet. main prints an empty error as nothing at all.
var errSilentNotRunning = silentErr{}

type silentErr struct{}

func (silentErr) Error() string { return "" }

func writeStatusReport(out io.Writer, cfg config.Config, info ipc.DaemonStatus) error {
	w := newReportWriter(out)

	fmt.Fprintln(w, "Daemon")
	if !info.Running {
		fmt.Fprintf(w, "  status\tnot running\n")
		fmt.Fprintf(w, "  socket\t%s\n", cfg.DaemonSocket)
		fmt.Fprintf(w, "  lock\t%s\n", info.LockPath)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Start one with `torrnado daemon`, or any command that needs it.")
		return w.Flush()
	}

	// Reached for before printing anything about the daemon's own state,
	// because whether it answers is the interesting half.
	global, reachable := probeDaemon(cfg.DaemonSocket)

	switch {
	case reachable:
		fmt.Fprintf(w, "  status\trunning\n")
	default:
		// Worth saying at length: this is the state that gets mistaken
		// for a dead daemon, and acting on that mistake means two
		// engines on one data directory.
		fmt.Fprintf(w, "  status\trunning, but not answering on its socket\n")
	}
	fmt.Fprintf(w, "  pid\t%s\n", pidText(info.PID))
	fmt.Fprintf(w, "  socket\t%s\n", cfg.DaemonSocket)
	fmt.Fprintf(w, "  lock\t%s\n", info.LockPath)

	if !reachable {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "It holds the lock, so it is alive - most likely busy hash-checking.")
		fmt.Fprintln(w, "Do not start a second daemon against this state directory.")
		return w.Flush()
	}

	// Empty rather than absent when the daemon predates these fields,
	// which is normal: it outlives its clients, so an old one answering a
	// new CLI is an ordinary Tuesday.
	fmt.Fprintf(w, "  version\t%s\n", orUnknown(global.Version))
	fmt.Fprintf(w, "  uptime\t%s\n", uptimeText(global.StartedAt))
	fmt.Fprintf(w, "  listen port\t%s\n", orUnknownInt(global.ListenPort))
	if global.VPNRequired {
		fmt.Fprintf(w, "  vpn\t%s\n", vpnText(global))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Transfers")
	fmt.Fprintf(w, "  torrents\t%d\n", global.NumTorrents)
	fmt.Fprintf(w, "  down\t%s\n", format.Rate(global.DownloadBPS))
	fmt.Fprintf(w, "  up\t%s\n", format.Rate(global.UploadBPS))
	if global.DiskTotalBytes > 0 {
		fmt.Fprintf(w, "  disk free\t%s of %s\n",
			format.Bytes(global.DiskFreeBytes), format.Bytes(global.DiskTotalBytes))
	}
	return w.Flush()
}

// probeDaemon connects and waits for the first pushed event, which is
// where the daemon's own view of itself lives.
//
// The state comes from an event rather than a request because that is
// how the protocol already carries it - the daemon pushes GlobalStats on
// every tick, and adding a method to ask for the same thing would be a
// second way to say it, free to drift from the first.
func probeDaemon(socket string) (engine.GlobalStats, bool) {
	c, err := ipc.Dial(socket)
	if err != nil {
		return engine.GlobalStats{}, false
	}
	defer c.Close()

	if err := c.Ping(); err != nil {
		return engine.GlobalStats{}, false
	}
	select {
	case ev, ok := <-c.Events():
		if !ok {
			return engine.GlobalStats{}, false
		}
		return ev.Global, true
	case <-time.After(statusWait):
		// It answered a ping, so it is alive and serving; it just has not
		// ticked yet. Reachable, with nothing to report.
		return engine.GlobalStats{}, true
	}
}

func pidText(pid int) string {
	if pid == 0 {
		// A daemon from before the pid was recorded. Saying why stops
		// this reading as a failure.
		return "unknown (started by a torrnado that did not record it)"
	}
	return fmt.Sprint(pid)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown (this daemon is too old to report it)"
	}
	return s
}

func orUnknownInt(n int) string {
	if n == 0 {
		return "unknown"
	}
	return fmt.Sprint(n)
}

func uptimeText(started time.Time) string {
	if started.IsZero() {
		return "unknown (this daemon is too old to report it)"
	}
	d := time.Since(started)
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return d.Round(time.Minute).String()
	default:
		return d.Round(time.Minute).String()
	}
}

func vpnText(g engine.GlobalStats) string {
	if g.VPNActive {
		return fmt.Sprintf("required, active on %s", g.VPNInterface)
	}
	return "required, NOT active - transfers are held"
}
