package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

// `torrnado seed-limit` sets when a torrent stops seeding.
//
// Per-torrent only. The defaults live in config.toml, because they are a
// policy for the machine rather than something to retype per torrent;
// this is the override for the one torrent that should not follow it.
func newSeedLimitCmd() *cobra.Command {
	var (
		ratioFlag string
		timeFlag  string
	)

	cmd := &cobra.Command{
		Use:   "seed-limit <id...>",
		Short: "Set when a torrent stops seeding (ratio, time, or both)",
		Long: "Sets a torrent's own seeding limits, overriding [seed_limit] in the\n" +
			"config. A torrent is stopped once it has finished downloading and\n" +
			"then meets either limit, whichever comes first.\n\n" +
			"Both flags take \"default\" to follow the configured value again, and\n" +
			"\"none\" to seed without limit however the config is set - which is\n" +
			"the only way one torrent can opt out of a default that would\n" +
			"otherwise stop it.\n\n" +
			"Stopping is a pause: it survives a restart, and resuming the torrent\n" +
			"starts it seeding again.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ratioFlag == "" && timeFlag == "" {
				return fmt.Errorf("give --ratio, --time, or both")
			}
			ratio, err := parseSeedRatio(ratioFlag)
			if err != nil {
				return err
			}
			seedTime, err := parseSeedTime(timeFlag)
			if err != nil {
				return err
			}

			return withClient(func(c *ipc.Client) error {
				for _, id := range args {
					if err := c.SetSeedLimit(engine.TorrentID(id), ratio, seedTime); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "seed limit set: %s\n", id)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&ratioFlag, "ratio", "",
		`stop at this ratio, "none" to seed without one, or "default"`)
	cmd.Flags().StringVar(&timeFlag, "time", "",
		`stop after this long seeding (e.g. 48h, 7d), "none", or "default"`)
	return cmd
}

// parseSeedRatio maps the flag onto the engine's convention: zero is
// "use the configured default" and negative is "no limit for this one".
//
// An unset flag is also zero, which means the same thing - so setting
// only --time leaves the ratio following the config rather than silently
// clearing it.
func parseSeedRatio(s string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default":
		return 0, nil
	case "none", "unlimited":
		return -1, nil
	}
	r, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || r <= 0 {
		return 0, fmt.Errorf("--ratio must be a positive number, \"none\" or \"default\", got %q", s)
	}
	return r, nil
}

func parseSeedTime(s string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "default":
		return 0, nil
	case "none", "unlimited":
		return -1, nil
	}
	d, err := config.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("--time must be positive, \"none\" or \"default\", got %q", s)
	}
	return d, nil
}
