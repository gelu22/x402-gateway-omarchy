// WalletSection.qml — wallet address + balance + MFA + logout (39.2).
// Pure composition of the former Panel.qml wallet block. Props in, signals
// out; socket calls (copyAddress, MFA enroll/reset, logout) stay in Panel.
import QtQuick
import QtQuick.Layouts
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
    // Copy feedback (39.1 pattern): the copy icon flashes ok-green while set.
    property bool addressCopied: false
    property int copyFeedbackMs: 2000

    signal copyAddress()
    signal startMfaEnroll()
    signal openMfaReset()
    signal requestLogout()

    width: parent ? parent.width : 0
    spacing: Style.space(4)

    // ---- Section header: icon + CAPS label (40.2 dashboard language). The
    // balance itself lives in the hero (40.1) — not repeated here.
    Row {
        spacing: Style.space(6)
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.ICON_WALLET
            color: Color.foreground
            font.family: Style.font.family
            font.pixelSize: Style.font.icon
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: ("Wallet (" + Model.USDC + " on " + Model.networkLabel(root.paymentNetwork) + ")").toUpperCase()
            color: Qt.darker(Color.foreground, 1.4)
            font.family: Style.font.family
            font.pixelSize: Style.font.caption
            font.letterSpacing: 1
            font.bold: true
        }
    }

    Text {
        width: parent.width
        text: "Network is set in the plugin config file — use \"Open config\" below."
        color: Color.foreground
        opacity: 0.7
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
    }

    Row {
        width: parent.width
        spacing: Style.space(8)

        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.shortAddress(root.walletAddress)
            color: Color.foreground
            font.pixelSize: Style.font.caption
        }

        Button {
            anchors.verticalCenter: parent.verticalCenter
            iconText: ""
            color: root.addressCopied ? Model.Palette.ok : Color.foreground
            tooltipText: root.addressCopied ? Model.copyDoneLabel() : "Copy full address"
            onClicked: { root.copyAddress(); root.addressCopied = true }
        }

        Timer {
            interval: root.copyFeedbackMs
            running: root.addressCopied
            onTriggered: root.addressCopied = false
        }
    }

    // MFA: explicit text + shield (on/off), optional enrollment.
    Row {
        width: parent.width
        spacing: Style.space(8)

        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: Model.mfaLabel(root.mfaEnrolled, root.mfaMethod)
            color: Model.mfaBadge(root.mfaEnrolled)
            font.pixelSize: Style.font.caption
        }

        Button {
            visible: !root.mfaEnrolled
            anchors.verticalCenter: parent.verticalCenter
            text: "Enable"
            enabled: !root.mfaBusy
            onClicked: root.startMfaEnroll()
        }

        Button {
            visible: root.mfaEnrolled
            anchors.verticalCenter: parent.verticalCenter
            text: "Reset"
            enabled: !root.mfaBusy
            onClicked: root.openMfaReset()
        }
    }

    PanelSeparator {}

    // Logout: destructive action with confirmation.
    Row {
        width: parent.width
        spacing: Style.space(8)

        Item { Layout.fillWidth: true }

        Button {
            text: "Logout"
            enabled: !root.busy
            onClicked: root.requestLogout()
        }
    }
}
