#!/usr/bin/env bash
# Gateway installer: downloads the latest (or given) release (binary +
# plugin bundle), verifies sha256 + sigstore provenance (when `gh` is
# available), installs binary to ~/.local/bin, QML
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
# Env (mirrors / dev-test hooks):
#   GATEWAY_RELEASE_BASE  override release download base URL
#                         (default: https://github.com/$REPO/releases/download/$VERSION;
#                          e.g. file:///tmp/fakerelease for offline tests)
set -euo pipefail

REPO="gelu22/x402-gateway-omarchy"
BIN_DIR="${HOME}/.local/bin"
STATE_DIR="${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway"
SHARE_DIR="${HOME}/.local/share/x402-gateway"
PLUGIN_DIR="${HOME}/.config/omarchy/plugins/gelu22.gateway"
CONFIG_DIR="${HOME}/.config/omarchy/x402-gateway"
CONFIG_FILE="${CONFIG_DIR}/config.json"
PLUGIN_ID="gelu22.gateway"
ARCH="$(uname -m)"; case "$ARCH" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo "unsupported arch $ARCH"; exit 1;; esac

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

# Sigstore/GitHub-OIDC provenance (012.1): hard-fail on a BAD attestation;
# loud warning + sha256-only when verification is impossible (no `gh`,
# test-override base) or inconclusive (legacy release predating signing,
# unreachable/rate-limited API). sha256 stays the fast integrity layer.
verify_provenance() {  # $1=file
  local file="$1" out
  if [ -n "${GATEWAY_RELEASE_BASE:-}" ]; then
    echo "  ⚠ attestation skipped (GATEWAY_RELEASE_BASE=$GATEWAY_RELEASE_BASE test-override; files not from GitHub)"
    return 0
  fi
  if ! command -v gh >/dev/null 2>&1; then
    echo "  ⚠ WARNING: 'gh' not found — skipping sigstore attestation verify for $(basename "$file"); sha256 only. Install gh for full chain-of-trust."
    return 0
  fi
  if out=$(gh attestation verify "$file" --repo "$REPO" 2>&1); then
    echo "  ✓ attestation OK: $(basename "$file")"
    return 0
  fi
  case "$out" in
    *[Nn]"o attestation"*|*[Rr]"ate limit"*|*401*|*403*|*[Aa]"uthenticat"*|*[Nn]"etwork"*|*[Tt]"imeout"*|*"ould not resolve"*|*[Cc]"onnection"*|*TLS*|*"ertificate"*)
      printf '  ⚠ WARNING: attestation check inconclusive for %s; sha256 only (%s).\n' "$(basename "$file")" "$(printf '%s' "$out" | head -c 160)"
      return 0
      ;;
  esac
  echo "ATTESTATION FAILED for $file — possible tampering"
  printf '%s\n' "$out" | head -5
  exit 1
}

install_binary() {  # $1=tmpdir
  mkdir -p "$BIN_DIR" "$STATE_DIR" && chmod 700 "$STATE_DIR"
  install -m 755 "$1/gateway" "$BIN_DIR/gateway"
  echo "✓ installed $BIN_DIR/gateway"
  echo "  state dir: $STATE_DIR"
}

install_scripts() {  # $1=extracted bundle dir
  mkdir -p "$SHARE_DIR"
  install -m 755 "$1/scripts/setup-agents.sh" "$SHARE_DIR/"
  rm -f "$SHARE_DIR/remember-override.sh" # retired in 009.7 (config file now)
  echo "✓ installed helper script to $SHARE_DIR"
}

install_plugin() {  # $1=extracted bundle dir
  if [ ! -d "${HOME}/.config/omarchy" ]; then
    echo "  ⚠ no ~/.config/omarchy — skipping QML plugin (manual: copy plugin/omarchy/ to ~/.config/omarchy/plugins/$PLUGIN_ID)"
    return 0
  fi
  mkdir -p "$PLUGIN_DIR"
  cp "$1/plugin/omarchy/"*.qml "$1/plugin/omarchy/"*.js "$1/plugin/omarchy/manifest.json" "$PLUGIN_DIR/"
  # Release stamp (41.3): shipped in the bundle; absent in older bundles.
  if [ -f "$1/plugin/omarchy/build-info.json" ]; then
    cp "$1/plugin/omarchy/build-info.json" "$PLUGIN_DIR/"
  fi
  if command -v omarchy >/dev/null 2>&1; then
    omarchy plugin validate "$PLUGIN_DIR" && echo "✓ plugin validated"
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
  mkdir -p "$CONFIG_DIR"
  if [ -f "$CONFIG_FILE" ]; then
    echo "✓ plugin config kept (already exists): $CONFIG_FILE"
  elif [ -f "$1/config/gateway-config.json" ]; then
    install -m 644 "$1/config/gateway-config.json" "$CONFIG_FILE"
    echo "✓ seeded plugin config: $CONFIG_FILE"
  else
    echo "  ⚠ bundle has no config template — skipping seed"
  fi
}

do_install() {  # $1=version tag (TMP intentionally global: EXIT trap)
  local VERSION="$1"
  TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
  download_release "$VERSION" "$TMP"
  install_binary "$TMP"
  mkdir -p "$TMP/bundle" && tar -xzf "$TMP/plugin-bundle.tar.gz" -C "$TMP/bundle"
  install_scripts "$TMP/bundle"
  install_plugin "$TMP/bundle"
  install_config "$TMP/bundle"
  echo "✓ installed $BIN_DIR/gateway ($VERSION)"
  case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "  ⚠ add to PATH: export PATH=\"$BIN_DIR:\$PATH\"";; esac
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
    rm -f "$BIN_DIR/gateway"
    rm -rf "$PLUGIN_DIR" "$SHARE_DIR/setup-agents.sh" "$SHARE_DIR/remember-override.sh"
    echo "removed binary, plugin and helper script (state kept at $STATE_DIR, config kept at $CONFIG_FILE)"
    ;;
  purge)
    shift
    # A checkout next to install.sh wins (offline/testable); otherwise download
    # the deinstaller and run it (no pipe-to-shell).
    if [ -f "$(dirname "$0")/uninstall.sh" ]; then
      bash "$(dirname "$0")/uninstall.sh" "$@"
    else
      PURGE_TMP="$(mktemp -d)"; trap 'rm -rf "$PURGE_TMP"' EXIT
      fetch "https://raw.githubusercontent.com/$REPO/master/scripts/uninstall.sh" -o "$PURGE_TMP/uninstall.sh"
      bash "$PURGE_TMP/uninstall.sh" "$@"
    fi
    ;;
  install|latest)
    VERSION="$(fetch "https://api.github.com/repos/$REPO/releases/latest" | grep tag_name | cut -d'"' -f4)"
    do_install "$VERSION"
    ;;
  v*)
    do_install "$1"
    ;;
  *)
    echo "usage: install.sh [vX.Y.Z|verify|remove|purge [--yes]]"; exit 1;;
esac
