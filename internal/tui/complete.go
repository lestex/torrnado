package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lestex/torrnado/internal/config"
)

// Tab completion for the palette's path arguments.
//
// The palette is not a shell and does not pretend to be one, but typing a
// path into it by hand is the one thing that is genuinely worse here than
// in the shell people came from: no completion, and a typo only shows up
// as a failed add. This closes that gap for the two commands that take a
// path, and nothing else - completing a torrent id or a theme name would
// be a different feature with different rules.

// pathArg is the argument the line ends on, which is the one Tab
// completes.
type pathArg struct {
	// start is where the argument begins in the line, counted in bytes
	// and including the opening quote when there is one.
	start int
	// text is the argument with its quotes removed, which is what has to
	// be matched against the filesystem.
	text string
	// quote is the character it was opened with, or 0. A completion has
	// to go back inside the same quotes it came out of.
	quote rune
}

// lastPathArg finds the argument the cursor is sitting on.
//
// Written against the raw line rather than splitArgs' output because
// completion has to put text back, and splitArgs throws away the
// positions and the quotes. It follows the same rules, so what Tab
// completes and what Enter would run cannot disagree.
func lastPathArg(line string) pathArg {
	arg := pathArg{start: len(line)}
	var quote rune
	inWord := false

	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			if !inWord {
				arg.start, inWord = i, true
			}
			quote = r
		case r == ' ' || r == '\t':
			if inWord {
				inWord = false
			}
			arg.start = i + 1
		default:
			if !inWord {
				arg.start, inWord = i, true
			}
		}
	}
	if !inWord && quote == 0 {
		arg.start = len(line)
	}

	raw := line[arg.start:]
	if len(raw) > 0 && (raw[0] == '\'' || raw[0] == '"') {
		arg.quote = rune(raw[0])
		raw = raw[1:]
		raw = strings.TrimSuffix(raw, string(arg.quote))
	}
	arg.text = raw
	return arg
}

// completesPaths reports whether a palette command's last argument names
// something on disk. Driven off the command table so a new path-taking
// command cannot quietly miss out.
func completesPaths(name string) bool {
	for _, c := range paletteCommands {
		if !c.completesPaths {
			continue
		}
		for _, n := range c.names {
			if n == name {
				return true
			}
		}
	}
	return false
}

// completePath returns the line with its last argument completed, and the
// candidates when more than one matched.
//
// The rules are a shell's, because that is what the fingers doing the
// typing expect: a single match completes and a directory gains a
// trailing separator, several extend to their common prefix, and none
// leaves the line alone.
func completePath(line string) (completed string, candidates []string) {
	fields := splitArgs(line)
	// The command itself is not a path, and neither is a line that has not
	// got as far as an argument.
	if len(fields) == 0 || !completesPaths(fields[0]) {
		return line, nil
	}
	arg := lastPathArg(line)
	if arg.start == 0 {
		return line, nil // still typing the command
	}
	// A magnet or a URL is not on the filesystem, and a stray Tab in the
	// middle of one should not mangle it.
	if isNotAPath(arg.text) {
		return line, nil
	}

	dir, prefix := splitForCompletion(arg.text)
	matches := matchesIn(dir, prefix)
	switch len(matches) {
	case 0:
		return line, nil
	case 1:
		return line[:arg.start] + quoteArg(joinCompletion(arg.text, prefix, matches[0]), arg.quote), nil
	}

	common := longestCommonPrefix(namesOf(matches))
	if len(common) > len(prefix) {
		line = line[:arg.start] + quoteArg(joinCompletion(arg.text, prefix, entry{name: common}), arg.quote)
	}
	return line, namesOf(matches)
}

// entry is one candidate: its name, and whether it is a directory, which
// decides the trailing separator.
type entry struct {
	name string
	dir  bool
}

func namesOf(es []entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.name)
	}
	return out
}

// isNotAPath reports the things that live in the same argument slot but
// are not on disk.
func isNotAPath(s string) bool {
	return strings.HasPrefix(s, "magnet:") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://")
}

// splitForCompletion divides what has been typed into the directory to
// list and the prefix to match inside it.
func splitForCompletion(text string) (dir, prefix string) {
	expanded := expandForListing(text)
	if strings.HasSuffix(text, string(filepath.Separator)) {
		return expanded, ""
	}
	return filepath.Dir(expanded), filepath.Base(expanded)
}

// expandForListing resolves what has been typed far enough to read the
// filesystem: a leading ~, and a bare relative path against the working
// directory.
//
// Failures fall back to the text as typed. Completion is a convenience,
// and one that cannot resolve a path simply has nothing to offer.
func expandForListing(text string) string {
	if text == "" {
		return "."
	}
	if expanded, err := config.ExpandHome(text); err == nil {
		return expanded
	}
	return text
}

func matchesIn(dir, prefix string) []entry {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []entry
	for _, it := range items {
		name := it.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		// A dotfile only shows up when it was asked for, the way a shell
		// does it: otherwise every completion in a home directory is
		// buried in configuration.
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		out = append(out, entry{name: name, dir: isDir(filepath.Join(dir, name))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// joinCompletion swaps the typed prefix for the matched name, leaving
// whatever directory part the user typed exactly as they typed it - so a
// path begun with ~ stays begun with ~ rather than jumping to an absolute
// one halfway through.
func joinCompletion(typed, prefix string, e entry) string {
	out := strings.TrimSuffix(typed, prefix) + e.name
	if e.dir {
		out += string(filepath.Separator)
	}
	return out
}

// quoteArg puts an argument back into the line so splitArgs will read it
// as one argument again.
//
// A completed name can contain a space - "Big Buck Bunny" is the normal
// case, not an exotic one - and an unquoted space is where the argument
// ends. Something that came out of quotes goes back into them.
func quoteArg(s string, quote rune) string {
	switch {
	case quote != 0:
		return string(quote) + s + string(quote)
	case strings.ContainsAny(s, " \t"):
		// Single quotes, because a path is far likelier to contain a
		// double quote than a single one.
		return "'" + s + "'"
	default:
		return s
	}
}

func longestCommonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
			if common == "" {
				return ""
			}
		}
	}
	return common
}

// candidateList renders several matches for the status bar.
//
// Truncated, because the status bar is one line and a directory of two
// hundred files would push everything else off it. The count is what
// tells someone the list is not all of it.
func candidateList(names []string) string {
	const most = 8
	if len(names) <= most {
		return strings.Join(names, "  ")
	}
	return strings.Join(names[:most], "  ") +
		"  … " + strconv.Itoa(len(names)-most) + " more"
}

// expandPathArgs resolves a leading ~ in each argument, so a path typed
// out by hand works the same as one Tab completed.
//
// The shell does this before a command ever sees its arguments, which
// makes the palette one of the few places it has to be done here - and
// leaving it out would mean Tab produced working paths while the same
// path typed in full quietly failed. Magnets and URLs pass through: they
// are not paths, and one beginning with ~ is not a thing.
func expandPathArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a
		if isNotAPath(a) {
			continue
		}
		if expanded, err := config.ExpandHome(a); err == nil {
			out[i] = expanded
		}
	}
	return out
}
