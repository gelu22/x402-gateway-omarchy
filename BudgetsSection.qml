// BudgetsSection.qml — single daily budget as a Spent/Cap/Remaining pillar row
// (40.2) + cap edit. Pure presentation; emits saveRequested(usd) on Enter/button.
// 0 = always ask. Pillar values are pre-formatted numbers (currency lives in the
// header + the hero balance); no money math here (AGENTS.md rule 7).
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

    width: parent ? parent.width : 0
    spacing: Style.space(8)

    // ---- Section header: icon + CAPS label (40.2 dashboard language) ----
    Row {
        spacing: Style.space(6)
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_BUDGET
            color: Color.foreground
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "DAILY BUDGET (USD)"
            color: Qt.darker(Color.foreground, 1.4)
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }
    }

    // ---- Pillar row: Spent / Cap / Remaining (the ratio is the hero meter) ----
    Row {
        width: parent.width
        spacing: Style.space(8)

        PillarStat {
            width: (parent.width - 2 * Style.space(8)) / 3
            label: "SPENT"
            value: Model.formatUsdExact(root.spendToday)
            tone: Color.foreground
        }
        PillarStat {
            width: (parent.width - 2 * Style.space(8)) / 3
            label: "CAP"
            value: root.capDaily > 0 ? Model.formatUsdExact(root.capDaily) : "Off"
            tone: Color.foreground
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

    // ---- Cap edit (field + Save) ----
    Row {
        spacing: Style.space(10)

        Column {
            spacing: Style.space(2)
            Text { text: "Daily cap"; color: Color.foreground; font.pixelSize: Style.font.caption }
            TextField {
                id: capField
                width: Style.space(90)
                placeholderText: Model.formatUsdExact(root.capDaily)
                onAccepted: root.submit()
            }
        }

        Button { text: "Save"; anchors.bottom: parent.bottom; onClicked: root.submit() }
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

    Text {
        width: parent.width
        text: "0 disables automatic payments — every purchase asks for approval."
        color: Color.foreground
        opacity: 0.7
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    function parseAmount(text, fallback) {
        var t = String(text || "").trim().replace(",", ".")
        if (t === "") return fallback // empty field keeps the current cap
        var n = Number(t)
        return (isFinite(n) && n >= 0) ? n : NaN
    }

    function submit() {
        root.validationError = ""
        root.capWarning = ""
        var cap = parseAmount(capField.text, root.capDaily)
        if (!isFinite(cap) || cap < 0) {
            root.validationError = "Enter an amount ≥ 0 (e.g. 5.00)."
            return
        }
        if (cap === 0) {
            root.capWarning = "Cap 0 = always ask (auto-pay off) until you raise it."
        } else if (cap < root.spendToday) {
            root.capWarning = "Cap is below today's spend (" + Model.formatUsdExact(root.spendToday) + ") — new payments will ask."
        }
        root.saveRequested(cap)
    }
}
