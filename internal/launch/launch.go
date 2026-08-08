// Package launch starts an external program on a path or URL and leaves
// it running: a media player against a stream, a file manager against a
// folder.
package launch

import (
	"fmt"
	"os/exec"
	"strings"
)

// PathPlaceholder is where the path or URL goes in a configured command.
// Spelled the way desktop entries and mailcap do it, because that is what
// anyone writing one of these will try first.
const PathPlaceholder = "%f"

// Detached starts command with path substituted for %f, or appended when
// the command names no placeholder, and returns as soon as it has
// started.
//
// The program is detached and released rather than waited on, for the
// same reason the daemon is: it has to outlive the process that started
// it. Quitting the TUI must not close what you are watching or the window
// you just opened, and the TUI cannot sit in wait() anyway.
//
// This deliberately does not use bubbletea's ExecProcess, which suspends
// the program and hands the terminal to the child. That is right for
// something rendering into the terminal itself; here the point is to keep
// the TUI usable alongside whatever was started.
func Detached(command, path string) error {
	name, args := parse(command)
	if name == "" {
		return fmt.Errorf("no command configured")
	}
	args = withPath(args, path)

	cmd := exec.Command(name, args...)
	// A child inheriting the TUI's stdio would scribble over the rendered
	// panes with its own output.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", name, err)
	}
	return cmd.Process.Release()
}

// parse splits a configured command into its program and any fixed
// arguments, so a config value can carry flags ("mpv --no-terminal")
// rather than only a bare binary name. Splitting on spaces is enough for
// that and avoids pulling in a shell - passing the value to `sh -c`
// would make the path a shell-injection surface.
func parse(command string) (name string, args []string) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// withPath puts path where the command asked for it.
//
// The substitution happens here, after the split, and never before: a
// path is chosen by the user's filesystem, not by us, and one containing
// a space would otherwise be split into two arguments - which is how
// "~/Downloads/Some Show/" reaches a file manager as two directories
// neither of which exists. Replacing inside an already-split field also
// makes "--working-directory=%f" work as well as "--working-directory %f".
//
// A command with no placeholder gets the path appended, which is what
// every player was doing before there was one.
func withPath(args []string, path string) []string {
	out := make([]string, 0, len(args)+1)
	substituted := false
	for _, a := range args {
		if strings.Contains(a, PathPlaceholder) {
			a = strings.ReplaceAll(a, PathPlaceholder, path)
			substituted = true
		}
		out = append(out, a)
	}
	if !substituted {
		out = append(out, path)
	}
	return out
}
