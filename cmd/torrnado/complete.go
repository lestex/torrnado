package main

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lestex/torrnado/internal/engine"
	"github.com/lestex/torrnado/internal/ipc"
)

// completionWait bounds how long a TAB press may wait on the daemon.
//
// Every other call has a thirty second timeout, which is right for a
// command someone is waiting on and ruinous for a completion: a daemon
// that has hung would freeze the shell prompt for half a minute. Past this
// there are simply no suggestions, which is a far smaller failure.
const completionWait = 1500 * time.Millisecond

// completeTorrents completes a torrent id at argument positions from min
// to max inclusive (max < 0 for no upper bound), so a command can say
// which of its arguments are torrents - `priority` takes one, first;
// `pause` takes any number.
//
// Full ids are offered, each described by its name, which is what a shell
// shows beside it. Typing the front of an id and pressing TAB fills in the
// rest; the short forms still work uncompleted (see resolveID), this is
// for when a hash is wanted in full.
func completeTorrents(min, max int) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if n := len(args); n < min || (max >= 0 && n > max) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completionCandidates(listForCompletion(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeMoveArgs: a torrent, then a directory to put it in.
func completeMoveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 1 {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	return completeTorrents(0, 0)(cmd, args, toComplete)
}

// completeLabelArgs: `label <name> <torrent>...`, or with --clear just the
// torrents. The name is free text, so nothing is offered for it.
func completeLabelArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	first := 1
	if clear, _ := cmd.Flags().GetBool("clear"); clear {
		first = 0
	}
	return completeTorrents(first, -1)(cmd, args, toComplete)
}

// listForCompletion asks a running daemon for its torrents, and never
// starts one: pressing TAB must not be what makes a daemon run, any more
// than `status` may be. No daemon, no answer in time, or any error all
// mean the same thing here - no suggestions.
func listForCompletion() []engine.TorrentSnapshot {
	cfg, _, err := loadConfig()
	if err != nil {
		return nil
	}
	c, err := ipc.Dial(cfg.DaemonSocket)
	if err != nil {
		return nil
	}
	defer c.Close()

	type answer struct {
		snaps []engine.TorrentSnapshot
		err   error
	}
	ch := make(chan answer, 1)
	go func() {
		s, err := c.List()
		ch <- answer{s, err}
	}()
	select {
	case a := <-ch:
		if a.err != nil {
			return nil
		}
		return a.snaps
	case <-time.After(completionWait):
		return nil
	}
}

// completionCandidates turns a listing into suggestions: "id<TAB>name",
// which cobra hands to the shell as a value and its description.
//
// Only ids that start with what is typed so far, and none already given -
// completing `pause <id> ` should not offer that same torrent again.
func completionCandidates(snaps []engine.TorrentSnapshot, args []string, toComplete string) []string {
	given := make(map[string]bool, len(args))
	for _, a := range args {
		given[strings.ToLower(a)] = true
	}
	prefix := strings.ToLower(toComplete)

	var out []string
	for _, s := range snaps {
		id := string(s.ID)
		if given[id] || !strings.HasPrefix(id, prefix) {
			continue
		}
		out = append(out, id+"\t"+describe(s.Name))
	}
	return out
}

// describe makes a name safe to sit after a tab on one line, which a
// shell's completion protocol needs, and short enough to be read there.
func describe(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, name)
	if r := []rune(name); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	if name == "" {
		return "(no name yet)"
	}
	return name
}
