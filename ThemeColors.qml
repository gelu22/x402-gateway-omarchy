// ThemeColors.qml — semantic colors for the panel, derived from the Omarchy theme
// (Color/Style roles, no hex literals). The theme has no success/warning role,
// so: positive/active/info → accent, alarms → urgent, dimmed → muted. State is
// also carried by icon/label, so color is never the only signal (a11y).
//
// Model.js cannot read Color (plain JS), so it returns role names; this object
// resolves them to theme colors. Instantiate once per file that needs it:
//     Palette { id: palette }
import QtQuick
import qs.Commons
import "Model.js" as Model

QtObject {
    readonly property color ok: Color.accent
    readonly property color warn: Color.urgent
    readonly property color error: Color.urgent
    readonly property color paused: Color.urgent
    readonly property color info: Color.accent
    readonly property color offline: Color.muted
    readonly property color bannerBg: Util.alpha(Color.urgent, 0.12)
    readonly property color hairline: Util.alpha(Color.foreground, 0.2)

    function colorForRole(role) {
        switch (role) {
            case "ok": return ok
            case "warn": return warn
            case "error": return error
            case "paused": return paused
            case "info": return info
            default: return offline
        }
    }

    function statusColor(state) { return colorForRole(Model.statusRole(state)) }
    function mfaBadge(enrolled) { return colorForRole(Model.mfaRole(enrolled)) }
}
