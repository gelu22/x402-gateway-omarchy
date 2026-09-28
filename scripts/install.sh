#!/usr/bin/env bash
# Gateway installer: downloads the latest (or given) release (binary +
# plugin bundle), verifies sha256 AND requires a valid sigstore provenance
# attestation (fail-closed), installs binary to ~/.local/bin, QML
# plugin to ~/.config/omarchy/plugins/gelu22.gateway (Omarchy only),
# helper script to ~/.local/share/x402-gateway, seeds the plugin config at
# ~/.config/omarchy/x402-gateway/config.json (only when absent), state dir 0700.
# Usage:
#   install.sh              # latest release
#   install.sh v0.1.0       # specific version
#   install.sh verify       # only verify an existing installation
#   install.sh remove       # uninstall (keeps state dir AND user config)
#   install.sh purge [--yes] # full uninstall via scripts/uninstall.sh
#                            # (interactive without --yes; wipes state+config)
# Env:
#   GATEWAY_RELEASE_BASE        override release download base URL
#                               (default: https://github.com/$REPO/releases/download/$VERSION;
#                                e.g. file:///tmp/fakerelease for offline tests)
#   GATEWAY_ALLOW_UNVERIFIED=1  deliberately skip the signature check (sha256
#                               only; for offline/dev — not recommended)
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

# -- installed-file registry (safety) ----------------------------------------
# $STATE_DIR/installed.sha256 records what THIS installer put on disk, so an
# install/update/remove never overwrites or deletes a path it did not create.
REGISTRY="$STATE_DIR/installed.sha256"

is_ours() {  # $1=path: true iff it is a regular file whose sha matches the recording
  local want
  [ -f "$1" ] && [ ! -L "$1" ] && [ -f "$REGISTRY" ] || return 1
  # sha256sum prints "HASH<space><space>PATH" (64 hex + 2 = path at col 67);
  # splitting on whitespace breaks paths that contain spaces ($HOME with a space).
  want="$(awk -v p="$1" 'substr($0,67)==p {print $1}' "$REGISTRY" | tail -1)"
  [ -n "$want" ] || return 1
  [ "$(sha256sum "$1" | awk '{print $1}')" = "$want" ]
}

registry_set() {  # $1=path: record/refresh its sha
  [ -f "$1" ] || return 0
  local dir tmp
  dir="$(dirname "$REGISTRY")"; mkdir -p "$dir"
  tmp="$(mktemp "$dir/.reg.XXXXXX")"
  if [ -f "$REGISTRY" ]; then
    awk -v p="$1" 'substr($0,67)!=p' "$REGISTRY" > "$tmp"
  fi
  sha256sum "$1" >> "$tmp"
  mv "$tmp" "$REGISTRY"
}

no_symlink() {  # $1=path: refuse a symlinked target (writes would follow it out)
  if [ -L "$1" ]; then
    echo "  ✗ $1 is a symlink — refusing to write through it." >&2
    exit 1
  fi
}

# Single-instance guard: two concurrent installs/removals must not interleave.
lock_state() {
  mkdir -p "$STATE_DIR"
  exec 9>"$STATE_DIR/.lock"
  if ! flock -n 9; then
    echo "  ✗ another install/remove/purge is already running." >&2
    exit 1
  fi
}

# The daemon's pid(s), matched by the /proc/<pid>/exe target (not a `pkill -f`
# regex over the command line, which also matches unrelated processes).
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

plugin_id_at() {  # $1=plugin dir: prints the manifest id, or ""
  [ -f "$1/manifest.json" ] || return 0
  sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1/manifest.json" | head -1
}

fetch() { curl -fsSL -m 60 "$@"; }

check_file() {  # $1=dir/file $2=expected-sha
  local actual
  actual=$(sha256sum "$1" | awk '{print $1}')
  [ "$actual" = "$2" ] || { echo "CHECKSUM MISMATCH for $1"; exit 1; }
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
  if ! command -v gh >/dev/null 2>&1; then
    echo "  ✗ 'gh' is required to verify the release signature." >&2
    echo "    Install GitHub CLI (https://cli.github.com), or re-run with" >&2
    echo "    GATEWAY_ALLOW_UNVERIFIED=1 to install sha256-only (not recommended)." >&2
    exit 1
  fi
  # Pin the signer workflow and the tag, so any other workflow that can mint an
  # attestation in the repo is not accepted.
  if out=$(gh attestation verify "$file" --repo "$REPO" \
      --signer-workflow "$REPO/.github/workflows/release.yml" \
      --source-ref "refs/tags/$RELEASE_VERSION" 2>&1); then
    echo "  ✓ attestation OK: $(basename "$file")"
    return 0
  fi
  echo "  ✗ ATTESTATION FAILED for $(basename "$file") — refusing to install." >&2
  printf '%s\n' "$out" | head -5 >&2
  exit 1
}

install_binary() {  # $1=tmpdir
  no_symlink "$STATE_DIR"; no_symlink "$GATEWAY_BIN"
  if [ -e "$GATEWAY_BIN" ] && ! is_ours "$GATEWAY_BIN"; then
    echo "  ✗ $GATEWAY_BIN already exists and was not installed by this installer." >&2
    echo "    Move it away first, or re-run with GATEWAY_FORCE=1 to overwrite." >&2
    [ "${GATEWAY_FORCE:-}" = "1" ] || exit 1
  fi
  mkdir -p "$BIN_DIR" "$STATE_DIR" && chmod 700 "$STATE_DIR"
  install -m 755 "$1/gateway" "$GATEWAY_BIN"
  registry_set "$GATEWAY_BIN"
  echo "✓ installed $GATEWAY_BIN"
  echo "  state dir: $STATE_DIR"
}

install_scripts() {  # $1=extracted bundle dir
  no_symlink "$SHARE_DIR"; no_symlink "$SHARE_DIR/setup-agents.sh"
  if [ -e "$SHARE_DIR/setup-agents.sh" ] && ! is_ours "$SHARE_DIR/setup-agents.sh"; then
    echo "  ✗ $SHARE_DIR/setup-agents.sh already exists and was not installed by this installer." >&2
    echo "    Move it away first, or re-run with GATEWAY_FORCE=1 to overwrite." >&2
    [ "${GATEWAY_FORCE:-}" = "1" ] || exit 1
  fi
  mkdir -p "$SHARE_DIR"
  install -m 755 "$1/scripts/setup-agents.sh" "$SHARE_DIR/"
  registry_set "$SHARE_DIR/setup-agents.sh"
  # Retired file (009.7): remove it only when it is ours.
  if [ -e "$SHARE_DIR/remember-override.sh" ] && ! is_ours "$SHARE_DIR/remember-override.sh"; then
    echo "  ⚠ keeping $SHARE_DIR/remember-override.sh (not ours)"
  else
    rm -f "$SHARE_DIR/remember-override.sh"
  fi
  echo "✓ installed helper script to $SHARE_DIR"
}

install_plugin() {  # $1=extracted bundle dir
  if [ ! -d "${HOME}/.config/omarchy" ]; then
    echo "  ⚠ no ~/.config/omarchy — skipping QML plugin (manual: copy plugin/omarchy/ to ~/.config/omarchy/plugins/$PLUGIN_ID)"
    return 0
  fi
  if [ -e "$PLUGIN_DIR" ] && [ ! -d "$PLUGIN_DIR" ] && [ ! -L "$PLUGIN_DIR" ]; then
    echo "  ✗ $PLUGIN_DIR exists and is not a directory — refusing." >&2
    exit 1
  fi
  no_symlink "$PLUGIN_DIR"
  existing_id="$(plugin_id_at "$PLUGIN_DIR")"
  if [ -n "$existing_id" ] && [ "$existing_id" != "$PLUGIN_ID" ]; then
    echo "  ✗ $PLUGIN_DIR holds plugin '$existing_id' — refusing to overwrite." >&2
    exit 1
  fi
  mkdir -p "$PLUGIN_DIR"
  cp "$1/plugin/omarchy/"*.qml "$1/plugin/omarchy/"*.js "$1/plugin/omarchy/manifest.json" "$PLUGIN_DIR/"
  # Release stamp (41.3): shipped in the bundle; absent in older bundles.
  if [ -f "$1/plugin/omarchy/build-info.json" ]; then
    cp "$1/plugin/omarchy/build-info.json" "$PLUGIN_DIR/"
  fi
  if command -v omarchy >/dev/null 2>&1; then
    if omarchy plugin validate "$PLUGIN_DIR"; then
      echo "✓ plugin validated"
    else
      echo "  ✗ plugin validation failed — refusing to install a broken plugin." >&2
      exit 1
    fi
  else
    echo "  ⚠ omarchy CLI not found — skipping plugin validation"
  fi
  echo "✓ installed QML plugin to $PLUGIN_DIR"
}

install_config() {  # $1=extracted bundle dir; seeds template only when absent
  if [ ! -d "${HOME}/.config/omarchy" ]; then
    echo "  ⚠ no ~/.config/omarchy — skipping plugin config seed"
    return 0
  fi
  mkdir -p "$CONFIG_DIR" && chmod 700 "$CONFIG_DIR"
  if [ -f "$CONFIG_FILE" ]; then
    echo "✓ plugin config kept (already exists): $CONFIG_FILE"
  elif [ -f "$1/config/gateway-config.json" ]; then
    install -m 600 "$1/config/gateway-config.json" "$CONFIG_FILE"
    echo "✓ seeded plugin config: $CONFIG_FILE"
  else
    echo "  ⚠ bundle has no config template — skipping seed"
  fi
}

do_install() {  # $1=version tag (TMP intentionally global: EXIT trap)
  local VERSION="$1"
  RELEASE_VERSION="$VERSION" # used by verify_provenance --source-ref
  lock_state
  TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
  download_release "$VERSION" "$TMP"
  install_binary "$TMP"
  mkdir -p "$TMP/bundle"
  # Reject absolute / traversal / symlink members before extracting.
  # No pipes: under `set -euo pipefail` a `tar | grep -q` would SIGPIPE tar when
  # grep exits early, and pipefail would turn the rejection into a silent pass.
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
  install_scripts "$TMP/bundle"
  install_plugin "$TMP/bundle"
  install_config "$TMP/bundle"
  echo "✓ installed $BIN_DIR/gateway ($VERSION)"
  case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "  ⚠ add to PATH: export PATH=\"$BIN_DIR:\$PATH\"";; esac
}

# Full wipe, inlined: never execute a downloaded or sibling script (a mutable
# uninstall.sh fetched over the network, or a foreign /tmp/uninstall.sh, would be
# unverified code execution). Only files this installer owns are removed.
do_purge() {
  if [ -x "$SHARE_DIR/setup-agents.sh" ] && is_ours "$SHARE_DIR/setup-agents.sh"; then
    "$SHARE_DIR/setup-agents.sh" --remove "$AGENTS" \
      || echo "  ! setup-agents --remove failed (check .bak-* backups)" >&2
  fi
  if [ -d "$PLUGIN_DIR" ] && [ "$(plugin_id_at "$PLUGIN_DIR")" = "$PLUGIN_ID" ]; then
    rm -rf "$PLUGIN_DIR"
  elif [ -e "$PLUGIN_DIR" ]; then
    echo "  ⚠ keeping $PLUGIN_DIR (not this plugin)"
  fi
  if is_ours "$GATEWAY_BIN"; then
    stop_daemon
    if command -v omarchy >/dev/null 2>&1; then
      echo "  → restarting the shell (unloads the plugin that respawns the daemon)"
      omarchy restart shell >/dev/null 2>&1 || true
      stop_daemon
    fi
    rm -f "$GATEWAY_BIN"
  elif [ -e "$GATEWAY_BIN" ]; then
    echo "  ⚠ keeping $GATEWAY_BIN (not installed by this installer)"
  fi
  for f in setup-agents.sh remember-override.sh; do
    p="$SHARE_DIR/$f"
    if [ -e "$p" ] && is_ours "$p"; then rm -f "$p"; fi
  done
  rmdir "$SHARE_DIR" 2>/dev/null || true
  rm -rf "$STATE_DIR" "$CONFIG_DIR"

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
    if [ -e "$GATEWAY_BIN" ] && ! is_ours "$GATEWAY_BIN"; then
      echo "  ⚠ keeping $GATEWAY_BIN (not installed by this installer)"
    else
      rm -f "$GATEWAY_BIN"
    fi
    if [ -d "$PLUGIN_DIR" ] && [ "$(plugin_id_at "$PLUGIN_DIR")" = "$PLUGIN_ID" ]; then
      rm -rf "$PLUGIN_DIR"
    elif [ -e "$PLUGIN_DIR" ]; then
      echo "  ⚠ keeping $PLUGIN_DIR (not this plugin)"
    fi
    if [ -e "$SHARE_DIR/setup-agents.sh" ] && ! is_ours "$SHARE_DIR/setup-agents.sh"; then
      echo "  ⚠ keeping $SHARE_DIR/setup-agents.sh (not ours)"
    else
      rm -f "$SHARE_DIR/setup-agents.sh"
    fi
    if [ -e "$SHARE_DIR/remember-override.sh" ] && ! is_ours "$SHARE_DIR/remember-override.sh"; then
      echo "  ⚠ keeping $SHARE_DIR/remember-override.sh (not ours)"
    else
      rm -f "$SHARE_DIR/remember-override.sh"
    fi
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
