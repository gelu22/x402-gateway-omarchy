// WizardSteps.qml — onboarding steps: 0 email → 1 OTP → 2 budgets → done.
// Pure presentation: emits submit signals, receives values via properties.
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
    id: root
    ThemeColors { id: pal }

    property int step: -1            // 0 email | 1 otp | 2 budgets | 3 done
    property bool busy: false
    property string walletAddress: ""
    property string budgetPrefill: ""
    // Celebration auto-hide timing (tunable in one place).
    property int celebrationMs: 6000

    signal submitEmail(string email)
    signal submitOtp(string otp)
    signal saveBudget(real usd)
    signal validationFailed(string message)

    width: parent ? parent.width : 0
    spacing: Style.space(10)

    Text { width: parent.width; visible: root.busy; text: "⏳ Working…"; color: Color.foreground; font.pixelSize: Style.font.bodySmall }

    // Step 0 — email
    Column {
        visible: root.step === 0
        width: parent.width
        spacing: Style.space(6)
        Text { width: parent.width; text: "1/3 Wallet email:"; color: Color.foreground; font.pixelSize: Style.font.bodySmall; wrapMode: Text.WordWrap }
        TextField {
            id: emailInput
            width: parent.width
            placeholderText: "you@example.com"
            onAccepted: function() { root.submitEmail(emailInput.text) }
        }
        Button { text: "Send code"; onClicked: root.submitEmail(emailInput.text) }
    }

    // Step 1 — OTP
    Column {
        visible: root.step === 1
        width: parent.width
        spacing: Style.space(6)
        Text { width: parent.width; text: "2/3 Code from email:"; color: Color.foreground; font.pixelSize: Style.font.bodySmall; wrapMode: Text.WordWrap }
        TextField {
            id: otpInput
            width: parent.width
            placeholderText: "123456"
            onAccepted: function() { root.submitOtp(otpInput.text) }
        }
        Button { text: "Confirm"; onClicked: root.submitOtp(otpInput.text) }
    }

    // Step 2 — daily budget (single, 0 = always ask) as a TEXT FIELD
    Column {
        visible: root.step === 2
        width: parent.width
        spacing: Style.space(6)

        PanelSectionHeader { text: "Daily budget (USDC)" }

        Column {
            spacing: Style.space(2)
            Text { text: "Daily cap (0 = ask each time)"; color: Color.foreground; font.pixelSize: Style.font.caption }
            TextField { id: budgetField; width: Style.space(90); placeholderText: root.budgetPrefill }
        }

        Button { text: "Save & activate"; onClicked: root.emitBudget() }
    }

    // Step 3 — shown only right after onboarding; sections below are the
    // permanent UI, so this celebration auto-hides after first view.
    property bool doneSeen: false
    onStepChanged: if (root.step === 3) { doneSeen = true; hideDoneTimer.restart() }
    Timer {
        id: hideDoneTimer
        interval: root.celebrationMs
        onTriggered: root.doneSeen = false
    }
    Column {
        visible: root.step === 3 && root.doneSeen
        width: parent.width
        spacing: Style.space(4)

        Text {
            width: parent.width
            text: "✅ Active. Agents pay automatically within limits."
            color: pal.ok
            font.pixelSize: Style.font.bodySmall
            wrapMode: Text.WordWrap
        }
    }

    function budgetValue() {
        return Number(String(budgetField.text).replace(",", "."))
    }

    function emitBudget() {
        var v = budgetValue()
        if (!(v >= 0)) {
            root.validationFailed("Budget must be a number ≥ 0")
            return
        }
        root.saveBudget(v)
    }
}
