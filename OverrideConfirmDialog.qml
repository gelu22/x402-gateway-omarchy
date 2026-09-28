// OverrideConfirmDialog.qml — compact modal confirmation for over-budget
// payments. Unique name avoids collision with qs.Ui.ConfirmDialog.
// Uses only QtQuick + qs kit components (no QtQuick.Layouts/Controls imports).
import Quickshell
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
    id: root

    // Numeric inputs (USD). Formatting happens HERE, once, with
    // Model.formatUsdExact — a pre-formatted string would be re-rounded to
    // 2 decimals and a real $0.002 payment would read as "0.00".
    property real amountUsd: 0
    property string targetUrl: ""
    property real budgetCapUsd: 0
    // Price-change re-approval (ad 1): the seller raised the price above what
    // was approved. Show it explicitly instead of the over-budget line.
    property bool priceChanged: false
    property real previousUsd: 0
    // Seller-trust denial code (013.2: "unknown_seller", "domain_cap_exceeded",
    // "" otherwise). Reason copy comes from Model (panel stays logic-free).
    property string code: ""
    property bool opened: false
    property bool rememberChecked: false
    // Busy (39.1): set by Panel while the accept call is in flight; freezes
    // scrim, Esc, buttons, and the switch so a double click cannot emit two
    // accepted() signals.
    property bool busy: false
    // URL copy feedback (39.1): the URL flashes Palette.ok while set; a Timer
    // reverts it after copyFeedbackMs (no layout shift — color only).
    property bool urlCopied: false
    property int copyFeedbackMs: 2000

    // Every fresh dialog starts with "remember" off: a price-change approval
    // must never inherit a stale, pre-checked consent (ad 1).
    onOpenedChanged: if (opened) rememberChecked = false

    signal accepted(bool remember)
    signal dismissed()

    // Single flip owned by root: the switch never flips itself (kit contract:
    // caller owns `checked`), so both the switch and the label call this.
    function toggleRemember() {
        if (root.busy) return
        root.rememberChecked = !root.rememberChecked
    }

    // Esc bubbles from the focused Pay button up to this root (blocked while
    // busy — same freeze as the scrim).
    Keys.onEscapePressed: if (root.opened && !root.busy) root.dismissed()

    // Invisible items get zero size in a Column layout, which would collapse
    // the scrim and the card: claim full width and the card height instead.
    width: parent.width
    height: root.opened ? card.height : 0
    visible: root.opened

    // Scrim overlay (click outside = dismiss; blocked while busy).
    Rectangle {
        anchors.fill: parent
        color: Util.alpha(Color.background, 0.7)
        MouseArea {
            anchors.fill: parent
            onClicked: if (!root.busy) root.dismissed()
        }
    }

    // Dialog card (compact: amount, reason, URL, remember, buttons).
    BorderSurface {
        id: card
        width: Math.min(parent.width - Style.space(24), Style.space(300))
        height: content.implicitHeight + Style.space(24)
        anchors.centerIn: parent
        color: Color.background
        padding: Style.space(12)
        radius: Style.cornerRadius

        // Swallow clicks so they don't reach the scrim.
        MouseArea { anchors.fill: parent }

        Column {
            id: content
            anchors.fill: parent
            anchors.topMargin: card.contentTopInset
            anchors.rightMargin: card.contentRightInset
            anchors.bottomMargin: card.contentBottomInset
            anchors.leftMargin: card.contentLeftInset
            spacing: Style.space(8)

            Row {
                width: parent.width
                spacing: Style.space(8)

                Text {
                    text: "⚠"
                    color: Model.Palette.warn
                    font.pixelSize: Style.font.title
                    anchors.verticalCenter: parent.verticalCenter
                }
                Text {
                    width: parent.width - Style.space(8) - Style.font.title
                    text: root.priceChanged ? "Price changed" : "Approve payment?"
                    color: Color.foreground
                    font.pixelSize: Style.font.subtitle
                    font.bold: true
                    anchors.verticalCenter: parent.verticalCenter
                }
            }

            // Amount as the hero number (40.3): CAPS label + big value in the
            // dialog's warn tone. Formatting stays formatUsdcExact (micro-USDC
            // safe) — only the scale and tone change, never the value.
            Text {
                width: parent.width
                text: "AMOUNT"
                color: Qt.darker(Color.foreground, 1.4)
                font.family: Style.font.family
                font.pixelSize: Style.font.caption
                font.letterSpacing: 1
                font.bold: true
            }

            Text {
                width: parent.width
                text: Model.formatUsdcExact(root.amountUsd)
                color: Model.Palette.warn
                font.pixelSize: Style.font.display
                font.weight: Font.Bold
            }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                visible: root.priceChanged
                text: (root.previousUsd > 0 ? "Was " + Model.formatUsdcExact(root.previousUsd) + ", now this. " : "")
                      + "The seller now asks more than was approved. Pay the new amount?"
                color: Model.Palette.warn
                font.pixelSize: Style.font.caption
            }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                visible: Model.overrideReasonText(root.code, root.targetUrl) !== ""
                text: Model.overrideReasonText(root.code, root.targetUrl)
                color: Model.Palette.warn
                font.pixelSize: Style.font.caption
            }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                visible: !root.priceChanged && Model.overrideReasonText(root.code, root.targetUrl) === ""
                text: root.budgetCapUsd > 0
                      ? "Over daily budget (" + Model.formatUsdcExact(root.budgetCapUsd) + ")"
                      : "Auto-pay is off — approve this payment?"
                color: Model.Palette.warn
                font.pixelSize: Style.font.caption
            }

            // URL (click copies the full URL; flashes ok-green as feedback).
            Text {
                width: parent.width
                text: root.targetUrl
                color: root.urlCopied ? Model.Palette.ok : Color.foreground
                opacity: 0.7
                font.pixelSize: Style.font.caption
                elide: Text.ElideMiddle

                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: if (root.targetUrl && !root.busy) {
                        Quickshell.execDetached(Model.clipboardCommand(root.targetUrl))
                        root.urlCopied = true
                    }
                }
            }

            Timer {
                interval: root.copyFeedbackMs
                running: root.urlCopied
                onTriggered: root.urlCopied = false
            }

            Row {
                width: parent.width
                spacing: Style.space(8)

                ToggleSwitch {
                    id: rememberSwitch
                    anchors.verticalCenter: parent.verticalCenter
                    checked: root.rememberChecked
                    busy: root.busy
                    onToggled: root.toggleRemember()
                }

                Text {
                    width: parent.width - rememberSwitch.implicitWidth - parent.spacing
                    anchors.verticalCenter: parent.verticalCenter
                    wrapMode: Text.WordWrap
                    text: "Remember this URL"
                    color: Color.foreground
                    font.pixelSize: Style.font.caption

                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.toggleRemember()
                    }
                }
            }

            Row {
                anchors.right: parent.right
                spacing: Style.space(8)

                Button {
                    text: "Cancel"
                    focusable: true
                    enabled: !root.busy
                    onClicked: if (!root.busy) root.dismissed()
                }

                Button {
                    text: "Pay"
                    selected: true
                    focusable: true
                    focus: root.opened
                    enabled: !root.busy
                    onClicked: if (!root.busy) root.accepted(root.rememberChecked)
                }
            }
        }
    }
}
