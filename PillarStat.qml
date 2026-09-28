// PillarStat.qml — one dashboard pillar: CAPS label + big number in tone (40.2).
// Pure renderer: label and value arrive pre-formatted; no math here. The spend/
// cap ratio already lives in the hero meter, so a pillar carries no meter of its
// own (a second bar would just repeat it).
import QtQuick
import qs.Commons

Column {
    id: root

    property string label: ""
    property string value: ""
    property color tone: Color.foreground

    spacing: Style.space(4)

    Text {
        text: root.label
        color: Qt.darker(Color.foreground, 1.4)
        font.family: Style.font.family
        font.pixelSize: Style.font.caption
        font.letterSpacing: 1
    }

    Text {
        width: parent ? parent.width : 0
        text: root.value
        color: root.tone
        font.family: Style.font.family
        font.pixelSize: Style.font.heading
        font.weight: Font.Bold
        elide: Text.ElideRight
    }
}
