// HistorySection.qml — recent payment audit rows (54.8). Formatting in Model.js.
pragma ComponentBehavior: Bound
import Quickshell.Io
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

CollapsibleSection {
    id: root

    property string socketPath: ""
    property var rows: []
    property bool truncated: false
    property string errorText: ""
    property bool loaded: false

    title: "HISTORY"
    iconText: Model.ICON_HISTORY

    width: parent ? parent.width : 0

    function reload() {
        if (root.socketPath === "") {
            root.errorText = "History unavailable"
            root.rows = []
            return
        }
        histProc.command = Model.buildCommand(
            root.socketPath, Model.Endpoint.HISTORY + "?limit=20", Model.Method.GET, "")
        histProc.running = true
    }

    function refresh() {
        if (root.expanded)
            root.reload()
    }

    Process {
        id: histProc
        stdout: StdioCollector {}
        onExited: (code) => {
            root.loaded = true
            if (code !== 0) {
                root.errorText = "History unavailable"
                root.rows = []
                root.truncated = false
                return
            }
            var parsed = Model.parseHistory(stdout.text)
            if (!parsed.ok) {
                root.errorText = "History unavailable"
                root.rows = []
                root.truncated = false
                return
            }
            root.errorText = ""
            root.truncated = parsed.truncated
            root.rows = Model.historyRows(parsed.entries, Date.now())
        }
    }

    Column {
        width: parent.width
        spacing: Style.space(4)

        Text {
            width: parent.width
            visible: root.errorText !== ""
            text: root.errorText
            textFormat: Text.PlainText
            color: Color.foreground
            opacity: 0.7
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
        }
        Text {
            width: parent.width
            visible: root.errorText === "" && root.loaded && root.rows.length === 0
            text: "No payments yet"
            color: Color.foreground
            opacity: 0.55
            font.pixelSize: Style.font.caption
        }
        Repeater {
            model: root.rows
            delegate: Row {
                id: histRow
                required property var modelData
                width: root.width
                spacing: Style.space(6)
                Text {
                    text: histRow.modelData.agent
                    textFormat: Text.PlainText
                    color: Color.foreground
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                    width: Math.min(72, parent.width * 0.18)
                }
                Text {
                    text: histRow.modelData.domain
                    textFormat: Text.PlainText
                    color: Color.foreground
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideMiddle
                    width: Math.min(120, parent.width * 0.28)
                }
                Text {
                    text: "$" + histRow.modelData.amount
                    textFormat: Text.PlainText
                    color: Color.foreground
                    font.pixelSize: Style.font.caption
                }
                Text {
                    text: histRow.modelData.outcome + (histRow.modelData.override ? " ★" : "")
                    textFormat: Text.PlainText
                    color: Color.foreground
                    opacity: 0.8
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                    width: Math.min(100, parent.width * 0.25)
                }
                Text {
                    text: histRow.modelData.when
                    textFormat: Text.PlainText
                    color: Color.foreground
                    opacity: 0.55
                    font.pixelSize: Style.font.caption
                }
            }
        }
        Text {
            width: parent.width
            visible: root.truncated && root.errorText === ""
            text: "showing latest"
            color: Color.foreground
            opacity: 0.45
            font.pixelSize: Style.font.caption
        }
    }
}
