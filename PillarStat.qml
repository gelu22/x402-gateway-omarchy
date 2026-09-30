// PillarStat.qml — one dashboard pillar: CAPS label + big number in tone (40.2).
// Pure renderer: label and value arrive pre-formatted; no math here. After 43.1
// the spend/cap ratio lives in these pillars (BudgetMeter left the hero), so a
// pillar still carries no meter of its own (a second bar would just repeat it).
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
        color: Qt.darker(Color.foreground, 1.45)
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
