package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
	"github.com/lestex/torrnado/internal/launch"
	"github.com/lestex/torrnado/internal/logging"
	"github.com/lestex/torrnado/internal/stream"
	"github.com/lestex/torrnado/internal/vpn"
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

	// Registered before anything slow, not just before the wait at the
	// bottom. Until this call every one of these signals still has its
	// default action, which for all three is to kill the process: a
	// SIGHUP arriving while the engine is starting or a session is being
	// restored would take the daemon down instead of reopening its log,
	// and a service manager's SIGTERM would be a hard kill rather than a
	// clean shutdown. The window is small, and it is exactly the window a
	// supervisor restarting the service aims at.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

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
		RequireVPN:        cfg.VPN.Required,
		VPNCheck:          vpnChecker(cfg.VPN.Interfaces),
		MinFreeSpace:      int64(cfg.MinFreeSpace),
		OnComplete:        completionHook(cfg.OnComplete, lg),
		SeedRatio:         cfg.SeedLimit.Ratio,
		SeedTime:          time.Duration(cfg.SeedLimit.Time),
		StateDir:          cfg.StateDir,
		Version:           currentBuild().String(),
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
		// The version first, because the daemon outlives its clients:
		// when a new CLI meets an old daemon the only symptom is an
		// "unknown method" error that says nothing about which is which.
		"version", currentBuild().String(),
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

	// Started after the socket is up, so a file dropped in during startup
	// is added by a daemon that can already answer for it.
	watchDone := make(chan struct{})
	if cfg.WatchDir != "" {
		if err := os.MkdirAll(cfg.WatchDir, 0o755); err != nil {
			return fmt.Errorf("create watch dir %s: %w", cfg.WatchDir, err)
		}
		w := newWatcher(cfg.WatchDir, func(path string) error {
			_, err := eng.AddTorrentFile(path, engine.AddOpts{})
			return err
		}, lg.Logger)
		go w.run(watchDone)
		lg.Info("watching for torrents", "dir", cfg.WatchDir)
	}
	defer close(watchDone)

	lg.Info("daemon ready", "socket", cfg.DaemonSocket, "stream", stm.Addr())

	// Block until asked to stop. Ctrl-C sends SIGINT; service managers
	// send SIGTERM. Without this the function would return immediately
	// and the deferred shutdown would tear everything down.
	//
	// SIGHUP is the convention for "reopen your log file", which is how
	// logrotate finishes a rotation: it renames the file away, and a
	// process that keeps writing to the same handle is writing to an
	// unlinked inode - the log goes nowhere and the disk never gets the
	// space back.
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

// completionHook adapts a configured command to the closure the engine
// takes, or returns nil when nothing is configured.
//
// The engine is handed a function for the same reason it is handed a VPN
// check: running a program means internal/launch, and the engine
// deliberately depends on nothing internal. This is the one place that
// knows about both.
//
// The command is detached, so a hook that runs for an hour does not hold
// up the tick that called it, and it outlives the daemon rather than
// being killed with it. Nothing waits for it, which also means nothing
// reports its exit status - a failure shows up in whatever the hook
// itself writes, not here.
func completionHook(command string, lg *logging.Logger) func(engine.TorrentSnapshot) {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return func(s engine.TorrentSnapshot) {
		env := []string{
			"TORRNADO_ID=" + string(s.ID),
			"TORRNADO_NAME=" + s.Name,
			"TORRNADO_PATH=" + s.DataDir,
			"TORRNADO_SIZE=" + strconv.FormatInt(s.TotalLength, 10),
		}
		// The folder rather than the torrent's name: a hook that unpacks,
		// moves or scans wants somewhere it can act on.
		if err := launch.DetachedEnv(command, s.DataDir, env); err != nil {
			lg.Error("the on_complete hook failed to start",
				"id", s.ID, "name", s.Name, "command", command, "err", err)
			return
		}
		lg.Info("on_complete hook started", "id", s.ID, "name", s.Name)
	}
}

// vpnChecker adapts internal/vpn to the closure the engine takes.
//
// The engine holds no dependency on the vpn package - it is handed a
// function and knows nothing about interfaces or routes - so the
// conversion between the two Status types lives here, at the one place
// that knows about both.
//
// The error is deliberately dropped: vpn.Detect already reports a failure
// as "not on a VPN" with a Reason, which is both what the guard should do
// with it and what the log should say.
func vpnChecker(interfaces []string) func() engine.VPNStatus {
	return func() engine.VPNStatus {
		st, _ := vpn.Detect(interfaces)
		return engine.VPNStatus{
			Active:    st.Active,
			Interface: st.Interface,
			Reason:    st.String(),
		}
	}
}
