package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// runCommand executes a palette line and returns whatever status message
// the resulting command produced, if any.
func runCommand(t *testing.T, m Model, line string) statusMsg {
	t.Helper()
	next, cmd := m.execCommand(line)
	_ = next
	if cmd == nil {
		return statusMsg{}
	}
	msg := cmd()
	s, _ := msg.(statusMsg)
	return s
}

func TestUnknownCommandIsReported(t *testing.T) {
	got := runCommand(t, testModel("a"), "frobnicate")

	if !got.isErr {
		t.Fatalf("unknown command produced %+v, want an error", got)
	}
	if got.text == "" {
		t.Error("the error should say something")
	}
}

// Every verb that acts on the marked torrents, checked against the one
// thing it must not do: report itself as unknown. ":purge" in particular
// reads like a typo for ":pause", and if it were missing that is exactly
// how a user would find out.
//
// With no torrents to act on, each of these returns no command at all,
// while an unrecognised word returns one that reports the error - which
// is the difference being asserted. They cannot simply be run: the
// command they return talks to a daemon this model does not have.
func TestKnownCommandsAreNotReportedAsUnknown(t *testing.T) {
	for _, line := range []string{"purge", "pause", "resume", "recheck", "rm", "remove"} {
		if _, cmd := testModel().execCommand(line); cmd != nil {
			t.Errorf("%q produced a command with nothing to act on: %+v",
				line, runCommand(t, testModel(), line))
		}
	}

	if _, cmd := testModel().execCommand("frobnicate"); cmd == nil {
		t.Error("an unknown command produced nothing, so the check above proves nothing")
	}
}

// A command needing an argument has to say so rather than doing nothing.
func TestCommandsRequiringArgumentsComplain(t *testing.T) {
	for _, line := range []string{"add", "limit-up", "limit-down", "move"} {
		if got := runCommand(t, testModel("a"), line); !got.isErr {
			t.Errorf("%q with no argument produced %+v, want an error", line, got)
		}
	}
}

func TestQuitCommand(t *testing.T) {
	for _, line := range []string{"q", "quit"} {
		next, _ := testModel("a").execCommand(line)
		if !next.(Model).quitting {
			t.Errorf("%q did not quit", line)
		}
	}
}

// An empty line is a no-op, not an error: pressing : then enter is a
// mistake worth forgiving silently.
func TestEmptyCommandDoesNothing(t *testing.T) {
	m := testModel("a")
	next, cmd := m.execCommand("   ")
	if cmd != nil {
		t.Error("an empty command should produce no work")
	}
	if next.(Model).quitting {
		t.Error("an empty command should not quit")
	}
}

// The prompt collects text and runs it on enter; escape abandons it.
func TestCommandPromptCollectsAndCancels(t *testing.T) {
	m := testModel("a")
	m = press(m, ":")
	if m.mode != modeCommand {
		t.Fatal(": did not open the command prompt")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("quit")})
	m = next.(Model)
	if m.commandBuf != "quit" {
		t.Errorf("buffer = %q, want the typed text", m.commandBuf)
	}

	m = press(m, "esc")
	if m.mode != modeNormal || m.commandBuf != "" {
		t.Errorf("escape left mode=%v buffer=%q", m.mode, m.commandBuf)
	}
	if m.quitting {
		t.Error("escaping the prompt should not have run the command")
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "add foo bar", []string{"add", "foo", "bar"}},
		{"extra spaces", "  add   foo  ", []string{"add", "foo"}},
		{"empty", "   ", nil},
		// The reason this function exists: zsh needs a magnet quoted, so
		// that is how people type it here too.
		{
			"single-quoted magnet",
			"add 'magnet:?xt=urn:btih:ed8507e22addc40fd6fb4f1677bf27fd75967f70&dn=arch.iso'",
			[]string{"add", "magnet:?xt=urn:btih:ed8507e22addc40fd6fb4f1677bf27fd75967f70&dn=arch.iso"},
		},
		{
			"double-quoted magnet",
			`add "magnet:?xt=urn:btih:aaaa&dn=x"`,
			[]string{"add", "magnet:?xt=urn:btih:aaaa&dn=x"},
		},
		// The other thing quotes buy: an argument with a space in it.
		{"quoted path with spaces", "move '/media/big disk'", []string{"move", "/media/big disk"}},
		{"quote inside a word", "add file'name'.torrent", []string{"add", "filename.torrent"}},
		{"a quote of the other kind is literal", `add "it's.torrent"`, []string{"add", "it's.torrent"}},
		{"two quoted arguments", "add 'a b' 'c d'", []string{"add", "a b", "c d"}},
		// Forgiving rather than an error: the intent is not in doubt.
		{"unclosed quote takes the rest", "add 'magnet:?xt=x", []string{"add", "magnet:?xt=x"}},
		{"empty quotes are still an argument", "add ''", []string{"add", ""}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitArgs(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("splitArgs(%q) = %q, want %q", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("splitArgs(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}

// :help opens the same screen the help key does. Without this it would
// be a command the palette accepts and then appears to ignore, since the
// screen it opens is drawn by the next frame rather than by a status
// message.
func TestHelpCommandOpensTheHelpScreen(t *testing.T) {
	m := testModel("a")
	if m.showHelp {
		t.Fatal("the test model starts with the help screen already open")
	}

	next, _ := m.execCommand("help")

	if !next.(Model).showHelp {
		t.Error(":help did not open the help screen")
	}
}

// The two halves of the palette - what execCommand answers to, and what
// the help screen says it answers to - are edited in the same file so
// they stay together. This is what makes "stay together" a test rather
// than a habit: it reads the switch itself, so a case added without a
// line in paletteCommands fails here, and so does a documented command
// that no case implements.
func TestPaletteCommandsMatchWhatExecCommandAccepts(t *testing.T) {
	implemented := execCommandCases(t)
	if len(implemented) == 0 {
		t.Fatal("no case clauses were found in execCommand, so this test proves nothing")
	}

	documented := map[string]bool{}
	for _, c := range paletteCommands {
		for _, name := range c.names {
			if documented[name] {
				t.Errorf("%q is listed twice in paletteCommands", name)
			}
			documented[name] = true
		}
	}

	for name := range implemented {
		if !documented[name] {
			t.Errorf("the palette accepts %q, but the help screen does not list it", name)
		}
	}
	for name := range documented {
		if !implemented[name] {
			t.Errorf("the help screen lists %q, but the palette does not accept it", name)
		}
	}
}

// execCommandCases returns every string the switch in execCommand
// matches on, read out of the source rather than the binary: a switch is
// not something a running program can enumerate.
func execCommandCases(t *testing.T) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "palette.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing palette.go: %v", err)
	}

	names := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "execCommand" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("case %s in execCommand does not unquote: %v", lit.Value, err)
				}
				names[name] = true
			}
			return true
		})
	}
	return names
}
