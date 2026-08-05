package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the torrent engine in the foreground, listening on the daemon socket",
		Long: "Runs the torrent engine and its IPC server in the foreground until\n" +
			"interrupted (Ctrl-C) or sent SIGTERM. Other torrnado commands normally\n" +
			"spawn this for you in the background; run it directly if you want to\n" +
			"manage it yourself (e.g. under systemd/launchd).",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}
}

func runDaemon() error {
	dir, err := downloadDir()
	if err != nil {
		return err
	}
	sock, err := socketPath()
	if err != nil {
		return err
	}

	eng, err := engine.New(engine.Config{DataDir: dir})
	if err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	// Shut down in the reverse order things started: stop accepting
	// clients first, then stop the engine underneath them. Deferred calls
	// run last-in-first-out, which gives that for free.
	defer eng.Close()

	srv, err := ipc.Serve(sock, eng)
	if err != nil {
		return fmt.Errorf("start ipc server: %w", err)
	}
	defer srv.Close()

	fmt.Fprintf(os.Stderr, "torrnado daemon: data dir %s, socket %s\n", dir, sock)

	// Block until asked to stop. Ctrl-C sends SIGINT; service managers
	// send SIGTERM. Without this the function would return immediately
	// and the deferred shutdown would tear everything down.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Fprintln(os.Stderr, "torrnado daemon: shutting down")
	return nil
}
