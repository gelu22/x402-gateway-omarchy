#!/usr/bin/env bash
# setup-agents.sh — opt-in MCP integration for detected AI agents.
# Modes:
#   (interactive)            T/N per detected connectable agent (terminal use)
#   --detect                 JSON {agents:[{name,path,format,integrated,connectable}]} on stdout
#   --apply opencode,cursor  non-interactive full pipeline per listed agent
#   --remove opencode,cursor remove the injected MCP block per listed agent
# Safety rules (all modes): backup before write, validate after write,
# automatic rollback on validation failure. Zero silent writes.
# Detection never executes agent launchers. Presence = Omarchy present:
# non-stub bin under ~/.local/bin, or `mise where <package>` (never `mise use`).
set -euo pipefail

GATEWAY_BIN="${GATEWAY_BIN:-${HOME}/.local/bin/gateway}"
SOCKET_PATH="${GATEWAY_SOCKET_PATH:-${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway/gw.sock}"
SERVER_KEY="${SERVER_KEY:-x402-gateway}"

# Literal detect order: five connectable writers first, then detect-only launchers.
DETECT_NAMES=(
  opencode claude-code cursor codex gemini
  pi omp grok copilot crush openclaw hermes muse cursor-agent ori agy
)

command -v python3 >/dev/null || { echo "ERROR: python3 required"; exit 1; }

# Known connectable agents: name → config path (empty = detect-only / unknown).
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

# Binary name under ~/.local/bin (cursor writer uses Cursor CLI launcher).
agent_launcher() {
  case "$1" in
    opencode)                         echo "opencode";;
    claude-code)                      echo "claude";;
    cursor)                           echo "cursor-agent";;
    codex)                            echo "codex";;
    gemini)                           echo "gemini";;
    pi|omp|grok|copilot|crush|openclaw|hermes|muse|cursor-agent|ori|agy)
                                      echo "$1";;
    *)                                echo "";;
  esac
}

# mise package id (Omarchy omarchy-default-agent map). Empty → no mise where.
agent_package() {
  case "$1" in
    claude-code) echo "claude";;
    cursor)      echo "cursor-agent";;
    grok)        echo "npm:@xai-official/grok";;
    omp)         echo "github:can1357/oh-my-pi";;
    muse)        echo "http:muse[url=https://api.meta.ai/muse-launcher.sh,bin=muse,version_list_url=https://api.meta.ai/muse-code/channels/muse-stable,version_json_path=.version]";;
    hermes|openclaw) echo "";;
    *)           agent_launcher "$1";;
  esac
}

agent_installer() {
  case "$1" in
    hermes)   echo "omarchy-install-hermes-cli";;
    openclaw) echo "omarchy-install-openclaw-cli";;
    *)        echo "";;
  esac
}

# Omarchy user_install: symlink-to-file or non-stub file (cold stub has `mise use -g`).
# Symlink-to-directory is not a launcher (stricter than Omarchy; avoids false present).
user_bin_installed() {
  local bin="$1" p
  [ -n "$bin" ] || return 1
  p="$HOME/.local/bin/$bin"
  [ -x "$p" ] || return 1
  if [ -L "$p" ]; then
    [ -f "$p" ] || return 1
    return 0
  fi
  [ -f "$p" ] || return 1
  ! grep -q '^mise use -g' "$p"
}

# Really installed = Omarchy agent_present (never exec launcher / never mise use).
really_installed() {
  local name="$1" launcher pkg installer
  launcher="$(agent_launcher "$name")"
  installer="$(agent_installer "$name")"
  if [ -n "$installer" ]; then
    if command -v "$installer" >/dev/null 2>&1; then
      "$installer" --check >/dev/null 2>&1 && return 0
    fi
    return 1
  fi
  user_bin_installed "$launcher" && return 0
  pkg="$(agent_package "$name")"
  [ -n "$pkg" ] || return 1
  command -v mise >/dev/null 2>&1 || return 1
  mise where "$pkg" >/dev/null 2>&1
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

# Writer exit codes shared by inject_json/inject_toml: nothing to write, the
# file is left untouched. Mapped to "SKIP name reason" by integrate_one.
RC_LABELED=3 # our entry, already carries GATEWAY_AGENT
RC_FOREIGN=4 # the key exists but the entry is not ours

# GATEWAY_AGENT value for the entry: the writer name, pinned to the agentlabel
# regex (internal/agentlabel). All five writer names match by construction;
# anything else is a bug here, not user input, so refuse to write it.
agent_label() {
  local name="$1"
  [[ "$name" =~ ^[a-z0-9][a-z0-9._-]{0,31}$ ]] \
    || { echo "ERROR: invalid agent label: $name" >&2; return 1; }
  printf '%s' "$name"
}

inject_json() {
  # opencode uses the native "mcp" container with its own entry shape; all other
  # JSON agents use "mcpServers" with the Claude-style shape.
  local file="$1" name="$2" label
  label="$(agent_label "$name")"
  [ -f "$file" ] || { mkdir -p "$(dirname "$file")"; echo '{}' > "$file"; }
  python3 - "$file" "$GATEWAY_BIN" "$SOCKET_PATH" "$SERVER_KEY" "$name" "$label" \
    "$RC_LABELED" "$RC_FOREIGN" <<'PY'
import json, os, sys, tempfile
path, binpath, sock, key, name, label = sys.argv[1:7]
rc_labeled, rc_foreign = int(sys.argv[7]), int(sys.argv[8])
container = "mcp" if name == "opencode" else "mcpServers"
env_key = "environment" if name == "opencode" else "env"
# A config file may be a symlink into a dotfiles repo: os.replace on the link
# path would swap the link for a plain file. Write the target instead.
path = os.path.realpath(path)
with open(path) as fh:
    data = json.load(fh)
servers = data.setdefault(container, {})
entry = servers.get(key)
if entry is None:
    if name == "opencode":
        servers[key] = {
            "type": "local",
            "command": [binpath, "-mcp"],
            "environment": {"GATEWAY_SOCKET_PATH": sock, "GATEWAY_AGENT": label},
            "enabled": True,
        }
    else:
        servers[key] = {
            "command": binpath,
            "args": ["-mcp"],
            "env": {"GATEWAY_SOCKET_PATH": sock, "GATEWAY_AGENT": label},
        }
else:
    # Upgrade only an entry we recognise as ours, byte-for-byte on command/args.
    # Anything else under this key is the user's own decision.
    ours = (entry.get("command") == [binpath, "-mcp"] if name == "opencode"
            else entry.get("command") == binpath and entry.get("args") == ["-mcp"])
    env = entry.get(env_key)
    if not ours or not isinstance(env, dict):
        sys.exit(rc_foreign)
    if env.get("GATEWAY_AGENT"):
        sys.exit(rc_labeled)
    env["GATEWAY_AGENT"] = label
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path))
with os.fdopen(fd, "w") as fh:
    json.dump(data, fh, indent=2)
os.replace(tmp, path)
PY
}

inject_toml() {
  # Decision via tomllib (shape), write via line edit (keeps comments and the
  # rest of the user's file byte-for-byte). Never sed -i on a user file.
  local file="$1" name="$2" label
  label="$(agent_label "$name")"
  python3 - "$file" "$GATEWAY_BIN" "$SOCKET_PATH" "$SERVER_KEY" "$label" \
    "$RC_LABELED" "$RC_FOREIGN" <<'PY'
import os, sys, tempfile, tomllib
path, binpath, sock, key, label = sys.argv[1:6]
rc_labeled, rc_foreign = int(sys.argv[6]), int(sys.argv[7])
path = os.path.realpath(path)  # dotfiles symlink: write the target, not the link
header = "[mcp_servers.%s]" % key


def q(s):  # TOML basic string
    return '"%s"' % s.replace("\\", "\\\\").replace('"', '\\"')


env_line = "env = { GATEWAY_SOCKET_PATH = %s, GATEWAY_AGENT = %s }\n" % (q(sock), q(label))
text = ""
if os.path.exists(path):
    with open(path, "rb") as fh:
        data = tomllib.load(fh)  # corrupt TOML raises: exit != 0, nothing written
    with open(path) as fh:
        text = fh.read()
else:
    data = {}
servers = data.get("mcp_servers")
entry = servers.get(key) if isinstance(servers, dict) else None
lines = text.splitlines(keepends=True)
if entry is None:
    if text and not text.endswith("\n"):
        lines.append("\n")
    lines += ["\n", header + "\n", "command = %s\n" % q(binpath), 'args = ["-mcp"]\n', env_line]
else:
    if entry.get("command") != binpath or entry.get("args") != ["-mcp"]:
        sys.exit(rc_foreign)
    if (entry.get("env") or {}).get("GATEWAY_AGENT"):
        sys.exit(rc_labeled)
    start = next(i for i, ln in enumerate(lines) if ln.strip() == header)
    end = next((i for i in range(start + 1, len(lines)) if lines[i].lstrip().startswith("[")), len(lines))
    at = next((i for i in range(start + 1, end) if lines[i].lstrip().startswith("env")), -1)
    if at < 0:
        lines.insert(end, env_line)  # no env line yet: add one inside the section
    else:
        lines[at] = env_line
mode = os.stat(path).st_mode & 0o777 if os.path.exists(path) else 0o600
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path))
with os.fdopen(fd, "w") as fh:
    fh.write("".join(lines))
os.chmod(tmp, mode)  # keep the user's file mode (cat >> used to)
os.replace(tmp, path)
PY
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
  local file fmt bak rc=0
  file="$(agent_path "$name")"
  [ -n "$file" ] || { echo "FAIL $name unknown-agent"; return 1; }
  fmt="$(agent_format "$name")"

  # No early "already-integrated" exit: an entry written before 54.6 carries no
  # GATEWAY_AGENT and has to be upgradable. The writer decides what (if
  # anything) to change; the backup is dropped again when it changes nothing.
  mkdir -p "$(dirname "$file")"
  bak="$file.bak-$(date +%Y%m%d%H%M%S)"
  [ -f "$file" ] && cp "$file" "$bak" && chmod 600 "$bak" && echo "  backup: $bak"

  case "$fmt" in
    json) inject_json "$file" "$name" || rc=$? ;;
    toml) inject_toml "$file" "$name" || rc=$? ;;
  esac
  case "$rc" in
    0) ;;
    "$RC_LABELED") rm -f "$bak"; echo "SKIP $name already-integrated"; return 0 ;;
    "$RC_FOREIGN") rm -f "$bak"; echo "SKIP $name not-ours"; return 0 ;;
    *) echo "FAIL $name write-failed"; return 1 ;;
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
  # Emit one TSV row per visible agent; python3 json.dumps keeps HOME-safe JSON.
  # Columns: name, path, format, integrated, connectable
  # Presence: really_installed (Omarchy) OR already integrated — never cold stub alone.
  local name file integrated emitted_cursor=0
  {
    for name in "${DETECT_NAMES[@]}"; do
      if [ "$name" = "cursor-agent" ] && [ "$emitted_cursor" -eq 1 ]; then
        continue
      fi
      file="$(agent_path "$name")"
      integrated=false
      if [ -n "$file" ] && [ -f "$file" ] && is_integrated "$file"; then
        integrated=true
      fi
      if [ -n "$file" ]; then
        really_installed "$name" || [ "$integrated" = true ] || continue
        printf '%s\t%s\t%s\t%s\ttrue\n' \
          "$name" "$file" "$(agent_format "$name")" "$integrated"
        if [ "$name" = "cursor" ]; then emitted_cursor=1; fi
      else
        really_installed "$name" || continue
        printf '%s\t\t\tfalse\tfalse\n' "$name"
      fi
    done
  } | python3 -c '
import json, sys
agents = []
for raw in sys.stdin:
    line = raw.rstrip("\n")
    name, path, fmt, integrated, connectable = line.split("\t")
    agents.append({
        "name": name,
        "path": path,
        "format": fmt,
        "integrated": integrated == "true",
        "connectable": connectable == "true",
    })
print(json.dumps({"agents": agents}, separators=(",", ":")))
'
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
  local any=0 name file integrated
  for name in "${DETECT_NAMES[@]}"; do
    file="$(agent_path "$name")"
    integrated=false
    if [ -n "$file" ] && [ -f "$file" ] && is_integrated "$file"; then
      integrated=true
    fi
    if [ -n "$file" ]; then
      really_installed "$name" || [ "$integrated" = true ] || continue
      if [ -f "$file" ]; then
        echo "Found: $name ($file)"
      else
        echo "Found: $name (installed, no config yet)"
      fi
      any=1
    elif really_installed "$name"; then
      echo "Found: $name — installed, not auto-connected"
    fi
  done
  [ $any -eq 0 ] && { echo "No connectable agents found."; exit 0; }
  echo
  for name in opencode claude-code cursor codex gemini; do
    file="$(agent_path "$name")"
    integrated=false
    if [ -f "$file" ] && is_integrated "$file"; then integrated=true; fi
    really_installed "$name" || [ "$integrated" = true ] || continue
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
