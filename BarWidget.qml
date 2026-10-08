// BarWidget.qml — bar entry: status dot + spend/budget, click opens Panel.
// Structure mirrors the built-in clock widget contract (develop.html §03).
import Quickshell
import Quickshell.Io
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

BarWidget {
    id: root

    moduleName: Model.PLUGIN_ID
    property string socketPath: ""

    // Live state (updated by statusProc polling).
    property string gatewayState: Model.State.OFFLINE   // offline|active|paused|exhausted|error|logged_out
    property string blockText: ""
    property string balanceText: Model.formatUsdExact(0)
    property string iconGlyph: Model.ICON_WALLET // nf-fa-credit_card — payment identity of the widget
    readonly property bool opened: panelLoader.item ? panelLoader.item.opened === true : false
    readonly property bool popoutSwitchClosing: panelLoader.item ? panelLoader.item.popoutSwitchClosing === true : false

    Component.onCompleted: {
        if (root.socketPath === "")
            root.socketPath = Model.defaultSocketPath(Quickshell.env("HOME") || "")
        root.refresh()
    }

    function refresh() {
        // Coalesce: never kill an in-flight /status (stale onExited → Offline).
        statusProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.STATUS, Model.Method.GET, "")
        if (statusProc.running)
            return
        statusProc.running = true
    }

    function open() { if (panelLoader.item) panelLoader.item.open() }
    function close() { if (panelLoader.item) panelLoader.item.close() }
    function toggle() { if (panelLoader.item) panelLoader.item.toggle() }
    function closeForPopoutSwitch() {
        if (panelLoader.item) panelLoader.item.closeForPopoutSwitch()
    }
    function injectPanel() {
        var target = panelLoader.item
        if (!target) return
        // Guarded assignments: QML throws on unknown properties.
        if ("bar" in target) target.bar = root.bar
        if ("settings" in target) target.settings = root.settings
        if ("anchorItem" in target) target.anchorItem = button
        if ("hostWidget" in target) target.hostWidget = root
        if ("socketPath" in target) target.socketPath = root.socketPath
    }

    property string tooltipLine1: ""
    property string tooltipLine2: ""

    // Poll interval for daemon status (tunable in one place).
    property int pollIntervalMs: 5000

    // Auto-surface: open the panel once per new over-budget approval request
    // (timestamp key) so the user sees the prompt without clicking the widget.
    property string lastOverrideTs: ""

    // Auto-surface: open the panel once per new mfa_required denial so the user
    // sees the OTP dialog without opening the panel manually. Deduped by the
    // denial's identity (30.1), not by its timestamp: a retry loop stamps a new
    // time on every attempt and would otherwise pop the panel open each time.
    property string seenMfaKey: ""
    property double mfaNagAtMs: 0

    // First poll completion (either outcome): before it the widget shows "…"
    // instead of a stale balance or a premature "Offline".
    property bool firstPollDone: false

    readonly property color stateColor: Model.statusColor(root.gatewayState)

    implicitWidth: button.implicitWidth
    implicitHeight: button.implicitHeight

    onStateChanged: refresh()

    Timer { interval: root.pollIntervalMs; running: true; repeat: true; onTriggered: root.refresh() }

    Process {
        id: statusProc
        // Deliver once per flight (stdout wins over a late non-zero exit).
        property bool delivered: false
        stdout: StdioCollector {
            onStreamFinished: {
                if (statusProc.delivered) return
                statusProc.delivered = true
                root.firstPollDone = true
                var st = Model.parseStatus(this.text)
                root.gatewayState = st.state
                root.tooltipLine1 = st.block_text !== "" ? st.block_text
                                                         : ("Status: " + Model.stateLabel(st.state))
                // logged_out: clear stale balance/spend from previous session
                if (st.state === Model.State.LOGGED_OUT) {
                    root.balanceText = Model.formatUsdExact(0)
                    root.tooltipLine2 = ""
                } else {
                    root.balanceText = st.balance_usd
                    root.tooltipLine2 = "Budget " + st.spend_today + "/" + st.budget_daily + Model.USD_SYMBOL
                }
                // New over-budget approval request → surface the prompt.
                var lfe = st.raw ? st.raw.last_fetch_error : null
                if (lfe && lfe.can_override === true && lfe.timestamp &&
                    lfe.timestamp !== root.lastOverrideTs) {
                    root.lastOverrideTs = lfe.timestamp
                    if (!root.opened) root.open()
                }

                // MFA required → surface the verify dialog without user action.
                var nagKey = Model.mfaNagKey(lfe)
                if (lfe && lfe.code === "mfa_required" &&
                    Model.shouldSurfaceMfa(root.seenMfaKey, root.mfaNagAtMs, nagKey, Date.now(), Model.MFA_REPROMPT_COOLDOWN_MS)) {
                    root.seenMfaKey = nagKey
                    root.mfaNagAtMs = Date.now()
                    if (!root.opened) root.open()
                }
            }
        }
        stderr: StdioCollector { }
        onStarted: statusProc.delivered = false
        onExited: (code, _) => {
            if (statusProc.delivered) return
            root.firstPollDone = true
            if (code !== 0) {
                statusProc.delivered = true
                root.gatewayState = Model.State.OFFLINE
                root.balanceText = Model.formatUsdExact(0)
                root.tooltipLine2 = ""
            }
        }
    }

    // Reinjection when the shell injects bar/settings AFTER creation —
    // clock contract: panel must always hold current bar/anchor references.
    onBarChanged: root.injectPanel()
    onSettingsChanged: root.injectPanel()

    Loader {
        id: panelLoader
        active: true // clock contract: panel stays instantiated; visibility is controller-managed
        source: Qt.resolvedUrl("Panel.qml")
        visible: false
        onLoaded: {
            root.injectPanel()
            Qt.callLater(root.injectPanel)
        }
    }

    IpcHandler {
        target: Model.PLUGIN_ID

        function toggle(): void { root.toggle() }
        function open(): void { root.open() }
        function close(): void { root.close() }
        function refresh(): void { root.refresh() }
    }

    WidgetButton {
        id: button
        anchors.fill: parent
        bar: root.bar
        // Status redundantly (39.3, a11y): icon + balance while active, the
        // state label otherwise — color alone is invisible to color-blind
        // users. Labels come from Model.stateLabel (no new strings in QML).
        // balanceText is already formatUsdExact from parseStatus — never re-round (NEW-P2-1).
        text: root.iconGlyph + "  " + (root.gatewayState === Model.State.ACTIVE ? root.balanceText + Model.USD_SYMBOL
              : !root.firstPollDone ? "…"
              : Model.stateLabel(root.gatewayState))
        fontSize: Style.font.caption
        foreground: root.stateColor
        tooltipText: root.tooltipLine1 + (root.tooltipLine2 !== "" ? ("\n" + root.tooltipLine2) : "")
        onPressed: function(buttonCode) {
            if (buttonCode === Qt.LeftButton) root.toggle()
        }
    }
}
