#!/usr/bin/env bash
# setup-agents.sh — opt-in MCP integration for detected AI agents.
# Modes:
#   (interactive)            T/N per detected agent (terminal use)
#   --detect                 JSON {agents:[{name,path,format,integrated}]} on stdout
#   --apply opencode,cursor  non-interactive full pipeline per listed agent
#   --remove opencode,cursor remove the injected MCP block per listed agent
# Safety rules (all modes): backup before write, validate after write,
# automatic rollback on validation failure. Zero silent writes.
set -euo pipefail

GATEWAY_BIN="${GATEWAY_BIN:-${HOME}/.local/bin/gateway}"
SOCKET_PATH="${GATEWAY_SOCKET_PATH:-${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway/gw.sock}"
SERVER_KEY="${SERVER_KEY:-x402-gateway}"

command -v python3 >/dev/null || { echo "ERROR: python3 required"; exit 1; }

# Known agents: name → "path|format"
agent_path() {
  case "$1" in
    opencode)    echo "$HOME/.config/opencode/opencode.json";;
    claude-code) echo "$HOME/.claude.json";;
    cursor)      echo "$HOME/.cursor/mcp.json";;
    codex)       echo "$HOME/.codex/config.toml";;
    gemini)      echo "$HOME/.gemini/settings.json";;
    *)           echo "";;
  esac
}
agent_format() {
  case "$1" in
    codex) echo "toml";;
    *)     echo "json";;
  esac
}

is_integrated() {
  local file="$1"
  [ -f "$file" ] && grep -q "$SERVER_KEY" "$file"
}

backup() {
  local f="$1"
  [ -f "$f" ] || return 0
  local bak="$f.bak-$(date +%Y%m%d%H%M%S)"
  cp "$f" "$bak" && chmod 600 "$bak" && echo "  backup: $bak"
}

restore() {
  local bak="$1" target="$2"
  [ -f "$bak" ] && cp "$bak" "$target" && echo "  ROLLED BACK: $target restored from backup"
}

validate_json() { python3 -m json.tool "$1" >/dev/null 2>&1; }
validate_toml() { python3 -c "import tomllib,sys; tomllib.load(open(sys.argv[1],'rb'))" "$1" 2>/dev/null; }

inject_json() {
  # opencode uses the native "mcp" container with its own entry shape; all other
  # JSON agents use "mcpServers" with the Claude-style shape.
  local file="$1" name="$2"
  [ -f "$file" ] || { mkdir -p "$(dirname "$file")"; echo '{}' > "$file"; }
  python3 - "$file" "$GATEWAY_BIN" "$SOCKET_PATH" "$SERVER_KEY" "$name" <<'PY'
import json, os, sys, tempfile
path, binpath, sock, key, name = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
container = "mcp" if name == "opencode" else "mcpServers"
with open(path) as fh:
    data = json.load(fh)
servers = data.setdefault(container, {})
if key in servers:
    print(f"SKIP {key} already integrated")
    sys.exit(0)
if name == "opencode":
    servers[key] = {
        "type": "local",
        "command": [binpath, "-mcp"],
        "environment": {"GATEWAY_SOCKET_PATH": sock},
        "enabled": True,
    }
else:
    servers[key] = {
        "command": binpath,
        "args": ["-mcp"],
        "env": {"GATEWAY_SOCKET_PATH": sock},
    }
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path))
with os.fdopen(fd, "w") as fh:
    json.dump(data, fh, indent=2)
os.replace(tmp, path)
PY
}

inject_toml() {
  local file="$1"
  if grep -q "\[mcp_servers.$SERVER_KEY\]" "$file" 2>/dev/null; then
    echo "SKIP $SERVER_KEY already integrated"
    return 0
  fi
  cat >> "$file" <<EOF

[mcp_servers.$SERVER_KEY]
command = "$GATEWAY_BIN"
args = ["-mcp"]
env = { GATEWAY_SOCKET_PATH = "$SOCKET_PATH" }
EOF
}

remove_json() {
  # Deletes the key from the agent's native container; for opencode also strips
  # a legacy "mcpServers" entry (wrong container written by older script).
  # Shape-agnostic: only the key is removed, siblings untouched.
  local file="$1" name="$2"
  python3 - "$file" "$SERVER_KEY" "$name" <<'PY'
import json, os, sys, tempfile
path, key, name = sys.argv[1], sys.argv[2], sys.argv[3]
primary = "mcp" if name == "opencode" else "mcpServers"
containers = [primary]
if name == "opencode":
    containers.append("mcpServers")
with open(path) as fh:
    data = json.load(fh)
found = False
for container in containers:
    servers = data.get(container, {})
    if key in servers:
        del servers[key]
        found = True
    if container in data and not data[container]:
        data.pop(container, None)   # drop the now-empty container
if not found:
    print(f"SKIP {key} not-integrated")
    sys.exit(0)
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path))
with os.fdopen(fd, "w") as fh:
    json.dump(data, fh, indent=2)
os.replace(tmp, path)
PY
}

remove_toml() {
  local file="$1"
  python3 - "$file" "$SERVER_KEY" <<'PY'
import os, sys, tempfile
path, key = sys.argv[1], sys.argv[2]
header = "[mcp_servers.%s]" % key
with open(path) as fh:
    lines = fh.readlines()
out, skip = [], False
for ln in lines:
    if ln.strip() == header:
        skip = True
        continue
    if skip and ln.lstrip().startswith("["):
        skip = False
    if not skip:
        out.append(ln)
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path))
with os.fdopen(fd, "w") as fh:
    fh.write("".join(out).rstrip("\n") + ("\n" if out else ""))
os.replace(tmp, path)
PY
}

# integrate_one NAME → prints "OK name" / "SKIP name reason" / "FAIL name reason"
integrate_one() {
  local name="$1"
  local file fmt bak
  file="$(agent_path "$name")"
  [ -n "$file" ] || { echo "FAIL $name unknown-agent"; return 1; }
  fmt="$(agent_format "$name")"

  if is_integrated "$file"; then
    echo "SKIP $name already-integrated"
    return 0
  fi

  mkdir -p "$(dirname "$file")"
  bak="$file.bak-$(date +%Y%m%d%H%M%S)"
  [ -f "$file" ] && cp "$file" "$bak" && chmod 600 "$bak" && echo "  backup: $bak"

  case "$fmt" in
    json) inject_json "$file" "$name" ;;
    toml) inject_toml "$file" ;;
  esac

  # A write that does not validate is rolled back, never reported OK. (The old
  # `|| true` swallowed this: a corrupt file with an unrelated valid backup
  # passed, and no restore happened.)
  if [ "$fmt" = "json" ]; then
    validate_json "$file" || { echo "FAIL $name invalid-after-write"; [ -f "$bak" ] && restore "$bak" "$file"; return 1; }
  else
    validate_toml "$file" || { echo "FAIL $name invalid-after-write"; [ -f "$bak" ] && restore "$bak" "$file"; return 1; }
  fi

  # Final authority: integration marker present?
  if grep -q "$SERVER_KEY" "$file" 2>/dev/null; then
    rm -f "$bak" # success: do not leave a copy behind (agent configs may hold secrets)
    echo "OK $name"
    return 0
  fi
  echo "FAIL $name marker-missing"
  [ -f "$bak" ] && restore "$bak" "$file"
  return 1
}

# remove_one NAME → prints "OK name" / "SKIP name reason" / "FAIL name reason".
# Exact inverse of integrate_one: backup, strip the injected block, confirm the
# marker is gone, else roll back to the integrated file.
remove_one() {
  local name="$1"
  local file fmt bak
  file="$(agent_path "$name")"
  [ -n "$file" ] || { echo "FAIL $name unknown-agent"; return 1; }
  fmt="$(agent_format "$name")"

  [ -f "$file" ] || { echo "SKIP $name no-file"; return 0; }
  if ! is_integrated "$file"; then
    echo "SKIP $name not-integrated"
    return 0
  fi

  bak="$file.bak-$(date +%Y%m%d%H%M%S)"
  cp "$file" "$bak" && chmod 600 "$bak" && echo "  backup: $bak"

  # Guarded: a strip failure (corrupt input) must roll back, not abort via set -e.
  case "$fmt" in
    json) remove_json "$file" "$name" || { restore "$bak" "$file"; echo "FAIL $name strip-failed"; return 1; } ;;
    toml) remove_toml "$file" || { restore "$bak" "$file"; echo "FAIL $name strip-failed"; return 1; } ;;
  esac

  # Final authority: integration marker gone?
  if ! is_integrated "$file"; then
    rm -f "$bak"
    echo "OK $name"
    return 0
  fi

  restore "$bak" "$file"
  echo "FAIL $name marker-present"
  return 1
}

cmd_detect() {
  printf '{"agents":['
  local first=1 name file
  for name in opencode claude-code cursor codex gemini; do
    file="$(agent_path "$name")"
    [ -n "$file" ] || continue
    [ -f "$file" ] || continue
    [ $first -eq 1 ] || printf ','
    first=0
    if is_integrated "$file"; then integrated=true; else integrated=false; fi
    printf '{"name":"%s","path":"%s","format":"%s","integrated":%s}' \
      "$name" "$file" "$(agent_format "$name")" "$integrated"
  done
  printf ']}'
  echo
}

cmd_apply() {
  local csv="$1" rc=0 name
  IFS=',' read -ra LIST <<< "$csv"
  declare -A seen
  for name in "${LIST[@]}"; do
    name="$(echo "$name" | tr -d '[:space:]')"
    [ -n "$name" ] || continue
    seen["$name"]=1
    integrate_one "$name" || rc=1
  done
  # Names requested twice collapse via seen map (no double-inject).
  return $rc
}

cmd_remove() {
  local csv="$1" rc=0 name
  IFS=',' read -ra LIST <<< "$csv"
  declare -A seen
  for name in "${LIST[@]}"; do
    name="$(echo "$name" | tr -d '[:space:]')"
    [ -n "$name" ] || continue
    if [ -n "${seen[$name]+x}" ]; then continue; fi
    seen["$name"]=1
    remove_one "$name" || rc=1
  done
  return $rc
}

cmd_interactive() {
  echo "=== x402 Gateway — AI agent integration ==="
  echo "MCP server to be added:"
  echo "  command: $GATEWAY_BIN -mcp"
  echo "  socket:  $SOCKET_PATH"
  echo
  local any=0 name file
  for name in opencode claude-code cursor codex gemini; do
    file="$(agent_path "$name")"
    [ -n "$file" ] && [ -f "$file" ] && { echo "Found: $name ($file)"; any=1; }
  done
  [ $any -eq 0 ] && { echo "No known agent configs found."; exit 0; }
  echo
  for name in opencode claude-code cursor codex gemini; do
    file="$(agent_path "$name")"
    [ -n "$file" ] && [ -f "$file" ] || continue
    printf "Integrate %s? [y/N] " "$name"
    read -r answer
    case "$answer" in
      y|Y|yes|YES|t|T|tak|TAK) integrate_one "$name" || true;;
      *) echo "  skipped";;
    esac
  done
  echo
  echo "Done. Restarting the agent may be required."
}

case "${1:-}" in
  --detect) cmd_detect ;;
  --apply)  [ -n "${2:-}" ] || { echo "usage: $0 --apply agent1,agent2"; exit 1; }
            cmd_apply "$2" ;;
  --remove) [ -n "${2:-}" ] || { echo "usage: $0 --remove agent1,agent2"; exit 1; }
            cmd_remove "$2" ;;
  *)        cmd_interactive ;;
esac
