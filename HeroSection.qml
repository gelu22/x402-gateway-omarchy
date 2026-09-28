// HeroSection.qml — panel hero: balance + spend/cap meter · state · power (40.1).
// Pure composition of the former Panel.qml hero block. Props in, one signal
// out; daemon truth (setPaused) stays in Panel — no Process/socket here.
// The balance number and meter fraction arrive pre-formatted/pre-computed
// (Panel + Model) — this file only renders (AGENTS.md rule 7).
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

    property string heroLabel: "Offline"
    property color heroColor: Model.Palette.offline
    property bool paused: false
    property bool pausing: false
    property bool busy: false
    property bool online: true
    property var bar: null
    property bool working: false
    property string balanceText: "0.00 USDC"
    property real spendToday: 0
    property real capDaily: 0

    signal togglePause(bool checked)

    width: parent ? parent.width : 0
    spacing: Style.space(8)

    // ---- Balance block (nexthop dashboard language): CAPS label, big number
    // in the state tone, thin spend/cap meter. The meter hides when the cap is
    // 0 (auto-pay off) — a 0% bar would lie with false precision.
    Column {
        width: parent.width
        spacing: Style.space(4)

        Text {
            text: "BALANCE"
            color: Qt.darker(Color.foreground, 1.4)
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
        }

        Text {
            text: root.balanceText
            color: root.heroColor
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.display
            font.weight: Font.Bold
        }

        BudgetMeter {
            width: parent.width
            visible: root.capDaily > 0
            fraction: Model.budgetFraction(root.spendToday, root.capDaily)
            tone: root.heroColor
        }
    }

    // ---- State row: icon · title/state · power (unchanged from 39.2) ----
    Item {
        width: parent.width
        implicitHeight: Math.max(heroIcon.implicitHeight, heroLabels.implicitHeight, heroPower.implicitHeight)

        Text {
            id: heroIcon
            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_WALLET
            color: root.heroColor
            font.pixelSize: Style.font.display
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
        }

        Column {
            id: heroLabels
            anchors.left: heroIcon.right
            anchors.leftMargin: Style.space(12)
            anchors.right: heroPower.left
            anchors.rightMargin: Style.space(12)
            anchors.verticalCenter: parent.verticalCenter
            spacing: Style.space(2)

            Text {
                width: parent.width
                text: "x402 Gateway"
                color: Color.foreground
                font.family: root.bar ? root.bar.fontFamily : Style.font.family
                font.pixelSize: Style.font.subtitle
                font.bold: true
                elide: Text.ElideRight
            }
            Text {
                width: parent.width
                text: root.heroLabel
                color: root.heroColor
                opacity: 0.8
                font.pixelSize: Style.font.bodySmall
                elide: Text.ElideRight
            }
        }

        ToggleSwitch {
            id: heroPower
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            checked: !root.paused
            busy: root.pausing || root.busy
            // Visible disabled state (not silent ignore): no daemon
            // to talk to while offline, no clicks mid-operation.
            interactive: !root.pausing && !root.busy && root.online
            hasCursor: true
            onToggled: root.togglePause(heroPower.checked)
        }
    }

    Text {
        width: parent.width
        visible: root.working
        text: "⏳ Working…"
        color: Color.foreground
        font.pixelSize: Style.font.bodySmall
    }
}
