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
