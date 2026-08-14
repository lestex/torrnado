# The README and landing-page demo, recorded with charmbracelet/vhs:
#
#   vhs demo.tape
#
# It needs a daemon with a torrent already downloading, or the recording
# is a list with nothing in it. Set one up first, throttled so it does
# not finish mid-take:
#
#   D=/tmp/tndemo; rm -rf $D; mkdir -p $D/bin $D/dl $D/cfg/torrnado
#   go build -o $D/bin/torrnado ./cmd/torrnado
#   printf 'download_dir = "%s/dl"\n\n[rate_limit]\ndownload = "1M"\n' "$D" \
#     > $D/cfg/torrnado/config.toml
#   HOME=$D XDG_DATA_HOME=$D/data XDG_CONFIG_HOME=$D/cfg $D/bin/torrnado daemon &
#   HOME=$D XDG_DATA_HOME=$D/data XDG_CONFIG_HOME=$D/cfg $D/bin/torrnado add '<magnet>'
#
# Kill it by socket afterwards, never by name:
#   kill $(lsof -t $D/data/torrnado/daemon.sock)

Output docs/assets/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1280
Set Height 680
Set Padding 12
Set Theme "Dracula"
Set TypingSpeed 45ms

# The environment is scaffolding, not part of the story.
Hide
Type "export HOME=/tmp/tndemo XDG_DATA_HOME=/tmp/tndemo/data XDG_CONFIG_HOME=/tmp/tndemo/cfg PATH=/tmp/tndemo/bin:$PATH PS1='$ '"
Enter
Type "clear"
Enter
Show

Sleep 800ms

# One command, and it keeps downloading after the shell is gone.
Type "torrnado list"
Enter
Sleep 2.5s

# Attach the interface to the daemon that was already running.
Type "torrnado"
Enter
Sleep 3s

# The detail pane tracks the cursor: pieces, then peers, then files.
Sleep 1.5s
Type "]"
Sleep 2.5s
Type "]"
Sleep 2.5s
Type "["
Sleep 1.5s

# Every key, generated from the live keymap.
Type "h"
Sleep 3.5s
Escape
Sleep 1s

# Quitting the interface does not stop the daemon.
Type "q"
Sleep 1.5s
Type "torrnado list"
Enter
Sleep 2.5s
