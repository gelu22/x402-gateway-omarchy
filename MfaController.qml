// MfaController.qml — MFA state, socket calls and the sudo gate (28.4).
//
// Extracted from Panel.qml, which had grown to 1164 LOC and held wizard, budgets,
// MFA, override, logout and the agent section at once. This object owns:
//   * the enroll/verify/reset dialog state consumed by MfaPopup,
//   * the two Processes it needs (MFA endpoints, policy POST),
//   * the sudo gate flow from 26.2 — denial → popup → code → replay of the exact
//     mutation the user asked for.
//
// It never reaches into Panel's state: everything it needs arrives as a bound
// property, and every side effect it cannot perform itself comes back as a
// signal (busy indicator, error banner, popup error text, refresh, override
// replay, enrollment change). Decisions stay in Model.js pure functions.
import QtQuick
import Quickshell
import Quickshell.Io
import "Model.js" as Model

QtObject {
    id: root

    // ---- inputs bound from the panel ----
    property string socketPath: ""
    property real capUsd: 0
    property real balanceUsd: 0
    // Enrollment is owned by the panel (it comes from /status); this mirror only
    // lets the controller pick the right copy and enroll-vs-verify mode.
    property bool enrolled: false

    // ---- dialog state consumed by MfaPopup (instantiated in Panel.qml) ----
    property bool dialogOpen: false
    property string dialogMode: "enroll"    // "enroll" | "verify" | "reset"
    property string qrDataUri: ""
    property string secret: ""
    property bool busy: false
    property var reasonLines: []

    // Dedup of the payment denial (24.4 + 30.1): the identity of the denial that
    // was last surfaced/dismissed, not a timestamp — the daemon stamps a fresh
    // time on every attempt, so an agent retrying in a loop would re-open the
    // popup forever. Key is Model.mfaNagKey (code|seller|amount).
    property string dismissedMfaKey: ""
    property double dismissedAtMs: 0
    // Key of the denial currently on screen; recorded as dismissed on close.
    property string currentNagKey: ""

    // A sudo-gated mutation waiting for a code (26.2). Replayed verbatim after a
    // successful verification; null outside that window.
    property var pendingSudo: null

    // ---- effects the panel applies ----
    signal refreshRequested()
    signal busyRequested(bool value)
    signal panelErrorRequested(string message)
    signal errorTextRequested(string message)
    signal enrolledUpdated(bool enrolled, string method)
    signal overrideReplayRequested(var po, bool remember, bool approved)

    // Process objects mirror Panel.qml's pattern: the callback slot is declared
    // as a dynamic property ("property var onDone"), not assigned — assigning
    // onDone bare is parsed as a signal handler and QML rejects it with
    // "Cannot assign to non-existent property".
    property Process policyProc: Process {
        id: policyProcess
        property var onDone: null
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector { onStreamFinished: if (policyProcess.onDone) policyProcess.onDone(this.text) }
        stderr: StdioCollector { }
        // 50.1: write only after started (write before that is a no-op), then
        // close stdin so curl --data-binary @- sees EOF.
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => { if (code !== 0 && policyProcess.onDone) policyProcess.onDone(Model.daemonOffline()) }
    }

    property Process mfaProc: Process {
        id: mfaProcess
        property var onDone: null
        property string pendingBody: ""
        stdinEnabled: false
        stdout: StdioCollector { onStreamFinished: if (mfaProcess.onDone) mfaProcess.onDone(this.text) }
        stderr: StdioCollector { }
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
                stdinEnabled = false
            }
        }
        onExited: (code) => { if (code !== 0 && mfaProcess.onDone) mfaProcess.onDone(Model.daemonOffline()) }
    }

    // Dedicated process: sharing mfaProc would let a copy clobber a code submit
    // (019.1). The secret is written to stdin, never passed as an argument.
    // QtObject has no default property, so this must be a declared property
    // (same shape as policyProc/mfaProc), not a child object.
    property Process copyProc: Process {
        id: copyProcess
        stdinEnabled: false
        onStarted: {
            if (secret !== "") {
                write(secret)
                stdinEnabled = false
            }
        }
        onExited: (code) => {
            if (code !== 0)
                errorTextRequested("Could not copy the authenticator secret")
        }
    }

    function callPolicy(body, onDone) {
        var payload = Model.stdinPayload(body)
        policyProc.pendingBody = payload
        policyProc.stdinEnabled = payload !== ""
        policyProc.command = Model.buildCommand(socketPath, Model.Endpoint.POLICY, Model.Method.POST, body)
        policyProc.onDone = onDone
        policyProc.running = true
    }

    function callMfa(path, body, onDone) {
        var payload = Model.stdinPayload(body)
        mfaProc.pendingBody = payload
        mfaProc.stdinEnabled = payload !== ""
        mfaProc.command = Model.buildCommand(socketPath, path, Model.Method.POST, body)
        mfaProc.onDone = onDone
        mfaProc.running = true
    }

    // surfacePaymentMfa opens the verify dialog once per mfa_required denial from
    // /status (24.4): the timestamp dedup survives dismiss and panel close.
    function surfacePaymentMfa(lfe) {
        if (!lfe || lfe.code !== "mfa_required" || dialogOpen) return
        var key = Model.mfaNagKey(lfe)
        if (!Model.shouldSurfaceMfa(dismissedMfaKey, dismissedAtMs, key, Date.now(), Model.MFA_REPROMPT_COOLDOWN_MS))
            return
        currentNagKey = key
        reasonLines = Model.mfaVerifyReason(lfe, capUsd, balanceUsd)
        console.warn(Model.LOG_TAG + " mfa_required: verification needed")
        openMfaVerify()
    }

    function startMfaEnroll() {
        busy = true; qrDataUri = ""; secret = ""
        callMfa(Model.Endpoint.MFA_ENROLL_INIT, "", function(raw) {
            busy = false
            try {
                var o = JSON.parse(raw)
                if (!o.qr_data_uri || !o.secret) {
                    // Popup is still closed — surface the failure in the panel
                    // (a closed popup would hide it entirely).
                    panelErrorRequested("Enrollment failed: " + (Model.errorDetail(o) || Model.errorCode(o) || raw))
                    return
                }
                qrDataUri = o.qr_data_uri
                secret = o.secret
                dialogMode = "enroll"
                dialogOpen = true
            } catch (e) { panelErrorRequested("Enrollment failed: " + raw) }
        })
    }

    function openMfaVerify() {
        qrDataUri = ""; secret = ""
        dialogMode = "verify"
        dialogOpen = true
        callMfa(Model.Endpoint.MFA_VERIFY_INIT, "", function(raw) {
            try {
                var o = JSON.parse(raw)
                if (Model.errorCode(o)) errorTextRequested("Could not start verification: " + (Model.errorDetail(o) || Model.errorCode(o)))
            } catch (e) { errorTextRequested("Could not start verification.") }
        })
    }

    // openSudoGate routes a sudo-gated denial (26.2) into the shared popup: mode
    // comes from the daemon code (enroll vs verify), context from
    // Model.mfaSudoReason, and the pending mutation is replayed afterwards.
    function openSudoGate(gate, lines, pending) {
        console.warn(Model.LOG_TAG + " sudo gate: " + gate.code)
        pendingSudo = pending
        reasonLines = lines
        if (gate.mode === "enroll") startMfaEnroll()
        else openMfaVerify()
    }

    // replaySudo re-sends the request the user already asked for, after the code
    // landed. Enroll can still come back mfa_stale (CDP may not count enrollment
    // as verification) — then the gate opens again in verify mode.
    function replaySudo(pending) {
        if (!pending) return
        if (pending.kind === "policy") {
            busyRequested(true)
            postCap(pending.body, pending.reasonLines, pending.onSaved)
        } else if (pending.kind === "override") {
            overrideReplayRequested(pending.po, pending.remember, pending.approved)
        }
    }

    // postCap posts a cap body and turns a sudo denial into the MFA gate instead
    // of a fake success: curl exits 0 on 403, so a blocked save used to parse as
    // valid JSON and look saved (26.2).
    function postCap(body, lines, onSaved) {
        callPolicy(body, function(raw) {
            busyRequested(false)
            var gate = Model.mfaGateFromError(raw)
            if (gate.mode !== "") {
                openSudoGate(gate, lines, { kind: "policy", body: body, reasonLines: lines, onSaved: onSaved })
                return
            }
            var o = null
            try { o = JSON.parse(raw) } catch (e) { o = null }
            if (o && Model.errorCode(o)) {
                panelErrorRequested("Saving limits failed: " + (Model.errorDetail(o) || Model.errorCode(o)))
                return
            }
            if (!o) { panelErrorRequested("Saving limits failed: " + raw); return }
            if (onSaved) onSaved()
            refreshRequested()
        })
    }

    // saveCap is the single entry point for a cap change (panel section + wizard
    // step), so the sudo gate behaves identically on both paths.
    function saveCap(usd, onSaved) {
        busyRequested(true)
        var body = JSON.stringify({ daily_cap_micro_usdc: Model.usdToMicro(usd) })
        postCap(body, Model.mfaSudoReason("policy", capUsd, usd, enrolled), onSaved)
    }

    function submitMfaCode(code) {
        busy = true
        var path = (dialogMode === "verify")
                 ? Model.Endpoint.MFA_VERIFY_SUBMIT
                 : Model.Endpoint.MFA_ENROLL_SUBMIT
        callMfa(path, JSON.stringify({ mfa_code: code }), function(raw) {
            busy = false
            try {
                var o = JSON.parse(raw)
                if (Model.errorCode(o)) {
                    errorTextRequested((dialogMode === "verify" ? "Verification failed: " : "Enrollment failed: ")
                                       + (Model.errorDetail(o) || Model.errorCode(o)))
                    return
                }
                dialogOpen = false
                currentNagKey = ""
                // The user just solved the problem: a later denial for the same
                // pair must be allowed to ask again.
                dismissedMfaKey = ""
                if (dialogMode === "enroll") enrolledUpdated(true, "totp")
                // A sudo-gated mutation waited for this code (26.2): replay it
                // now instead of asking the user to repeat the action.
                var pending = pendingSudo
                pendingSudo = null
                if (pending !== null) replaySudo(pending)
                refreshRequested()
            } catch (e) { errorTextRequested("MFA response parse failed: " + raw) }
        })
    }

    // Dismissal remembers the denial's identity (30.1), never a timestamp: it
    // must survive close, or the same denial reopens the popup on every refresh
    // (the 57823ae regression), and it must be an identity, or a retry loop
    // reopens it on every attempt. A different denial has a different key.
    function closeMfaDialog() {
        if (currentNagKey !== "") {
            dismissedMfaKey = currentNagKey
            dismissedAtMs = Date.now()
            currentNagKey = ""
        }
        pendingSudo = null
        dialogOpen = false
        secret = ""
        qrDataUri = ""
        reasonLines = []
    }

    function copyMfaSecret() {
        if (secret === "") return
        copyProc.command = Model.secretClipboardCommand()
        copyProc.stdinEnabled = true
        copyProc.running = true
    }

    function openMfaReset() {
        qrDataUri = ""; secret = ""
        dialogMode = "reset"
        dialogOpen = true
    }
}
