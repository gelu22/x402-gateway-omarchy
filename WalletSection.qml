// WalletSection.qml — Account: collapsed summary + Open config / MFA / Logout
// (43.3). Pure composition; socket calls stay in Panel via signals.
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

CollapsibleSection {
    id: root

    property string walletAddress: ""
    property string paymentNetwork: ""
    property bool mfaEnrolled: false
    property string mfaMethod: ""
    property bool busy: false
    property bool mfaBusy: false
    property string configPath: ""
    // Copy feedback (39.1): the copy control flashes ok-green while set.
    property bool addressCopied: false
    property int copyFeedbackMs: 2000

    signal copyAddress()
    signal openConfig()
    signal startMfaEnroll()
    signal openMfaReset()
    signal requestLogout()

    iconText: Model.ICON_WALLET
    title: Model.networkLabel(root.paymentNetwork) + " · " + Model.shortAddress(root.walletAddress)
    trailingText: Model.mfaLabel(root.mfaEnrolled, root.mfaMethod)
    trailingColor: Model.mfaBadge(root.mfaEnrolled)

    // ---- Body (visible when expanded) ----
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

    Button {
        text: "Open config"
        enabled: root.configPath !== ""
        tooltipText: root.configPath !== "" ? root.configPath : "Config path unavailable"
        onClicked: root.openConfig()
    }

    // MFA: explicit text + Enable/Reset (enroll/reset semantics unchanged).
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

    Button {
        text: "Logout"
        enabled: !root.busy
        onClicked: root.requestLogout()
    }
}
