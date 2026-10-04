// CollapsibleSection.qml — dumb disclosure (43.3); keyboard like Agents (39.3).
// bodyIndent (52.10) offsets nested content so a child ▸ does not align with
// this header's chevron (SETUP vs account).
import QtQuick
import QtQuick.Layouts
import qs.Commons

Column {
    id: root
    property string title: ""
    property string iconText: ""
    property bool expanded: false
    property string trailingText: ""
    property color trailingColor: Color.foreground
    // Left offset for body only (header stays full width). 0 = flush.
    property real bodyIndent: 0
    // SETUP uses gear+label; chevron is redundant there (52.10 follow-up).
    property bool showChevron: true
    signal toggle()
    default property alias content: body.data
    width: parent ? parent.width : 0
    spacing: Style.space(4)

    Item {
        id: header
        width: parent.width
        implicitHeight: headerRow.implicitHeight
        activeFocusOnTab: true
        Keys.onSpacePressed: root.toggle()
        Keys.onReturnPressed: root.toggle()
        Keys.onEnterPressed: root.toggle()
        RowLayout {
            id: headerRow
            width: parent.width
            spacing: Style.space(6)
            Text {
                visible: root.showChevron
                text: root.expanded ? "▾" : "▸"
                color: header.activeFocus ? Color.accent : Color.foreground
                font.pixelSize: Style.font.caption
                Layout.preferredWidth: root.showChevron ? 12 : 0
            }
            Text {
                visible: root.iconText !== ""
                text: root.iconText
                color: Color.foreground
                font.family: Style.font.family
                font.pixelSize: Style.font.icon
                Layout.preferredWidth: 16
            }
            // Title sizes to content and must not shrink (52.12b): a long
            // trailing used to crush "ACCOUNT" to zero width via fillWidth+elide.
            Text {
                text: root.title
                color: Qt.darker(Color.foreground, 1.4)
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1
                font.bold: true
                Layout.fillWidth: false
            }
            Item { Layout.fillWidth: true }
            Text {
                visible: root.trailingText !== ""
                text: root.trailingText
                color: root.trailingColor
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
                Layout.fillWidth: false
                Layout.maximumWidth: Math.max(80, headerRow.width * 0.55)
            }
        }
        MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.toggle()
        }
    }

    Item {
        id: bodyWrap
        width: parent.width
        visible: root.expanded
        implicitHeight: body.implicitHeight
        implicitWidth: parent.width

        Column {
            id: body
            x: root.bodyIndent
            width: parent.width - root.bodyIndent
            spacing: Style.space(8)
        }
    }
}
