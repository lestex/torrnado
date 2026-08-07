package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Default()
	if err != nil {
		t.Fatalf("config.Default: %v", err)
	}
	return cfg
}

func report(t *testing.T, cfg config.Config, path string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := writeConfigReport(&buf, cfg, path); err != nil {
		t.Fatalf("writeConfigReport: %v", err)
	}
	return buf.String()
}

// The point of the command: every path a user might go looking for, in
// one place, without having to know how XDG resolves on this machine.
func TestReportPrintsEveryPath(t *testing.T) {
	cfg := testConfig(t)
	cfg.DownloadDir = "/srv/torrent"
	cfg.StateDir = "/var/lib/torrnado"
	cfg.DaemonSocket = "/var/lib/torrnado/daemon.sock"

	out := report(t, cfg, "/etc/torrnado/config.toml")

	for _, want := range []string{
		"/etc/torrnado/config.toml",
		"/srv/torrent",
		"/var/lib/torrnado",
		"/var/lib/torrnado/daemon.sock",
		// Derived from state_dir rather than configured, so this is
		// where someone would otherwise have to guess.
		"/var/lib/torrnado/session.json",
		"/var/lib/torrnado/torrents",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not mention %q:\n%s", want, out)
		}
	}
}

// A missing file is not an error -- torrnado runs on defaults -- but it
// is the first thing to know when a setting is not taking effect.
func TestReportSaysWhenThereIsNoConfigFile(t *testing.T) {
	out := report(t, testConfig(t), "/nonexistent/torrnado/config.toml")

	if !strings.Contains(out, "not found") {
		t.Errorf("a missing config file is not called out:\n%s", out)
	}
}

func TestReportDoesNotClaimAMissingFileWhenOneExists(t *testing.T) {
	// Any file that certainly exists; the report only stats the path.
	out := report(t, testConfig(t), t.TempDir())

	if strings.Contains(out, "not found") {
		t.Errorf("an existing config file was reported missing:\n%s", out)
	}
}

// A rate of zero means unlimited, and printing a bare "0" would read as
// "stopped".
func TestReportPrintsRatesAsRates(t *testing.T) {
	cfg := testConfig(t)
	cfg.RateLimit.Upload = 0
	cfg.RateLimit.Download = 2 * 1024 * 1024

	out := report(t, cfg, "/tmp/config.toml")

	if !strings.Contains(out, "unlimited") {
		t.Errorf("an unset limit should read as unlimited:\n%s", out)
	}
	if !strings.Contains(out, "2.0MiB/s") {
		t.Errorf("a set limit should read as a rate:\n%s", out)
	}
}

func TestPortRangeText(t *testing.T) {
	cases := []struct {
		in   config.PortRange
		want string
	}{
		{config.PortRange{Low: 51413, High: 51433}, "51413-51433"},
		// One port, however it was written.
		{config.PortRange{Low: 51413, High: 51413}, "51413"},
		{config.PortRange{Low: 51413}, "51413"},
		// Zero is the documented "let the OS choose", and printing it as
		// "0-0" would look like a broken setting.
		{config.PortRange{}, "any (chosen by the OS)"},
	}
	for _, c := range cases {
		if got := portRangeText(c.in); got != c.want {
			t.Errorf("portRangeText(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Keybinds come out of a map, and a listing that reshuffles itself
// between runs is hard to read and impossible to diff.
func TestReportSortsKeybinds(t *testing.T) {
	cfg := testConfig(t)
	cfg.Keybinds = map[string]string{"quit": "Q", "pause": "space", "help": "?"}

	out := report(t, cfg, "/tmp/config.toml")

	help := strings.Index(out, "help")
	pause := strings.Index(out, "pause")
	quit := strings.Index(out, "quit")
	if !(help < pause && pause < quit) {
		t.Errorf("keybinds are not in sorted order:\n%s", out)
	}
}

func TestReportSaysNoKeybindsRatherThanNothing(t *testing.T) {
	cfg := testConfig(t)
	cfg.Keybinds = nil

	if out := report(t, cfg, "/tmp/config.toml"); !strings.Contains(out, "none") {
		t.Errorf("an empty keybind list should say so:\n%s", out)
	}
}

// log.file being empty means stderr, which is a real answer and not a
// missing one.
func TestReportNamesStderrForAnEmptyLogFile(t *testing.T) {
	cfg := testConfig(t)
	cfg.Log.File = ""

	if out := report(t, cfg, "/tmp/config.toml"); !strings.Contains(out, "stderr") {
		t.Errorf("an unset log file should read as stderr:\n%s", out)
	}
}
