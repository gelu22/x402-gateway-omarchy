//go:build !linux

package gateway

// desktopNotify is a no-op off Linux: notify-send is an Omarchy/Linux tool.
// The Linux implementation lives in notify_linux.go.
func desktopNotify(string, string) {}
