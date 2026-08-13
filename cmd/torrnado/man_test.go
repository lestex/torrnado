package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func generatedMan(t *testing.T) string {
	t.Helper()

	root := &cobra.Command{Use: "torrnado", Short: "A terminal BitTorrent client", Long: "Long description."}
	root.PersistentFlags().String("config", "", "path to config.toml")

	sub := &cobra.Command{Use: "add <source>...", Short: "Add torrents", Run: func(*cobra.Command, []string) {}}
	sub.Flags().Bool("paused", false, "add the torrents but leave them paused")
	sub.Flags().String("save-path", "", "download into this directory instead")
	root.AddCommand(sub)

	var b strings.Builder
	if err := writeManPage(&b, root, "1.2.3", time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("writeManPage: %v", err)
	}
	return b.String()
}

// The page is only useful if man can find its way around it, and the
// header is what `man torrnado` matches on.
func TestManPageHasTheSectionsAManExpects(t *testing.T) {
	page := generatedMan(t)

	if !strings.HasPrefix(page, `.TH TORRNADO 1 "2026-08-13" "torrnado 1.2.3" "User Commands"`) {
		t.Errorf("header line is wrong:\n%s", strings.SplitN(page, "\n", 2)[0])
	}
	for _, s := range []string{
		".SH NAME", ".SH SYNOPSIS", ".SH DESCRIPTION", ".SH OPTIONS",
		".SH COMMANDS", ".SH FILES", ".SH ENVIRONMENT", ".SH EXAMPLES", ".SH SEE ALSO",
	} {
		if !strings.Contains(page, s+"\n") {
			t.Errorf("missing section %s", s)
		}
	}
}

// Generated from the command tree, so a subcommand and its flags appear
// without anyone remembering to write them down.
func TestManPageDescribesTheCommandTree(t *testing.T) {
	page := generatedMan(t)

	for _, want := range []string{"torrnado add", `\-\-paused`, `\-\-save\-path`, `\-\-config`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}
	// A flag that takes a value says so; a bool must not.
	if !strings.Contains(page, `\-\-save\-path\fR \fIstring\fR`) {
		t.Error("a value-taking flag does not name its argument")
	}
	if strings.Contains(page, `\-\-paused\fR \fI`) {
		t.Error("a bool flag was given an argument")
	}
}

// Every hyphen has to be escaped or it renders as a typographic dash,
// and a flag copied out of the page then fails to parse.
func TestManPageEscapesRoffSyntax(t *testing.T) {
	if got := roff("--save-path"); got != `\-\-save\-path` {
		t.Errorf("roff(--save-path) = %q", got)
	}
	if got := roff(`a\b`); got != `a\eb` {
		t.Errorf("backslash was not escaped: %q", got)
	}
	// A line starting with . is a request, so a description opening with
	// one would vanish from the rendered page.
	if got := roff(".torrent files"); !strings.HasPrefix(got, `\&.`) {
		t.Errorf("a leading dot was not neutralised: %q", got)
	}
}
