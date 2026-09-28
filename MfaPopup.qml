// MfaPopup.qml — MFA as a centered Omarchy overlay window (PanelWindow on the
// WlrLayer.Overlay layer), NOT a modal inside the plugin panel. Reason: the
// enrollment card (QR + key + code) is taller than the panel can fit, which
// clipped the OTP field. A layer-shell overlay floats above windows and does
// not get tiled (a plain FloatingWindow did get tiled by Hyprland here).
//
// Modes: "enroll" (QR from the daemon + backup key + code), "verify" (code
// only, opened when a payment was blocked), "reset" (portal pointer; CDP has
// no API to reset TOTP). Pure presentation: all socket calls live in Panel.qml.
import QtQuick
import Quickshell
import Quickshell.Wayland
import qs.Commons
import qs.Ui
import "Model.js" as Model

PanelWindow {
    id: root

    property bool open: false
    property string mode: "enroll"        // enroll | verify | reset
    property string qrDataUri: ""
    property string secret: ""
    property string errorText: ""
    property bool busy: false
    // Verify context (24.4): strings built by Model.mfaVerifyReason in
    // Panel.qml; QML only renders them. Empty = bare code prompt.
    property var reasonLines: []
    // Copy-feedback flags (39.1): buttons show Model.copyDoneLabel() while
    // set; a Timer reverts each flag after copyFeedbackMs.
    property bool keyCopied: false
    property bool linkCopied: false
    property int copyFeedbackMs: 2000

    signal submitCode(string code)
    signal copySecret()
    signal copyPortalUrl()
    signal openPortal()
    signal dismissed()

    visible: root.open
    color: "transparent"
    anchors { top: true; bottom: true; left: true; right: true }
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.namespace: "omarchy-x402-mfa"
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.keyboardFocus: WlrKeyboardFocus.Exclusive

    // Fresh dialog each open; autofocus the code field (reset has none).
    onOpenChanged: if (open) {
        codeField.text = ""
        root.errorText = ""
        root.keyCopied = false
        root.linkCopied = false
        if (root.mode !== "reset") codeField.forceActiveFocus()
    }

    // Esc bubbles from the focused item to the window root; blocked while busy
    // (an in-flight verify finishes on the daemon, 30.2b).
    Keys.onEscapePressed: if (!root.busy) root.dismissed()

    // Local validation: a malformed code never reaches the socket.
    // Busy re-check: enabled-bindings already freeze the buttons, but a
    // double-Enter racing the flag must not emit a second submitCode.
    function submit() {
        if (root.busy) return
        if (!Model.isMfaCodeValid(codeField.text)) {
            root.errorText = "Enter the 6-digit code."
            return
        }
        root.errorText = ""
        root.submitCode(codeField.text)
    }

    // Scrim: click outside the card dismisses (blocked while busy).
    Rectangle {
        anchors.fill: parent
        color: Util.alpha(Color.background, 0.7)

        MouseArea {
            anchors.fill: parent
            onClicked: if (!root.busy) root.dismissed()
        }
    }

    BorderSurface {
        id: card
        anchors.centerIn: parent
        width: Math.min(parent.width - Style.space(24), Style.space(360))
        height: root.mode === "enroll" ? Style.space(470) + verifyContext.implicitHeight
              : root.mode === "verify" ? Style.space(300) + verifyContext.implicitHeight
              : Style.space(300)
        color: Color.background
        padding: Style.space(12)
        radius: Style.cornerRadius

        // Swallow clicks so only the scrim dismisses.
        MouseArea { anchors.fill: parent }

        Column {
            id: column
            anchors.fill: parent
            anchors.topMargin: card.contentTopInset
            anchors.rightMargin: card.contentRightInset
            anchors.bottomMargin: card.contentBottomInset
            anchors.leftMargin: card.contentLeftInset
            spacing: Style.space(8)

            Text {
                width: parent.width
                text: root.mode === "reset" ? "Reset MFA"
                    : root.mode === "verify" ? "MFA code required"
                    : "Set up MFA (authenticator app)"
                color: Color.foreground
                font.pixelSize: Style.font.heading
                font.bold: true
                wrapMode: Text.WordWrap
            }

            // Verify context: why this payment was blocked (24.4). The lines
            // come from Model.mfaVerifyReason — no copy or formatting here.
            Column {
                id: verifyContext
                width: parent.width
                spacing: Style.space(4)
                // Enroll also shows the context (26.2): when the daemon refuses
                // a cap change with mfa_not_enrolled, the user must see why they
                // are being asked to enable MFA.
                visible: (root.mode === "verify" || root.mode === "enroll") && root.reasonLines.length > 0

                Repeater {
                    model: root.reasonLines

                    delegate: Text {
                        required property var modelData // string from reasonLines
                        width: parent.width
                        text: modelData
                        color: Color.foreground
                        opacity: 0.8
                        font.pixelSize: Style.font.caption
                        wrapMode: Text.WordWrap
                    }
                }
            }

            // QR (40.3): framed card (padding + hairline + radius); size unchanged (160).
            Rectangle {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: root.mode === "enroll" && root.qrDataUri !== ""
                width: Style.space(160) + 2 * Style.space(8)
                height: Style.space(160) + 2 * Style.space(8)
                radius: Style.cornerRadius
                color: Color.background
                border.color: Model.Palette.hairline
                border.width: 1

                Image {
                    anchors.centerIn: parent
                    source: root.qrDataUri
                    width: Style.space(160)
                    height: Style.space(160)
                    fillMode: Image.PreserveAspectFit
                    smooth: false
                }
            }

            // Manual entry / backup. CDP has no recovery codes, so this key is
            // the user's backup — shown once, copyable.
            Text {
                width: parent.width
                visible: root.mode === "enroll" && root.secret !== ""
                text: "Manual key (copy as backup):\n" + root.secret
                color: Color.foreground
                opacity: 0.8
                font.pixelSize: Style.font.caption
                wrapMode: Text.WrapAnywhere
            }

            Button {
                visible: root.mode === "enroll" && root.secret !== ""
                text: root.keyCopied ? Model.copyDoneLabel() : "Copy key (backup)"
                    focusable: true
                enabled: !root.busy
                onClicked: { root.copySecret(); root.keyCopied = true }
            }

            Timer {
                interval: root.copyFeedbackMs
                running: root.keyCopied
                onTriggered: root.keyCopied = false
            }

            // Field label (40.3): short CAPS label; title + context carry the *why*.
            Text {
                width: parent.width
                visible: root.mode !== "reset"
                text: "AUTHENTICATOR CODE"
                color: Qt.darker(Color.foreground, 1.4)
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1
                font.bold: true
            }

            TextField {
                id: codeField
                width: parent.width
                visible: root.mode !== "reset"
                placeholderText: "123456"
                // 6 digits max; paste still lands in submit() (Model.isMfaCodeValid).
                maximumLength: 6
                validator: RegularExpressionValidator { regularExpression: /[0-9]{0,6}/ }
                enabled: !root.busy
                onAccepted: root.submit()
            }

            Text {
                width: parent.width
                visible: root.mode === "reset"
                text: "CDP has no API to reset an authenticator (TOTP). If you lost your "
                      + "authenticator, reset it from the CDP portal as the account owner — "
                      + "this panel cannot do it for you."
                color: Color.foreground
                font.pixelSize: Style.font.caption
                wrapMode: Text.WordWrap
            }

            Text {
                width: parent.width
                visible: root.mode === "reset"
                text: Model.MFA_RESET_URL
                color: Color.foreground
                opacity: 0.7
                font.pixelSize: Style.font.caption
                elide: Text.ElideMiddle
            }

            Text {
                width: parent.width
                visible: root.errorText !== ""
                text: root.errorText
                color: Model.Palette.error
                font.pixelSize: Style.font.caption
                wrapMode: Text.WordWrap
            }

            Text {
                width: parent.width
                visible: root.busy
                text: "⏳ Working…"
                color: Color.foreground
                font.pixelSize: Style.font.bodySmall
            }

            Row {
                anchors.right: parent.right
                spacing: Style.space(8)

                Button {
                    visible: root.mode === "reset"
                    text: root.linkCopied ? Model.copyDoneLabel() : "Copy link"
                    focusable: true
                    enabled: !root.busy
                    onClicked: { root.copyPortalUrl(); root.linkCopied = true }
                }

                Timer {
                    interval: root.copyFeedbackMs
                    running: root.linkCopied
                    onTriggered: root.linkCopied = false
                }

                Button {
                    visible: root.mode === "reset"
                    text: "Open portal"
                    focusable: true
                    enabled: !root.busy
                    onClicked: root.openPortal()
                }

                Button {
                    text: "Cancel"
                    focusable: true
                    enabled: !root.busy
                    onClicked: if (!root.busy) root.dismissed()
                }

                Button {
                    visible: root.mode !== "reset"
                    text: root.mode === "enroll" ? "Enable" : "Verify"
                    focusable: true
                    selected: true
                    enabled: !root.busy
                    onClicked: root.submit()
                }
            }
        }
    }
}
