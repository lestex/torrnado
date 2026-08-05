package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/logging"
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

	lg, err := logging.New(cfg.Log.Level, cfg.Log.File)
	if err != nil {
		return err
	}
	defer lg.Close()

	libLevel, err := logging.ParseLevel(cfg.Log.LibraryLevel)
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
		StateDir:          cfg.StateDir,
		Logger:            lg.Logger,
		LibraryLevel:      libLevel,
	})
	if err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	// Shut down in the reverse order things started: stop accepting
	// clients first, then stop the engine underneath them. Deferred calls
	// run last-in-first-out, which gives that for free.
	defer eng.Close()

	// Logged before the restore rather than only once everything is up,
	// so a journal reads in the order things happened. The pid is here
	// because the README tells people to kill the daemon by it, and until
	// now no line actually carried one.
	lg.Info("daemon starting",
		"pid", os.Getpid(),
		"config", path,
		"download_dir", cfg.DownloadDir,
		"state_dir", cfg.StateDir,
	)

	// Restored before the socket is listening, so the first client to
	// connect sees the full list rather than one filling in underneath
	// it. A broken session file is logged and skipped: refusing to start
	// over one bad record would be worse than starting with fewer
	// torrents.
	if n, err := eng.RestoreSession(); err != nil {
		lg.Error("restoring the session failed", "err", err)
	} else if n > 0 {
		lg.Info("session restored", "torrents", n)
	}

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

	lg.Info("daemon ready", "socket", cfg.DaemonSocket, "stream", stm.Addr())

	// Block until asked to stop. Ctrl-C sends SIGINT; service managers
	// send SIGTERM. Without this the function would return immediately
	// and the deferred shutdown would tear everything down.
	//
	// SIGHUP is the convention for "reopen your log file", which is how
	// logrotate finishes a rotation: it renames the file away, and a
	// process that keeps writing to the same handle is writing to an
	// unlinked inode -- the log goes nowhere and the disk never gets the
	// space back.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for sig := range sigCh {
		if sig == syscall.SIGHUP {
			if err := lg.Reopen(); err != nil {
				lg.Error("reopening the log file failed", "err", err)
				continue
			}
			lg.Info("log file reopened")
			continue
		}
		lg.Info("daemon shutting down", "signal", sig.String())
		return nil
	}
	return nil
}
