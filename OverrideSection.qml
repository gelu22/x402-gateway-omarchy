// OverrideSection.qml — remembered over-budget approvals: count + empty state.
// The header opens the plugin config in the editor; the remembered list lives
// in config.json and removal is manual there (no inline remove, no MFA).
import QtQuick
import qs.Commons
import "Model.js" as Model

Column {
    id: root

    property int count: 0
    property string configPath: ""

    signal openConfig()

    readonly property color formMuted: Qt.darker(Color.foreground, 1.45)

    width: parent ? parent.width : 0
    spacing: Style.space(8)

    // Header is the action (Omarchy pattern): hand cursor only when a config
    // path exists; never a silent no-op.
    MouseArea {
        width: parent.width
        height: headerRow.implicitHeight
        enabled: root.configPath !== ""
        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
        onClicked: root.openConfig()

        Row {
            id: headerRow
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
}
