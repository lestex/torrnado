package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func defaultConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	return cfg
}

// The whole point of the generated file: it describes the defaults it was
// generated from. A template that decoded to anything else would hand
// someone a file that silently changes how their daemon behaves the
// moment they save it.
func TestTemplateRoundTripsToTheConfigItRendered(t *testing.T) {
	want := defaultConfig(t)

	var got Config
	meta, err := toml.Decode(string(Template(want)), &got)
	if err != nil {
		t.Fatalf("the generated config does not parse: %v", err)
	}
	// A key here that the decoder does not recognise is a key Load()
	// would reject outright - the generated file would not even load.
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		t.Errorf("the generated config has key(s) Config does not define: %v", undecoded)
	}

	// `interfaces = []` decodes to an empty slice where Default() leaves a
	// nil one, and an empty [keybinds] table to an empty map. Both are the
	// same "nothing configured" the config code treats them as, so neither
	// nilness is worth failing over.
	if len(got.VPN.Interfaces) == 0 && len(want.VPN.Interfaces) == 0 {
		got.VPN.Interfaces, want.VPN.Interfaces = nil, nil
	}
	if len(got.Keybinds) == 0 && len(want.Keybinds) == 0 {
		got.Keybinds, want.Keybinds = nil, nil
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the generated config does not describe the config it came from:\n got %+v\nwant %+v", got, want)
	}
}

// Load() is stricter than the decoder - it validates as well - and it is
// what actually reads this file in anger.
func TestTemplateLoadsFromDisk(t *testing.T) {
	want := defaultConfig(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, Template(want), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("the generated config does not load: %v", err)
	}
	if got.DownloadDir != want.DownloadDir || got.Port != want.Port || got.Network != want.Network {
		t.Errorf("loaded config differs from the one rendered:\n got %+v\nwant %+v", got, want)
	}
}

// A key added to Config with no line in the template would leave the
// generated file quietly incomplete - present, plausible, and missing a
// setting. Walking the tags is what makes that a test failure rather than
// something noticed a release later.
//
// Checked per table rather than per key name: "url" appears under both
// search sources, so merely finding one "url =" somewhere would pass a
// template that had forgotten the other.
func TestTemplateWritesEveryKey(t *testing.T) {
	out := string(Template(defaultConfig(t)))
	sections := tomlSections(out)

	leaves, tables := tomlKeys(reflect.TypeFor[Config](), "")

	// A map is a table of keys we cannot know in advance (keybinds), so
	// all that can be asserted is that its header is there.
	for _, table := range tables {
		if _, ok := sections[table]; !ok {
			t.Errorf("config table [%s] is missing from the generated file:\n%s", table, out)
		}
	}

	for _, key := range leaves {
		table, leaf := splitKey(key)
		body, ok := sections[table]
		if !ok {
			t.Errorf("config key %q needs table [%s], which the generated file has no header for:\n%s",
				key, table, out)
			continue
		}
		// Anchored on the assignment, so a key that appears only inside
		// a prose comment does not count as written. Keys are padded for
		// alignment, hence the run of spaces.
		assigned := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(leaf) + ` *=`)
		if !assigned.MatchString(body) {
			t.Errorf("config key %q is missing from the generated file:\n%s", key, out)
		}
	}
}

// tomlSections splits a rendered config by its [table] headers. The
// top-level keys, written before any header, are under "".
func tomlSections(out string) map[string]string {
	sections := map[string]string{}
	table := ""
	var b strings.Builder
	flush := func() { sections[table] += b.String(); b.Reset() }

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			flush()
			table = trimmed[1 : len(trimmed)-1]
			if _, ok := sections[table]; !ok {
				sections[table] = ""
			}
			continue
		}
		b.WriteString(line + "\n")
	}
	flush()
	return sections
}

// splitKey divides a dotted key into the table it belongs to and the name
// written inside it. A top-level key has no table.
func splitKey(key string) (table, leaf string) {
	i := strings.LastIndex(key, ".")
	if i < 0 {
		return "", key
	}
	return key[:i], key[i+1:]
}

// tomlKeys collects the dotted path of every leaf field of t, and
// separately of every field that is a table of its own.
//
// A nested struct is a [table] whose own path is not a key to be
// assigned; its leaves carry the full dotted path so they can be checked
// inside the right one. A map is a table whose keys are the user's to
// choose, so only its header can be asserted.
func tomlKeys(t reflect.Type, prefix string) (leaves, tables []string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("toml")
		if tag == "" || tag == "-" {
			continue
		}
		path := tag
		if prefix != "" {
			path = prefix + "." + tag
		}
		switch f.Type.Kind() {
		case reflect.Struct:
			l, tb := tomlKeys(f.Type, path)
			leaves = append(leaves, l...)
			tables = append(tables, tb...)
		case reflect.Map:
			tables = append(tables, path)
		default:
			leaves = append(leaves, path)
		}
	}
	return leaves, tables
}

// Rate.String() renders "2.0MiB/s", which is for people to read and does
// not parse back. Writing that into a config file would produce one that
// fails to load - and only for someone who had set a limit.
func TestTemplateWritesRatesInAFormThatParses(t *testing.T) {
	cfg := defaultConfig(t)
	cfg.RateLimit.Download = 2 * 1024 * 1024

	var got Config
	if _, err := toml.Decode(string(Template(cfg)), &got); err != nil {
		t.Fatalf("the generated config does not parse: %v", err)
	}
	if got.RateLimit.Download != cfg.RateLimit.Download {
		t.Errorf("rate_limit.download = %d, want %d", got.RateLimit.Download, cfg.RateLimit.Download)
	}
	if got.RateLimit.Upload != 0 {
		t.Errorf("an unlimited rate came back as %d, want 0", got.RateLimit.Upload)
	}
}

// Values are rendered from the Config passed in, not hard-coded, or the
// file would name paths this machine never resolved.
func TestTemplateRendersTheValuesItWasGiven(t *testing.T) {
	cfg := defaultConfig(t)
	cfg.DownloadDir = "/srv/torrent"
	cfg.VPN.Interfaces = []string{"tun0", "wg0"}
	cfg.Keybinds = map[string]string{"quit": "Q", "pause": "space"}

	out := string(Template(cfg))

	for _, want := range []string{`"/srv/torrent"`, `["tun0", "wg0"]`, `quit  = "Q"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the generated config does not contain %s:\n%s", want, out)
		}
	}
	// Sorted, so regenerating produces a file that diffs against the last
	// one rather than reshuffling.
	if strings.Index(out, "pause") > strings.Index(out, "quit") {
		t.Errorf("keybinds are not in sorted order:\n%s", out)
	}
}
