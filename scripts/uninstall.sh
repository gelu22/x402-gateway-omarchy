#!/usr/bin/env bash
# uninstall.sh — x402 Gateway deinstaller.
# Modes: interactive (asks) or --yes (full wipe). Program/data mutations go
# through `gateway self-remove` (42.3); bash keeps lock, stop, and agent helper.
set -euo pipefail

REPO="${X402_REPO:-gelu22/x402-gateway-omarchy}"
BIN_DIR="${HOME}/.local/bin"
GATEWAY_BIN="${BIN_DIR}/gateway"
STATE_DIR="${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway"
SHARE_DIR="${HOME}/.local/share/x402-gateway"
PLUGIN_DIR="${HOME}/.config/omarchy/plugins/gelu22.gateway"
CONFIG_DIR="${HOME}/.config/omarchy/x402-gateway"
AGENTS="opencode,claude-code,cursor,codex,gemini"

say() { printf '%s\n' "$*"; }

lock_state() {
  mkdir -p "$STATE_DIR"
  exec 9>"$STATE_DIR/.lock"
  if ! flock -n 9; then
    say "  ✗ another install/remove/purge is already running." >&2
    exit 1
  fi
}

daemon_pids() {
  local p pid
  for p in /proc/[0-9]*/exe; do
    pid="${p#/proc/}"; pid="${pid%/exe}"
    [ "$(readlink "$p" 2>/dev/null)" = "$GATEWAY_BIN" ] && printf '%s\n' "$pid"
  done
}

stop_daemon() {
  local pid
  for pid in $(daemon_pids); do kill -TERM "$pid" 2>/dev/null || true; done
  sleep 1
  for pid in $(daemon_pids); do kill -KILL "$pid" 2>/dev/null || true; done
}

purge_agents() {
  local helper="${SHARE_DIR}/setup-agents.sh"
  if [ ! -x "$helper" ]; then
    say "  – no ${helper} (skipping MCP entries)"
    return 0
  fi
  "$helper" --remove "$AGENTS" || say "  ! setup-agents --remove failed (check .bak-* backups)"
}

purge_program() {
  stop_daemon
  if command -v omarchy >/dev/null 2>&1; then
    say "  → restarting the shell (unloads the plugin that respawns the daemon)"
    omarchy restart shell >/dev/null 2>&1 || true
    stop_daemon
  fi
  if [ -x "$GATEWAY_BIN" ]; then
    "$GATEWAY_BIN" self-remove --home "$HOME" --keep-state --keep-config || true
  else
    say "  ⚠ no installed gateway binary — skipping program remove"
  fi
}

purge_data() {
  if [ -x "$GATEWAY_BIN" ]; then
    "$GATEWAY_BIN" self-remove --home "$HOME" || true
  fi
  rm -rf "$STATE_DIR" "$CONFIG_DIR"
}

leftovers() {
  local left=""
  [ -e "$GATEWAY_BIN" ] && left="$left $GATEWAY_BIN"
  [ -e "$PLUGIN_DIR" ] && left="$left $PLUGIN_DIR"
  [ -e "$SHARE_DIR" ] && left="$left $SHARE_DIR"
  [ -e "$STATE_DIR" ] && left="$left $STATE_DIR"
  [ -e "$CONFIG_DIR" ] && left="$left $CONFIG_DIR"
  [ -n "$(daemon_pids)" ] && left="$left [process]"
  printf '%s' "$left"
}

verify_zero() {
  local left
  left="$(leftovers)"
  if [ -n "$left" ]; then
    say "LEFTOVER:$left"
    say "  remove manually, or report: https://github.com/$REPO/issues" >&2
    exit 1
  fi
  say "0 leftovers — the gateway is gone."
}

do_all() {
  lock_state
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
  lock_state
  say "x402 Gateway — deinstaller"
  say "Will remove:"
  say "  • MCP entries in AI agent configs (${AGENTS})"
  say "  • processes (daemon + MCP bridge), binary, QML plugin, helper, shell restart"
  say "  • data: ${STATE_DIR} (session, spend, policy, sellers, audit.log) and ${CONFIG_DIR}"
  say ""
  if ask "Remove EVERYTHING without further questions? [y/N]" N; then
    do_all
    return 0
  fi
  say ""
  local agents=0 program=0 data=0
  if ask "Remove MCP entries in agent configs? [Y/n]" Y; then agents=1; fi
  if ask "Remove the program (processes, binary, plugin, helper)? [Y/n]" Y; then program=1; fi
  if ask "Remove data (state + config, including audit.log)? [y/N]" N; then data=1; fi
  if [ $((agents + program + data)) -eq 0 ]; then
    say "Nothing selected — no changes."
    return 0
  fi
  say ""
  say "Summary: agents=${agents} program=${program} data=${data}"
  if ! ask "Proceed? [Y/n]" Y; then
    say "Cancelled — no changes."
    return 0
  fi
  if [ "$agents" = 1 ]; then purge_agents; fi
  if [ "$program" = 1 ]; then purge_program; fi
  if [ "$data" = 1 ]; then purge_data; fi
  local left
  left="$(leftovers)"
  if [ -n "$left" ]; then say "Kept (as selected):${left}"; else say "0 leftovers — the gateway is gone."; fi
}

usage() { say "usage: uninstall.sh [--yes]"; }

case "${1:-}" in
  --yes) do_all ;;
  "")
    if [ -r /dev/tty ] && exec 3<>/dev/tty 2>/dev/null; then
      interactive
    else
      say "No terminal (stdin is not a TTY) — interactive mode unavailable."
      say "To remove everything without questions, pick a released tag, then run:"
      say "  TAG=<released tag> && TMP=\"\$(mktemp -d)\" && curl -fsSL -o \"\$TMP/uninstall.sh\" \"https://github.com/${REPO}/releases/download/\$TAG/uninstall.sh\" && gh attestation verify \"\$TMP/uninstall.sh\" --repo ${REPO} --signer-workflow ${REPO}/.github/workflows/release.yml --source-ref \"refs/tags/\$TAG\" && bash \"\$TMP/uninstall.sh\" --yes"
      exit 1
    fi
    ;;
  *) usage; exit 1 ;;
esac
