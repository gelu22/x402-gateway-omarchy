// WalletSection.qml — ACCOUNT block inside SETUP (52.13): flat CAPS header,
// muted status lines, one action row. Logout lives in the SETUP footer (panel).
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root

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
    spacing: Style.space(8)

    Row {
        spacing: Style.space(6)
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_WALLET
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "ACCOUNT"
            color: root.formMuted
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }
    }

    Text {
        width: parent.width
        text: Model.accountNetworkLine(root.paymentNetwork, root.walletAddress)
        textFormat: Text.PlainText
        color: root.formMuted
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
        visible: text !== ""
    }

    Text {
        width: parent.width
        text: Model.mfaLabel(root.mfaEnrolled, root.mfaMethod)
        color: Model.mfaBadge(root.mfaEnrolled)
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    Flow {
        width: parent.width
        spacing: Style.space(8)

        Button {
            text: root.addressCopied ? Model.copyDoneLabel() : "Copy address"
            tooltipText: root.addressCopied ? Model.copyDoneLabel() : "Copy full address"
            onClicked: { root.copyAddress(); root.addressCopied = true }
        }

        Button {
            visible: !root.mfaEnrolled
            text: "Enable MFA"
            enabled: !root.mfaBusy
            onClicked: root.startMfaEnroll()
        }

        Button {
            visible: root.mfaEnrolled
            text: "Reset MFA"
            enabled: !root.mfaBusy
            onClicked: root.openMfaReset()
        }
    }

    Timer {
        interval: root.copyFeedbackMs
        running: root.addressCopied
        onTriggered: root.addressCopied = false
    }
}
