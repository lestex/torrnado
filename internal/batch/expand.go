// Package batch turns command-line/command-palette add arguments -
// magnet URIs, .torrent files, directories of .torrent files, glob
// patterns, and text files listing one magnet URI per line - into a
// flat, self-describing list of sources
// ready for ipc.Client.AddBatch: each entry is either a "magnet:" URI or
// a local path to a .torrent file.
package batch

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lestex/torrnado/internal/indexer"
)

// httpFetchTimeout bounds how long Expand waits for a .torrent file to
// download from an http(s) URL before giving up.
const httpFetchTimeout = 30 * time.Second

// maxRedirects bounds the redirect chain. net/http enforces its own limit
// of ten *inside* the default CheckRedirect, so supplying one replaces
// that limit rather than adding to it - without this, a link that
// redirects to itself would spin until httpFetchTimeout.
const maxRedirects = 10

// Expand resolves each argument into zero or more sources. Rules, applied
// per argument:
//   - starts with "magnet:"            -> the URI itself
//   - starts with "http://"/"https://" -> download it and use the local
//     copy, or, if it redirects to a magnet, that magnet (checked before
//     the glob rule below, since URLs routinely contain "?" and would
//     otherwise be misread as a glob pattern)
//   - is a directory                   -> every "*.torrent" file directly inside it
//   - contains glob metacharacters     -> filepath.Glob it (covers shells/contexts
//     that don't expand globs themselves, e.g. quoted arguments or Windows)
//   - is a regular file named "*.torrent" -> the file itself
//   - is any other regular file        -> read as a text file of magnet URIs,
//     one per line, blank lines and lines starting with "#" ignored
//   - anything else                    -> an error
//
// The returned cleanup function removes the temp files any downloads
// left behind, and must be called once the daemon has read them - which
// is safe as soon as AddBatch returns, because the daemon keeps its own
// copy of the metainfo. It is never nil, so a caller can defer it
// without checking the error first.
func Expand(args []string) (sources []string, cleanup func(), err error) {
	ex := &expander{}
	for _, arg := range args {
		srcs, err := ex.one(arg)
		if err != nil {
			ex.cleanup()
			return nil, func() {}, err
		}
		sources = append(sources, srcs...)
	}
	return sources, ex.cleanup, nil
}

// expander accumulates the temp files one Expand call downloaded, so that
// a caller who cannot tell which sources were downloaded and which the
// user named can still clean up after itself.
type expander struct {
	tmp []string
}

// cleanup removes the downloaded files. A failed unlink is ignored: there
// is no recovery from it, and a complaint about a temp file would bury
// the report of whatever the user was actually doing.
func (ex *expander) cleanup() {
	for _, path := range ex.tmp {
		os.Remove(path)
	}
	ex.tmp = nil
}

func (ex *expander) one(arg string) ([]string, error) {
	if strings.HasPrefix(arg, "magnet:") {
		return []string{arg}, nil
	}

	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		src, err := ex.fetch(arg)
		if err != nil {
			return nil, err
		}
		return []string{src}, nil
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

	// A source token - "rutracker:6916386" - goes through untouched for
	// the daemon to resolve. It cannot be resolved here: the page it
	// names needs a login and a browser, both of which live in the
	// daemon, and only the daemon knows which sources are configured.
	//
	// After the stat above, so a file that genuinely is called
	// "name:thing" is still read as a file, and before the glob below,
	// which a token would otherwise fall past into "no such file".
	if source, id, ok := indexer.ParseToken(arg); ok && source != "" && id != "" {
		return []string{arg}, nil
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
			srcs, err := ex.one(m)
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

// fetch resolves an http(s) argument into a source: a local copy of the
// .torrent the URL serves, or - when it redirects to one - the magnet URI
// it points at.
//
// That redirect is the ordinary case for anything found through `torrnado
// find`: an indexer's download link for a release with no .torrent of its
// own is a 302 to the magnet. net/http would follow it and fail with
// `unsupported protocol scheme "magnet"`, which names the scheme as
// though the link were malformed when in fact it worked, so CheckRedirect
// stops there and keeps the target.
//
// A downloaded file is not validated beyond the HTTP status - an
// invalid/non-torrent response surfaces as a clear bencode-parse error
// from the daemon's AddTorrentFile instead.
func (ex *expander) fetch(url string) (string, error) {
	var magnet string
	client := &http.Client{
		Timeout: httpFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme == "magnet" {
				magnet = req.URL.String()
				return http.ErrUseLastResponse
			}
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Before the status check, because the status here is the redirect's
	// own 302 and the redirect is the success case.
	if magnet != "" {
		return magnet, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: server returned %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp("", "torrnado-*.torrent")
	if err != nil {
		return "", fmt.Errorf("create temp file for %s: %w", url, err)
	}
	defer tmp.Close()
	// Recorded before the copy, so a half-written file is cleaned up too.
	ex.tmp = append(ex.tmp, tmp.Name())

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", fmt.Errorf("save %s: %w", url, err)
	}
	return tmp.Name(), nil
}
