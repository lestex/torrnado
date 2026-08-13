package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The man page is written from the live command tree rather than kept as
// a file someone has to remember to edit, for the same reason the TUI's
// help screen is generated from the live keymap: a flag added next month
// appears in it by itself, and one removed cannot linger.
//
// Hand-rolled roff rather than cobra/doc, which would pull go-md2man and
// blackfriday in for a single generated file. The subset a man page needs
// is small: .TH, .SH, .SS, .TP and .nf.

func newManCmd() *cobra.Command {
	var (
		out     string
		release string
	)
	cmd := &cobra.Command{
		Use:   "man",
		Short: "Write the man page for this program",
		Long: "Writes troff source for torrnado(1) to stdout, or to a file with\n" +
			"--output. Generated from the command tree, so it describes exactly\n" +
			"the binary that produced it. The release build runs this and ships\n" +
			"the result in the archive.",
		// Not something anyone needs in day-to-day use, and a `man`
		// subcommand sitting in the help output invites the guess that it
		// *reads* the manual rather than writes it.
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if out != "" {
				f, err := os.Create(out)
				if err != nil {
					return fmt.Errorf("create %s: %w", out, err)
				}
				defer f.Close()
				w = f
			}
			if release == "" {
				release = currentBuild().Version
			}
			return writeManPage(w, cmd.Root(), release, time.Now().UTC())
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "write to this file instead of stdout")
	cmd.Flags().StringVar(&release, "release", "",
		"version to name in the page header (default: this binary's)")
	return cmd
}

func writeManPage(w io.Writer, root *cobra.Command, release string, now time.Time) error {
	b := &strings.Builder{}

	fmt.Fprintf(b, ".TH TORRNADO 1 %q %q %q\n",
		now.Format("2006-01-02"), "torrnado "+release, "User Commands")

	section(b, "NAME")
	fmt.Fprintf(b, "torrnado \\- %s\n", roff(root.Short))

	section(b, "SYNOPSIS")
	fmt.Fprintf(b, ".B torrnado\n[\\fIcommand\\fR] [\\fIflags\\fR]\n")

	section(b, "DESCRIPTION")
	fmt.Fprintf(b, "%s\n", roff(root.Long))
	fmt.Fprintf(b, ".PP\n%s\n", roff(
		"Quitting the interface does not stop the daemon. Every subcommand "+
			"below dials the daemon's socket, spawning one if nothing answers, "+
			"makes a single call and exits."))

	section(b, "OPTIONS")
	if !writeFlags(b, root.PersistentFlags()) {
		fmt.Fprintln(b, "None.")
	}

	section(b, "COMMANDS")
	for _, c := range root.Commands() {
		if c.Hidden || !c.IsAvailableCommand() {
			continue
		}
		fmt.Fprintf(b, ".SS \\fB%s\\fR\n", roff(c.UseLine()))
		body := c.Long
		if body == "" {
			body = c.Short
		}
		fmt.Fprintf(b, "%s\n", roff(body))
		writeFlags(b, c.NonInheritedFlags())
	}

	section(b, "FILES")
	for _, f := range [][2]string{
		{"$XDG_CONFIG_HOME/torrnado/config.toml", "Configuration. A missing file is not an error; an invalid one is."},
		{"$XDG_CONFIG_HOME/torrnado/themes/<name>.toml", "Your own palettes, which shadow a built-in of the same name."},
		{"$XDG_DATA_HOME/torrnado/daemon.sock", "The socket every client dials."},
		{"$XDG_DATA_HOME/torrnado/session.json", "The torrent list and everything chosen about it, restored on start."},
		{"$XDG_DATA_HOME/torrnado/torrents/", "A copy of each torrent's metainfo, so a restart re-adds without finding a peer."},
		{"$XDG_DATA_HOME/torrnado/daemon.log", "Where a daemon spawned by a client writes."},
	} {
		fmt.Fprintf(b, ".TP\n.I %s\n%s\n", roff(f[0]), roff(f[1]))
	}

	section(b, "ENVIRONMENT")
	fmt.Fprintf(b, ".TP\n.B XDG_CONFIG_HOME\n%s\n",
		roff("Where the config and themes live. Honored on every platform, not only Linux; defaults to ~/.config."))
	fmt.Fprintf(b, ".TP\n.B XDG_DATA_HOME\n%s\n",
		roff("Where the socket, session and logs live; defaults to ~/.local/share."))

	section(b, "EXAMPLES")
	for _, ex := range [][2]string{
		{"torrnado", "Attach the interface, spawning a daemon if none is running."},
		{"torrnado add 'magnet:?xt=urn:btih:...'", "Add a torrent and exit; it keeps downloading."},
		{"torrnado list --watch", "Follow the daemon's own state changes in a shell."},
		{"torrnado priority <id> 0 none", "Stop wanting the first file of a torrent."},
		{"torrnado config", "Print the paths and settings in effect, without contacting the daemon."},
	} {
		fmt.Fprintf(b, ".TP\n.B %s\n%s\n", roff(ex[0]), roff(ex[1]))
	}

	section(b, "SEE ALSO")
	fmt.Fprintf(b, "%s\n", roff("Full documentation at https://torrnado.dev"))

	_, err := io.WriteString(w, b.String())
	return err
}

func section(w io.Writer, name string) { fmt.Fprintf(w, ".SH %s\n", name) }

// writeFlags renders a flag set as a definition list, reporting whether
// it had anything to render.
func writeFlags(w io.Writer, fs *pflag.FlagSet) bool {
	any := false
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		any = true
		// roff on the name too: a flag like --save-path carries a hyphen
		// of its own, and an unescaped one is a typographic dash that
		// will not paste back into a shell.
		names := `\fB` + roff("--"+f.Name) + `\fR`
		if f.Shorthand != "" {
			names = `\fB` + roff("-"+f.Shorthand) + `\fR, ` + names
		}
		// A bool flag takes no argument; everything else is worth naming
		// the shape of.
		if f.Value.Type() != "bool" {
			names += ` \fI` + f.Value.Type() + `\fR`
		}
		fmt.Fprintf(w, ".TP\n%s\n%s", names, roff(f.Usage))
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(w, " (default %s)", roff(f.DefValue))
		}
		fmt.Fprintln(w)
	})
	return any
}

// roff escapes text for a man page body.
//
// Backslashes start an escape and hyphens are typographic unless spelled
// out, so both are written the long way. A line beginning with "." or "'"
// is read as a request, which is how a description that happens to start
// with an ellipsis disappears from the page.
func roff(s string) string {
	s = strings.ReplaceAll(s, `\`, `\e`)
	s = strings.ReplaceAll(s, "-", `\-`)

	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "'") {
			lines[i] = `\&` + l
		}
	}
	return strings.Join(lines, "\n")
}
