// PANEL-V3-MARKER
// Panel.qml — gateway control panel in the Omarchy style.
// Sections: hero (title/state/power) · budgets · wallet · AI agents · wizard.
// Daemon communication goes through callDaemon()/callStatus()/callOverride() —
// one Process per operation class; no secrets in QML. MFA (state, socket calls
// and the sudo gate) lives in MfaController.qml (28.4), not here.
import Quickshell
import Quickshell.Io
import QtQuick
import QtQuick.Layouts
import qs.Commons
import qs.Ui
import "Model.js" as Model

Panel {
    id: root

    moduleName: Model.PLUGIN_ID
    manageIpc: false

    Component.onCompleted: console.warn(Model.LOG_TAG + " panel loaded")

    // Injected by BarWidget.injectPanel (clock contract).
    property var anchorItem: null
    property var hostWidget: null

    property string socketPath: ""
    property int step: -1           // -1 = resolved on first /status
    property string flowId: ""
    property bool busy: false
    property string walletAddress: ""
    property bool paused: false
    property bool pausing: false
    property bool online: true
    property string errorMessage: ""

    // fail() is the single funnel for daemon/backend failures: visible UI
    // plus a durable journal line (the panel X clears only the UI).
    // Local validation hints (email/OTP/amounts) set errorMessage directly
    // with no log — typos are not diagnostics.
    function fail(msg) {
        root.errorMessage = msg
        console.warn(Model.LOG_TAG + " error: " + msg)
    }

    // Hero + alert snapshot (set in refresh() via Model.heroState).
    property string heroLabel: "Offline"
    property color heroColor: Model.Palette.offline
    property string alertText: ""
    property int clockSkewMs: 0

    // Budget snapshot for BudgetsSection (set in refresh()).
    property real spendToday: 0
    property real capDaily: 5

    // Wallet balance (display-only, from /status wallet_balance_usdc).
    property real balanceNum: 0

    // Payment network for display (from /status payment_network).
    property string paymentNetwork: ""

    // Daemon version for bug reports (from /status version).
    property string daemonVersion: ""
    // Plugin release stamp (41.3) from the bundle's build-info.json; "" when the
    // file is absent (older installs), so the footer row stays clean.
    property string pluginStamp: ""
    // Short SETUP stamp (52.16): "plugin vX.Y.Z" without sha; "" when absent.
    property string pluginVersion: ""
    // Poll interval for auto-refresh while open (tunable in one place).
    property int pollIntervalMs: 15000

    // SETUP disclosure: session-only (reset on each open/close).
    property bool advancedExpanded: false
    property bool historyExpanded: false
    // Version stamp is debug-only (43.4); fail-closed without GATEWAY_PANEL_DEBUG=1.
    readonly property bool panelDebug: Model.panelDebugEnabled(Quickshell.env("GATEWAY_PANEL_DEBUG"))

    property string agentScriptPath: Model.shareFilePath(Quickshell.env("HOME"), "setup-agents.sh")

    // Over-budget approval (gateway 005.10): explicit user decision above
    // the daily budget; everything within budget auto-pays.
    property var pendingOverride: null

    // Set when a price-change re-approval is triggered after a failed override
    // pay, so the dialog can show the previously approved amount (ad 1).
    property var priceChangeFrom: null

    // Plugin config file (009.7): network + remembered URLs live in
    // ~/.config/omarchy/x402-gateway/config.json. The panel reads/writes it via
    // FileView (atomic); the daemon network is driven by Service from the same
    // file. configNetwork preserves the file's network so a URL write never
    // clobbers it (and never falls back to /status while the daemon is down).
    property string configPath: Model.gatewayConfigPath(Quickshell.env("HOME"))
    property string configNetwork: Model.DEFAULT_NETWORK
    property var rememberedUrls: []
    property var blockedRows: []
    property bool configLoaded: false

    // Timestamp of the override request already surfaced (dismissed or shown);
    // prevents the dialog from reappearing on every refresh for the same event.
    property string seenOverrideTs: ""

    // ---- MFA (017.11; controller extracted in 28.4) ----
    // Enrollment lives here because it comes from /status and drives the status
    // badge; dialog state, socket calls, the sudo gate and the replay of a
    // blocked mutation live in MfaController.qml.
    property bool mfaEnrolled: false
    property string mfaMethod: ""

    MfaController {
        id: mfa
        socketPath: root.socketPath
        capUsd: root.capDaily
        balanceUsd: root.balanceNum
        enrolled: root.mfaEnrolled
        onRefreshRequested: root.refresh()
        onBusyRequested: (value) => { root.busy = value }
        onPanelErrorRequested: (message) => root.fail(message)
        onErrorTextRequested: (message) => { mfaPopup.errorText = message }
        onEnrolledUpdated: (enrolled, method) => { root.mfaEnrolled = enrolled; root.mfaMethod = method }
        onOverrideReplayRequested: (po, remember, approved) => root.payOverride(po, remember, approved)
    }

    function open() {
        root.advancedExpanded = false
        root.historyExpanded = false
        root.controller.show()
        root.refresh()
    }
    function close() {
        root.advancedExpanded = false
        root.historyExpanded = false
        root.pendingOverride = null
        root.priceChangeFrom = null
        mfa.closeMfaDialog()
        root.controller.hide()
    }
    function switchPanel(direction) {
        if (root.bar && typeof root.bar.switchPanelFrom === "function")
            return root.bar.switchPanelFrom(root.hostWidget || root, direction)
        return false
    }

    function refresh() {
        // Guard mirrors the poll Timer: a /status landing mid-operation would
        // repaint stale state over the in-flight action.
        if (root.busy || root.pausing) return
        callStatus(function(raw) {
            try {
                var o = JSON.parse(raw)
                // Daemon unreachable: the Process handler synthesizes this
                // shape on curl failure. It is VALID json, so handle it before the
                // normal path (else we'd show "Sign-in required" + step 3).
                if (Model.errorCode(o) === "daemon_offline") {
                    root.online = false
                    root.daemonVersion = ""
                    root.heroLabel = "Offline"
                    root.heroColor = Model.Palette.offline
                    root.alertText = ""
                    return
                }
                root.online = true
                var num = function(v) { var n = Number(v || 0); return isFinite(n) ? n : 0 }
                root.walletAddress = o.wallet_address || ""
                root.paused = o.paused === true
                root.spendToday = num(o.spend_today_usdc)
                root.capDaily = num(o.budget_daily_usdc)
                root.balanceNum = num(o.wallet_balance_usdc)
                root.clockSkewMs = num(o.clock_skew_ms)
                root.paymentNetwork = o.payment_network || ""
                root.daemonVersion = o.version || ""
                // Payments the daemon is holding for the owner (49.2/49.4).
                root.blockedRows = Model.parseBlocked(o)
                if (typeof historySection !== "undefined" && historySection)
                    historySection.refresh()
                // Single transition point: resolveStep() decides from daemon
                // truth. User-initiated steps (submitEmail/submitOtp) set their
                // own; never inline step logic here.
                root.step = Model.resolveStep(root.step, o.state || "")
                root.mfaEnrolled = o.mfa_enrolled === true
                root.mfaMethod = o.mfa_method || ""

                // MFA required: the daemon blocked a payment (fail-closed). The
                // controller surfaces the verify dialog once per denial.
                mfa.surfacePaymentMfa(o.last_fetch_error)

                // Honest hero + alert from live values (single computation).
                var hs = Model.heroState({
                    paused: root.paused,
                    online: true,
                    session: o.state || "",
                    wallet: root.walletAddress,
                    signerOk: o.signer_ok !== false,
                    spend: root.spendToday,
                    cap: root.capDaily
                })
                root.heroLabel = hs.label
                root.heroColor = hs.color
                // Alert only for the over-budget hero (paused/error heroes
                // speak for themselves; error text keeps its own line).
                root.alertText = hs.over ? Model.overBudgetAlert(hs.over) : ""
                // A wrong clock breaks every signature, so it outranks the
                // downstream block reason it causes.
                if (root.alertText === "")
                    root.alertText = Model.clockSkewWarning(root.clockSkewMs)
                // A blocked payment with no dialog of its own (policy_violation,
                // network_denied, …) still needs a reason on screen.
                if (root.alertText === "")
                    root.alertText = Model.blockedText(o.last_fetch_error)

                // Over-budget approval: dialog, or silent auto-pay for
                // remembered URLs (fail-closed: unknown URL always asks).
                var ov = Model.parseOverrideError(raw)
                if (ov.isOverride && !root.pendingOverride && ov.timestamp !== root.seenOverrideTs) {
                    root.seenOverrideTs = ov.timestamp
                    console.warn(Model.LOG_TAG + " override detected: " + Model.formatUsdcExact(ov.amountUsd) + " @ " + ov.targetUrl)
                    var cap = root.capDaily
                    // Remember only authorizes up to the approved amount; any
                    // raise (or legacy/unknown limit) forces a fresh question.
                    var decision = Model.needsApproval(ov, root.rememberedUrls)
                    var prevUsd = (root.priceChangeFrom && root.priceChangeFrom.url === ov.targetUrl)
                                ? root.priceChangeFrom.previousUsd
                                : (decision.previousMicro > 0 ? Model.microToUsd(decision.previousMicro) : 0)
                    var po = {
                        url: ov.targetUrl,
                        amountMicro: ov.amountMicro,
                        amountUsd: ov.amountUsd,
                        targetUrl: ov.targetUrl,
                        budgetCapUsd: cap,
                        priceChanged: decision.priceChanged,
                        previousUsd: prevUsd,
                        code: ov.code || ""
                    }
                    root.priceChangeFrom = null
                    if (decision.autoPay)
                        root.payOverride(po, false, false)
                    else
                        root.pendingOverride = po
                }
            } catch (e) {
                root.online = false
                root.daemonVersion = ""
                root.heroLabel = "Offline"
                root.heroColor = Model.Palette.offline
                root.alertText = ""
            }
        })
    }

    function callDaemon(path, method, body, onDone) {
        var payload = Model.stdinPayload(body)
        callProc.pendingBody = payload
        callProc.stdinEnabled = payload !== ""
        callProc.command = Model.buildCommand(root.socketPath, path, method, body)
        callProc.onDone = onDone
        callProc.running = true
    }

    // Each operation class gets its own Process. Sharing one (the 019.1 lesson,
    // repeated for pause/logout) lets a concurrent call overwrite the pending
    // callback: the response then invokes the wrong handler → spurious errors,
    // lost results or a wedged `busy` ("dead panel"). The 15 s poll made that
    // collision routine, not exotic, so /status no longer shares callProc with
    // the user-triggered mutations (policy save, override pay, MFA).
    function callStatus(onDone) {
        statusProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.STATUS, Model.Method.GET, "")
        statusProc.onDone = onDone
        statusProc.running = true
    }

    function callOverride(body, onDone) {
        // An override can wait for an MFA code (30.2b), so it gets the longer
        // socket budget instead of the generic 30 s.
        var payload = Model.stdinPayload(body)
        overrideProc.pendingBody = payload
        overrideProc.stdinEnabled = payload !== ""
        overrideProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.FETCH_OVERRIDE, Model.Method.POST, body, Model.CURL_TIMEOUT_WAIT_S)
        overrideProc.onDone = onDone
        overrideProc.running = true
    }

    function submitEmail(email) {
        if (email.indexOf("@") < 1) { root.errorMessage = "Enter a valid email"; return }
        root.busy = true; root.errorMessage = ""
        callDaemon(Model.Endpoint.PAIR_INIT, Model.Method.POST, JSON.stringify({ email: email }), function(raw) {
            root.busy = false
            try {
                var o = JSON.parse(raw)
                root.flowId = o.flowId || o.flow_id || ""
                if (root.flowId === "") { root.fail("Sign-in failed to start: " + raw); return }
                root.step = 1
            } catch (e) { root.fail("Sign-in failed to start: " + raw) }
        })
    }

    function submitOtp(otp) {
        if (otp.length !== 6) { root.errorMessage = "Code is 6 digits"; return }
        root.busy = true; root.errorMessage = ""
        callDaemon(Model.Endpoint.PAIR_VERIFY, Model.Method.POST, JSON.stringify({ flowId: root.flowId, otp: otp }), function(raw) {
            root.busy = false
            try {
                var o = JSON.parse(raw)
                if (o.state === Model.State.ACTIVE) { root.step = 3; root.refresh() }
                else { root.fail("Sign-in failed: " + raw) }
            } catch (e) { root.fail("Verification failed: " + raw) }
        })
    }

    function copyAddress() {
        var clean = root.walletAddress.replace(/[^0-9a-fA-Fx]/g, "")
        if (clean === "") return
        addressCopyProc.pendingText = Model.clipboardStdin(clean)
        addressCopyProc.command = Model.clipboardCommand(clean)
        addressCopyProc.stdinEnabled = true
        addressCopyProc.running = true
    }

    // Shared by Account and Remembered overrides (52.11) — one editor path.
    function openConfigEditor() {
        if (root.configPath === "") return
        Quickshell.execDetached(["omarchy", "launch", "config", "editor", root.configPath])
    }

    // setPaused flips the daemon pause flag through a DEDICATED process
    // (never the shared callProc — a concurrent /status refresh must not
    // swallow the pause call). No optimistic flip: the knob follows daemon
    // truth; failures surface in root.errorMessage.
    // NOTE: ToggleSwitch never flips itself (caller-owned `checked`), so at
    // click time checked still holds the OLD value — and for this power
    // switch checked === !paused, hence want === checked (NOT negated).
    function setPaused(want) {
        if (root.pausing || root.busy) return
        root.pausing = true
        root.errorMessage = ""
        console.warn(Model.LOG_TAG + " pause requested: " + want)
        var pauseBody = JSON.stringify({ paused: want })
        var pausePayload = Model.stdinPayload(pauseBody)
        pauseProc.pendingBody = pausePayload
        pauseProc.stdinEnabled = pausePayload !== ""
        pauseProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.PAUSE, Model.Method.POST, pauseBody)
        pauseProc.onDone = function(raw) {
            root.pausing = false
            try {
                var o = JSON.parse(raw)
                if (typeof o.paused === "boolean") {
                    root.paused = o.paused
                    root.refresh()
                } else {
                    root.fail("Pause failed: " + raw)
                    root.refresh()
                }
            } catch (e) {
                root.fail("Pause failed: " + raw)
                root.refresh()
            }
        }
        pauseProc.running = true
    }

    // Clipboard copies do not share pendingText (019.1): address and the
    // MFA portal URL are different operations. Text is written on stdin after
    // started; argv stays ["wl-copy"]. A missing wl-copy stays silent here
    // (no copied text in errorMessage).
    Process {
        id: addressCopyProc
        property string pendingText: ""
        stdinEnabled: false
        onStarted: {
            if (pendingText !== "") {
                write(pendingText)
                pendingText = ""
                stdinEnabled = false
            }
        }
    }

    Process {
        id: portalCopyProc
        property string pendingText: ""
        stdinEnabled: false
        onStarted: {
            if (pendingText !== "") {
                write(pendingText)
                pendingText = ""
                stdinEnabled = false
            }
        }
    }

    Process {
        id: callProc
        property var onDone: null
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector { onStreamFinished: if (callProc.onDone) callProc.onDone(this.text) }
        stderr: StdioCollector { }
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => { if (code !== 0 && callProc.onDone) callProc.onDone(Model.daemonOffline()) }
    }

    Process {
        id: pauseProc
        property var onDone: null
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector { onStreamFinished: if (pauseProc.onDone) pauseProc.onDone(this.text) }
        stderr: StdioCollector { }
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => { if (code !== 0 && pauseProc.onDone) pauseProc.onDone(Model.daemonOffline()) }
    }

    // Dedicated logout call (017.16 endpoint, 019.1 race fix): logout must
    // never share callProc — a concurrent refresh would swallow it and wedge
    // busy (same lesson as pauseProc above).
    Process {
        id: logoutProc
        property var onDone: null
        stdout: StdioCollector { onStreamFinished: if (logoutProc.onDone) logoutProc.onDone(this.text) }
        stderr: StdioCollector { }
        onExited: (code) => { if (code !== 0 && logoutProc.onDone) logoutProc.onDone(Model.daemonOffline()) }
    }

    // One Process per operation class (callStatus/callOverride above) — same
    // reason pauseProc/logoutProc are dedicated; MFA has its own in the controller.
    Process {
        id: statusProc
        property var onDone: null
        stdout: StdioCollector { onStreamFinished: if (statusProc.onDone) statusProc.onDone(this.text) }
        stderr: StdioCollector { }
        onExited: (code) => { if (code !== 0 && statusProc.onDone) statusProc.onDone(Model.daemonOffline()) }
    }

    Process {
        id: overrideProc
        property var onDone: null
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector { onStreamFinished: if (overrideProc.onDone) overrideProc.onDone(this.text) }
        stderr: StdioCollector { }
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => { if (code !== 0 && overrideProc.onDone) overrideProc.onDone(Model.daemonOffline()) }
    }

    // Plugin config file: read on load / watched change; written atomically on
    // remember. applyConfig never writes (no write→watch→write loop).
    FileView {
        id: configFile
        path: root.configPath
        watchChanges: true
        printErrors: false
        atomicWrites: true
        onLoaded: root.applyConfig(text())
        onLoadFailed: root.applyConfig("") // missing config → empty list
        onFileChanged: reload()
        onSaveFailed: root.fail("Could not save plugin config")
    }

    // Release stamp (41.3): build-info.json shipped next to this panel by the
    // bundle. Missing or malformed → "" → no stamp row (an install from before
    // 41.3 must not error). Read-only; formatting lives in Model.buildInfoLabel /
    // pluginVersionLabel (52.16 short SETUP line).
    FileView {
        id: buildInfoFile
        path: Qt.resolvedUrl("build-info.json")
        printErrors: false
        onLoaded: {
            var raw = text()
            root.pluginStamp = Model.buildInfoLabel(raw)
            root.pluginVersion = Model.pluginVersionLabel(raw)
        }
        onLoadFailed: {
            root.pluginStamp = ""
            root.pluginVersion = ""
        }
    }

    function applyConfig(raw) {
        var cfg = Model.parseGatewayConfig(raw)
        if (cfg.ok && cfg.error === "")
            root.configNetwork = cfg.paymentNetwork
        root.rememberedUrls = cfg.rememberedUrls
        root.configLoaded = true
        console.warn(Model.LOG_TAG + " config loaded: " + root.rememberedUrls.length +
                     " remembered URL(s), network " + root.configNetwork +
                     (cfg.error !== "" ? " (" + cfg.error + ")" : ""))
    }

    // writeConfig persists the remembered list, preserving the file's network.
    function writeConfig(list) {
        configFile.setText(Model.serializeGatewayConfig({
            paymentNetwork: root.configNetwork,
            rememberedUrls: list
        }))
    }

    // Fail-closed: unknown URL always asks (configLoaded guards the first read).
    function isRemembered(url) {
        if (!root.configLoaded || !url) return false
        for (var i = 0; i < root.rememberedUrls.length; i++)
            if (root.rememberedUrls[i].url === url) return true
        return false
    }

    function remember(url, approvedMicro) {
        if (!url || !Model.isValidRememberedUrl(url)) return
        if (!root.configLoaded) {
            // Don't write before the first read: we'd persist the default
            // network over the file's real one. The window is tiny.
            console.warn(Model.LOG_TAG + " remember ignored: config not loaded yet")
            return
        }
        var micro = (typeof approvedMicro === "number" && approvedMicro > 0) ? Math.round(approvedMicro) : 0
        var list = root.rememberedUrls.slice()
        var found = false
        for (var i = 0; i < list.length; i++) {
            if (list[i].url === url) {
                // Re-approval (e.g. a higher price the user accepted): keep the
                // original "added" stamp, record the new approved amount.
                list[i] = { url: url, added: list[i].added || new Date().toISOString(), approvedMicro: micro }
                found = true
            }
        }
        if (!found)
            list.push({ url: url, added: new Date().toISOString(), approvedMicro: micro })
        if (list.length > Model.MAX_REMEMBERED_URLS)
            list = list.slice(list.length - Model.MAX_REMEMBERED_URLS)
        root.writeConfig(list)
        // Optimistic state: the watcher reload may be dropped while the write
        // is in flight (FileView `saved`, not `loaded`), so update the count
        // and dedup source now.
        root.rememberedUrls = list
        // Log the endpoint without userinfo/query/fragment (may carry secrets).
        console.warn(Model.LOG_TAG + (found ? " remembered URL updated: " : " remembered URL added: ") + url.split(/[?#]/)[0].replace(/\/\/[^@/]*@/, "//"))
    }

    // Keep counters fresh while the panel stays open (agent payments land
    // without any panel action). Skipped mid-operation to avoid clobbering —
    // including pausing: a stale /status landing after setPaused would
    // overwrite daemon truth (review P2).
    Timer {
        interval: root.pollIntervalMs
        running: root.opened && !root.busy && !root.pausing
        repeat: true
        onTriggered: root.refresh()
    }

    KeyboardPanel {
        id: panel
        anchorItem: root.anchorItem
        owner: root.hostWidget || root
        bar: root.bar
        open: root.opened
        focusTarget: keyCatcher
        contentWidth: panel.fittedContentWidth(Style.space(320))
        contentHeight: panel.fittedContentHeight(content.implicitHeight)

        PanelKeyCatcher {
            id: keyCatcher
            anchors.fill: parent
            onCloseRequested: root.close()
        }

        Column {
            id: content
            width: parent.width
            spacing: Style.space(12)

            // ---- Hero: Status row — balance + chip + power (43.1) ----
            HeroSection {
                width: parent.width
                heroLabel: root.heroLabel
                heroColor: root.heroColor
                paused: root.paused
                pausing: root.pausing
                busy: root.busy
                online: root.online
                bar: root.bar
                working: root.busy || root.pausing
                balanceText: Model.formatUsdcExact(root.balanceNum)
                onTogglePause: function(checked) { root.setPaused(checked) }
            }

            // ---- Offline: one readable state (bluetooth-panel pattern);
            // content sections below hide while the daemon is unreachable.
            Text {
                width: parent.width
                visible: !root.online
                text: "Offline — waiting for the daemon."
                color: Model.Palette.offline
                font.pixelSize: Style.font.bodySmall
                wrapMode: Text.WordWrap
            }

            // ---- Alert slot (single component): daemon-driven notices
            // (over-budget, clock skew, blocked). Not dismissable — daemon
            // truth returns on the next refresh anyway. Local action errors
            // live in the second AlertBanner near the wizard (39.2).
            AlertBanner {
                text: root.alertText
                tone: "warn"
                dismissable: false
            }

            PanelSeparator { visible: root.online }

            // ---- Daily budget (quiet card after Status, 43.2) ----
            BudgetsSection {
                visible: root.online
                spendToday: root.spendToday
                capDaily: root.capDaily
                onSaveRequested: function(usd) { root.errorMessage = ""; mfa.saveCap(usd) }
            }

            PanelSeparator { visible: root.walletAddress !== "" && root.step !== 0 && root.step !== 1 && root.online }

            // ---- SETUP: Account + AI Agents + Remembered overrides (52.13) ----
            CollapsibleSection {
                visible: root.walletAddress !== "" && root.step !== 0 && root.step !== 1 && root.online
                width: parent.width
                title: "SETUP"
                iconText: Model.ICON_SETUP
                showChevron: false
                bodyIndent: Style.space(18)
                expanded: root.advancedExpanded
                onToggle: root.advancedExpanded = !root.advancedExpanded

                // Flat Account block (no nested ▸) — Budgets/Agents language.
                WalletSection {
                    width: parent.width
                    walletAddress: root.walletAddress
                    paymentNetwork: root.paymentNetwork
                    mfaEnrolled: root.mfaEnrolled
                    mfaMethod: root.mfaMethod
                    busy: root.busy
                    mfaBusy: mfa.busy
                    onCopyAddress: function() { root.copyAddress() }
                    onStartMfaEnroll: function() { mfa.startMfaEnroll() }
                    onOpenMfaReset: function() { mfa.openMfaReset() }
                    onRequestLogout: function() { root.confirmLogoutDialog = true }
                }

                // Static CAPS label — list lives in AgentSection.
                Text {
                    text: "AI AGENTS"
                    color: Qt.darker(Color.foreground, 1.45)
                    font.family: Style.font.family
                    font.pixelSize: Style.font.caption
                    font.letterSpacing: 1
                    font.bold: true
                }

                AgentSection {
                    width: parent.width
                    scriptPath: root.agentScriptPath
                    onFailed: function(msg) { root.fail(msg) }
                }

                BlockedSection {
                    id: blockedSection
                    width: parent.width
                    visible: root.blockedRows.length > 0
                    rows: root.blockedRows
                    socketPath: root.socketPath
                    onRefresh: root.refresh()
                    onFailed: function(msg) { root.fail(msg) }
                }

                HistorySection {
                    id: historySection
                    width: parent.width
                    socketPath: root.socketPath
                    expanded: root.historyExpanded
                    onToggle: {
                        root.historyExpanded = !root.historyExpanded
                        if (root.historyExpanded)
                            historySection.reload()
                    }
                }

                OverrideSection {
                    id: overrideSection
                    width: parent.width
                    count: root.rememberedUrls.length
                    configPath: root.configPath
                    onOpenConfig: function() { root.openConfigEditor() }
                }

                // Installed plugin version (52.16): compare with catalog/Releases;
                // update remains attested install.sh (not omarchy plugin update).
                Text {
                    width: parent.width
                    visible: root.pluginVersion !== ""
                    text: root.pluginVersion
                    color: Color.foreground
                    opacity: 0.55
                    font.pixelSize: Style.font.caption
                }
            }

            PanelSeparator {
                visible: wizard.step === 0 || wizard.step === 1 || wizard.step === 2
                         || (wizard.step === 3 && wizard.doneSeen)
                         || root.errorMessage !== ""
                         || (root.panelDebug && (root.pluginStamp !== "" || root.daemonVersion !== ""))
            }

            // ---- Wizard steps (onboarding / edit limits) ----
            WizardSteps {
                id: wizard
                step: root.step
                busy: root.busy
                budgetPrefill: Model.formatUsdExact(root.capDaily)
                onSubmitEmail: function(email) { root.submitEmail(email) }
                onSubmitOtp: function(otp) { root.submitOtp(otp) }
                onSaveBudget: function(usd) { root.saveBudgetValue(usd) }
                onValidationFailed: function(msg) { root.errorMessage = msg }
            }

            // ---- Local action errors (validation, save failures): second
            // AlertBanner instance near the wizard that caused them (39.2).
            // Same component as the top banner — one visual pattern, two
            // placements (global truth vs action proximity carry disjoint
            // content, so merging positions would hurt, not help).
            AlertBanner {
                text: root.errorMessage
                tone: "error"
                dismissable: true
                onDismissed: function() { root.errorMessage = "" }
            }

            Text {
                width: parent.width
                // Debug-only stamp (43.4): keep reading build-info + /status version;
                // hide the row unless GATEWAY_PANEL_DEBUG=1.
                visible: root.panelDebug && (root.pluginStamp !== "" || root.daemonVersion !== "")
                text: {
                    var parts = []
                    if (root.pluginStamp !== "") parts.push(root.pluginStamp)
                    if (root.daemonVersion !== "") parts.push("daemon v" + root.daemonVersion)
                    return parts.join(" · ")
                }
                color: Color.foreground
                opacity: 0.7
                font.pixelSize: Style.font.caption
            }

            // ---- Over-budget approval dialog (visibility follows state) ----
            OverrideConfirmDialog {
                id: confirmDialog
                opened: root.pendingOverride !== null
                amountUsd: root.pendingOverride ? root.pendingOverride.amountUsd : 0
                targetUrl: root.pendingOverride ? root.pendingOverride.targetUrl : ""
                budgetCapUsd: root.pendingOverride ? root.pendingOverride.budgetCapUsd : 0
                priceChanged: root.pendingOverride ? root.pendingOverride.priceChanged === true : false
                previousUsd: root.pendingOverride ? root.pendingOverride.previousUsd : 0
                code: root.pendingOverride ? root.pendingOverride.code || "" : ""
                busy: root.busy
                onAccepted: function(remember) { root.acceptOverride(remember) }
                onDismissed: function() { root.cancelOverride() }
            }

            // ---- Logout confirmation (019.2): lives in the panel window
            // like OverrideConfirmDialog above — a root-child overlay has no
            // window geometry and never shows. Zero positional anchors in
            // Column children (else "Column will not function").
            Item {
                width: parent.width
                height: root.confirmLogoutDialog ? logoutCard.height : 0
                visible: root.confirmLogoutDialog
                // Modal focus: Esc bubbles from the buttons up to here.
                // Blocked while busy — same freeze as the other dialogs (39.1).
                focus: root.confirmLogoutDialog
                Keys.onEscapePressed: if (root.confirmLogoutDialog && !root.busy) root.confirmLogoutDialog = false

                Rectangle {
                    anchors.fill: parent
                    color: Util.alpha(Color.background, 0.7)

                    MouseArea {
                        anchors.fill: parent
                        onClicked: if (!root.busy) root.confirmLogoutDialog = false
                    }
                }

                BorderSurface {
                    id: logoutCard
                    width: Math.min(parent.width - Style.space(24), Style.space(300))
                    height: logoutCol.implicitHeight + Style.space(24)
                    anchors.centerIn: parent
                    color: Color.background
                    padding: Style.space(12)
                    radius: Style.cornerRadius

                    Column {
                        id: logoutCol
                        anchors.fill: parent
                        anchors.topMargin: logoutCard.contentTopInset
                        anchors.rightMargin: logoutCard.contentRightInset
                        anchors.bottomMargin: logoutCard.contentBottomInset
                        anchors.leftMargin: logoutCard.contentLeftInset
                        spacing: Style.space(8)

                        Text {
                            width: parent.width
                            horizontalAlignment: Text.AlignHCenter
                            text: "Logout?"
                            font.pixelSize: Style.font.subtitle
                            font.bold: true
                            color: Color.foreground
                        }

                        Text {
                            width: parent.width
                            horizontalAlignment: Text.AlignHCenter
                            wrapMode: Text.WordWrap
                            text: "Clear this device's session. You'll need to sign in again."
                            font.pixelSize: Style.font.caption
                            color: Color.foreground
                            opacity: 0.7
                        }

                        Row {
                            anchors.right: parent.right
                            spacing: Style.space(8)

                            Button {
                                text: "Cancel"
                                focusable: true
                                enabled: !root.busy
                                onClicked: if (!root.busy) root.confirmLogoutDialog = false
                            }
                            Button {
                                text: "Logout"
                                color: Model.Palette.error
                                focusable: true
                                enabled: !root.busy
                                onClicked: if (!root.busy) root.doLogout()
                            }
                        }
                    }
                }
            }
        }
    }

    // MFA runs in a standalone popout window (qs.Ui.PopupCard), NOT in the
    // panel: the enroll card (QR + key + code) is taller than the panel can fit,
    // which clipped the OTP field. State and calls live in MfaController (28.4).
    MfaPopup {
        id: mfaPopup
        open: mfa.dialogOpen
        mode: mfa.dialogMode
        qrDataUri: mfa.qrDataUri
        secret: mfa.secret
        reasonLines: mfa.reasonLines
        busy: mfa.busy
        onSubmitCode: function(code) { mfa.submitMfaCode(code) }
        onCopySecret: function() { mfa.copyMfaSecret() }
        onCopyPortalUrl: function() {
            var text = Model.clipboardStdin(Model.MFA_RESET_URL)
            if (text === "") return
            portalCopyProc.pendingText = text
            portalCopyProc.command = Model.clipboardCommand(Model.MFA_RESET_URL)
            portalCopyProc.stdinEnabled = true
            portalCopyProc.running = true
        }
        onOpenPortal: function() { Quickshell.execDetached(Model.openUrlCommand(Model.MFA_RESET_URL)) }
        onDismissed: function() { mfa.closeMfaDialog() }
    }

    function saveBudgetValue(usd) {
        root.errorMessage = ""
        // Persist to shell.json layout entry (native Omarchy settings surface).
        // Legacy bucket keys are dropped; only dailyCapUsd is written.
        var entry = { id: root.moduleName }
        for (var k in root.settings) {
            if (k === "id" || k === "ppvDailyCapUsd" || k === "ppcDailyCapUsd") continue
            entry[k] = root.settings[k]
        }
        entry.dailyCapUsd = usd
        root.settings = entry

        // Persist the Omarchy layout entry only after the daemon accepted the
        // cap: a sudo-blocked change must not leave shell.json ahead of the
        // daemon's policy (26.2).
        mfa.saveCap(usd, function() {
            if (root.bar && root.bar.shell && typeof root.bar.shell.updateEntryInline === "function")
                root.bar.shell.updateEntryInline(root.moduleName, entry)
            console.warn(Model.LOG_TAG + " budget saved: " + Model.formatUsdExact(usd))
        })
    }

    // payOverride executes an approved payment. `approved` is true ONLY for
    // fresh dialog Pay clicks: it lands unknown sellers and bypasses the
    // per-seller sub-cap. Silent paths (remembered auto-pay) must pass false
    // so unknown/raised sellers force a dialog instead of paying quietly.
    // It takes the override object explicitly (not root.pendingOverride) so the
    // remembered auto-pay path and the dialog path share one code path without
    // racing on mutable panel state.
    function payOverride(po, remember, approved) {
        if (!po) return
        root.busy = true
        root.errorMessage = ""
        callOverride(JSON.stringify({
            url: po.url,
            method: Model.Method.GET,
            override_amount_micro: po.amountMicro,
            domain: po.targetUrl,
            approve_seller: approved === true
        }), function(raw) {
            root.busy = false
            var gate = Model.mfaGateFromError(raw)
            if (gate.mode !== "") {
                mfa.openSudoGate(gate, Model.mfaSudoReason("override", 0, po.amountUsd, root.mfaEnrolled),
                                 { kind: "override", po: po, remember: remember, approved: approved })
                root.refresh()
                return
            }
            try {
                var o = JSON.parse(raw)
                var code = Model.errorCode(o)
                if (code) {
                    if (code === "price_changed") {
                        // Seller raised the price above the approved amount: not
                        // a failure — remember the old amount and let the poll
                        // open a fresh approval dialog for the new price (ad 1).
                        console.warn(Model.LOG_TAG + " price changed; re-approval required @" + po.targetUrl)
                        root.priceChangeFrom = { url: po.url, previousUsd: po.amountUsd }
                    } else {
                        root.fail("Override failed: " + (Model.errorDetail(o) || code))
                    }
                } else {
                    console.warn(Model.LOG_TAG + " override paid: " + Model.formatUsdcExact(po.amountUsd) + " @ " + po.targetUrl)
                    if (remember) root.remember(po.url, po.amountMicro)
                }
            } catch (e) {
                root.fail("Override response parse failed: " + raw)
            }
            root.refresh()
        })
    }

    // Dialog Pay: close first (binding clears), then run the payment as a
    // fresh explicit approval (lands unknown sellers, bypasses sub-cap).
    function acceptOverride(remember) {
        var po = root.pendingOverride
        root.pendingOverride = null
        root.payOverride(po, remember, true)
    }

    function cancelOverride() {
        if (root.pendingOverride)
            console.warn(Model.LOG_TAG + " override denied: " + Model.formatUsdcExact(root.pendingOverride.amountUsd) + " @ " + root.pendingOverride.targetUrl)
        root.pendingOverride = null
        root.priceChangeFrom = null
    }

    // ---- Logout (017.16; dialog w oknie — patrz blok przy OverrideConfirmDialog) ----

    property bool confirmLogoutDialog: false

    function doLogout() {
        if (root.busy || root.pausing) return
        root.confirmLogoutDialog = false
        root.busy = true; root.errorMessage = ""
        root.pendingOverride = null
        mfa.closeMfaDialog()
        logoutProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.PAIR_LOGOUT, Model.Method.POST, "")
        logoutProc.onDone = function(raw) {
            root.busy = false
            var r = Model.parseLogoutResponse(raw)
            if (r.ok) {
                root.step = 0
                root.refresh()
            } else {
                root.fail("Logout failed: " + r.text)
            }
        }
        logoutProc.running = true
    }
}
