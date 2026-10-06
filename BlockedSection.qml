// BlockedSection.qml — payments the daemon refused and kept for the owner (49.2/49.4).
// Rows come from Model.parseBlocked(/status.blocked): summary only, never the
// request body/headers. Actions are daemon calls over the socket; this file has
// no business logic and no secrets (rule 6) — decisions live in Model.js.
pragma ComponentBehavior: Bound
import Quickshell.Io
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

    // rows: array from Model.parseBlocked(status).
    property var rows: []
    property string socketPath: ""
    // Emitted after a successful action so the panel reloads /status.
    signal refresh()
    signal failed(string msg)

    width: parent ? parent.width : 0
    spacing: Style.space(6)

    // One Process per action class: sharing one lets a concurrent call overwrite
    // the pending callback (019.1 lesson).
    Process {
        id: approveProc
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector {}
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => {
            if (code === 0) { root.refresh() } else { root.failed("Could not pay (code " + code + ") — check the daemon journal") }
        }
    }
    Process {
        id: dismissProc
        stdout: StdioCollector {}
        onExited: (code) => { if (code === 0) root.refresh() }
    }
    Process {
        id: permitProc
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector {}
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => {
            if (code === 0) { root.refresh() } else { root.failed("Could not add the permission (code " + code + ")") }
        }
    }

    function payNow(row) {
        var body = Model.approveBody(row.id)
        var payload = Model.stdinPayload(body)
        approveProc.pendingBody = payload
        approveProc.stdinEnabled = payload !== ""
        approveProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.FETCH_APPROVE, Model.Method.POST, body)
        approveProc.running = true
    }
    function dismiss(row) {
        var path = Model.Endpoint.FETCH_APPROVE + "?id=" + encodeURIComponent(row.id)
        dismissProc.command = Model.buildCommand(root.socketPath, path, Model.Method.DELETE, "")
        dismissProc.running = true
    }
    function allow(row) {
        var body = Model.permissionBody(row.url, Model.microToUsd(row.amountMicro), false, 0)
        var payload = Model.stdinPayload(body)
        permitProc.pendingBody = payload
        permitProc.stdinEnabled = payload !== ""
        permitProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.PERMISSIONS, Model.Method.POST, body)
        permitProc.running = true
    }

    Row {
        width: parent.width
        spacing: Style.space(8)
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_URLS
            color: Color.foreground
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: ("Waiting for you (" + root.rows.length + ")").toUpperCase()
            color: Qt.darker(Color.foreground, 1.4)
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }
    }

    Repeater {
        model: root.rows
        delegate: Column {
            id: blockedRow
            required property var modelData
            width: root.width
            spacing: Style.space(2)
            Text {
                width: parent.width
                text: blockedRow.modelData.host + " — " + Model.blockedAmountText(blockedRow.modelData.amountMicro) + " USDC"
                textFormat: Text.PlainText
                color: Color.foreground
                font.pixelSize: Style.font.body
                elide: Text.ElideRight
            }
            Text {
                width: parent.width
                text: blockedRow.modelData.reason
                textFormat: Text.PlainText
                color: Qt.darker(Color.foreground, 1.4)
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
            }
            Row {
                spacing: Style.space(6)
                Button {
                    text: "Pay now"
                    focusable: true
                    onClicked: root.payNow(blockedRow.modelData)
                }
                Button {
                    text: "Allow"
                    focusable: true
                    onClicked: root.allow(blockedRow.modelData)
                }
                Button {
                    text: "Dismiss"
                    focusable: true
                    onClicked: root.dismiss(blockedRow.modelData)
                }
            }
        }
    }
}
