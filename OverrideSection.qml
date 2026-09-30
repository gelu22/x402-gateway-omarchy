// OverrideSection.qml — remembered over-budget approvals: count + empty state.
// Open config lives in Account (43.3). Empty-state hint only — no config-path
// prose (removed in 43.4). List file: ~/.config/omarchy/x402-gateway/config.json.
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

    property int count: 0

    width: parent ? parent.width : 0
    spacing: Style.space(4)

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
            text: ("Remembered overrides (" + root.count + ")").toUpperCase()
            color: Qt.darker(Color.foreground, 1.4)
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }
    }

    // Empty state (39.2): zero looks the same as N by design, so say what
    // zero means instead of looking broken.
    Text {
        width: parent.width
        visible: root.count === 0
        text: "No remembered URLs — auto-pay always asks."
        color: Color.foreground
        opacity: 0.7
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }
}
