import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Bar icon (aggregate down/up rate) plus a click-to-open panel listing every
// torrent the daemon knows about. Talks to torrnado only through its CLI -
// the wire protocol (internal/ipc) is gob-encoded Go, not something a QML
// process can speak.
//
// `status` polls on a plain timer and never spawns a daemon (torrnado's own
// guarantee - see `torrnado status --help`). `list` does dial-or-spawn like
// every other subcommand, so it only ever runs once `status` has confirmed a
// daemon is already up, and only while the panel is open - otherwise an idle
// bar icon would spawn a daemon on its own just by existing.
Panel {
  id: root
  moduleName: "lestex.torrnado"
  ipcTarget: "lestex.torrnado"

  property bool daemonRunning: false
  property int torrentCount: 0
  property string downRate: "0B/s"
  property string upRate: "0B/s"
  property string diskFree: ""
  property var torrents: []
  property bool actionBusy: false
  property bool listRefreshPending: false

  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color dim: Qt.darker(foreground, 1.55)
  readonly property color urgent: bar ? bar.urgent : Color.urgent

  // A fixed-width bar icon needs a narrow, predictable range of text
  // widths - the full "26.7MiB/s" form swings from ~4 chars idle to
  // 10+ chars at real download speed, which either leaves a big gap
  // (sized for the long case) or overflows into the next widget (sized
  // for the short case). Rounding to a whole number with a single-letter
  // unit keeps every realistic value within a couple characters of "0B/s".
  function shortRate(rateStr) {
    var m = String(rateStr || "").match(/^(-?[0-9.]+)([A-Za-z]*)\/s$/)
    if (!m) return rateStr
    var unit = m[2].charAt(0) || "B"
    return Math.round(parseFloat(m[1])) + unit + "/s"
  }

  readonly property string summaryText: root.daemonRunning
    ? ("↓" + root.shortRate(root.downRate) + " ↑" + root.shortRate(root.upRate))
    : "↓--"

  readonly property string tooltip: root.daemonRunning
    ? (root.torrentCount + " torrent" + (root.torrentCount === 1 ? "" : "s")
        + "\n↓ " + root.downRate + "   ↑ " + root.upRate
        + (root.diskFree !== "" ? ("\n" + root.diskFree + " free") : ""))
    : "torrnado daemon not running"

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  function refreshStatus() {
    if (!statusProc.running) statusProc.running = true
  }

  // A refresh requested while `list` is already in flight (e.g. the
  // periodic timer racing a pause/resume-triggered refresh) is queued
  // rather than dropped, so the row reflects the new state within one
  // process round-trip instead of waiting out the rest of the poll
  // interval - that stale window was reading as "unresponsive," and a
  // click landing in it looked like it silently did nothing.
  function refreshList() {
    if (!root.daemonRunning) return
    if (listProc.running) {
      root.listRefreshPending = true
      return
    }
    listProc.running = true
  }

  function torrentsEqual(a, b) {
    if (a.length !== b.length) return false
    for (var i = 0; i < a.length; i++) {
      var x = a[i], y = b[i]
      if (x.id !== y.id || x.name !== y.name || x.state !== y.state || x.progress !== y.progress
          || x.down !== y.down || x.up !== y.up || x.ratio !== y.ratio
          || x.peers !== y.peers || x.label !== y.label) return false
    }
    return true
  }

  function pauseTorrent(id) {
    if (root.actionBusy || !id || actionProc.running) return
    root.actionBusy = true
    actionProc.command = ["torrnado", "pause", id]
    actionProc.running = true
  }

  function resumeTorrent(id) {
    if (root.actionBusy || !id || actionProc.running) return
    root.actionBusy = true
    actionProc.command = ["torrnado", "resume", id]
    actionProc.running = true
  }

  // torrnado prints progress with one decimal ("32.1%"); round to a plain
  // whole-number percentage for the row.
  function formatProgress(value) {
    var n = parseFloat(value)
    return isNaN(n) ? value : Math.round(n) + "%"
  }

  // `torrnado list` prints a left-aligned tabwriter table (ID NAME STATE
  // PROGRESS DOWN UP RATIO PEERS LABEL), columns separated by 2+ spaces.
  function parseList(text) {
    var lines = String(text || "").split("\n")
    var rows = []
    for (var i = 1; i < lines.length; i++) {
      var line = lines[i]
      if (line.trim() === "") continue
      var cols = line.trim().split(/\s{2,}/)
      if (cols.length < 7) continue
      rows.push({
        id: cols[0],
        name: cols[1],
        state: cols[2],
        progress: root.formatProgress(cols[3]),
        down: cols[4],
        up: cols[5],
        ratio: cols[6],
        peers: cols.length > 7 ? cols[7] : "",
        label: cols.length > 8 ? cols.slice(8).join(" ") : ""
      })
    }
    // The daemon's own iteration order isn't stable between calls (two
    // separate `torrnado list` invocations can come back with torrents
    // swapped even with nothing changed), so rows would randomly flip
    // position on every poll unless sorted here into a fixed order.
    rows.sort(function(a, b) {
      var an = a.name.toLowerCase(), bn = b.name.toLowerCase()
      if (an < bn) return -1
      if (an > bn) return 1
      return a.id < b.id ? -1 : (a.id > b.id ? 1 : 0)
    })
    return rows
  }

  onOpenedChanged: if (opened) root.refreshList()

  Process {
    id: statusProc
    command: ["torrnado", "status"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var out = text || ""
        var statusMatch = out.match(/status\s+(.+)/)
        var running = !!statusMatch && statusMatch[1].trim() === "running"
        root.daemonRunning = running
        if (!running) {
          root.torrents = []
          return
        }

        var countMatch = out.match(/torrents\s+(\d+)/)
        var downMatch = out.match(/down\s+(\S+)/)
        var upMatch = out.match(/up\s+(\S+)/)
        var diskMatch = out.match(/disk free\s+(.+)/)
        root.torrentCount = countMatch ? parseInt(countMatch[1], 10) : 0
        root.downRate = downMatch ? downMatch[1] : "0B/s"
        root.upRate = upMatch ? upMatch[1] : "0B/s"
        root.diskFree = diskMatch ? diskMatch[1].trim() : ""
      }
    }
  }

  Process {
    id: listProc
    command: ["torrnado", "list"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        var parsed = root.parseList(text)
        if (!root.torrentsEqual(root.torrents, parsed)) root.torrents = parsed
      }
    }
    onExited: {
      if (root.listRefreshPending) {
        root.listRefreshPending = false
        root.refreshList()
      }
    }
  }

  Process {
    id: actionProc
    onExited: {
      root.actionBusy = false
      root.refreshList()
    }
  }

  Timer {
    interval: 3000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.refreshStatus()
  }

  Timer {
    interval: 2000
    running: root.opened
    repeat: true
    triggeredOnStart: true
    onTriggered: root.refreshList()
  }

  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    // Fixed so the rate text's varying length never resizes the slot -
    // that shifted this icon's x in the bar's right section on every
    // poll, and since the popup is anchored to this item, the whole
    // panel (and every button in it) jumped with it. clip is a safety
    // net for the rare value wider than this budget (shortRate keeps
    // the common range close to it either way) - it truncates instead
    // of painting over the next widget.
    fixedWidth: Style.space(82)
    clip: true
    fontFamily: root.fontFamily
    fontSize: Style.font.caption
    text: root.summaryText
    tooltipText: root.tooltip
    onPressed: function(mouseButton) { root.toggle() }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(420))
    contentHeight: panel.fittedContentHeight(column.implicitHeight, Style.space(520))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: root.close()

      Flickable {
        id: panelFlick
        anchors.fill: parent
        contentWidth: width
        contentHeight: column.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        flickableDirection: Flickable.VerticalFlick
        interactive: contentHeight > height
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        Column {
          id: column
          width: panelFlick.width
          spacing: Style.space(12)

          PanelHero {
            width: parent.width
            title: "Torrnado"
            meta: root.daemonRunning
              ? (root.torrentCount + " torrent" + (root.torrentCount === 1 ? "" : "s")
                  + " · ↓ " + root.downRate + "  ↑ " + root.upRate)
              : "Daemon not running"
            foreground: root.foreground
            fontFamily: root.fontFamily
            iconComponent: Component {
              Text {
                textFormat: Text.PlainText
                text: ""
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.display
              }
            }
          }

          Text {
            visible: root.daemonRunning && root.diskFree !== ""
            width: parent.width
            textFormat: Text.PlainText
            text: root.diskFree + " free"
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
          }

          Text {
            visible: !root.daemonRunning
            width: parent.width
            textFormat: Text.PlainText
            text: "Start it with `torrnado daemon`, or any command that needs it."
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          PanelSeparator { foreground: root.foreground }

          PanelSectionHeader {
            visible: root.daemonRunning
            text: "TORRENTS"
            foreground: root.foreground
            fontFamily: root.fontFamily
          }

          Text {
            visible: root.daemonRunning && root.torrents.length === 0
            width: parent.width
            textFormat: Text.PlainText
            text: "No torrents added yet."
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.font.body
            horizontalAlignment: Text.AlignHCenter
          }

          Column {
            id: torrentColumn
            visible: root.daemonRunning && root.torrents.length > 0
            width: parent.width
            spacing: Style.space(8)

            Repeater {
              model: root.torrents
              TorrentRow {
                required property var modelData
                width: torrentColumn.width
                torrent: modelData
              }
            }
          }
        }
      }
    }
  }

  component TorrentRow: Item {
    id: row
    property var torrent: null
    readonly property string state: torrent ? String(torrent.state || "") : ""
    readonly property bool paused: state === "paused"
    readonly property color stateColor: state === "error" ? root.urgent : (paused ? root.dim : root.foreground)

    implicitHeight: content.implicitHeight

    ColumnLayout {
      id: content
      anchors.left: parent.left
      anchors.right: parent.right
      spacing: Style.space(2)

      RowLayout {
        Layout.fillWidth: true
        spacing: Style.space(8)

        Text {
          textFormat: Text.PlainText
          Layout.fillWidth: true
          text: row.torrent ? row.torrent.name : ""
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: Style.font.body
          elide: Text.ElideRight
        }

        Text {
          textFormat: Text.PlainText
          text: row.torrent ? row.torrent.progress : ""
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: Style.font.bodySmall
        }

        PanelActionButton {
          iconText: row.paused ? "" : ""
          tooltipText: row.paused ? "Resume" : "Pause"
          foreground: root.foreground
          fontFamily: root.fontFamily
          fontSize: Style.font.bodySmall
          size: Style.space(22)
          bordered: true
          // RowLayout stretches children to its cross-axis size by default;
          // without pinning these, the button's hit area (always
          // anchors.fill'd to its own bounds) grew taller to match the
          // row's height while staying `size` wide, so only a thin strip
          // near the glyph still lined up with where it visually looked
          // clickable.
          Layout.preferredWidth: size
          Layout.preferredHeight: size
          Layout.alignment: Qt.AlignVCenter
          enabled: !root.actionBusy && row.torrent !== null
          onClicked: {
            if (!row.torrent) return
            if (row.paused) root.resumeTorrent(row.torrent.id)
            else root.pauseTorrent(row.torrent.id)
          }
        }
      }

      RowLayout {
        Layout.fillWidth: true
        spacing: Style.space(8)

        Text {
          textFormat: Text.PlainText
          text: row.state + (row.torrent && row.torrent.label !== "" ? " · " + row.torrent.label : "")
          color: row.stateColor
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
          Layout.fillWidth: true
          elide: Text.ElideRight
        }

        Text {
          textFormat: Text.PlainText
          text: "↓ " + (row.torrent ? row.torrent.down : "") + "  ↑ " + (row.torrent ? row.torrent.up : "")
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
        }
      }
    }
  }
}
