#!/usr/bin/env bash
# uninstall.sh — x402 Gateway deinstaller.
#
# Modes:
#   (interactive)  asks: remove EVERYTHING without further questions, or step by
#                  step (pick groups). Reads from /dev/tty, so it also works
#                  when the script is downloaded and run directly.
#   --yes          remove everything, no questions (scripts, CI)
#
# Groups: agents (MCP entries), program (processes+binary+plugin+helper+shell),
# data (state dir with session/audit log + plugin config).
#
# Safety: without --yes and without a terminal it refuses and changes nothing.
# It never touches ~/.local/state/omarchy/notifications (shared with other apps).
set -euo pipefail

REPO="${X402_REPO:-gelu22/x402-gateway-omarchy}"
BIN_DIR="${HOME}/.local/bin"
GATEWAY_BIN="${BIN_DIR}/gateway"
STATE_DIR="${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway"
SHARE_DIR="${HOME}/.local/share/x402-gateway"
PLUGIN_DIR="${HOME}/.config/omarchy/plugins/gelu22.gateway"
CONFIG_DIR="${HOME}/.config/omarchy/x402-gateway"
AGENTS="opencode,claude-code,cursor,codex,gemini"
PLUGIN_ID="gelu22.gateway"
REGISTRY="$STATE_DIR/installed.sha256"

say() { printf '%s\n' "$*"; }

is_ours() {  # $1=path: true iff it exists and its sha matches the recording
  local want
  [ -f "$1" ] && [ -f "$REGISTRY" ] || return 1
  want="$(awk -v p="$1" '$2 == p {print $1}' "$REGISTRY" | tail -1)"
  [ -n "$want" ] || return 1
  [ "$(sha256sum "$1" | awk '{print $1}')" = "$want" ]
}

plugin_id_at() {  # $1=plugin dir: prints the manifest id, or ""
  [ -f "$1/manifest.json" ] || return 0
  sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1/manifest.json" | head -1
}

purge_agents() {
  local helper="${SHARE_DIR}/setup-agents.sh"
  if [ ! -x "$helper" ]; then
    say "  – no ${helper} (skipping MCP entries)"
    return 0
  fi
  if ! is_ours "$helper"; then
    say "  ⚠ ${helper} is not the helper this installer placed (skipping MCP entries)"
    return 0
  fi
  "$helper" --remove "$AGENTS" || say "  ! setup-agents --remove failed (check .bak-* backups)"
}

purge_program() {
  if [ -d "$PLUGIN_DIR" ] && [ "$(plugin_id_at "$PLUGIN_DIR")" = "$PLUGIN_ID" ]; then
    rm -rf "$PLUGIN_DIR"
  elif [ -e "$PLUGIN_DIR" ]; then
    say "  ⚠ keeping $PLUGIN_DIR (not this plugin)"
  fi

  # Stop the daemon only when the binary at the fixed path is ours.
  if is_ours "$GATEWAY_BIN"; then
    pkill -TERM -f "$GATEWAY_BIN" 2>/dev/null || true
    sleep 1
    pkill -KILL -f "$GATEWAY_BIN" 2>/dev/null || true
    if command -v omarchy >/dev/null 2>&1; then
      say "  → restarting the shell (unloads the plugin that respawns the daemon)"
      omarchy restart shell >/dev/null 2>&1 || true
      pkill -KILL -f "$GATEWAY_BIN" 2>/dev/null || true # race: respawn before unload
    fi
    rm -f "$GATEWAY_BIN"
  elif [ -e "$GATEWAY_BIN" ]; then
    say "  ⚠ keeping $GATEWAY_BIN (not installed by this installer)"
  fi

  # Shared dir: remove only the files we own; never wipe the whole directory.
  for f in setup-agents.sh remember-override.sh; do
    local p="$SHARE_DIR/$f"
    if [ ! -e "$p" ]; then continue
    elif is_ours "$p"; then rm -f "$p"
    else say "  ⚠ keeping $p (not ours)"; fi
  done
  rmdir "$SHARE_DIR" 2>/dev/null || true # only if it is now empty
}

purge_data() { rm -rf "$STATE_DIR" "$CONFIG_DIR"; }

leftovers() {
  local out=""
  [ -e "$GATEWAY_BIN" ] && out="${out} ${GATEWAY_BIN}"
  [ -e "$PLUGIN_DIR" ] && out="${out} ${PLUGIN_DIR}"
  [ -e "$SHARE_DIR" ] && out="${out} ${SHARE_DIR}"
  [ -e "$STATE_DIR" ] && out="${out} ${STATE_DIR}"
  [ -e "$CONFIG_DIR" ] && out="${out} ${CONFIG_DIR}"
  pgrep -f "$GATEWAY_BIN" >/dev/null 2>&1 && out="${out} [process]"
  printf '%s' "$out"
}

note_cdp() {
  [ -e "$STATE_DIR" ] && return 0
  say "Note: the wallet and session stay in the CDP project; remove them in the"
  say "CDP portal if you want a full cleanup."
}

verify_zero() {
  local left
  left="$(leftovers)"
  if [ -n "$left" ]; then
    say "LEFTOVER:${left}"
    say "  remove manually, or report: https://github.com/${REPO}/issues"
    return 1
  fi
  say "0 leftovers — the gateway is gone."
  note_cdp
}

# Selective runs keep what the user unchecked: leftovers are the choice, not a
# failure (only a full purge must end at zero).
report_kept() {
  local left
  left="$(leftovers)"
  if [ -n "$left" ]; then say "Kept (as selected):${left}"; else say "0 leftovers — the gateway is gone."; fi
  note_cdp
}

do_all() {
  say "Removing everything (no questions)..."
  purge_agents
  purge_program
  purge_data
  verify_zero
}

ask() { # $1=prompt $2=default (T|N); reads the terminal, not stdin
  local answer=""
  read -r -u 3 -p "$1 " answer || true
  answer="${answer:-$2}"
  case "$answer" in t|T|y|Y|tak|yes) return 0 ;; *) return 1 ;; esac
}

interactive() {
  say "x402 Gateway — deinstaller"
  say "Will remove:"
  say "  • MCP entries in AI agent configs (${AGENTS})"
  say "  • processes (daemon + MCP bridge), binary, QML plugin, helper, shell restart"
  say "  • data: ${STATE_DIR} (session, spend, policy, sellers, audit.log) and ${CONFIG_DIR}"
  say ""
  if ask "Remove EVERYTHING without further questions? [T/n]" T; then
    do_all
    return 0
  fi
  say ""
  local agents=0 program=0 data=0
  if ask "Remove MCP entries in agent configs? [T/n]" T; then agents=1; fi
  if ask "Remove the program (processes, binary, plugin, helper)? [T/n]" T; then program=1; fi
  if ask "Remove data (state + config, including audit.log)? [T/n]" T; then data=1; fi
  if [ $((agents + program + data)) -eq 0 ]; then
    say "Nothing selected — no changes."
    return 0
  fi
  say ""
  say "Summary: agents=${agents} program=${program} data=${data}"
  if ! ask "Proceed? [T/n]" T; then
    say "Cancelled — no changes."
    return 0
  fi
  if [ "$agents" = 1 ]; then purge_agents; fi
  if [ "$program" = 1 ]; then purge_program; fi
  if [ "$data" = 1 ]; then purge_data; fi
  report_kept
}

usage() { say "usage: uninstall.sh [--yes]"; }

case "${1:-}" in
  --yes) do_all ;;
  "")
    # Opening /dev/tty is the real test: a session without a controlling
    # terminal has the device node but cannot open it (setsid, CI, cron).
    if [ -r /dev/tty ] && exec 3<>/dev/tty 2>/dev/null; then
      interactive
    else
      say "No terminal (stdin is not a TTY) — interactive mode unavailable."
      say "To remove everything without questions, run:"
      say "  curl -fsSL -o /tmp/x402-uninstall.sh https://raw.githubusercontent.com/${REPO}/master/scripts/uninstall.sh"
      say "  bash /tmp/x402-uninstall.sh --yes"
      exit 1
    fi
    ;;
  *) usage; exit 1 ;;
esac
