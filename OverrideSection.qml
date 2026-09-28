// OverrideSection.qml — remembered over-budget approvals: count + link to the
// plugin config file. The list lives in ~/.config/omarchy/x402-gateway/config.json
// (edited by hand or written from the approval dialog); there is no inline list
// and no helper script (009.7).
import Quickshell
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

    property int count: 0
    property string configPath: ""

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

        Button {
            anchors.verticalCenter: parent.verticalCenter
            text: "Open config"
            enabled: root.configPath !== ""
            tooltipText: root.configPath !== "" ? root.configPath : "Config path unavailable"
            onClicked: root.openConfig()
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

    // Opens the config in the user's editor via the Omarchy-native launcher
    // (surfaces a toast); creates nothing — the file is seeded by install.sh.
    function openConfig() {
        if (root.configPath === "") return
        Quickshell.execDetached(["omarchy", "launch", "config", "editor", root.configPath])
    }
}
