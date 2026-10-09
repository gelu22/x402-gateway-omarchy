// AgentSection.qml — detection and one-click opt-in integration of AI agents.
// A Button means a one-time action (no lying switches): each non-integrated
// agent gets "Integrate", each integrated one gets "Remove". All file mutations
// go through scripts/setup-agents.sh --apply/--remove <name>: backups,
// validation and rollback remain in the script (SSOT safety).
// Agents are split into CONNECTED (always shown) and AVAILABLE (collapsed by
// default, capped with "Show more"): the panel has no scroll.
pragma ComponentBehavior: Bound
import Quickshell
import Quickshell.Io
import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
    id: root
    ThemeColors { id: pal }

    // Absolute path to setup-agents.sh on this machine.
    property string scriptPath: ""
    property bool busy: false
    property string resultText: ""
    property var agents: []
    // /status snapshot from Panel (parseStatus result) for spend/limit rows.
    property var statusSnapshot: null

    // Which script op is in flight ("apply" | "remove") — routes the failure
    // message and the busy label, since both ops share one Process.
    // lastAgent names the op's target for the audit line.
    property string lastOp: ""
    property string lastAgent: ""
    property string busyLabel: "⏳ …"
    property string editingLabel: ""
    property string editError: ""

    // Available group is collapsed by default; a long list is capped.
    property bool availableExpanded: false
    property int availableLimit: Model.DEFAULT_AVAILABLE_LIMIT

    // Failures surface in the panel-wide error sink (009.2), not inline.
    signal failed(string message)
    signal saveAgentCap(string label, real usd)

    readonly property var groups: Model.agentGroups(root.statusSnapshot, root.agents)
    readonly property var availablePage: Model.capList(root.groups.available, root.availableLimit)

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

        // Connected (integrated) agents are always visible.
        Text {
            width: parent.width
            visible: root.groups.connected.length > 0
            text: "CONNECTED"
            color: Qt.darker(Color.foreground, 1.45)
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }

        Repeater {
            model: root.groups.connected
            delegate: agentRow
        }

        // Available (detectable, not integrated): collapsed by default; capped
        // with "Show more" so a long list cannot overflow the panel.
        CollapsibleSection {
            width: parent.width
            visible: root.groups.available.length > 0
            title: "AVAILABLE (" + root.groups.available.length + ")"
            showChevron: true
            expanded: root.availableExpanded
            onToggle: root.availableExpanded = !root.availableExpanded

            Repeater {
                model: root.availablePage.shown
                delegate: agentRow
            }

            Button {
                visible: root.availablePage.hidden > 0
                text: "Show more (+" + root.availablePage.hidden + ")"
                fontSize: Style.font.caption
                horizontalPadding: Style.space(4)
                verticalPadding: Style.space(2)
                onClicked: root.availableLimit += Model.DEFAULT_AVAILABLE_LIMIT
            }
        }

        // Unlabeled (read-only "" bucket) renders after the two groups.
        Repeater {
            model: root.groups.unlabeled
            delegate: agentRow
        }

        Text {
            width: parent.width
            visible: root.editError !== ""
            text: root.editError
            color: pal.error
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
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

    // One row shape shared by the CONNECTED, AVAILABLE and unlabeled Repeaters.
    Component {
        id: agentRow

        Column {
            id: agentCol
            required property var modelData
            width: parent.width
            spacing: Style.space(2)

            RowLayout {
                id: agentRowLayout
                width: parent.width
                spacing: Style.space(8)

                Text {
                    text: agentCol.modelData.integrated ? "✓" : "·"
                    color: agentCol.modelData.integrated ? pal.ok : Color.foreground
                    font.pixelSize: Style.font.bodySmall
                    Layout.preferredWidth: 14
                }

                Text {
                    text: agentCol.modelData.name
                    textFormat: Text.PlainText
                    color: Color.foreground
                    font.pixelSize: Style.font.bodySmall
                    Layout.fillWidth: true
                    elide: Text.ElideMiddle
                }

                Text {
                    visible: root.editingLabel !== agentCol.modelData.name
                    text: agentCol.modelData.spentText + " / " + agentCol.modelData.limitText
                    textFormat: Text.PlainText
                    color: Color.foreground
                    opacity: 0.75
                    font.pixelSize: Style.font.caption
                    Layout.alignment: Qt.AlignVCenter
                    MouseArea {
                        anchors.fill: parent
                        enabled: !agentCol.modelData.readOnly
                        cursorShape: agentCol.modelData.readOnly
                                     ? Qt.ArrowCursor : Qt.PointingHandCursor
                        onClicked: {
                            root.editError = ""
                            root.editingLabel = agentCol.modelData.name
                            capField.text = agentCol.modelData.capMicro > 0
                                ? Model.formatUsdExact(agentCol.modelData.capMicro / 1000000)
                                : "0"
                        }
                    }
                }

                TextField {
                    id: capField
                    visible: root.editingLabel === agentCol.modelData.name
                               && !agentCol.modelData.readOnly
                    Layout.preferredWidth: 72
                    font.pixelSize: Style.font.caption
                    onAccepted: function() {
                        var n = Number(String(text).trim().replace(",", "."))
                        if (!isFinite(n) || n < 0) {
                            root.editError = "Enter an amount ≥ 0"
                            return
                        }
                        root.editingLabel = ""
                        root.editError = ""
                        root.saveAgentCap(agentCol.modelData.name, n)
                    }
                    Keys.onEscapePressed: function(event) {
                        root.editingLabel = ""
                        root.editError = ""
                        event.accepted = true
                    }
                }

                Button {
                    visible: !agentCol.modelData.readOnly
                             && agentCol.modelData.connectable !== false
                             && !agentCol.modelData.integrated
                    enabled: !root.busy
                    text: root.busy ? root.busyLabel : "Integrate"
                    fontSize: Style.font.caption
                    horizontalPadding: Style.space(4)
                    verticalPadding: Style.space(2)
                    onClicked: root.integrate(agentCol.modelData.name)
                }

                Text {
                    visible: !agentCol.modelData.readOnly
                             && agentCol.modelData.connectable === false
                             && !agentCol.modelData.integrated
                    text: "Installed — no auto-connect"
                    color: Color.foreground
                    font.pixelSize: Style.font.caption
                    Layout.alignment: Qt.AlignVCenter
                }

                Button {
                    visible: !agentCol.modelData.readOnly
                             && agentCol.modelData.integrated === true
                    enabled: !root.busy
                    text: root.busy ? root.busyLabel : "Remove"
                    fontSize: Style.font.caption
                    horizontalPadding: Style.space(4)
                    verticalPadding: Style.space(2)
                    onClicked: root.remove(agentCol.modelData.name)
                }
            }
        }
    }
}
