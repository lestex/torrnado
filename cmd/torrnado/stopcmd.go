package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/ipc"
)

// stopPoll is how often to re-ask whether the daemon has let the lock go.
const stopPoll = 100 * time.Millisecond

// `torrnado stop` shuts the daemon down and waits for it to finish.
//
// The docs used to say there was deliberately no such command, and the
// reasoning was sound but aimed at the wrong thing: a daemon that is
// still seeding is a normal state to leave a machine in, so stopping
// should not be casual - which is an argument against stopping by
// reflex, not against having a way to do it. What was there instead was
// an lsof incantation in the documentation, which is worse: it is easy to
// get wrong, and `pkill -f "torrnado daemon"` - the obvious thing to
// reach for - misses a daemon running from a renamed binary while
// happily matching processes that are not daemons at all.
//
// Waiting rather than firing and forgetting, because SIGTERM is where
// the session is saved: returning before that has happened would invite
// starting a new daemon while the old one is still writing.
func newStopCmd() *cobra.Command {
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon and wait for it to shut down",
		Long: "Sends SIGTERM to the daemon that owns the configured socket and waits\n" +
			"for it to exit, which is when it has saved its session and closed the\n" +
			"torrent engine cleanly.\n\n" +
			"The daemon is found by the lock it holds beside its socket, not by\n" +
			"matching on a process name - so it works for a daemon running from a\n" +
			"renamed binary, and it will never signal something that is not the\n" +
			"daemon for this state directory.\n\n" +
			"Stopping is not something to do by reflex: a daemon that is still\n" +
			"seeding is a normal state to leave a machine in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			info, err := ipc.DaemonInfo(cfg.DaemonSocket)
			if err != nil {
				return err
			}
			// Stopping something already stopped is a success, not a
			// failure - a script that stops before starting should not
			// have to special-case the first run.
			if !info.Running {
				fmt.Fprintf(out, "no daemon is running on %s\n", cfg.DaemonSocket)
				return nil
			}
			if info.PID == 0 {
				return fmt.Errorf("a daemon holds %s but did not record its pid "+
					"(it predates `torrnado stop`); stop it with the pid from its "+
					"`daemon starting` log line", info.LockPath)
			}

			if err := signalStop(info.PID); err != nil {
				return fmt.Errorf("signal daemon %d: %w", info.PID, err)
			}
			fmt.Fprintf(out, "asked daemon %d to stop\n", info.PID)

			if timeout <= 0 {
				return nil // asked not to wait
			}
			if err := waitForStop(cfg.DaemonSocket, timeout); err != nil {
				return err
			}
			fmt.Fprintln(out, "stopped")
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second,
		"how long to wait for the daemon to exit (0 to return immediately)")
	return cmd
}

// waitForStop blocks until nothing holds the daemon lock any more.
//
// The lock is what is waited on rather than the process, because the
// lock is what the next daemon needs: the kernel drops it when the
// process dies, so its release is exactly the moment a restart would
// succeed. Watching the pid instead would answer a moment too early and
// leave a race with anything about to start one.
func waitForStop(socket string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := ipc.DaemonInfo(socket)
		if err != nil {
			return err
		}
		if !info.Running {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon %d did not stop within %s; "+
				"it may be finishing a hash check - wait, or send SIGKILL to give up on a clean shutdown",
				info.PID, timeout)
		}
		time.Sleep(stopPoll)
	}
}
