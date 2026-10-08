// HistorySection.qml — payment history opens as a text file in the system
// editor (narrow SETUP column cannot fit readable columns). Formatting in Model.js.
pragma ComponentBehavior: Bound
import Quickshell
import Quickshell.Io
import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

CollapsibleSection {
    id: root

    property string socketPath: ""
    property string homeDir: ""
    property string errorText: ""
    property bool busy: false
    property string pendingDoc: ""

    title: "HISTORY"
    iconText: Model.ICON_HISTORY
    trailingText: root.busy ? "…" : (root.errorText !== "" ? root.errorText : "Opens in editor")
    trailingColor: root.errorText !== "" ? Model.Palette.error : Color.foreground
    showChevron: true
    expanded: false

    width: parent ? parent.width : 0

    signal failed(string message)

    function openHistory() {
        if (root.busy) return
        if (root.socketPath === "" || root.homeDir === "") {
            root.errorText = "History unavailable"
            root.failed(root.errorText)
            return
        }
        var path = Model.historyFilePath(root.homeDir)
        if (path === "") {
            root.errorText = "History unavailable"
            root.failed(root.errorText)
            return
        }
        root.busy = true
        root.errorText = ""
        root.pendingDoc = ""
        histProc.historyPath = path
        histProc.command = Model.buildCommand(
            root.socketPath, Model.Endpoint.HISTORY + "?limit=50", Model.Method.GET, "")
        histProc.running = true
    }

    onToggle: {
        root.expanded = false
        root.openHistory()
    }

    Process {
        id: histProc
        property string historyPath: ""
        stdout: StdioCollector {}
        onExited: (code) => {
            if (code !== 0) {
                root.busy = false
                root.errorText = "History unavailable"
                root.failed(root.errorText)
                return
            }
            var parsed = Model.parseHistory(stdout.text)
            if (!parsed.ok) {
                root.busy = false
                root.errorText = "History unavailable"
                root.failed(root.errorText)
                return
            }
            root.pendingDoc = Model.historyDocument(parsed.entries, Date.now())
            var p = histProc.historyPath
            var i = p.lastIndexOf("/")
            var dir = i > 0 ? p.slice(0, i) : ""
            mkdirProc.command = ["mkdir", "-p", dir]
            mkdirProc.running = true
        }
    }

    Process {
        id: mkdirProc
        onExited: (code) => {
            if (code !== 0 || root.pendingDoc === "") {
                root.busy = false
                root.errorText = "Could not write history file"
                root.failed(root.errorText)
                return
            }
            writeProc.pendingBody = root.pendingDoc
            writeProc.stdinEnabled = true
            writeProc.command = ["tee", histProc.historyPath]
            writeProc.running = true
        }
    }

    Process {
        id: writeProc
        property string pendingBody: ""
        stdinEnabled: false
        onStarted: {
            if (pendingBody !== "") {
                write(pendingBody)
                pendingBody = ""
            }
        }
        onExited: (code) => {
            root.busy = false
            if (code !== 0) {
                root.errorText = "Could not write history file"
                root.failed(root.errorText)
                return
            }
            var cmd = Model.openEditorCommand(histProc.historyPath)
            if (cmd === null) {
                root.errorText = "Could not open history file"
                root.failed(root.errorText)
                return
            }
            root.errorText = ""
            Quickshell.execDetached(cmd)
        }
    }
}
