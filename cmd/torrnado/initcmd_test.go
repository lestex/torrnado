package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lestex/torrnado/internal/config"
)

func TestInitWritesALoadableConfig(t *testing.T) {
	// A directory that does not exist yet: on a machine that has never
	// been configured, ~/.config/torrnado is exactly that.
	path := filepath.Join(t.TempDir(), "torrnado", "config.toml")
	cfg := testConfig(t)

	var out bytes.Buffer
	if err := runInit(&out, path, cfg, false, false); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("the file it wrote does not load: %v", err)
	}
	if got.DownloadDir != cfg.DownloadDir || got.Theme != cfg.Theme {
		t.Errorf("loaded config differs from the defaults written:\n got %+v\nwant %+v", got, cfg)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("the command does not say where it wrote:\n%s", out.String())
	}
}

// The one thing this command must never do. Someone runs it a second time
// on a machine they configured months ago, and the config they are
// running on is the thing at stake.
func TestInitRefusesToOverwriteAnExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	const mine = "theme = \"nord\"\n"
	if err := os.WriteFile(path, []byte(mine), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runInit(&bytes.Buffer{}, path, testConfig(t), false, false)
	if err == nil {
		t.Fatal("an existing config file was overwritten without --force")
	}
	// The error has to name the way out, or it is just a refusal.
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("the error does not mention --force: %v", err)
	}

	if data, _ := os.ReadFile(path); string(data) != mine {
		t.Errorf("the existing file was modified: %q", data)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("theme = \"nord\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := runInit(&bytes.Buffer{}, path, testConfig(t), true, false); err != nil {
		t.Fatalf("runInit --force: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("the file it wrote does not load: %v", err)
	}
	if got.Theme != testConfig(t).Theme {
		t.Errorf("theme = %q, want the default - the old file survived", got.Theme)
	}
}

// --print is for looking, and looking should not leave anything behind -
// including on a machine that already has a config file.
func TestInitPrintWritesNothingToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	var out bytes.Buffer
	if err := runInit(&out, path, testConfig(t), false, true); err != nil {
		t.Fatalf("runInit --print: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("--print created %s", path)
	}
	if !strings.Contains(out.String(), "[network]") {
		t.Errorf("--print did not print the config:\n%s", out.String())
	}
}
