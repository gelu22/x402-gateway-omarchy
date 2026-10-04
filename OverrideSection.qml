// OverrideSection.qml — remembered over-budget approvals: count + empty state
// + Edit in config (52.11/52.13). Sole SETUP path to config.json.
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

    property int count: 0
    property string configPath: ""

    signal openConfig()

    readonly property color formMuted: Qt.darker(Color.foreground, 1.45)

    width: parent ? parent.width : 0
    spacing: Style.space(8)

    Row {
        width: parent.width
        spacing: Style.space(6)

        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_URLS
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }

        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: ("Remembered overrides (" + root.count + ")").toUpperCase()
            color: root.formMuted
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
        color: root.formMuted
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    Button {
        text: "Edit in config"
        enabled: root.configPath !== ""
        tooltipText: root.configPath !== "" ? root.configPath : "Config path unavailable"
        onClicked: root.openConfig()
    }
}
