package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
)

// `torrnado init` writes the config file that would otherwise have to be
// typed from the documentation.
//
// A missing config is not an error anywhere else in this program, so this
// is a convenience rather than a setup step: what it writes is exactly
// what torrnado already does. The value is that a file listing every key,
// with this machine's resolved paths in it, is something you can edit -
// where an empty ~/.config/torrnado is something you have to research.
func newInitCmd() *cobra.Command {
	var (
		force  bool
		stdout bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a config file containing the built-in defaults",
		Long: "Writes an annotated config.toml with every key set to its built-in\n" +
			"default, to $XDG_CONFIG_HOME/torrnado/config.toml or wherever --config\n" +
			"points, creating the directory if needed.\n\n" +
			"An existing file is never overwritten without --force. torrnado does\n" +
			"not need this file - a missing config is not an error - so this is a\n" +
			"starting point to edit, not a step to run before anything else works.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := initPath()
			if err != nil {
				return err
			}
			// Deliberately Default() rather than loadConfig(): this command
			// writes the defaults, and reading the existing file first would
			// both echo back settings that are already there and refuse to
			// run at all when the file it is being asked to replace is the
			// invalid one.
			cfg, err := config.Default()
			if err != nil {
				return err
			}
			return runInit(cmd.OutOrStdout(), path, cfg, force, stdout)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false,
		"overwrite an existing config file")
	cmd.Flags().BoolVar(&stdout, "print", false,
		"write to stdout instead of to the config file")
	return cmd
}

// initPath resolves where the file goes: --config if given, the XDG
// default otherwise - the same path every other command reads, or the
// generated file would land somewhere nothing looks at.
func initPath() (string, error) {
	if configPathFlag != "" {
		return configPathFlag, nil
	}
	return config.DefaultPath()
}

func runInit(out io.Writer, path string, cfg config.Config, force, stdout bool) error {
	doc := config.Template(cfg)

	if stdout {
		_, err := out.Write(doc)
		return err
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// O_EXCL rather than a Stat first: the check and the write are one
	// operation, so nothing can appear in between, and a config someone
	// spent an evening on is not something to lose to a race.
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	if force {
		flags = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists (pass --force to overwrite it, "+
				"or --print to see the defaults without writing)", path)
		}
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(doc); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	// Checked rather than deferred: a write that fails on close is a
	// truncated config file, and reporting success over it would send
	// someone looking for the problem everywhere but here.
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(out, "wrote %s\n", path)
	fmt.Fprintln(out, "every key is optional - deleting a line restores its default")
	return nil
}
