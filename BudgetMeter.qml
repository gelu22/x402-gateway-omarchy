// BudgetMeter.qml — thin spend/cap meter (40.1). Pure renderer: fraction
// (0..1) and tone arrive pre-computed; no math, no daemon truth here.
import QtQuick
import qs.Commons
import "Model.js" as Model

Rectangle {
    id: root

    property real fraction: 0
    property color tone: Model.Palette.ok

    readonly property real meterHeight: Math.max(2, Style.space(3))

    width: parent ? parent.width : 0
    height: meterHeight
    implicitHeight: meterHeight
    color: Qt.rgba(Color.foreground.r, Color.foreground.g, Color.foreground.b, 0.12)

    Rectangle {
        height: parent.height
        width: parent.width * Math.max(0, Math.min(1, root.fraction))
        color: root.tone
        Behavior on width { NumberAnimation { duration: 300; easing.type: Easing.OutCubic } }
    }
}
