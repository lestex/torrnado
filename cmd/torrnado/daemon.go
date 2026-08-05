package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/stream"
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
	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}

	eng, err := engine.New(engine.Config{
		DataDir:           cfg.DownloadDir,
		ListenPortLow:     cfg.Port.Low,
		ListenPortHigh:    cfg.Port.High,
		DisableDHT:        !cfg.Network.DHT,
		DisablePEX:        !cfg.Network.PEX,
		DisableEncryption: !cfg.Network.Encryption,
		UploadRateLimit:   int64(cfg.RateLimit.Upload),
		DownloadRateLimit: int64(cfg.RateLimit.Download),
		Seed:              cfg.Network.Seed,
	})
	if err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	// Shut down in the reverse order things started: stop accepting
	// clients first, then stop the engine underneath them. Deferred calls
	// run last-in-first-out, which gives that for free.
	defer eng.Close()

	// The stream server carries file data for previews; it cannot ride
	// the IPC socket (see internal/stream). Started first so its URL
	// builder is available to the RPC server.
	stm, err := stream.Serve(eng)
	if err != nil {
		return fmt.Errorf("start stream server: %w", err)
	}
	defer stm.Close()

	srv, err := ipc.Serve(cfg.DaemonSocket, eng, stm.URL)
	if err != nil {
		return fmt.Errorf("start ipc server: %w", err)
	}
	defer srv.Close()

	fmt.Fprintf(os.Stderr, "torrnado daemon: config %s, data dir %s, socket %s, stream %s\n",
		path, cfg.DownloadDir, cfg.DaemonSocket, stm.Addr())

	// Block until asked to stop. Ctrl-C sends SIGINT; service managers
	// send SIGTERM. Without this the function would return immediately
	// and the deferred shutdown would tear everything down.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Fprintln(os.Stderr, "torrnado daemon: shutting down")
	return nil
}
