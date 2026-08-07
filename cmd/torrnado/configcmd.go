package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
)

// `torrnado config` answers the two questions a config file provokes:
// which file is being read, and what is actually in effect.
//
// Both are worth printing because neither is obvious. The path is
// XDG-derived and differs per machine; the settings are defaults merged
// with whatever the file overrode, so a value shown here may appear
// nowhere on disk.
//
// It never contacts the daemon. What it prints is what a daemon started
// now would use -- which is not necessarily what the one already running
// was started with.
func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the config file location and the settings in effect",
		Long: "Prints where torrnado looks for its configuration and the values\n" +
			"currently in effect (defaults, plus anything the file overrides).\n\n" +
			"Reads only the config file -- a running daemon may have been started\n" +
			"with different settings.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadConfig()
			if err != nil {
				return err
			}
			return writeConfigReport(cmd.OutOrStdout(), cfg, path)
		},
	}
}

func writeConfigReport(out io.Writer, cfg config.Config, path string) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)

	// A file that is not there is not an error -- torrnado runs on
	// defaults -- but it is the first thing to know when a setting is not
	// taking effect, so it is said plainly rather than left to be
	// inferred from the values below.
	note := ""
	if _, err := os.Stat(path); os.IsNotExist(err) {
		note = "\t(not found -- built-in defaults in use)"
	}

	fmt.Fprintln(w, "Paths")
	fmt.Fprintf(w, "  config\t%s%s\n", path, note)
	if themes, err := config.DefaultThemesDir(); err == nil {
		fmt.Fprintf(w, "  themes\t%s\n", themes)
	}
	fmt.Fprintf(w, "  download_dir\t%s\n", cfg.DownloadDir)
	fmt.Fprintf(w, "  state_dir\t%s\n", cfg.StateDir)
	fmt.Fprintf(w, "  daemon_socket\t%s\n", cfg.DaemonSocket)
	fmt.Fprintf(w, "  session\t%s\n", filepath.Join(cfg.StateDir, "session.json"))
	fmt.Fprintf(w, "  saved metainfo\t%s\n", filepath.Join(cfg.StateDir, "torrents"))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Settings")
	fmt.Fprintf(w, "  theme\t%s\n", cfg.Theme)
	fmt.Fprintf(w, "  player\t%s\n", cfg.Player)
	fmt.Fprintf(w, "  rate_limit.upload\t%s\n", cfg.RateLimit.Upload)
	fmt.Fprintf(w, "  rate_limit.download\t%s\n", cfg.RateLimit.Download)
	fmt.Fprintf(w, "  port\t%s\n", portRangeText(cfg.Port))
	fmt.Fprintf(w, "  network.dht\t%t\n", cfg.Network.DHT)
	fmt.Fprintf(w, "  network.pex\t%t\n", cfg.Network.PEX)
	// Called out where someone reading the value would otherwise believe
	// it, rather than only in the README.
	fmt.Fprintf(w, "  network.lsd\t%t\t(accepted, but the torrent library has no LSD)\n", cfg.Network.LSD)
	fmt.Fprintf(w, "  network.encryption\t%t\n", cfg.Network.Encryption)
	fmt.Fprintf(w, "  network.seed\t%t\n", cfg.Network.Seed)
	// What it does and does not cover, said where someone turning it on
	// will read it rather than only in the docs.
	if cfg.VPN.Required {
		fmt.Fprintf(w, "  vpn.required\ttrue\t%s\n", vpnRequiredNote)
	} else {
		fmt.Fprintln(w, "  vpn.required\tfalse")
	}
	if len(cfg.VPN.Interfaces) > 0 {
		fmt.Fprintf(w, "  vpn.interfaces\t%s\t(also counted as a VPN)\n", strings.Join(cfg.VPN.Interfaces, ", "))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Logging")
	fmt.Fprintf(w, "  log.level\t%s\n", cfg.Log.Level)
	fmt.Fprintf(w, "  log.library_level\t%s\t(the torrent library, filtered separately)\n", cfg.Log.LibraryLevel)
	if cfg.Log.File == "" {
		fmt.Fprintf(w, "  log.file\tstderr\t(whatever started the daemon captures it)\n")
	} else {
		fmt.Fprintf(w, "  log.file\t%s\n", cfg.Log.File)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Keybinds")
	if len(cfg.Keybinds) == 0 {
		fmt.Fprintln(w, "  (none -- press h in the TUI for the defaults)")
	} else {
		// Sorted, because ranging a map gives a different order every
		// run and a listing that reshuffles itself is hard to read.
		actions := make([]string, 0, len(cfg.Keybinds))
		for action := range cfg.Keybinds {
			actions = append(actions, action)
		}
		sort.Strings(actions)
		for _, action := range actions {
			fmt.Fprintf(w, "  %s\t%s\n", action, cfg.Keybinds[action])
		}
	}

	return w.Flush()
}

// vpnRequiredNote says what the guard covers, beside the value.
//
// Someone reading `true` here has to know that it holds transfers but not
// announces, or they will believe they are covered in a way they are not.
const vpnRequiredNote = "(transfers held off-VPN; tracker/DHT announces still go out)"

// portRangeText renders the listen-port range the way the config file
// describes it, including the 0 that means "let the OS choose".
func portRangeText(p config.PortRange) string {
	switch {
	case p.Low <= 0:
		return "any (chosen by the OS)"
	case p.High <= p.Low:
		return strconv.Itoa(p.Low)
	default:
		return strings.Join([]string{strconv.Itoa(p.Low), strconv.Itoa(p.High)}, "-")
	}
}
