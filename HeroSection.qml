// HeroSection.qml — Status row: BALANCE + amount + status chip + power (43.1).
// Pure composition. Props in, one signal out; daemon truth (setPaused) stays
// in Panel — no Process/socket here. The balance number arrives pre-formatted
// (Panel → Model.formatUsdcExact) — this file only renders (AGENTS.md #6).
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

    signal togglePause(bool checked)

    width: parent ? parent.width : 0
    spacing: Style.space(8)

    // CAPS label keeps the money path labeled without a second chrome row.
    Text {
        text: "BALANCE"
        color: Qt.darker(Color.foreground, 1.4)
        font.family: root.bar ? root.bar.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        font.letterSpacing: 1
    }

    // One Status row: large amount · status chip · power toggle.
    Item {
        width: parent.width
        implicitHeight: Math.max(balanceLabel.implicitHeight, statusChip.implicitHeight, heroPower.implicitHeight)

        Text {
            id: balanceLabel
            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            text: root.balanceText
            color: root.heroColor
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.display
            font.weight: Font.Bold
        }

        // Pill chip: heroLabel + heroColor (no hardcoded "Gateway Active").
        Rectangle {
            id: statusChip
            anchors.left: balanceLabel.right
            anchors.leftMargin: Style.space(10)
            anchors.verticalCenter: parent.verticalCenter
            // Cap so a long label never collides with the toggle.
            readonly property real room: Math.max(
                0,
                parent.width - balanceLabel.width - heroPower.width
                    - Style.space(10) - Style.space(12)
            )
            implicitWidth: chipText.implicitWidth + Style.space(14)
            width: room > 0 ? Math.min(implicitWidth, room) : implicitWidth
            height: chipText.implicitHeight + Style.space(6)
            radius: height / 2
            color: Qt.rgba(root.heroColor.r, root.heroColor.g, root.heroColor.b, 0.15)
            border.color: Qt.rgba(root.heroColor.r, root.heroColor.g, root.heroColor.b, 0.45)
            border.width: 1

            Text {
                id: chipText
                anchors.centerIn: parent
                width: parent.width - Style.space(14)
                text: root.heroLabel
                color: root.heroColor
                font.family: root.bar ? root.bar.fontFamily : Style.font.family
                font.pixelSize: Style.font.caption
                font.weight: Font.DemiBold
                elide: Text.ElideRight
                horizontalAlignment: Text.AlignHCenter
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
