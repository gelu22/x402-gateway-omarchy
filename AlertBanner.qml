// AlertBanner.qml — the single notice/error card of the panel (39.2).
// Two instances: daemon-driven notices at the top (tone "warn", not
// dismissable — daemon truth returns on the next refresh anyway) and local
// action errors near the wizard (tone "error", dismissable, Tab-reachable).
// Props in, one signal out; no socket, no decisions.
import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "Model.js" as Model

Rectangle {
    id: root

    property string text: ""
    property string tone: "warn" // warn | error
    property bool dismissable: true

    signal dismissed()

    width: parent ? parent.width : 0
    height: bannerRow.implicitHeight + Style.space(12)
    visible: root.text !== ""
    color: Model.Palette.bannerBg
    radius: Style.cornerRadius
    border.color: root.tone === "error" ? Model.Palette.error : Model.Palette.warn
    border.width: 1

    RowLayout {
        id: bannerRow
        anchors.fill: parent
        anchors.margins: Style.space(6)
        spacing: Style.space(8)

        Text {
            Layout.fillWidth: true
            text: root.text
            color: root.tone === "error" ? Model.Palette.error : Model.Palette.warn
            font.pixelSize: Style.font.bodySmall
            wrapMode: Text.WordWrap
        }

        Button {
            visible: root.dismissable
            focusable: true
            text: "Dismiss"
            onClicked: root.dismissed()
        }
    }
}
