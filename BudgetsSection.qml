// BudgetsSection.qml — Daily Budget: Spent/Cap/Remaining pillars; CAP is the
// sole editor (click value → inline field). Emits saveRequested(usd) on
// commit. 0 = always ask. No money math beyond parseAmount (AGENTS.md #6).
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

    property real spendToday: 0
    property real capDaily: 5

    signal saveRequested(real usd)

    // Inline feedback (009.2): validation errors block save, cap warnings
    // do not (v2 allows spending past the cap with approval).
    property string validationError: ""
    property string capWarning: ""

    // CAP pillar edit mode — one field at a time; Escape cancels without save.
    property bool editingCap: false

    readonly property color formMuted: Qt.darker(Color.foreground, 1.45)

    width: parent ? parent.width : 0
    spacing: Style.space(8)

    // ---- Section header: icon + CAPS label (40.2 dashboard language) ----
    Row {
        spacing: Style.space(6)
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_BUDGET
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "DAILY BUDGET (USDC)"
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }
    }

    // ---- Pillar row: Spent / Cap (editable) / Remaining ----
    Row {
        width: parent.width
        spacing: Style.space(8)

        PillarStat {
            width: (parent.width - 2 * Style.space(8)) / 3
            label: "SPENT"
            value: Model.formatUsdExact(root.spendToday)
            tone: Color.foreground
        }

        // CAP: same chrome as PillarStat; click value to edit inline.
        Column {
            id: capPillar
            width: (parent.width - 2 * Style.space(8)) / 3
            spacing: Style.space(4)

            Text {
                text: "CAP"
                color: root.formMuted
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1
            }

            Item {
                width: parent.width
                height: root.editingCap
                    ? capField.implicitHeight
                    : capDisplay.implicitHeight

                Text {
                    id: capDisplay
                    width: parent.width
                    visible: !root.editingCap
                    text: root.capDaily > 0 ? Model.formatUsdExact(root.capDaily) : "Off"
                    color: Color.foreground
                    font.family: Style.font.family
                    font.pixelSize: Style.font.heading
                    font.weight: Font.Bold
                    elide: Text.ElideRight

                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.beginEdit()
                    }
                }

                TextField {
                    id: capField
                    width: parent.width
                    visible: root.editingCap
                    // Compact so the field stays within the pillar width.
                    verticalPadding: Style.space(2)
                    horizontalPadding: Style.space(4)
                    font.pixelSize: Style.font.heading
                    font.weight: Font.Bold
                    foreground: Color.foreground

                    onVisibleChanged: {
                        if (visible)
                            forceActiveFocus()
                    }
                    onAccepted: root.commitEdit()
                    onEditingFinished: {
                        // Escape sets editingCap false first; skip commit then.
                        if (root.editingCap)
                            root.commitEdit()
                    }
                    Keys.onEscapePressed: (event) => {
                        root.cancelEdit()
                        event.accepted = true
                    }
                }
            }
        }

        PillarStat {
            width: (parent.width - 2 * Style.space(8)) / 3
            label: "REMAINING"
            value: root.capDaily > 0
                ? Model.formatUsdExact(Model.budgetRemaining(root.spendToday, root.capDaily))
                : "—"
            tone: Color.foreground
        }
    }

    // One permanent hint under the pillars (0 = always ask).
    Text {
        width: parent.width
        text: "0 disables automatic payments — every purchase asks for approval."
        color: root.formMuted
        opacity: 0.85
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    Text {
        width: parent.width
        visible: root.validationError !== ""
        text: root.validationError
        color: Model.Palette.error
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    Text {
        width: parent.width
        visible: root.capWarning !== ""
        text: root.capWarning
        color: Model.Palette.warn
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    function beginEdit() {
        if (root.editingCap)
            return
        root.validationError = ""
        root.capWarning = ""
        root.editingCap = true
        // Prefill numeric current cap (including 0), not the "Off" label.
        capField.text = Model.formatUsdExact(root.capDaily)
    }

    function cancelEdit() {
        root.editingCap = false
        root.validationError = ""
    }

    function parseAmount(text, fallback) {
        var t = String(text || "").trim().replace(",", ".")
        if (t === "")
            return fallback // empty field keeps the current cap
        var n = Number(t)
        return (isFinite(n) && n >= 0) ? n : NaN
    }

    function commitEdit() {
        if (!root.editingCap)
            return
        root.validationError = ""
        root.capWarning = ""
        var cap = parseAmount(capField.text, root.capDaily)
        if (!isFinite(cap) || cap < 0) {
            root.validationError = "Enter an amount ≥ 0 (e.g. 5.00)."
            return // stay in edit mode
        }
        // No-op when unchanged — avoid a needless MFA round-trip.
        if (cap === root.capDaily) {
            root.editingCap = false
            return
        }
        if (cap === 0) {
            root.capWarning = "Cap 0 = always ask (auto-pay off) until you raise it."
        } else if (cap < root.spendToday) {
            root.capWarning = "Cap is below today's spend (" + Model.formatUsdExact(root.spendToday) + ") — new payments will ask."
        }
        root.editingCap = false
        root.saveRequested(cap)
    }
}
