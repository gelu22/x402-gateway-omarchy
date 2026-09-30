// CollapsibleSection.qml — dumb disclosure (43.3); keyboard like Agents (39.3).
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
                text: root.expanded ? "▾" : "▸"
                color: header.activeFocus ? Color.accent : Color.foreground
                font.pixelSize: Style.font.caption
                Layout.preferredWidth: 12
            }
            Text {
                visible: root.iconText !== ""
                text: root.iconText
                color: Color.foreground
                font.family: Style.font.family
                font.pixelSize: Style.font.icon
                Layout.preferredWidth: 16
            }
            Text {
                text: root.title
                color: Qt.darker(Color.foreground, 1.4)
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1
                font.bold: true
                elide: Text.ElideRight
                Layout.fillWidth: true
            }
            Text {
                visible: root.trailingText !== ""
                text: root.trailingText
                color: root.trailingColor
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
            }
        }
        MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.toggle()
        }
    }

    Column {
        id: body
        width: parent.width
        spacing: Style.space(4)
        visible: root.expanded
    }
}
