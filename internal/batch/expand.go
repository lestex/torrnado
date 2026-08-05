// Package batch turns command-line/command-palette add arguments --
// magnet URIs, .torrent files, directories of .torrent files, glob
// patterns, and text files listing one magnet URI per line -- into a
// flat, self-describing list of sources
// ready for ipc.Client.AddBatch: each entry is either a "magnet:" URI or
// a local path to a .torrent file.
package batch

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Expand resolves each argument into zero or more sources. Rules, applied
// per argument:
//   - starts with "magnet:"            -> the URI itself
//   - is a directory                   -> every "*.torrent" file directly inside it
//   - contains glob metacharacters     -> filepath.Glob it (covers shells/contexts
//     that don't expand globs themselves, e.g. quoted arguments or Windows)
//   - is a regular file named "*.torrent" -> the file itself
//   - is any other regular file        -> read as a text file of magnet URIs,
//     one per line, blank lines and lines starting with "#" ignored
//   - anything else                    -> an error
func Expand(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		srcs, err := expandOne(arg)
		if err != nil {
			return nil, err
		}
		out = append(out, srcs...)
	}
	return out, nil
}

func expandOne(arg string) ([]string, error) {
	if strings.HasPrefix(arg, "magnet:") {
		return []string{arg}, nil
	}

	if info, err := os.Stat(arg); err == nil {
		if info.IsDir() {
			return expandDir(arg)
		}
		if strings.EqualFold(filepath.Ext(arg), ".torrent") {
			return []string{arg}, nil
		}
		return expandMagnetListFile(arg)
	}

	if strings.ContainsAny(arg, "*?[") {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return nil, fmt.Errorf("bad glob pattern %q: %w", arg, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob %q matched no files", arg)
		}
		var out []string
		for _, m := range matches {
			srcs, err := expandOne(m)
			if err != nil {
				return nil, err
			}
			out = append(out, srcs...)
		}
		return out, nil
	}

	return nil, fmt.Errorf("%s: no such file or directory", arg)
}

func expandDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}
	var out []string
	for _, ent := range entries {
		if ent.IsDir() || !strings.EqualFold(filepath.Ext(ent.Name()), ".torrent") {
			continue
		}
		out = append(out, filepath.Join(dir, ent.Name()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no .torrent files found", dir)
	}
	return out, nil
}

func expandMagnetListFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "magnet:") {
			return nil, fmt.Errorf("%s: line %q is not a magnet uri", path, line)
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no magnet uris found", path)
	}
	return out, nil
}
