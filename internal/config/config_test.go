package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig puts a config file in a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// A missing config file is not a failure -- it is the normal case for
// somebody who has never written one.
func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DownloadDir == "" || cfg.DaemonSocket == "" {
		t.Errorf("defaults not filled in: %+v", cfg)
	}
}

// Anything the file does not mention keeps its default, so a config with
// one line in it is a valid config.
func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	defaults, _ := Default()
	cfg, err := Load(writeConfig(t, `download_dir = "/tmp/somewhere"`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DownloadDir != "/tmp/somewhere" {
		t.Errorf("DownloadDir = %q", cfg.DownloadDir)
	}
	if cfg.DaemonSocket != defaults.DaemonSocket {
		t.Errorf("DaemonSocket should have kept its default, got %q", cfg.DaemonSocket)
	}
}

func TestLoadParsesRates(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[rate_limit]
upload = "500k"
download = "unlimited"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RateLimit.Upload != 500<<10 {
		t.Errorf("upload = %d, want %d", cfg.RateLimit.Upload, 500<<10)
	}
	if cfg.RateLimit.Download != 0 {
		t.Errorf("download = %d, want 0 for unlimited", cfg.RateLimit.Download)
	}
}

// A typo must be reported, not ignored. Silently dropping an unknown key
// means a setting a user believes they changed does nothing.
func TestLoadRejectsUnknownKeys(t *testing.T) {
	_, err := Load(writeConfig(t, `downlaod_dir = "/tmp/x"`))
	if err == nil {
		t.Fatal("an unknown key should be an error")
	}
	if !strings.Contains(err.Error(), "downlaod_dir") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

// Every validation failure has to name the key at fault, or the user is
// left hunting through the file.
func TestValidateNamesTheOffendingKey(t *testing.T) {
	cases := []struct {
		body    string
		wantKey string
	}{
		{`download_dir = ""`, "download_dir"},
		{`daemon_socket = ""`, "daemon_socket"},
		{"[port]\nlow = 0\nhigh = 70000", "port.low"}, // 0 is only valid paired with 0
		{"[port]\nlow = 6000\nhigh = 70000", "port.high"},
		{"[port]\nlow = 6000\nhigh = 5000", "port.high"},
		{"[rate_limit]\nupload = \"nonsense\"", "rate"},
	}
	for _, c := range cases {
		_, err := Load(writeConfig(t, c.body))
		if err == nil {
			t.Errorf("%q should have failed", c.body)
			continue
		}
		if !strings.Contains(err.Error(), c.wantKey) {
			t.Errorf("%q: error %q should mention %q", c.body, err, c.wantKey)
		}
	}
}

// A port range of 0/0 means "let the OS choose" and must stay legal.
func TestValidateAllowsZeroPortRange(t *testing.T) {
	if _, err := Load(writeConfig(t, "[port]\nlow = 0\nhigh = 0")); err != nil {
		t.Errorf("0/0 should be allowed: %v", err)
	}
}

func TestLoadLoggingSettings(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[log]
level = "debug"
library_level = "error"
file = "/var/log/torrnado.log"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Log.Level != "debug" || cfg.Log.LibraryLevel != "error" {
		t.Errorf("levels = %+v", cfg.Log)
	}
	if cfg.Log.File != "/var/log/torrnado.log" {
		t.Errorf("file = %q", cfg.Log.File)
	}
}

// Defaults have to be usable unconfigured: stderr, and the library a level
// quieter than us because it reports every misbehaving tracker.
func TestLoggingDefaults(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("default level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.LibraryLevel != "warn" {
		t.Errorf("default library level = %q, want warn", cfg.Log.LibraryLevel)
	}
	if cfg.Log.File != "" {
		t.Errorf("default log file = %q, want empty (stderr)", cfg.Log.File)
	}
	if cfg.StateDir == "" {
		t.Error("state_dir has no default")
	}
}

// A misspelled level is a typo worth reporting: silently falling back
// leaves someone convinced they enabled debug logging.
func TestValidateRejectsUnknownLogLevel(t *testing.T) {
	for _, body := range []string{
		"[log]\nlevel = \"verbose\"",
		"[log]\nlibrary_level = \"trace\"",
	} {
		_, err := Load(writeConfig(t, body))
		if err == nil {
			t.Errorf("%q should have failed", body)
			continue
		}
		if !strings.Contains(err.Error(), "log.") {
			t.Errorf("error should name the key, got: %v", err)
		}
	}
}

// The VPN guard is opt-in: installing torrnado must never stop a download
// for a reason the user did not ask for.
func TestVPNGuardIsOffByDefault(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if cfg.VPN.Required {
		t.Error("vpn.required defaults to true; it must be opt-in")
	}
	if len(cfg.VPN.Interfaces) != 0 {
		t.Errorf("vpn.interfaces defaults to %v, want empty -- detection needs no configuration",
			cfg.VPN.Interfaces)
	}
}

func TestLoadParsesTheVPNGuard(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[vpn]
required = true
interfaces = ["utun4", "wg0"]
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.VPN.Required {
		t.Error("vpn.required = false after being set to true")
	}
	if got := strings.Join(cfg.VPN.Interfaces, ","); got != "utun4,wg0" {
		t.Errorf("vpn.interfaces = %q, want \"utun4,wg0\"", got)
	}
}

// An empty name matches no interface, so accepting one would quietly
// weaken a guard the user believes they configured.
func TestValidateRejectsAnEmptyVPNInterface(t *testing.T) {
	_, err := Load(writeConfig(t, "[vpn]\nrequired = true\ninterfaces = [\"utun4\", \"\"]"))
	if err == nil {
		t.Fatal("an empty interface name should be an error")
	}
	if !strings.Contains(err.Error(), "vpn.interfaces") {
		t.Errorf("error should name the key, got: %v", err)
	}
}
