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

  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color dim: Qt.darker(foreground, 1.55)
  readonly property color urgent: bar ? bar.urgent : Color.urgent

  readonly property string summaryText: root.daemonRunning
    ? (" " + root.downRate + "   " + root.upRate)
    : " --"

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

  function refreshList() {
    if (root.daemonRunning && !listProc.running) listProc.running = true
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
        progress: cols[3],
        down: cols[4],
        up: cols[5],
        ratio: cols[6],
        peers: cols.length > 7 ? cols[7] : "",
        label: cols.length > 8 ? cols.slice(8).join(" ") : ""
      })
    }
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
      onStreamFinished: root.torrents = root.parseList(text)
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
    readonly property color stateColor: state === "error" ? root.urgent : (state === "paused" ? root.dim : root.foreground)

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
