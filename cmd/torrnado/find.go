package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/config"
	"github.com/lestex/torrnado/internal/format"
	"github.com/lestex/torrnado/internal/indexer"
)

func newFindCmd() *cobra.Command {
	var (
		limit  int
		asJSON bool
		check  bool
		link   int
	)

	cmd := &cobra.Command{
		Use:   "find <query>...",
		Short: "Search a configured indexer for torrents",
		Long: `Search a self-hosted Jackett or Prowlarr server, which fans the query
out to every tracker configured in it, and print what came back.

Configure it under [indexer] in config.toml; "torrnado find --check"
reports whether the server answers and accepts the key.

This needs no daemon: it talks to the indexer, not to torrnado. Adding a
result is a separate step, which keeps find usable in a pipeline:

  torrnado add "$(torrnado find ubuntu --link 1)"
  torrnado find ubuntu --json | jq -r '.[0].link' | xargs torrnado add

Arguments are joined with spaces, so a multi-word query needs no quoting.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfig()
			if err != nil {
				return err
			}
			searcher, err := newSearcher(cfg)
			if err != nil {
				return err
			}

			// Ctrl-C ends a slow search promptly instead of after every
			// attempt has timed out.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if check {
				if err := searcher.Health(ctx); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s is reachable and accepted the API key\n", searcher.Name())
				return nil
			}

			query := strings.Join(args, " ")
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("find: needs a query (or --check)")
			}

			results, err := searcher.Search(ctx, query)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			switch {
			case link > 0:
				return writeFindLink(out, results, link)
			case asJSON:
				return writeFindJSON(out, results, limit)
			default:
				return writeFindTable(out, results, limit)
			}
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 25, "show at most this many results (0 for all)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print results as JSON")
	cmd.Flags().BoolVar(&check, "check", false, "report whether the indexer answers, and run no search")
	cmd.Flags().IntVar(&link, "link", 0, "print only the Nth result's magnet or URL, for piping into add")
	return cmd
}

// newSearcher builds an indexer client from the config, or explains what
// is missing.
//
// Shared with the TUI's wiring so "not configured" reads the same in both
// places. config.Validate has already rejected a half-filled table, so
// the only case to describe here is an empty one.
func newSearcher(cfg config.Config) (indexer.Searcher, error) {
	if cfg.Indexer.URL == "" {
		return nil, fmt.Errorf("no indexer configured: set indexer.backend (%s), indexer.url and "+
			"indexer.api_key in config.toml - see `torrnado config` for where that is",
			strings.Join(config.IndexerBackends, " or "))
	}
	return indexer.New(indexer.Options{
		Backend: cfg.Indexer.Backend,
		URL:     cfg.Indexer.URL,
		APIKey:  cfg.Indexer.APIKey,
	})
}

// capResults applies --limit. Results arrive seeders-first, so cutting
// the tail keeps the useful end.
func capResults(results []indexer.Result, limit int) []indexer.Result {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

// writeFindTable prints the human-readable table.
//
// There is no LINK column on purpose: a magnet runs past 200 characters
// and would push every other column off the screen. --link and --json are
// how a link gets out of here.
func writeFindTable(out io.Writer, results []indexer.Result, limit int) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(out, "no results")
		return err
	}

	shown := capResults(results, limit)
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "#\tTITLE\tSIZE\tSEEDS\tPEERS\tTRACKER")
	for i, r := range shown {
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		size := "-"
		if r.Size > 0 {
			size = format.Bytes(r.Size)
		}
		marker := ""
		if r.Link == "" {
			// Listed, because the count should match what the indexer
			// said it found, but flagged: there is nothing to add.
			marker = " (no link)"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s%s\n",
			i+1, title, size, format.Count(r.Seeders), format.Count(r.Leechers), r.Tracker, marker)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if len(shown) < len(results) {
		_, err := fmt.Fprintf(out, "\n%d of %d shown; --limit 0 for all\n", len(shown), len(results))
		return err
	}
	return nil
}

func writeFindJSON(out io.Writer, results []indexer.Result, limit int) error {
	shown := capResults(results, limit)
	if shown == nil {
		shown = []indexer.Result{} // "[]" rather than "null", for jq
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(shown)
}

// writeFindLink prints one result's link and nothing else, so it can be
// substituted straight into `torrnado add`.
//
// Numbered from 1, matching the table's own # column rather than a Go
// index, since the number comes off the screen.
func writeFindLink(out io.Writer, results []indexer.Result, n int) error {
	if n > len(results) {
		return fmt.Errorf("find: --link %d but only %d results came back", n, len(results))
	}
	r := results[n-1]
	if r.Link == "" {
		return fmt.Errorf("find: result %d has no magnet or download URL", n)
	}
	_, err := fmt.Fprintln(out, r.Link)
	return err
}
