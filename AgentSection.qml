// AgentSection.qml — detection and one-click opt-in integration of AI agents.
// A Button means a one-time action (no lying switches): each non-integrated
// agent gets "Integrate", each integrated one gets "Remove". All file mutations
// go through scripts/setup-agents.sh --apply/--remove <name>: backups,
// validation and rollback remain in the script (SSOT safety).
import Quickshell
import Quickshell.Io
import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
    id: root

    // Absolute path to setup-agents.sh on this machine.
    property string scriptPath: ""
    property bool busy: false
    property string resultText: ""
    property var agents: []

    // Which script op is in flight ("apply" | "remove") — routes the failure
    // message and the busy label, since both ops share one Process.
    // lastAgent names the op's target for the audit line.
    property string lastOp: ""
    property string lastAgent: ""
    property string busyLabel: "⏳ …"

    // Failures surface in the panel-wide error sink (009.2), not inline.
    signal failed(string message)

    implicitWidth: parent ? parent.width : 0
    implicitHeight: column.implicitHeight

    function detect(keepResult) {
        if (root.busy) return
        if (root.scriptPath === "") {
            root.failed(Model.AGENT_SCRIPT_MISSING)
            return
        }
        root.busy = true
        if (!keepResult) root.resultText = ""
        detectProc.command = [root.scriptPath, "--detect"]
        detectProc.running = true
    }

    // One-click integration of a single agent. Busy-guard: one flight at a
    // time; a missing script or name is a loud error, never silence.
    function integrate(name) {
        if (root.busy) return
        if (!name) {
            root.failed("Integration failed (unknown agent)")
            return
        }
        if (root.scriptPath === "") {
            root.failed(Model.AGENT_SCRIPT_MISSING)
            return
        }
        root.busy = true
        root.lastOp = "apply"
        root.lastAgent = name
        root.busyLabel = "⏳ Integrating…"
        root.resultText = ""
        applyProc.command = [root.scriptPath, "--apply", name]
        applyProc.running = true
    }

    // One-click reversal of a single agent's integration. Same busy-guard and
    // error routing as integrate(); the script owns backup/validate/rollback.
    function remove(name) {
        if (root.busy) return
        if (!name) {
            root.failed("Removal failed (unknown agent)")
            return
        }
        if (root.scriptPath === "") {
            root.failed(Model.AGENT_SCRIPT_MISSING)
            return
        }
        root.busy = true
        root.lastOp = "remove"
        root.lastAgent = name
        root.busyLabel = "⏳ Removing…"
        root.resultText = ""
        applyProc.command = [root.scriptPath, "--remove", name]
        applyProc.running = true
    }

    onVisibleChanged: {
        if (visible) detect()
    }

    Process {
        id: detectProc
        stdout: StdioCollector {
            onStreamFinished: {
                root.busy = false
                try {
                    var o = JSON.parse(this.text)
                    var list = []
                    var src = o.agents || []
                    for (var i = 0; i < src.length; i++)
                        list.push(src[i])
                    root.agents = list
                } catch (e) {
                    root.failed("Agent detection failed")
                }
            }
        }
        stderr: StdioCollector { }
        onExited: (code) => {
            if (code !== 0) {
                root.busy = false
                root.failed("Agent detection failed (exit " + code + ")")
            }
        }
    }

    Process {
        id: applyProc
        stdout: StdioCollector {
            onStreamFinished: root.resultText = this.text
        }
        stderr: StdioCollector { }
        onExited: (code) => {
            root.busy = false
            if (code === 0) root.detect(true) // refresh flags, keep success output
            else if (root.lastOp === "remove") root.failed("Removal failed (exit " + code + ")")
            else root.failed("Integration failed (exit " + code + ")")
            console.warn(Model.LOG_TAG + " agent " + root.lastOp + " " + root.lastAgent + " exit=" + code + (root.resultText !== "" ? " :: " + root.resultText.slice(0, Model.LOG_TRIM_RESULT).replace(/\n/g, " ") : ""))
        }
    }

    Column {
        id: column
        width: parent.width
        spacing: Style.space(6)

        Text {
            width: parent.width
            visible: root.agents.length === 0 && !root.busy
            text: "No known agents found — manual setup in README."
            color: Color.foreground
            font.pixelSize: Style.font.bodySmall
            wrapMode: Text.WordWrap
        }

        Repeater {
            model: root.agents

            delegate: RowLayout {
                required property var modelData
                readonly property bool integrated: modelData.integrated === true

                width: parent.width
                spacing: Style.space(8)

                Text {
                    text: modelData.integrated ? "✓" : "·"
                    color: modelData.integrated ? Model.Palette.ok : Color.foreground
                    font.pixelSize: Style.font.bodySmall
                    Layout.preferredWidth: 14
                }

                Text {
                    text: modelData.name
                    color: Color.foreground
                    font.pixelSize: Style.font.bodySmall
                    Layout.fillWidth: true
                    elide: Text.ElideMiddle
                }

                Button {
                    visible: modelData.connectable !== false && !modelData.integrated
                    enabled: !root.busy
                    text: root.busy ? root.busyLabel : "Integrate"
                    fontSize: Style.font.caption
                    horizontalPadding: Style.space(4)
                    verticalPadding: Style.space(2)
                    onClicked: root.integrate(modelData.name)
                }

                Text {
                    visible: modelData.connectable === false && !modelData.integrated
                    text: "Installed — no auto-connect"
                    color: Color.foreground
                    font.pixelSize: Style.font.caption
                    Layout.alignment: Qt.AlignVCenter
                }

                Button {
                    visible: modelData.integrated === true
                    enabled: !root.busy
                    text: root.busy ? root.busyLabel : "Remove"
                    fontSize: Style.font.caption
                    horizontalPadding: Style.space(4)
                    verticalPadding: Style.space(2)
                    onClicked: root.remove(modelData.name)
                }
            }
        }

        Text {
            width: parent.width
            visible: root.resultText !== ""
            text: root.resultText
            color: Color.foreground
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
        }
    }
}
