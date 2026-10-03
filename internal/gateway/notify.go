package gateway

// Notify posts a desktop notification through the platform implementation
// (notify-send on Linux, no-op elsewhere). Exported so the socket layer can
// tell the owner that a payment needs a decision (49.2) without duplicating the
// platform code.
func Notify(title, body string) { desktopNotify(title, body) }
