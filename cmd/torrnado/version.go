package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Stamped at build time by the release build and by `make build`:
//
//	-ldflags "-X main.version=v0.1.0 -X main.commit=abc1234 -X main.date=..."
//
// Left at their defaults by a plain `go build`, which is why buildInfo
// below falls back to what the toolchain records on its own.
var (
	version = ""
	commit  = ""
	date    = ""
)

// buildInfo is what `torrnado version` reports, and what the daemon logs
// on startup.
//
// It is worth having at all because the daemon outlives its clients by
// design: a CLI built this afternoon routinely talks to a daemon started
// last week, and when the two disagree the failure is a bare `unknown
// method "PurgeData"` with nothing to say which side is old.
type buildInfo struct {
	Version string
	Commit  string
	Date    string
	Go      string
}

func currentBuild() buildInfo {
	b := buildInfo{Version: version, Commit: commit, Date: date, Go: runtime.Version()}

	// Nothing was stamped: this is a plain `go build` or a `go install
	// ...@latest`, both of which the toolchain describes itself. Reading
	// it back is the difference between "dev" and a revision someone can
	// actually look up.
	info, ok := debug.ReadBuildInfo()
	if !ok {
		if b.Version == "" {
			b.Version = "dev"
		}
		return b
	}

	if b.Version == "" {
		b.Version = moduleVersion(info.Main.Version)
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if b.Commit == "" {
				b.Commit = s.Value
			}
		case "vcs.time":
			if b.Date == "" {
				b.Date = s.Value
			}
		case "vcs.modified":
			// An uncommitted tree is worth saying: the revision alone
			// would name a commit this binary does not actually match.
			if s.Value == "true" {
				b.Commit += "-dirty"
			}
		}
	}
	return b
}

// moduleVersion turns what the module graph says into something worth
// showing.
//
// A `go install github.com/lestex/torrnado/cmd/torrnado@v0.1.0` records
// the tag, which is exactly what someone wants to see. A build from a
// working tree records either "(devel)" or a pseudo-version
// (v0.0.0-20260807014500-0f0fcc253799), and the pseudo-version is worse
// than useless here: it is mostly a restatement of the commit and the
// timestamp reported on the next two lines.
func moduleVersion(v string) string {
	if v == "" || v == "(devel)" || strings.HasPrefix(v, "v0.0.0-") {
		return "dev"
	}
	// The toolchain marks an uncommitted tree with +dirty; the commit
	// below carries that already.
	return strings.TrimSuffix(v, "+dirty")
}

// String is the one-line form, for a log field or a --version.
func (b buildInfo) String() string {
	parts := []string{b.Version}
	if b.Commit != "" {
		parts = append(parts, "("+shortCommit(b.Commit)+")")
	}
	return strings.Join(parts, " ")
}

func shortCommit(c string) string {
	// Long enough to be unambiguous, short enough to sit in a log line.
	if base, suffix, found := strings.Cut(c, "-"); found && len(base) > 12 {
		return base[:12] + "-" + suffix
	}
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			writeVersion(cmd.OutOrStdout(), currentBuild())
			return nil
		},
	}
}

func writeVersion(out io.Writer, b buildInfo) {
	fmt.Fprintf(out, "torrnado %s\n", b.Version)
	if b.Commit != "" {
		fmt.Fprintf(out, "  commit  %s\n", b.Commit)
	}
	if b.Date != "" {
		fmt.Fprintf(out, "  built   %s\n", b.Date)
	}
	fmt.Fprintf(out, "  go      %s\n", b.Go)
}
