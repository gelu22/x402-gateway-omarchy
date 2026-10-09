// Model.js — helpers shared by the Gateway plugin widgets.
// All daemon communication goes through `curl --unix-socket` subprocesses:
// no secrets and no business logic live in QML (AGENTS.md rule 7).
//
// Socket path resolution happens in QML via Quickshell.env() and is passed
// explicitly to these helpers.
.pragma library

// ---- Shared constants (008.1: single source, no magic literals) ----

// MICRO_USDC is the protocol unit (1 USDC = 1e6 micro). Conversion happens ONLY
// in usdToMicro/microToUsd below — QML must never multiply or divide by this
// constant itself (28.2), or a rounding mistake becomes a magnitude mistake.
var MICRO_USDC = 1000000

// usdToMicro converts a human amount to the protocol unit. Non-finite input
// becomes 0 (auto-pay off) rather than NaN: NaN would fail JSON encoding and
// look like a transport fault, while 0 is the safe, readable side. UI
// validation is expected to reject garbage long before this point. Pure.
function usdToMicro(usd) {
    var n = Number(usd)
    if (!isFinite(n)) return 0
    return Math.round(n * MICRO_USDC)
}

// microToUsd converts a protocol amount back to USDC for display. Non-finite
// input becomes 0. Formatting stays in formatUsdExact — this returns a number.
// Pure.
function microToUsd(micro) {
    var n = Number(micro)
    if (!isFinite(n)) return 0
    return n / MICRO_USDC
}

// MFA_BALANCE_WARN_RATIO is the share of the wallet balance at which the verify
// dialog warns that a single payment looks like a drain (26.3). Product
// decision, not logic — one named constant to change.
var MFA_BALANCE_WARN_RATIO = 0.5

// CURL_TIMEOUT_S bounds a normal daemon socket call (status, policy, MFA).
var CURL_TIMEOUT_S = 30

// CURL_TIMEOUT_WAIT_S bounds the calls that may legitimately WAIT for a human:
// /fetch and /fetch-override hold the request until the MFA code arrives
// (30.2b, daemon mfaWaitTimeout = 180 s), so the generic 30 s would cut the
// answer mid-wait. Keep this above the daemon's wait plus its grace.
var CURL_TIMEOUT_WAIT_S = 200

// SHORT_ADDR_MIN is the minimum address length worth shortening.
var SHORT_ADDR_MIN = 12

// LOG_TAG prefixes all plugin journal lines (grep-friendly).
var LOG_TAG = "[gelu22.gateway]"

// Panel colors are Omarchy theme roles resolved in Palette.qml. Model.js is
// plain JS with no access to the Color singleton, so it returns role NAMES
// (see statusRole/mfaRole/heroState), never colors or hex literals.

// ---- Shared enums/constants (010.1: single source, no magic literals) ----

// State keys (daemon session/UI state machine; labels live in stateLabel).
var State = {
    ACTIVE: "active",
    PAUSED: "paused",
    EXHAUSTED: "exhausted",
    LOGGED_OUT: "logged_out",
    OFFLINE: "offline",
    ERROR: "error"
}

// Daemon socket endpoints (CONTRACTS §1).
var Endpoint = {
    STATUS: "/status",
    POLICY: "/policy",
    PAUSE: "/pause",
    PAIR_INIT: "/pair/init",
    PAIR_VERIFY: "/pair/verify",
    PAIR_LOGOUT: "/pair/logout",
    FETCH_OVERRIDE: "/fetch-override",
    FETCH_APPROVE: "/fetch-approve",
    PERMISSIONS: "/permissions",
    MFA_ENROLL_INIT: "/mfa/enroll/init",
    MFA_ENROLL_SUBMIT: "/mfa/enroll/submit",
    MFA_VERIFY_INIT: "/mfa/verify/init",
    MFA_VERIFY_SUBMIT: "/mfa/verify/submit",
    HISTORY: "/history"
}

// MFA_RESET_URL is the CDP portal page for wallet/MFA settings (documented:
// docs.cdp.coinbase.com — Portal → wallets → non-custodial → authentication).
// CDP exposes no API to reset an end user's MFA, so the panel points the
// account owner here instead of pretending it can reset.
var MFA_RESET_URL = "https://portal.cdp.coinbase.com/wallets/non-custodial/authentication"

// Supported networks (CAIP-2).
var NET_SEPOLIA = "eip155:84532"
var NET_MAINNET = "eip155:8453"

// Display precision (decimal places) for money values.
var Precision = { MONEY: 2, SPEND: 3, BALANCE: 4 }

// Currency unit + symbol for display (single source).
var USDC = "USDC"
var USD_SYMBOL = "$"

// HTTP methods for daemon socket calls.
var Method = { GET: "GET", POST: "POST", DELETE: "DELETE" }

// ---- Shared literals (015.1: leftovers — single source, no magic literals) ----

// ICON_WALLET is the Nerd Font payment glyph (nf-fa-credit_card) — the widget
// and hero identity. Centralized so both stay the same icon.
var ICON_WALLET = "\uF09D"

// Section identity glyphs (40.2): Nerd Font (Font Awesome) codepoints, kept
// beside ICON_WALLET so every section icon lives in one place.
var ICON_BUDGET = "\uF0D6"   // nf-fa-money — classic FA (F53E coins is tofu on Omarchy Nerd Font)
var ICON_AGENTS = "\uF0C0"   // nf-fa-users — AI agent integration
var ICON_URLS = "\uF0C1"     // nf-fa-link — remembered overrides
var ICON_SETUP = "\uF013"    // nf-fa-cog — SETUP section (classic FA; not agents icon)
var ICON_HISTORY = "\uF1DA"  // nf-fa-history — payment history

// AGENT_SCRIPT_MISSING is the shared failure text when setup-agents.sh is
// absent (detect/integrate/remove all surface the same actionable message).
var AGENT_SCRIPT_MISSING = "Agent setup script not found"

// LOG_TRIM_RESULT caps inline script output kept in a single journal line;
// LOG_TRIM_PROC caps daemon stdout/stderr tails dumped at process exit.
var LOG_TRIM_RESULT = 300
var LOG_TRIM_PROC = 1500

// daemonOffline builds the synthetic daemon-offline error payload handed to
// proc callbacks when the curl subprocess itself exits non-zero.
function daemonOffline() {
    return '{"error":"daemon_offline"}'
}

// errorCode reads the error code from a daemon envelope (018.1: canonical
// pair is error/detail; the deprecated code fallback was cut in 018.5).
// Null-safe; "" when no code is present. Pure + unit-tested.
function errorCode(o) {
    if (!o || typeof o !== "object") return ""
    var c = o.error
    return (typeof c === "string") ? c : ""
}

// errorDetail reads the human text from a daemon envelope (canonical pair
// error/detail; the deprecated message fallback was cut in 018.5).
// Null-safe; "" when no text is present. Pure + unit-tested.
function errorDetail(o) {
    if (!o || typeof o !== "object") return ""
    var d = o.detail
    return (typeof d === "string") ? d : ""
}

// formatUsd renders a money value with fixed decimals (NaN-safe; default 2).
function formatUsd(value, decimals) {
    var n = Number(value)
    if (!isFinite(n)) n = 0
    return n.toFixed(decimals === undefined ? Precision.MONEY : decimals)
}

// formatUsdc renders "<amount> USDC" (amount NaN-safe via formatUsd).
function formatUsdc(value, decimals) {
    return formatUsd(value, decimals) + " " + USDC
}

// formatUsdExact renders micro-scale money without collapsing it to "0.00":
// 2 decimals minimum, up to 6, trailing zeros trimmed. x402 payments are
// micro-USDC, so a flat 2-decimal format shows a real $0.002 as "0.00".
// Idempotent on its own output. Pure + unit-tested.
function formatUsdExact(value) {
    var n = Number(value)
    if (!isFinite(n)) n = 0
    var s = n.toFixed(6)
    if (s.indexOf("e") >= 0) return formatUsd(n) // absurd magnitudes: don't guess
    s = s.replace(/0+$/, "")
    if (s.charAt(s.length - 1) === ".") s += "0"
    var dot = s.indexOf(".")
    if (dot < 0) return s + ".00"
    while (s.length - dot - 1 < 2) s += "0"
    return s
}

// formatUsdcExact renders "<amount> USDC" with formatUsdExact precision.
function formatUsdcExact(value) {
    return formatUsdExact(value) + " " + USDC
}

// budgetFraction is the share of the daily cap spent today (0..1) for the hero
// meter. NaN-safe, clamped to [0,1]; cap <= 0 (auto-pay off) returns 0 so the
// meter hides instead of showing a fake 0% bar. Pure + unit-tested.
function budgetFraction(spent, cap) {
    var s = Number(spent), c = Number(cap)
    if (!isFinite(s) || !isFinite(c) || c <= 0) return 0
    var f = s / c
    if (!isFinite(f)) return 0
    return Math.max(0, Math.min(1, f))
}

// budgetRemaining is the unspent part of the daily cap (cap - spent), clamped
// to >= 0 so an over-cap day reads $0.00, not a negative. cap <= 0 (auto-pay
// off) returns 0 — the caller shows "—" instead of a fake $0.00. NaN-safe.
// Pure + unit-tested.
function budgetRemaining(spent, cap) {
    var s = Number(spent), c = Number(cap)
    if (!isFinite(s) || !isFinite(c) || c <= 0) return 0
    var r = c - s
    if (!isFinite(r)) return 0
    return Math.max(0, r)
}

// buildInfoLabel renders the release stamp shipped in build-info.json
// ({version, git_sha}) as "plugin vX.Y.Z (abc1234)" for the panel footer.
// Returns "" when the text is empty, malformed, or carries no version — the
// panel then renders no stamp (older installs), never a crash. Pure + tested.
function buildInfoLabel(raw) {
    if (raw === undefined || raw === null) return ""
    var s = String(raw).trim()
    if (s === "") return ""
    var o = null
    try { o = JSON.parse(s) } catch (e) { return "" }
    if (!o || typeof o !== "object") return ""
    var v = String(o.version || "").trim()
    if (v === "") return ""
    var sha = String(o.git_sha || "").trim().slice(0, 7)
    return sha !== "" ? "plugin v" + v + " (" + sha + ")" : "plugin v" + v
}

// pluginVersionLabel is the short SETUP stamp: "plugin vX.Y.Z" without sha
// (compare with catalog / Releases). Same fail-closed empty cases as buildInfoLabel.
function pluginVersionLabel(raw) {
    if (raw === undefined || raw === null) return ""
    var s = String(raw).trim()
    if (s === "") return ""
    var o = null
    try { o = JSON.parse(s) } catch (e) { return "" }
    if (!o || typeof o !== "object") return ""
    var v = String(o.version || "").trim()
    if (v === "") return ""
    return "plugin v" + v
}

// footerVersionLabel prefers the live daemon /status version (updates with the
// running binary); falls back to the stamped plugin build-info line.
function footerVersionLabel(pluginVersion, daemonVersion) {
    var d = String(daemonVersion || "").trim()
    if (d !== "") return "gateway v" + d
    return String(pluginVersion || "")
}

// historyFilePath is where the panel writes readable payment history for the
// system editor (not inline in the narrow SETUP column).
function historyFilePath(home) {
    if (typeof home !== "string" || home === "" || home.charAt(0) !== "/") return ""
    if (home.indexOf("\0") >= 0 || home.indexOf("..") >= 0) return ""
    return home.replace(/\/+$/, "") + "/.local/state/x402-gateway/history.txt"
}

// historyDocument formats /history entries as a plain-text file body.
function historyDocument(entries, nowMs) {
    var rows = historyRows(entries, nowMs)
    if (rows.length === 0) return "No payments yet.\n"
    var lines = ["Payment history", ""]
    for (var i = 0; i < rows.length; i++) {
        var r = rows[i]
        var star = r.override ? " *" : ""
        lines.push(
            r.when + "  " + r.agent + "  " + r.domain + "  $" + r.amount
            + "  " + r.outcome + star
        )
    }
    return lines.join("\n") + "\n"
}

// openEditorCommand opens a local path in the Omarchy config editor (same path
// as "Edit in config"). Absolute path only; rejects .. and empty.
function openEditorCommand(path) {
    if (typeof path !== "string" || path === "") return null
    if (path.charAt(0) !== "/") return null
    if (path.indexOf("..") >= 0 || path.indexOf("\0") >= 0) return null
    return ["omarchy", "launch", "config", "editor", path]
}

// panelDebugEnabled gates the panel version stamp (43.4). Fail-closed: only
// the exact string "1" enables it; missing/empty/other values stay hidden.
function panelDebugEnabled(envVal) {
    // Strict equality — env from Quickshell is a string; never coerce 1/"true".
    return envVal === "1"
}

// clipboardCommand returns the argv that copies text to the Wayland
// clipboard. The text is NOT in argv: the caller writes clipboardStdin(text)
// to the process stdin after `started` (same path as the TOTP secret).
// Pure — .pragma library cannot touch Quickshell.
function clipboardCommand(text) {
    if (!text) return null
    return secretClipboardCommand()
}

// clipboardStdin is the only string a clipboard Process may write. Empty
// when there is nothing to copy. Never put this return value into argv.
function clipboardStdin(text) {
    if (!text) return ""
    return String(text)
}

// secretClipboardCommand is the argv for copying a TOTP enrollment secret.
// It takes no text: the secret must be written to the process stdin after
// `started` (Quickshell Process.write), never placed in argv (/proc cmdline).
function secretClipboardCommand() {
    return ["wl-copy"]
}

// stdinPayload is the only string QML may pass to Process.write. Empty when
// there is nothing to send, so callers leave stdin closed and curl does not
// block. Never put the return value into argv.
function stdinPayload(body) {
    if (body === undefined || body === null) return ""
    var s = String(body)
    if (s.length === 0) return ""
    return s
}

// openUrlCommand returns the argv that opens a URL in the default browser.
// This is not a clipboard copy: xdg-open takes the URL as an argument.
// Only http(s) URLs pass — a leading "-" would be an option (argument
// injection), and this function is exported, so the guard belongs here.
function openUrlCommand(url) {
    if (!isValidRememberedUrl(url)) return null
    return ["xdg-open", String(url)]
}

// isMfaCodeValid mirrors the daemon/CDP rule (6 digits). Local validation so
// a malformed code never reaches the socket. Pure + unit-tested.
function isMfaCodeValid(code) {
    return /^[0-9]{6}$/.test(String(code === undefined || code === null ? "" : code))
}

// copyDoneLabel is the transient caption copy buttons show after a copy
// (39.1). The swap timing lives in QML (a Timer reverting the caption);
// this helper owns only the string so tests pin it. Pure + unit-tested.
function copyDoneLabel() {
    return "Copied ✓"
}

// mfaRole maps MFA enrollment to a semantic role (on = ok, off = offline).
// Palette.qml resolves the role to a theme color.
function mfaRole(mfaEnrolled) {
    return mfaEnrolled === true ? "ok" : "offline"
}

// mfaLabel is the friendly two-factor status line. The state is carried by the
// text (color is only reinforcement); the method lives in mfaTooltip, not here.
function mfaLabel(mfaEnrolled) {
    return mfaEnrolled === true ? "Two-factor protection: on" : "Two-factor protection: off"
}

// mfaTooltip explains the two-factor state and what a click does. method is the
// raw enrollment method ("totp"); it is described in plain words, never shown.
function mfaTooltip(mfaEnrolled, method) {
    if (mfaEnrolled === true) {
        var how = String(method || "").trim() !== ""
            ? "a one-time code from your authenticator app"
            : "a one-time code"
        return "On — payments are approved with " + how + ". Click to change or reset it."
    }
    return "Off — click to add an authenticator app and protect payments with a one-time code."
}

// walletCopyValue is the only value the wallet-address action copies: the
// address reduced to hex/x characters (same filter as before the refactor).
// Returns "" when there is no usable address, so callers cannot copy junk or
// show a false "Copied".
function walletCopyValue(address) {
    if (typeof address !== "string") return ""
    return address.replace(/[^0-9a-fA-Fx]/g, "")
}

// mfaVerifyReason builds the verify-dialog context from the blocking fetch
// error plus the panel's cap/balance snapshot (24.4). Copy and money
// formatting live here so QML only renders strings (AGENTS.md rule 7):
// sub-cent amounts stay visible via formatUsdExact, cap=0 reads as
// "auto-pay off" (never a fake "over budget 0.00"), and a missing/zero
// balance is omitted instead of printed as "0.0000". Returns [] when there is
// no last_fetch_error: the popup then shows the bare code prompt. Pure.
function mfaVerifyReason(lfe, capUsd, balanceUsd) {
    if (!lfe || typeof lfe !== "object") return []
    var lines = []
    var usd = Number(lfe.amount_micro) / MICRO_USDC
    if (!isFinite(usd) || usd <= 0) usd = Number(lfe.amount_usdc)
    if (isFinite(usd) && usd > 0)
        lines.push("Amount: " + formatUsdExact(usd) + " " + USDC)
    var host = urlHost(lfe.target_url)
    if (host !== "") lines.push("Seller: " + host)
    var cap = Number(capUsd)
    if (isFinite(cap) && cap > 0)
        lines.push("Daily budget: " + formatUsdExact(cap) + " " + USDC)
    else
        lines.push("Auto-pay is off — every payment asks for your code.")
    var balance = Number(balanceUsd)
    if (isFinite(balance) && balance > 0) {
        lines.push("Wallet balance: " + formatUsdExact(balance) + " " + USDC)
        // Drain warning (26.3): a single payment eating most of the wallet is
        // the shape of a theft, not of a purchase. Warn, never block — the
        // decision stays with the user.
        if (isFinite(usd) && usd > 0 && usd >= MFA_BALANCE_WARN_RATIO * balance) {
            if (usd > balance)
                lines.push("Warning: this payment is larger than your wallet balance (" + formatUsdExact(balance) + " " + USDC + ") — verify only if you expect it.")
            else
                lines.push("Warning: this payment is " + Math.round((usd / balance) * 100) + "% of your wallet balance (" + formatUsdExact(balance) + " " + USDC + ") — verify only if you expect it.")
        }
    }
    lines.push("Your code completes this pending payment.")
    lines.push("If no code arrives in time, nothing is paid — the client can ask again.")
    return lines
}

// mfaGateFromError classifies a daemon error payload from a sudo-blocked
// mutation (26.1/26.2). mfa_not_enrolled means "enable MFA first", mfa_stale
// means "confirm with a code"; every other code (including mfa_unavailable,
// "no session") and any non-JSON transport text returns an empty mode so the
// panel keeps its plain error path instead of sending the user for a code that
// cannot work. Pure; never throws. Returns { code, mode, detail }.
function mfaGateFromError(raw) {
    var out = { code: "", mode: "", detail: "" }
    if (raw === undefined || raw === null) return out
    var o = null
    try { o = JSON.parse(String(raw)) } catch (e) { return out }
    if (!o || typeof o !== "object") return out
    out.code = String(o.error || "")
    out.detail = String(o.detail || "")
    if (out.code === "mfa_not_enrolled") out.mode = "enroll"
    else if (out.code === "mfa_stale") out.mode = "verify"
    // CDP asking for a code at signing time (not our authority gate) means the
    // same thing to the user: enter the code and this operation finishes. The
    // override path (30.2a) already has the gate + replay, so it must route here
    // instead of dying as "Override failed".
    else if (out.code === "mfa_required") out.mode = "verify"
    return out
}

// mfaSudoReason explains a sudo-gated denial in human terms (26.2): what is
// being changed, why a code is required, and — truthfully — that after the code
// in-budget payments keep running without one until CDP asks again. With
// enrolled === false (the mfa_not_enrolled case) it reads as "enable MFA first"
// instead. action: "policy" (cap change) | anything else (override payment).
// Pure.
function mfaSudoReason(action, fromUsd, toUsd, enrolled) {
    var lines = []
    var from = Number(fromUsd), to = Number(toUsd)
    if (action === "policy") {
        if (isFinite(from) && isFinite(to) && from >= 0 && to >= 0)
            lines.push("This changes the daily limit from " + formatUsdExact(from) + " to " + formatUsdExact(to) + " " + USDC + ".")
        else
            lines.push("This changes the daily spending limit.")
    } else {
        lines.push("This payment exceeds the daily limit.")
    }
    if (enrolled === true) {
        lines.push("Raising spending authority needs a code from your authenticator app.")
        lines.push("After the code, payments within the budget keep running without one until CDP asks again.")
    } else {
        lines.push("This wallet has no authenticator code (MFA) enabled yet.")
        lines.push("Enable it first — without it spending limits cannot be changed.")
    }
    return lines
}

// MFA_REPROMPT_COOLDOWN_MS is how long the same denial stays quiet after the
// user dismisses it (30.1). Long enough to kill an agent's retry loop (which
// re-denies every second), short enough that a real, later block surfaces again.
var MFA_REPROMPT_COOLDOWN_MS = 5 * 60 * 1000

// mfaNagKey is the identity of a denial: code | seller | amount. The daemon
// stamps a FRESH timestamp on every attempt (internal/gateway/last_error.go),
// so a timestamp is not an identity — deduping on it made the popup return after
// every dismiss while an agent retried. Returns "" when there is nothing to
// identify (no popup without a denial). Pure.
function mfaNagKey(lfe) {
    if (!lfe || typeof lfe !== "object") return ""
    var code = String(lfe.code || "")
    if (code === "") return ""
    return code + "|" + String(lfe.target_url || "") + "|" + String(lfe.amount_micro || 0)
}

// shouldSurfaceMfa decides whether to surface a denial again: a new key is a new
// problem (always surface), the same key is suppressed for cooldownMs after the
// previous one. nowMs/cooldownMs are arguments so this stays pure and testable.
function shouldSurfaceMfa(prevKey, prevAtMs, key, nowMs, cooldownMs) {
    if (key === "") return false
    if (key !== prevKey) return true
    var elapsed = Number(nowMs) - Number(prevAtMs)
    return !isFinite(elapsed) || elapsed >= Number(cooldownMs)
}

// statusRole maps widget states to a semantic role (fail-safe: unknown →
// offline). Palette.qml resolves the role to a theme color.
function statusRole(state) {
    switch (state) {
        case State.ACTIVE:     return "ok"
        case State.PAUSED:     return "paused"
        case State.EXHAUSTED:  return "warn"
        case State.LOGGED_OUT: return "info"
        case State.ERROR:      return "error"
        default:               return "offline"
    }
}

// PLUGIN_ID is the Omarchy plugin identity (must match manifest.json id;
// used as BarWidget moduleName and IPC target).
var PLUGIN_ID = "gelu22.gateway"

function defaultSocketPath(home) {
    return home + "/.local/state/x402-gateway/gw.sock"
}

// localBinPath / shareFilePath build install-layout paths from $HOME,
// mirroring scripts/install.sh (BIN_DIR / SHARE_DIR).
function localBinPath(home) {
    return home + "/.local/bin/gateway"
}

function shareFilePath(home, file) {
    return home + "/.local/share/x402-gateway/" + file
}

// buildCommand returns the argv for a curl call against the daemon socket.
function buildCommand(socketPath, path, method, body, timeoutS) {
    // timeoutS defaults to the generic socket budget; waiting calls pass
    // CURL_TIMEOUT_WAIT_S so a pending MFA prompt cannot cut the response.
    var cmd = ["curl", "-sS", "-m", String(timeoutS || CURL_TIMEOUT_S),
               "--unix-socket", socketPath,
               "-X", method, "-H", "Content-Type: application/json"]
    // Non-empty body goes through stdin (--data-binary @-), never -d.
    // -d would both publish the bytes in argv and strip a trailing newline.
    if (stdinPayload(body) !== "")
        cmd.push("--data-binary", "@-")
    cmd.push("http://localhost" + path)
    return cmd
}

// stateLabel maps internal states to English labels.
function stateLabel(state) {
    switch (state) {
        case State.ACTIVE:     return "Active"
        case State.PAUSED:     return "Paused"
        case State.EXHAUSTED:  return "Over budget"
        case State.LOGGED_OUT: return "Sign-in required"
        case State.ERROR:      return "Error"
        default:               return "Offline"
    }
}

// resolveStep computes the wizard step from live daemon state. Single source
// of truth for /status-driven step transitions — the panel calls it on every
// refresh instead of inlining step logic. Steps 0 (email) and 1 (OTP) are the
// in-flow path and are never interrupted: while logged out the user may be
// typing email or waiting for the code. Any other step under logged_out means
// a dropped session with no wizard — return to the email step (0). Fresh
// panels (-1) resolve from daemon truth. Pure + unit-tested.
function resolveStep(currentStep, daemonState) {
    if (currentStep === -1)
        return daemonState === State.LOGGED_OUT ? 0 : 3
    if (daemonState === State.LOGGED_OUT && currentStep !== 0 && currentStep !== 1)
        return 0
    return currentStep
}

// heroState computes the honest hero line from live daemon values.
// opts: {paused, online, session, wallet, signerOk, spend, cap} (numbers).
// Returns {label, role, over} — role is a Palette role name (resolved to a
// theme color in QML); over is true when today's spend is past the daily cap.
// Over-budget is neutral info (warn), never alarm: the daemon keeps paying with
// per-payment approval.
function heroState(o) {
    var over = o.cap > 0 && o.spend > o.cap
    if (o.paused === true)
        return { label: "Paused", role: "paused", over: over }
    if (o.online !== true)
        return { label: "Offline", role: "offline", over: over }
    if (o.session === State.LOGGED_OUT || !o.wallet)
        return { label: "Sign-in required", role: "info", over: over }
    if (o.signerOk === false)
        return { label: "Error", role: "error", over: over }
    if (over)
        return { label: "Over budget", role: "warn", over: over }
    return { label: "Active", role: "ok", over: over }
}

// overBudgetAlert renders the alert-slot line when over budget (empty string =
// slot hidden). Copy lives here so tests pin the user-facing sentence.
function overBudgetAlert(over) {
    if (over !== true) return ""
    return "⚠ Over budget — new payments will ask for approval."
}

// shortAddress renders 0x1234…abcd; the full value stays in copy payloads.
function shortAddress(address) {
    if (!address || address.length < SHORT_ADDR_MIN) return address || ""
    return address.slice(0, 6) + "…" + address.slice(-4)
}

// networkLabel maps CAIP-2 network to a human-readable label (both
// chains named explicitly — "Base" alone is ambiguous with Base Sepolia).
function networkLabel(caip2) {
    switch (caip2) {
        case NET_SEPOLIA: return "Base Sepolia"
        case NET_MAINNET: return "Base Mainnet"
        default:          return caip2
    }
}

// ---- Plugin config file (009.5) ----
//
// ~/.config/omarchy/x402-gateway/config.json — JSONC (comments + trailing
// commas), hand-editable like other Omarchy configs. Holds UI preferences
// (network + remembered URLs); spend policy stays in the daemon's policy.json.
// JSONC parsing mirrors Omarchy's own MenuModel.stripJsonc (whole-line // and
// trailing commas only — no inline // or /* */).

var DEFAULT_NETWORK = NET_SEPOLIA
var SUPPORTED_NETWORKS = [NET_SEPOLIA, NET_MAINNET]
var MAX_REMEMBERED_URLS = 100

// ENV_NETWORK is the env var the daemon reads for its startup network
// (mirror of internal/config config.Load `GATEWAY_NETWORK`).
var ENV_NETWORK = "GATEWAY_NETWORK"

// gatewayConfigPath returns the plugin config file path for a given HOME.
function gatewayConfigPath(home) {
    return home + "/.config/omarchy/x402-gateway/config.json"
}

// isSupportedNetwork mirrors internal/chains allowlist (both Base networks).
function isSupportedNetwork(caip2) {
    for (var i = 0; i < SUPPORTED_NETWORKS.length; i++)
        if (SUPPORTED_NETWORKS[i] === caip2) return true
    return false
}

// isValidRememberedUrl accepts only http(s) URLs with a non-empty host.
function isValidRememberedUrl(url) {
    return /^https?:\/\/[^\s/$.?#][^\s]*$/.test(String(url || ""))
}

// rememberedLimitMicro returns the user-approved micro-amount for a remembered
// URL. 0 means unknown (legacy entry) → callers must ask, never auto-pay.
// This caps silent auto-pay: a seller price raise above the approved amount
// forces re-approval instead of draining the wallet via "remember".
function rememberedLimitMicro(list, url) {
    if (!Array.isArray(list) || !url) return 0
    for (var i = 0; i < list.length; i++)
        if (list[i] && list[i].url === url)
            return (typeof list[i].approvedMicro === "number" && isFinite(list[i].approvedMicro) && list[i].approvedMicro > 0)
                ? list[i].approvedMicro : 0
    return 0
}

// needsApproval classifies an overridable status error for the panel. A
// remembered URL auto-pays only up to its approved amount; anything above (or a
// legacy 0/unknown limit) forces a fresh question. Pure + unit-tested so the
// "remember must not consent to any future price" rule is pinned in JS, not
// only in QML. Returns {remembered, priceChanged, previousMicro, autoPay}.
function needsApproval(ov, rememberedUrls) {
    var target = (ov && typeof ov.targetUrl === "string") ? ov.targetUrl : ""
    var remMicro = rememberedLimitMicro(rememberedUrls, target)
    var isRem = false
    if (Array.isArray(rememberedUrls) && target !== "") {
        for (var i = 0; i < rememberedUrls.length; i++)
            if (rememberedUrls[i] && rememberedUrls[i].url === target) { isRem = true; break }
    }
    var amount = (ov && typeof ov.amountMicro === "number") ? ov.amountMicro : 0
    // Non-positive/non-finite amounts are nonsense from a payer: fail closed
    // (ask) instead of auto-paying. Caught by hostile-input tests (016.4).
    var amountSane = isFinite(amount) && amount > 0
    var beyond = isRem && amountSane && amount > remMicro
    var priceChanged = (ov && ov.code === "price_changed") || beyond
    // Unknown sellers never auto-pay, even when remembered: a fresh daemon
    // state (or wiped registry) must re-ask instead of silently paying.
    var autoPay = isRem && !priceChanged && amountSane && (!ov || ov.code !== "unknown_seller")
    return {
        remembered: isRem,
        priceChanged: priceChanged,
        previousMicro: (beyond && remMicro > 0) ? remMicro : 0,
        autoPay: autoPay
    }
}

// urlHost extracts the lowercase host from an http(s) URL (bracket-aware for
// IPv6; userinfo and port stripped). Returns "" when there is no host. Pure —
// single source shared by overrideReasonText and mfaVerifyReason.
function urlHost(url) {
    var m = /^https?:\/\/([^\/]+)/i.exec(String(url || ""))
    if (!m) return ""
    var host = m[1]
    var at = host.lastIndexOf("@")
    if (at >= 0) host = host.slice(at + 1)
    if (host.charAt(0) === "[") {
        var close = host.indexOf("]")
        host = close > 0 ? host.slice(1, close) : ""
    } else {
        var colon = host.indexOf(":")
        if (colon >= 0) host = host.slice(0, colon)
    }
    return host.toLowerCase()
}

// blockedText is the panel's one-line reason for a blocked payment that has no
// dialog of its own — the hard per-payment ceiling at Coinbase, a rejected
// signature, a bad offer. Codes that open their own dialog (budget_exceeded,
// unknown_seller, price_changed, domain_cap_exceeded) or the MFA popup
// (mfa_required) return "" so nothing is said twice. Pure.
function blockedText(lfe) {
    if (!lfe || typeof lfe !== "object") return ""
    switch (String(lfe.code || "")) {
        case "policy_violation":
            return "Blocked by the per-payment limit set at Coinbase (Policy Engine) — it cannot be approved from the panel."
        case "network_denied":
            return "Blocked: unsupported network or token in the payment request."
        case "invalid_amount":
            return "Blocked: the payment amount is invalid."
        case "bad_target":
            return "Blocked: the target address is not allowed."
        case "duplicate_payment":
            return "Already paid — the same payment was not sent twice."
        case "insufficient_funds":
            return "Not enough USDC in the wallet — top it up and ask the client to retry."
        case "agent_cap_exceeded":
            return "" // overridable — OverrideConfirmDialog / blocked list
        default:
            return ""
    }
}

// errorLabel is a short English label for a daemon error code (panel copy).
function errorLabel(code) {
    switch (String(code || "")) {
        case "agent_cap_exceeded":
            return "This agent's daily limit is reached — approve to pay anyway."
        case "budget_exceeded":
            return "Daily budget reached — approve to pay anyway."
        default:
            return String(code || "")
    }
}

// Clock skew: EIP-3009 authorizations are valid in a ±5 minute window, so a
// machine with a wrong clock fails every payment. Warn well before that.
var CLOCK_SKEW_WARN_MS = 60000

// clockSkewWarning is the panel's one-line warning for a system clock that is
// off enough to break signatures. Pure: "" when unknown or within tolerance.
function clockSkewWarning(skewMs) {
    if (typeof skewMs !== "number" || !isFinite(skewMs)) return ""
    if (Math.abs(skewMs) < CLOCK_SKEW_WARN_MS) return ""
    var s = Math.round(Math.abs(skewMs) / 1000)
    return "System clock is off by " + s + " s — payments may be rejected until it syncs (enable NTP)."
}

// overrideReasonText renders the dialog reason line for seller-trust denials
// (013.2). Pure function — panel/dialog stay logic-free. Returns "" for codes
// with dedicated UI (budget_exceeded, price_changed) and unknown codes.
function overrideReasonText(code, targetUrl) {
    var host = urlHost(targetUrl)
    if (code === "unknown_seller") {
        if (host === "")
            return "First payment to this seller — approve it?"
        return "First payment to " + host + " — approve this seller?"
    }
    if (code === "domain_cap_exceeded")
        return "Over this seller's share of today's budget."
    return ""
}

// parseJsonc strips whole-line // comments (Omarchy MenuModel pattern) and
// trailing commas OUTSIDE string literals. The string-aware scan matters:
// a naive regex would corrupt a URL containing ",]" or ",}". Pure function.
function parseJsonc(raw) {
    var s = String(raw || "").replace(/^\s*\/\/[^\n]*(\n|$)/gm, "")
    var out = ""
    var inStr = false
    var esc = false
    for (var i = 0; i < s.length; i++) {
        var ch = s.charAt(i)
        if (inStr) {
            out += ch
            if (esc) esc = false
            else if (ch === "\\") esc = true
            else if (ch === "\"") inStr = false
            continue
        }
        if (ch === "\"") { inStr = true; out += ch; continue }
        if (ch === ",") {
            var j = i + 1
            while (j < s.length && /\s/.test(s.charAt(j))) j++
            if (j < s.length && (s.charAt(j) === "}" || s.charAt(j) === "]")) continue
        }
        out += ch
    }
    return out
}

// parseGatewayConfig parses the config text into a usable object.
// Missing/empty/corrupt input yields defaults (never throws). Shape:
//   { ok, paymentNetwork, rememberedUrls: [{url, added}], error }
// `ok` = text parsed as an object; `error` = non-fatal note (bad network etc).
function parseGatewayConfig(text) {
    var out = { ok: false, paymentNetwork: DEFAULT_NETWORK, rememberedUrls: [], error: "" }
    var stripped = parseJsonc(text)
    if (stripped.trim() === "") return out // missing/empty → defaults
    var parsed
    try {
        parsed = JSON.parse(stripped)
    } catch (e) {
        out.error = "parse failed"
        return out
    }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        out.error = "not an object"
        return out
    }
    var net = String(parsed.paymentNetwork || "")
    if (isSupportedNetwork(net)) {
        out.paymentNetwork = net
    } else if (net !== "") {
        out.error = "unsupported network: " + net
    }
    if (Array.isArray(parsed.rememberedUrls)) {
        for (var i = 0; i < parsed.rememberedUrls.length; i++) {
            var e = parsed.rememberedUrls[i]
            if (e && typeof e.url === "string" && isValidRememberedUrl(e.url))
                out.rememberedUrls.push({
                    url: e.url,
                    added: typeof e.added === "string" ? e.added : "",
                    approvedMicro: (typeof e.approvedMicro === "number" && isFinite(e.approvedMicro) && e.approvedMicro > 0)
                        ? Math.round(e.approvedMicro) : 0
                })
        }
    }
    out.ok = true
    return out
}

// serializeGatewayConfig renders the config as commented JSONC (stable shape,
// standard header comments). Used by the panel write path (009.7). Entries are
// re-validated here so serialize→parse is identity for accepted input.
function serializeGatewayConfig(cfg) {
    var net = (cfg && isSupportedNetwork(cfg.paymentNetwork)) ? cfg.paymentNetwork : DEFAULT_NETWORK
    var src = (cfg && Array.isArray(cfg.rememberedUrls)) ? cfg.rememberedUrls : []
    var urls = []
    for (var k = 0; k < src.length && urls.length < MAX_REMEMBERED_URLS; k++) {
        var e = src[k]
        if (e && typeof e.url === "string" && isValidRememberedUrl(e.url))
            urls.push({
                url: e.url,
                added: typeof e.added === "string" ? e.added : "",
                approvedMicro: (typeof e.approvedMicro === "number" && isFinite(e.approvedMicro) && e.approvedMicro > 0)
                    ? Math.round(e.approvedMicro) : 0
            })
    }
    var lines = [
        "{",
        "  // Payment network: \"" + NET_SEPOLIA + "\" (Base Sepolia) or \"" + NET_MAINNET + "\" (Base Mainnet)",
        "  \"paymentNetwork\": " + JSON.stringify(net) + ","
    ]
    if (urls.length === 0) {
        lines.push("  // URLs that pay without asking (added from the approval dialog)")
        lines.push("  \"rememberedUrls\": []")
    } else {
        lines.push("  // URLs that pay without asking (added from the approval dialog)")
        lines.push("  \"rememberedUrls\": [")
        for (var i = 0; i < urls.length; i++)
            lines.push("    " + JSON.stringify(urls[i]) + (i < urls.length - 1 ? "," : ""))
        lines.push("  ]")
    }
    lines.push("}")
    return lines.join("\n") + "\n"
}

// parseStatus maps /status JSON to display fields (single cap + last_block).
// parseBlocked turns /status.blocked (49.2 summaries) into render rows. Pure and
// fail-closed: an entry without an id or url is skipped, never rendered with a
// "pay" action. `status` is the object returned by parseStatus.
function parseBlocked(status) {
    var o = status && status.raw ? status.raw : status
    if (!o || !Array.isArray(o.blocked)) return []
    var out = []
    for (var i = 0; i < o.blocked.length; i++) {
        var b = o.blocked[i]
        if (!b || typeof b.id !== "string" || b.id === "") continue
        if (typeof b.url !== "string" || b.url === "") continue
        out.push({
            id: b.id,
            method: String(b.method || "GET"),
            url: b.url,
            host: urlHost(b.url),
            amountMicro: (typeof b.amount_micro === "number" && isFinite(b.amount_micro)) ? b.amount_micro : 0,
            reason: String(b.reason || "")
        })
    }
    return out
}

// blockedAmountText formats the blocked amount from micro-USDC.
function blockedAmountText(amountMicro) {
    return formatUsdExact(amountMicro / 1000000)
}

// parseHistory turns GET /history JSON into {ok, entries, truncated, error}.
function parseHistory(raw) {
    if (typeof raw !== "string" || raw === "")
        return { ok: false, entries: [], truncated: false, error: "empty" }
    var o
    try { o = JSON.parse(raw) } catch (e) {
        return { ok: false, entries: [], truncated: false, error: "bad_json" }
    }
    if (!o || typeof o !== "object" || !Array.isArray(o.entries))
        return { ok: false, entries: [], truncated: false, error: "bad_shape" }
    return {
        ok: true,
        entries: o.entries,
        truncated: o.truncated === true,
        error: ""
    }
}

// outcomeLabel maps audit outcome codes to short English labels.
function outcomeLabel(outcome) {
    var s = String(outcome || "")
    if (s === "paid") return "Paid"
    if (s.indexOf("failed:") === 0) {
        var code = s.slice(7).replace(/_/g, " ")
        if (code.length === 0) return "Failed"
        return code.charAt(0).toUpperCase() + code.slice(1)
    }
    return s || "—"
}

// relativeWhen formats an RFC3339 time relative to nowMs (injectable for tests).
function relativeWhen(iso, nowMs) {
    var t = Date.parse(String(iso || ""))
    if (!isFinite(t)) return "—"
    var now = (typeof nowMs === "number" && isFinite(nowMs)) ? nowMs : Date.now()
    var sec = Math.floor((now - t) / 1000)
    if (sec < 60) return "just now"
    if (sec < 3600) return Math.floor(sec / 60) + " min ago"
    if (sec < 86400) return Math.floor(sec / 3600) + " h ago"
    var d = new Date(t)
    var y = d.getUTCFullYear()
    var m = String(d.getUTCMonth() + 1).padStart(2, "0")
    var day = String(d.getUTCDate()).padStart(2, "0")
    return y + "-" + m + "-" + day
}

// historyRows maps /history entries to display rows (pure; nowMs injectable).
function historyRows(entries, nowMs) {
    if (!Array.isArray(entries)) return []
    var out = []
    for (var i = 0; i < entries.length; i++) {
        var e = entries[i]
        if (!e || typeof e !== "object") continue
        var micro = (typeof e.amount_micro === "number" && isFinite(e.amount_micro)) ? e.amount_micro : 0
        var agent = (typeof e.agent === "string" && e.agent !== "") ? e.agent : "—"
        out.push({
            agent: agent,
            domain: String(e.domain || "—"),
            amount: formatUsd(micro / MICRO_USDC, Precision.SPEND),
            outcome: outcomeLabel(e.outcome),
            when: relativeWhen(e.time, nowMs),
            override: e.override === true
        })
    }
    return out
}

// agentLabelValid mirrors daemon agentlabel.Valid (54.1).
function agentLabelValid(label) {
    if (typeof label !== "string" || label.length === 0 || label.length > 32) return false
    return /^[a-z0-9][a-z0-9._-]{0,31}$/.test(label)
}

// agentLimitCaption: CapMicro 0 + any positive cap elsewhere → override "asks";
// CapMicro 0 with no positive caps → feature off "no limit".
function agentLimitCaption(capMicro, statusAgents) {
    if (typeof capMicro === "number" && isFinite(capMicro) && capMicro > 0)
        return formatUsdExact(capMicro / MICRO_USDC)
    var anyPositive = false
    var list = Array.isArray(statusAgents) ? statusAgents : []
    for (var i = 0; i < list.length; i++) {
        var c = list[i] && list[i].cap_micro
        if (typeof c === "number" && c > 0) { anyPositive = true; break }
    }
    return anyPositive ? "asks every time" : "no limit"
}

// agentSpendRows joins detect() agents with /status.agents by label===name.
// Empty-label spend (legacy MCP) appends a read-only "unlabeled" row.
function agentSpendRows(status, agents) {
    var raw = status && status.raw ? status.raw : status
    var spendBy = {}
    var statusAgents = (raw && Array.isArray(raw.agents)) ? raw.agents : []
    for (var i = 0; i < statusAgents.length; i++) {
        var a = statusAgents[i]
        if (!a || typeof a.label !== "string") continue
        spendBy[a.label] = a
    }
    var list = Array.isArray(agents) ? agents : []
    var out = []
    for (var j = 0; j < list.length; j++) {
        var row = list[j]
        if (!row || typeof row.name !== "string") continue
        var hit = spendBy[row.name] || {}
        var spent = (typeof hit.spent_today_micro === "number" && isFinite(hit.spent_today_micro))
                  ? hit.spent_today_micro : 0
        var cap = (typeof hit.cap_micro === "number" && isFinite(hit.cap_micro)) ? hit.cap_micro : 0
        out.push({
            name: row.name,
            spentMicro: spent,
            capMicro: cap,
            spentText: formatUsdExact(spent / MICRO_USDC),
            limitText: agentLimitCaption(cap, statusAgents),
            integrated: row.integrated === true,
            connectable: row.connectable !== false,
            readOnly: false
        })
    }
    if (Object.prototype.hasOwnProperty.call(spendBy, "")) {
        var blank = spendBy[""]
        var bSpent = (typeof blank.spent_today_micro === "number" && isFinite(blank.spent_today_micro))
                   ? blank.spent_today_micro : 0
        var bCap = (typeof blank.cap_micro === "number" && isFinite(blank.cap_micro)) ? blank.cap_micro : 0
        out.push({
            name: "unlabeled",
            spentMicro: bSpent,
            capMicro: bCap,
            spentText: formatUsdExact(bSpent / MICRO_USDC),
            limitText: agentLimitCaption(bCap, statusAgents),
            integrated: false,
            connectable: false,
            readOnly: true
        })
    }
    return out
}

// DEFAULT_AVAILABLE_LIMIT is how many "available" agents render before the
// panel asks to "Show more" (the panel has no scroll).
var DEFAULT_AVAILABLE_LIMIT = 8

// agentGroups splits the agent rows from agentSpendRows into the three groups
// the panel renders: connected (integrated), available (detectable, not
// integrated), and unlabeled (the read-only "" bucket). Order is preserved.
function agentGroups(status, agents) {
    var rows = agentSpendRows(status, agents)
    var connected = [], available = [], unlabeled = []
    for (var i = 0; i < rows.length; i++) {
        var r = rows[i]
        if (r.readOnly) unlabeled.push(r)
        else if (r.integrated) connected.push(r)
        else available.push(r)
    }
    return { connected: connected, available: available, unlabeled: unlabeled }
}

// capList returns the first `limit` items plus how many the cap hid. limit <= 0
// or missing means "no cap" (show everything, hidden 0).
function capList(list, limit) {
    var l = Array.isArray(list) ? list : []
    var n = (typeof limit === "number" && limit > 0) ? limit : l.length
    if (l.length <= n) return { shown: l.slice(), hidden: 0 }
    return { shown: l.slice(0, n), hidden: l.length - n }
}

// agentCapsBody builds POST /policy body replacing agent_caps_micro_usdc.
// Returns { body, error }. currentMap is the existing override map (may be null).
function agentCapsBody(currentMap, label, usd) {
    if (!agentLabelValid(label))
        return { body: "", error: "Invalid agent label" }
    if (typeof usd !== "number" || !isFinite(usd) || usd < 0)
        return { body: "", error: "Enter an amount ≥ 0" }
    var micro = usdToMicro(usd)
    if (!isFinite(micro) || micro < 0)
        return { body: "", error: "Enter an amount ≥ 0" }
    var map = {}
    if (currentMap && typeof currentMap === "object") {
        for (var k in currentMap) {
            if (!Object.prototype.hasOwnProperty.call(currentMap, k)) continue
            var v = currentMap[k]
            if (typeof v === "number" && isFinite(v) && v >= 0) map[k] = v
        }
    }
    map[label] = micro
    return { body: JSON.stringify({ agent_caps_micro_usdc: map }), error: "" }
}

// approveBody is the POST /fetch-approve payload (pay now).
function approveBody(id) { return JSON.stringify({ id: String(id) }) }

// permissionBody is the POST /permissions payload. temporary:true requires a
// positive ttl_seconds (the daemon enforces it too).
function permissionBody(url, usd, temporary, ttlSeconds) {
    return JSON.stringify({
        url: String(url),
        limit_micro: usdToMicro(usd),
        temporary: temporary === true,
        ttl_seconds: temporary === true ? Math.floor(Number(ttlSeconds) || 0) : 0
    })
}

// validatePermission mirrors the daemon's checks for the form: empty string means
// valid, otherwise a short reason. UX pre-check only — the daemon is the source
// of truth.
function validatePermission(url, usd) {
    if (!isValidRememberedUrl(url)) return "Enter an http(s) URL"
    var micro = usdToMicro(usd)
    if (!isFinite(micro) || micro <= 0) return "Limit must be greater than zero"
    return ""
}

function parseStatus(raw) {
    try {
        var o = JSON.parse(raw)
        var state
        if (o.paused === true)                       state = State.PAUSED
        else if (o.state === State.LOGGED_OUT)       state = State.LOGGED_OUT
        else if (o.last_block && o.last_block.reason === "budget_exceeded") state = State.EXHAUSTED
        else if (o.signer_ok === false && o.wallet_address) state = State.ERROR
        else if (o.wallet_address)                   state = State.ACTIVE
        else                                         state = State.LOGGED_OUT

        var blockText = ""
        if (o.last_block && o.last_block.reason === "budget_exceeded")
            blockText = "Over daily budget — new payments will ask for approval"
        else if (o.last_block && o.last_block.reason === "invalid_amount")
            blockText = "Single request rejected (invalid amount) — unpaid"

        // Exact micro-USDC strings for bar/tooltip (NEW-P2-1 / 44.6): never
        // collapse a non-zero micro amount to "0.00" via fixed-decimal formatUsd.
        return {
            ok: true,
            state: state,
            spend_today: formatUsdExact(o.spend_today_usdc),
            budget_daily: formatUsdExact(o.budget_daily_usdc),
            balance_usd: formatUsdExact(o.wallet_balance_usdc),
            payment_network: o.payment_network || "",
            address: o.wallet_address || "",
            block_text: blockText,
            mfa_enrolled: o.mfa_enrolled === true,
            mfa_method: o.mfa_method || "",
            raw: o
        }
    } catch (e) {
        return { ok: false, state: State.OFFLINE,
                 spend_today: formatUsdExact(0), budget_daily: formatUsdExact(0),
                 balance_usd: formatUsdExact(0), address: "", block_text: "",
                 mfa_enrolled: false, mfa_method: "", raw: null }
    }
}

// parseLogoutResponse classifies a POST /pair/logout raw body (019.1).
// {ok:true} only for {"state":"logged_out"}; anything else (error envelope,
// daemon_offline, garbage) is {ok:false, text} for fail(). Pure + tested.
function parseLogoutResponse(raw) {
    try {
        var o = JSON.parse(raw)
        if (o && o.state === State.LOGGED_OUT) return { ok: true, text: "" }
        var t = errorDetail(o) || errorCode(o)
        return { ok: false, text: t !== "" ? t : String(raw).slice(0, LOG_TRIM_RESULT) }
    } catch (e) {
        return { ok: false, text: String(raw).slice(0, LOG_TRIM_RESULT) }
    }
}

// parseOverrideError extracts user-approval payment info from /status raw
// JSON (gateway: last_fetch_error). Model v2 (005.10): any challenge with
// can_override=true awaits explicit approval (over daily budget).
// Pure function — no I/O here.
function parseOverrideError(raw) {
    try {
        var o = JSON.parse(raw)
        var lfe = o.last_fetch_error
        if (lfe && lfe.can_override === true) {
            return {
                isOverride: true,
                amountMicro: lfe.amount_micro || 0,
                amountUsd: Number(lfe.amount_micro || 0) / MICRO_USDC,
                canOverride: true,
                // last_fetch_error.code is a live daemon field
                // (FetchErrorInfo, status.go) — not the cut envelope pair.
                code: (typeof lfe.code === "string") ? lfe.code : "",
                targetUrl: lfe.target_url || "",
                timestamp: lfe.timestamp || ""
            }
        }
    } catch (e) {}
    return { isOverride: false }
}
