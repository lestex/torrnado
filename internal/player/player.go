// Package player launches an external media player against a stream URL.
package player

import (
	"fmt"
	"os/exec"
	"strings"
)

// Launch starts player with url as its argument and returns as soon as it
// has started.
//
// The player is detached and released rather than waited on, for the same
// reason the daemon is: it has to outlive the process that started it.
// Quitting the TUI must not kill whatever you are watching, and the TUI
// cannot sit in wait() anyway.
//
// This deliberately does not use bubbletea's ExecProcess, which suspends
// the program and hands the terminal to the child. That is right for a
// player rendering into the terminal itself; here the point is to keep
// the TUI usable alongside the video.
func Launch(player, url string) error {
	name, args := parse(player)
	if name == "" {
		return fmt.Errorf("no player configured")
	}

	cmd := exec.Command(name, append(args, url)...)
	// A player inheriting the TUI's stdio would scribble over the
	// rendered panes with its own progress output.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", name, err)
	}
	return cmd.Process.Release()
}

// parse splits a configured player into its command and any fixed
// arguments, so a config value can carry flags ("mpv --no-terminal")
// rather than only a bare binary name. Splitting on spaces is enough for
// that and avoids pulling in a shell -- passing the value to `sh -c`
// would make the URL a shell-injection surface.
func parse(player string) (name string, args []string) {
	fields := strings.Fields(player)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}
