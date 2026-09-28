// Service.qml — headless lifecycle manager for the gateway daemon.
// Spawns the binary, restarts with backoff on crash, and health-checks the
// socket every 30 s. Exposes live status via the `status` property so bar
// widgets and panels can bind to it.
//
// Network comes from the plugin config file (009.6): the daemon is started
// only after the config is read, receives it as GATEWAY_NETWORK, and is
// restarted only when the network actually changes (editing remembered URLs
// must not bounce the daemon).
import Quickshell
import Quickshell.Io
import QtQuick
import "Model.js" as Model

Item {
    id: root

    // Configuration (override via plugin config file in a future version).
    property string daemonPath: ""
    property string homeDir: ""
    property string socketPath: ""
    property string configPath: ""

    // Payment network: effective (from config) and the one the running daemon
    // was started with (used to decide restart-on-change).
    property string paymentNetwork: Model.DEFAULT_NETWORK
    property string runningNetwork: ""

    // Live state for other components.
    property string serviceState: Model.State.OFFLINE // offline | active | paused | error | logged_out

    // Timings (tunable in one place).
    property int healthIntervalMs: 30000
    property int backoffBaseMs: 3000
    property int backoffMaxMs: 60000

    property int restartCount: 0
    property bool daemonRunning: false
    property bool pendingRestart: false

    Component.onCompleted: {
        if (root.homeDir === "")
            root.homeDir = Quickshell.env("HOME") || ""
        if (root.socketPath === "")
            root.socketPath = Model.defaultSocketPath(root.homeDir)
        if (root.daemonPath === "")
            root.daemonPath = Model.localBinPath(root.homeDir)
        if (root.configPath === "")
            root.configPath = Model.gatewayConfigPath(root.homeDir)
        // The daemon starts from configFile.onLoaded/onLoadFailed: the network
        // must be known before spawning (env is read once at startup).
        healthTimer.start()
    }

    // Reads the plugin config; called on first load and on every watched change.
    // Fail-safe (009.6): on a missing/corrupt/unsupported config we NEVER switch
    // a running daemon's network — first start falls back to the default, a
    // running daemon keeps its current network (warning only, no restart).
    function applyConfig(raw) {
        var cfg = Model.parseGatewayConfig(raw)
        var usable = cfg.ok && cfg.error === ""
        if (!usable)
            console.warn(Model.LOG_TAG + " config: " +
                         (cfg.error !== "" ? cfg.error : "missing/empty") +
                         (root.runningNetwork === "" ? "; using " + Model.DEFAULT_NETWORK
                                                     : "; keeping " + root.runningNetwork))
        if (root.runningNetwork === "") {
            root.paymentNetwork = usable ? cfg.paymentNetwork : Model.DEFAULT_NETWORK
            root.startDaemon()
            return
        }
        if (!usable) return // keep the running network
        if (cfg.paymentNetwork === root.runningNetwork) return
        root.paymentNetwork = cfg.paymentNetwork
        console.warn(Model.LOG_TAG + " network changed " + root.runningNetwork +
                     " → " + cfg.paymentNetwork + ", restarting daemon")
        root.restartDaemon()
    }

    function startDaemon() {
        if (root.daemonPath.length === 0) {
            console.warn(Model.LOG_TAG + " daemon path unknown; set daemonPath or install to ~/.local/bin/gateway")
            return
        }
        var env = {}
        env[Model.ENV_NETWORK] = root.paymentNetwork
        daemonProc.environment = env
        root.runningNetwork = root.paymentNetwork
        daemonProc.command = [root.daemonPath]
        daemonProc.running = true
    }

    // stop → onExited → start with the new env (flock guards a slow stop).
    // If the daemon is already stopped (crash-backoff window), start directly
    // and cancel the pending backoff — otherwise pendingRestart would leak.
    function restartDaemon() {
        if (!daemonProc.running) {
            restartTimer.stop()
            root.startDaemon()
            return
        }
        root.pendingRestart = true
        daemonProc.running = false
    }

    function refreshStatus() {
        statusProc.command = Model.buildCommand(root.socketPath, Model.Endpoint.STATUS, Model.Method.GET, "")
        statusProc.running = true
    }

    function applyStatus(raw) {
        var st = Model.parseStatus(raw)
        root.serviceState = st.state
    }

    // Plugin config watcher: network changes bounce the daemon; URL edits are
    // ignored here (same network → no restart). FileView also watches the
    // parent directory, so a config created later is picked up — but only if
    // the directory existed when the watcher started (else: restart the shell).
    FileView {
        id: configFile
        path: root.configPath
        watchChanges: true
        printErrors: false
        onLoaded: root.applyConfig(text())
        onLoadFailed: root.applyConfig("") // missing config → defaults
        onFileChanged: reload()
    }

    Timer {
        id: healthTimer
        interval: root.healthIntervalMs
        repeat: true
        onTriggered: root.refreshStatus()
    }

    Timer {
        // Delayed restart with linear backoff (base × attempts, capped).
        id: restartTimer
        interval: Math.min(root.backoffBaseMs * (root.restartCount + 1), root.backoffMaxMs)
        onTriggered: root.startDaemon()
    }

    Process {
        id: daemonProc
        // Crash triage: daemon stderr/stdout are quiet in normal operation
        // (startup + warnings), so dumping the tail on exit is cheap and
        // puts panic traces into the journal for post-mortem.
        // NOTE (011.1): raw daemon FDs do NOT reach journald live (verified:
        // zero daemon output in journal; only QML console.warn lands there).
        // So money audit does NOT use stderr — it appends to audit.log
        // (state dir, durable, shell-independent). This buffer stays for
        // crash post-mortem only.
        stdout: StdioCollector { onStreamFinished: if (this.text !== "") console.warn(Model.LOG_TAG + " daemon stdout at exit: " + this.text.slice(-Model.LOG_TRIM_PROC)) }
        stderr: StdioCollector { onStreamFinished: if (this.text !== "") console.warn(Model.LOG_TAG + " daemon stderr at exit: " + this.text.slice(-Model.LOG_TRIM_PROC)) }
        onStarted: {
            root.daemonRunning = true
            root.restartCount = 0
            root.refreshStatus()
        }
        onExited: (code) => {
            root.daemonRunning = false
            root.serviceState = Model.State.OFFLINE
            if (root.pendingRestart) {
                root.pendingRestart = false
                root.startDaemon()
                return
            }
            // Any exit we did not ask for must come back. A clean stop
            // (code 0 — e.g. an external `kill -TERM`) used to skip the
            // restart and silently disable every agent payment.
            if (code !== 0) {
                root.restartCount += 1
                console.warn(Model.LOG_TAG + " daemon exited code=" + code + ", restart #" + root.restartCount)
            } else {
                console.warn(Model.LOG_TAG + " daemon stopped externally (code 0), restarting")
            }
            restartTimer.restart()
        }
    }

    Process {
        id: statusProc
        stdout: StdioCollector {
            onStreamFinished: root.applyStatus(this.text)
        }
        stderr: StdioCollector { }
        onExited: (code) => {
            if (code !== 0)
                root.serviceState = Model.State.OFFLINE
        }
    }
}
