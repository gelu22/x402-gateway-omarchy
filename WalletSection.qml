// WalletSection.qml — ACCOUNT block inside SETUP. Values are the actions:
// the short address next to the header copies it, and the two-factor line
// manages MFA. No buttons (Omarchy pattern); the network sits below the header.
import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root
    ThemeColors { id: pal }

    property string walletAddress: ""
    property string paymentNetwork: ""
    property bool mfaEnrolled: false
    property string mfaMethod: ""
    property bool busy: false
    property bool mfaBusy: false
    property bool addressCopied: false
    property int copyFeedbackMs: 2000

    signal copyAddress()
    signal startMfaEnroll()
    signal openMfaReset()

    readonly property color formMuted: Qt.darker(Color.foreground, 1.45)

    width: parent ? parent.width : 0
    spacing: Style.space(6)

    // Header: ACCOUNT + short address (click to copy). Hand cursor only when
    // there is an address to copy.
    RowLayout {
        width: parent.width
        spacing: Style.space(6)

        Text {
            text: Model.ICON_WALLET
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }

        Text {
            text: "ACCOUNT"
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }

        Item { Layout.fillWidth: true }

        Item {
            visible: root.walletAddress !== ""
            Layout.preferredWidth: addressText.implicitWidth
            Layout.preferredHeight: addressText.implicitHeight

            MouseArea {
                id: addressHit
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                hoverEnabled: true
                onClicked: {
                    root.copyAddress()
                    root.addressCopied = true
                }
            }

            Text {
                id: addressText
                text: root.addressCopied ? Model.copyDoneLabel() : Model.shortAddress(root.walletAddress)
                textFormat: Text.PlainText
                color: root.formMuted
                font.pixelSize: Style.font.caption
            }

            PanelToolTip {
                visible: addressHit.containsMouse && !root.addressCopied
                text: "Copy full address"
            }
        }
    }

    // Network, below the header (empty when unknown → no blank row).
    Text {
        width: parent.width
        visible: text !== ""
        text: Model.networkLabel(root.paymentNetwork)
        textFormat: Text.PlainText
        color: root.formMuted
        font.pixelSize: Style.font.caption
    }

    // Two-factor: friendly clickable line. On → manage/reset, off → set up.
    Item {
        width: parent.width
        height: mfaText.implicitHeight

        MouseArea {
            id: mfaHit
            anchors.fill: parent
            enabled: !root.mfaBusy
            cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
            hoverEnabled: true
            onClicked: root.mfaEnrolled ? root.openMfaReset() : root.startMfaEnroll()
        }

        Text {
            id: mfaText
            width: parent.width
            text: Model.mfaLabel(root.mfaEnrolled)
            textFormat: Text.PlainText
            color: pal.mfaBadge(root.mfaEnrolled)
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
        }

        PanelToolTip {
            visible: mfaHit.containsMouse
            text: Model.mfaTooltip(root.mfaEnrolled, root.mfaMethod)
        }
    }

    Timer {
        interval: root.copyFeedbackMs
        running: root.addressCopied
        onTriggered: root.addressCopied = false
    }
}
