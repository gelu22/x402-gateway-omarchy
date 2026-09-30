#!/usr/bin/env bash
# Gateway installer: download+verify release, then `gateway install` (Go) mutates
# the filesystem (42.3). Bash never writes under ~/.local/bin or the plugin dir.
# Usage: install.sh [vX.Y.Z|verify|remove|purge [--yes]]
# Env: GATEWAY_RELEASE_BASE, GATEWAY_ALLOW_UNVERIFIED=1, GATEWAY_FORCE=1
set -euo pipefail

REPO="gelu22/x402-gateway-omarchy"
BIN_DIR="${HOME}/.local/bin"
STATE_DIR="${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway"
SHARE_DIR="${HOME}/.local/share/x402-gateway"
PLUGIN_DIR="${HOME}/.config/omarchy/plugins/gelu22.gateway"
CONFIG_DIR="${HOME}/.config/omarchy/x402-gateway"
CONFIG_FILE="${CONFIG_DIR}/config.json"
PLUGIN_ID="gelu22.gateway"
AGENTS="opencode,claude-code,cursor,codex,gemini"
GATEWAY_BIN="${BIN_DIR}/gateway"
ARCH="$(uname -m)"; case "$ARCH" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo "unsupported arch $ARCH"; exit 1;; esac

# Resolve an external tool to an absolute path. Rejects shell functions/aliases
# (type -P only returns executables). PATH control by an attacker is still RCE
# (SECURITY.md); this only removes the cheapest spoof.
resolve_tool() {
  local name="$1" path
  path="$(type -P "$name" 2>/dev/null || true)"
  if [ -z "$path" ] || [ ! -x "$path" ]; then
    echo "  ✗ required tool '$name' not found as an executable on PATH" >&2
    return 1
  fi
  # Prefer canonical path when available.
  if command -v realpath >/dev/null 2>&1; then
    path="$(realpath "$path")"
  fi
  printf '%s\n' "$path"
}

fetch() {
  local curl_bin
  curl_bin="$(resolve_tool curl)" || exit 1
  "$curl_bin" -fsSL -m 60 "$@"
}

check_file() {  # $1=dir/file $2=expected-sha
  local actual
  actual=$(sha256sum "$1" | awk '{print $1}')
  [ "$actual" = "$2" ] || { echo "CHECKSUM MISMATCH for $1"; exit 1; }
}


# is_ours: same contract as Go install.IsOurs — regular non-symlink file whose
# sha256 matches $STATE_DIR/installed.sha256 (sha256sum "sum  path" lines).
is_ours() {
  local path="$1" reg want got
  reg="${STATE_DIR}/installed.sha256"
  [ -f "$path" ] || return 1
  [ ! -L "$path" ] || return 1
  [ -f "$reg" ] || return 1
  # Match Go: line[66:] == path (64-hex digest + two spaces).
  want="$(awk -v p="$path" 'length($0) >= 67 && substr($0, 67) == p { print $1 }' "$reg" | tail -n1)"
  [ -n "$want" ] || return 1
  got="$(sha256sum "$path" | awk '{print $1}')"
  [ "$got" = "$want" ]
}


download_release() {  # $1=version $2=tmpdir; sets BUNDLE + SHA files
  local version="$1" tmp="$2" sums base
  base="${GATEWAY_RELEASE_BASE:-https://github.com/$REPO/releases/download/$version}"
  echo "→ downloading gateway-linux-$ARCH + plugin-bundle ($version)"
  fetch "$base/gateway-linux-$ARCH" -o "$tmp/gateway"
  fetch "$base/plugin-bundle.tar.gz" -o "$tmp/plugin-bundle.tar.gz"
  fetch "$base/sha256sums.txt" -o "$tmp/sha256sums.txt"
  sums="$tmp/sha256sums.txt"
  check_file "$tmp/gateway" "$(grep "gateway-linux-$ARCH$" "$sums" | awk '{print $1}')"
  check_file "$tmp/plugin-bundle.tar.gz" "$(grep "plugin-bundle.tar.gz$" "$sums" | awk '{print $1}')"
  verify_provenance "$tmp/gateway"
  verify_provenance "$tmp/plugin-bundle.tar.gz"
}

# Sigstore/GitHub-OIDC provenance: FAIL-CLOSED. A checksum downloaded from the
# same release as the binary is not an independent binding; the sigstore
# attestation (signed by the build workflow identity) is. Verification is
# therefore REQUIRED — a missing `gh` or an inconclusive result aborts the
# install. Two explicit escapes: GATEWAY_RELEASE_BASE (file:// test/dev source)
# and GATEWAY_ALLOW_UNVERIFIED=1 (deliberate, loud opt-out). sha256 stays as the
# fast integrity layer.
verify_provenance() {  # $1=file
  local file="$1" out
  # Offline/test source: only file:// is trusted without a signature.
  if [ -n "${GATEWAY_RELEASE_BASE:-}" ]; then
    case "$GATEWAY_RELEASE_BASE" in
      file://*)
        echo "  ⚠ attestation skipped (GATEWAY_RELEASE_BASE test-override, offline source)"
        return 0 ;;
      *)
        echo "  ✗ GATEWAY_RELEASE_BASE=$GATEWAY_RELEASE_BASE is not a file:// path." >&2
        echo "    A remote override needs GATEWAY_ALLOW_UNVERIFIED=1 (not recommended)." >&2
        [ "${GATEWAY_ALLOW_UNVERIFIED:-}" = "1" ] || exit 1
        echo "  ⚠ remote override: attestation skipped (GATEWAY_ALLOW_UNVERIFIED=1)"
        return 0 ;;
    esac
  fi
  if [ "${GATEWAY_ALLOW_UNVERIFIED:-}" = "1" ]; then
    echo "  ⚠ WARNING: attestation DISABLED (GATEWAY_ALLOW_UNVERIFIED=1) — sha256 only, no signature check"
    return 0
  fi
  local gh_bin
  if ! gh_bin="$(resolve_tool gh)"; then
    echo "  ✗ 'gh' is required to verify the release signature." >&2
    echo "    Install GitHub CLI (https://cli.github.com), or re-run with" >&2
    echo "    GATEWAY_ALLOW_UNVERIFIED=1 to install sha256-only (not recommended)." >&2
    exit 1
  fi
  # Pin the signer workflow and the tag, so any other workflow that can mint an
  # attestation in the repo is not accepted.
  if out=$("$gh_bin" attestation verify "$file" --repo "$REPO" \
      --signer-workflow "$REPO/.github/workflows/release.yml" \
      --source-ref "refs/tags/$RELEASE_VERSION" 2>&1); then
    echo "  ✓ attestation OK: $(basename "$file")"
    return 0
  fi
  echo "  ✗ ATTESTATION FAILED for $(basename "$file") — refusing to install." >&2
  printf '%s\n' "$out" | head -5 >&2
  exit 1
}

# -- lifecycle mutations live in Go (42.3): bash only downloads + verifies -----
lock_state() {
  mkdir -p "$STATE_DIR"
  exec 9>"$STATE_DIR/.lock"
  if ! flock -n 9; then
    echo "  ✗ another install/remove/purge is already running." >&2
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

run_gateway_install() {  # $1=tmpdir with gateway + extracted bundle/
  local tmp="$1" force=()
  [ "${GATEWAY_FORCE:-}" = "1" ] && force=(--force)
  chmod +x "$tmp/gateway"
  "$tmp/gateway" install --bundle "$tmp/bundle" --home "$HOME" --binary "$tmp/gateway" "${force[@]}"
}

run_self_remove() {  # $1=keep_state $2=keep_config
  local keep_state="$1" keep_config="$2" args=()
  [ "$keep_state" = 1 ] && args+=(--keep-state)
  [ "$keep_config" = 1 ] && args+=(--keep-config)
  [ "${GATEWAY_FORCE:-}" = "1" ] && args+=(--force)
  if [ -x "$GATEWAY_BIN" ]; then
    "$GATEWAY_BIN" self-remove --home "$HOME" "${args[@]}"
  else
    echo "  ⚠ no installed gateway binary — nothing to remove" >&2
  fi
}

do_install() {
  local VERSION="$1"
  RELEASE_VERSION="$VERSION"
  lock_state
  TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
  download_release "$VERSION" "$TMP"
  mkdir -p "$TMP/bundle"
  local tar_names unsafe_paths tar_verbose symlink_members
  tar_names="$(tar -tzf "$TMP/plugin-bundle.tar.gz")"
  unsafe_paths="$(printf '%s\n' "$tar_names" | grep -E '(^/|(^|/)\.\.(/|$))' || true)"
  if [ -n "$unsafe_paths" ]; then
    echo "  ✗ plugin bundle contains an unsafe path:" >&2
    printf '%s\n' "$unsafe_paths" | head -5 >&2
    exit 1
  fi
  tar_verbose="$(tar -tvzf "$TMP/plugin-bundle.tar.gz")"
  symlink_members="$(printf '%s\n' "$tar_verbose" | grep -E '(^l| -> )' || true)"
  if [ -n "$symlink_members" ]; then
    echo "  ✗ plugin bundle contains a symlink member:" >&2
    printf '%s\n' "$symlink_members" | head -5 >&2
    exit 1
  fi
  tar --no-same-owner --no-same-permissions -xzf "$TMP/plugin-bundle.tar.gz" -C "$TMP/bundle"
  run_gateway_install "$TMP"
  echo "✓ installed $BIN_DIR/gateway ($VERSION)"
  case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "  ⚠ add to PATH: export PATH=\"$BIN_DIR:\$PATH\"";; esac
}

do_purge() {
  local helper="${SHARE_DIR}/setup-agents.sh"
  if [ -x "$helper" ] && is_ours "$helper"; then
    "$helper" --remove "$AGENTS" \
      || echo "  ! setup-agents --remove failed (check .bak-* backups)" >&2
  elif [ -e "$helper" ]; then
    echo "  – skipping MCP cleanup (helper is not ours / not in registry)" >&2
  fi
  stop_daemon
  if command -v omarchy >/dev/null 2>&1; then
    echo "  → restarting the shell (unloads the plugin that respawns the daemon)"
    omarchy restart shell >/dev/null 2>&1 || true
    stop_daemon
  fi
  run_self_remove 0 0
  # Drop state/config again (SelfRemove already did; flock may keep an empty dir).
  rm -rf "$STATE_DIR" "$CONFIG_DIR" 2>/dev/null || true
  local left=""
  [ -e "$GATEWAY_BIN" ] && left="$left $GATEWAY_BIN"
  [ -e "$PLUGIN_DIR" ] && left="$left $PLUGIN_DIR"
  [ -e "$SHARE_DIR" ] && left="$left $SHARE_DIR"
  [ -e "$STATE_DIR" ] && left="$left $STATE_DIR"
  [ -e "$CONFIG_DIR" ] && left="$left $CONFIG_DIR"
  [ -n "$(daemon_pids)" ] && left="$left [process]"
  if [ -n "$left" ]; then
    echo "LEFTOVER:$left"
    echo "  remove manually, or report: https://github.com/$REPO/issues" >&2
    exit 1
  fi
  echo "0 leftovers — the gateway is gone."
}

case "${1:-install}" in
  verify)
    command -v gateway >/dev/null && echo "gateway installed: $(gateway --version 2>/dev/null || echo unknown)" \
      || { echo "gateway not found in PATH"; exit 1; }
    [ -f "$PLUGIN_DIR/manifest.json" ] && echo "plugin installed: $PLUGIN_DIR" \
      || echo "plugin not installed (Omarchy only)"
    [ -f "$CONFIG_FILE" ] && echo "plugin config: $CONFIG_FILE" \
      || echo "plugin config not seeded (Omarchy only)"
    [ -x "$SHARE_DIR/setup-agents.sh" ] \
      && echo "helper script installed" || echo "helper script missing"
    ;;
  remove)
    lock_state
    stop_daemon
    run_self_remove 1 1
    echo "removed binary, plugin and helper script (state kept at $STATE_DIR, config kept at $CONFIG_FILE)"
    ;;
  purge)
    shift
    [ "${1:-}" = "--yes" ] || {
      echo "usage: install.sh purge --yes  (full wipe; the interactive deinstaller is scripts/uninstall.sh in a checkout)" >&2
      exit 1
    }
    lock_state
    do_purge
    ;;
  install|latest)
    VERSION="$(fetch "https://api.github.com/repos/$REPO/releases/latest" | grep tag_name | cut -d'"' -f4)"
    case "$VERSION" in
      v[0-9]*.[0-9]*.[0-9]*) ;;
      *) echo "unexpected release tag from the API: '$VERSION'" >&2; exit 1 ;;
    esac
    do_install "$VERSION"
    ;;
  v*)
    case "$1" in
      v[0-9]*.[0-9]*.[0-9]*) ;;
      *) echo "unexpected version argument: '$1' (want vX.Y.Z)" >&2; exit 1 ;;
    esac
    do_install "$1"
    ;;
  *)
    echo "usage: install.sh [vX.Y.Z|verify|remove|purge [--yes]]"; exit 1;;
esac
