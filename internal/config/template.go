package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Template renders c as an annotated config.toml - the file `torrnado
// init` writes.
//
// It lives here, beside the struct it mirrors, because a template kept
// anywhere else drifts: a key added to Config with no line here would
// leave the generated file quietly incomplete. The tests walk Config's
// toml tags and fail on any key this does not write, which is what turns
// that from a promise into a check.
//
// Every value is written from c rather than hard-coded, so the file names
// the paths this machine actually resolved (XDG differs per platform, and
// so does the default opener) instead of the ones the documentation
// happens to use.
func Template(c Config) []byte {
	b := &strings.Builder{}
	b.WriteString(templateHeader)

	writeRows(b, []row{
		{"download_dir", tomlString(c.DownloadDir), "where torrents are saved"},
		{"daemon_socket", tomlString(c.DaemonSocket), "the socket every client dials"},
		{"state_dir", tomlString(c.StateDir), "session file + saved metainfo"},
		{"theme", tomlString(c.Theme), "a built-in, or a .toml in <config>/torrnado/themes"},
		{"player", tomlString(c.Player), "preview command; %f places the URL"},
		{"opener", tomlString(c.Opener), "folder command; %f places the path"},
	})

	b.WriteString(templateRateLimit)
	writeRows(b, []row{
		{"upload", tomlString(rateLiteral(c.RateLimit.Upload)), ""},
		{"download", tomlString(rateLiteral(c.RateLimit.Download)), ""},
	})

	b.WriteString(templatePort)
	writeRows(b, []row{
		{"low", strconv.Itoa(c.Port.Low), ""},
		{"high", strconv.Itoa(c.Port.High), ""},
	})

	b.WriteString(templateNetwork)
	writeRows(b, []row{
		{"dht", tomlBool(c.Network.DHT), ""},
		{"pex", tomlBool(c.Network.PEX), ""},
		{"encryption", tomlBool(c.Network.Encryption), ""},
		{"seed", tomlBool(c.Network.Seed), "keep uploading after a torrent completes"},
	})

	b.WriteString(templateVPN)
	writeRows(b, []row{
		{"required", tomlBool(c.VPN.Required), ""},
		{"interfaces", tomlStrings(c.VPN.Interfaces), "extra devices to count as a VPN"},
	})

	b.WriteString(templateLog)
	writeRows(b, []row{
		{"level", tomlString(c.Log.Level), strings.Join(LogLevels, ", ")},
		{"library_level", tomlString(c.Log.LibraryLevel), "the torrent library, filtered separately"},
		{"file", tomlString(c.Log.File), "empty = stderr, which is what a service manager wants"},
	})

	b.WriteString(templateKeybinds)
	writeKeybinds(b, c.Keybinds)

	return []byte(b.String())
}

const templateHeader = `# torrnado configuration.
#
# Written by ` + "`torrnado init`" + ` from the built-in defaults: this file says
# what torrnado would already do without it. Every key is optional, so
# deleting a line restores that default rather than losing the setting -
# which also means a value left here stays put when the built-in default
# changes in a later release.
#
# An unknown key or a bad value is an error naming the key, not something
# quietly ignored. ` + "`torrnado config`" + ` prints what is in effect.

`

const templateRateLimit = `
[rate_limit]
# Client-wide, not per torrent. A bare byte count, "500k", "2M", "1.5G",
# or "unlimited". Per-torrent limits are set at runtime with
# ` + "`torrnado limit --torrent <id>`" + `.
`

const templatePort = `
[port]
# Tried in order until one binds; 0 for both lets the OS choose. A fixed
# range is what you forward at the router to get incoming connections.
`

const templateNetwork = `
[network]
`

const templateVPN = `
[vpn]
# required = true holds every transfer - download and upload - while the
# system is not on a VPN, and releases them when it comes back. Tracker
# and DHT announces still go out, so this is "nothing transfers off-VPN",
# not a network kill switch.
#
# interfaces names devices to count as a VPN whatever the system says
# about them - the escape hatch for a tunnel the kernel cannot label, such
# as policy-based IPsec. Detection needs no configuration otherwise.
`

const templateLog = `
[log]
`

const templateKeybinds = `
[keybinds]
# Rebind any TUI action: action = "key". Press h in the interface for the
# defaults. An action that does not exist is an error, so a typo cannot
# leave a key silently doing the old thing.
`

// row is one `key = value  # comment` line, held before rendering so a
// block can be aligned on its widest key and value.
type row struct{ key, value, comment string }

func writeRows(b *strings.Builder, rows []row) {
	keyW, valueW := 0, 0
	for _, r := range rows {
		if len(r.key) > keyW {
			keyW = len(r.key)
		}
		// Only commented rows need their value padded; a row without one
		// would otherwise carry trailing spaces to nowhere.
		if r.comment != "" && len(r.value) > valueW {
			valueW = len(r.value)
		}
	}
	for _, r := range rows {
		if r.comment == "" {
			fmt.Fprintf(b, "%-*s = %s\n", keyW, r.key, r.value)
			continue
		}
		fmt.Fprintf(b, "%-*s = %-*s  # %s\n", keyW, r.key, valueW, r.value, r.comment)
	}
}

// writeKeybinds renders the [keybinds] table, which is empty by default.
//
// The known actions are listed as a comment either way: an empty table
// tells you nothing about what could go in it, and the alternative is
// reading the source or the docs to find out.
func writeKeybinds(b *strings.Builder, binds map[string]string) {
	actions := make([]string, 0, len(binds))
	for action := range binds {
		actions = append(actions, action)
	}
	// Ranging a map gives a different order every run, and a generated
	// file that reshuffles itself cannot be diffed against the last one.
	sort.Strings(actions)

	rows := make([]row, 0, len(actions))
	for _, action := range actions {
		rows = append(rows, row{action, tomlString(binds[action]), ""})
	}
	writeRows(b, rows)

	b.WriteString("#\n# Actions:\n")
	for _, line := range wrapList(KnownActions, 64) {
		fmt.Fprintf(b, "#   %s\n", line)
	}
}

// wrapList packs items onto comment lines no wider than width, so the
// action list reads as a paragraph rather than one long line that wraps
// wherever the terminal happens to end.
func wrapList(items []string, width int) []string {
	var lines []string
	cur := ""
	for _, item := range items {
		switch {
		case cur == "":
			cur = item
		case len(cur)+2+len(item) > width:
			lines = append(lines, cur+",")
			cur = item
		default:
			cur += ", " + item
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// rateLiteral writes a Rate the way the config parser reads it back.
//
// Rate.String() is for people ("2.0MiB/s") and does not parse; this is
// the round-trippable form, which is what a generated file needs.
func rateLiteral(r Rate) string {
	if r <= 0 {
		return "unlimited"
	}
	return strconv.FormatInt(int64(r), 10)
}

// tomlString quotes a value as a TOML basic string. Go's quoting escapes
// the same characters TOML does for anything that can appear in a path.
func tomlString(s string) string { return strconv.Quote(s) }

func tomlBool(v bool) string { return strconv.FormatBool(v) }

func tomlStrings(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = tomlString(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
