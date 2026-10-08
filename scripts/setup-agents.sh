#!/usr/bin/env bash
# setup-agents.sh — opt-in MCP integration for detected AI agents.
#
# Agents are declared in a registry (TOML), not in this script. The built-in
# defaults live in DEFAULT_REGISTRY below; a user registry overrides them by
# name and appends new agents:
#   $GATEWAY_AGENTS_REGISTRY  (default: ~/.config/x402-gateway/agents.toml)
# Run `setup-agents.sh --write-template` for a commented template + instructions.
#
# Modes:
#   (interactive)            T/N per detected connectable agent (terminal use)
#   --detect                 JSON {agents:[{name,path,format,integrated,connectable}]} on stdout
#   --apply opencode,cursor  non-interactive full pipeline per listed agent
#   --remove opencode,cursor remove the injected MCP block per listed agent
#   --write-template         print a commented registry template on stdout
# Safety rules (all modes): backup before write, validate after write,
# automatic rollback on validation failure. Zero silent writes.
# Detection never executes agent launchers. Presence = Omarchy present:
# non-stub bin under ~/.local/bin, a non-stub launcher in $GATEWAY_AGENTS_BIN_DIRS
# (defaults to $PATH), or `mise where <package>` (never `mise use`).
set -euo pipefail

GATEWAY_BIN="${GATEWAY_BIN:-${HOME}/.local/bin/gateway}"
SOCKET_PATH="${GATEWAY_SOCKET_PATH:-${XDG_STATE_DIR:-$HOME/.local/state}/x402-gateway/gw.sock}"
SERVER_KEY="${SERVER_KEY:-x402-gateway}"
USER_REGISTRY="${GATEWAY_AGENTS_REGISTRY:-$HOME/.config/x402-gateway/agents.toml}"
# Directories scanned for a non-stub launcher (the system-install signal).
# Defaults to $PATH; tests pin it to a hermetic dir.
BIN_DIRS="${GATEWAY_AGENTS_BIN_DIRS:-$PATH}"

# --- default registry (single source) ---------------------------------------
# Columns per [[agent]]: name, launcher, package, installer, config, format,
# container, env_key, style, path_probe. Missing keys default: format=json,
# container=mcpServers, env_key=env, style=claude, package=launcher,
# path_probe=(config non-empty). config empty → detect-only (no write).
# Order is the detect order.
read -r -d '' DEFAULT_REGISTRY <<'TOML' || true
# --- connectable writers (have a config path) ---

[[agent]]
name = "opencode"
launcher = "opencode"
config = "~/.config/opencode/opencode.json"
container = "mcp"
env_key = "environment"
style = "opencode"

[[agent]]
name = "claude-code"
launcher = "claude"
config = "~/.claude.json"

[[agent]]
name = "cursor"
launcher = "cursor-agent"
config = "~/.cursor/mcp.json"

[[agent]]
name = "codex"
launcher = "codex"
config = "~/.codex/config.toml"
format = "toml"
container = "mcp_servers"

[[agent]]
name = "gemini"
launcher = "gemini"
config = "~/.gemini/settings.json"

[[agent]]
name = "cline"
launcher = "cline"
config = "~/.cline/data/settings/cline_mcp_settings.json"

# --- detect-only launchers (no config path) ---

[[agent]]
name = "pi"
launcher = "pi"

[[agent]]
name = "omp"
launcher = "omp"
package = "github:can1357/oh-my-pi"

[[agent]]
name = "grok"
launcher = "grok"
package = "npm:@xai-official/grok"

[[agent]]
name = "copilot"
launcher = "copilot"

[[agent]]
name = "crush"
launcher = "crush"

[[agent]]
name = "openclaw"
launcher = "openclaw"
package = ""
installer = "omarchy-install-openclaw-cli"

[[agent]]
name = "hermes"
launcher = "hermes"
package = ""
installer = "omarchy-install-hermes-cli"

[[agent]]
name = "muse"
launcher = "muse"
package = "http:muse"

[[agent]]
name = "cursor-agent"
launcher = "cursor-agent"

[[agent]]
name = "ori"
launcher = "ori"

[[agent]]
name = "agy"
launcher = "agy"
TOML

# Commented template printed by --write-template (kept next to the defaults so
# both stay in one file).
read -r -d '' REGISTRY_TEMPLATE <<'TOML' || true
# agents.toml — registry of AI agents x402 Gateway can detect and integrate.
#
# Location read at runtime:
#   $GATEWAY_AGENTS_REGISTRY  (default: ~/.config/x402-gateway/agents.toml)
# Entries here override the built-in defaults by `name` and append new agents.
# Remove this file to fall back to the built-in registry.
#
# FIELDS (only `name` and `launcher` are required):
#   name       etykieta, regex: ^[a-z0-9][a-z0-9._-]{0,31}$   (np. "cline")
#   launcher   nazwa binarki w PATH / ~/.local/bin / mise     (np. "cline")
#   config     plik MCP agenta; PUSTY = tylko wykrywanie, brak przycisku Integrate
#   format     "json" (domyślnie) albo "toml"
#   container  klucz/table z serwerami MCP: "mcpServers" (domyślnie), "mcp", "mcp_servers"
#   env_key    klucz env we wpisie serwera: "env" (domyślnie) albo "environment"
#   style      kształt wpisu: "claude" (domyślnie) albo "opencode"
#   package    pakiet mise do sprawdzenia (`mise where`); domyślnie = launcher
#   installer  instalator Omarchy z `--check`; gdy ustawiony, presence = installer only
#   path_probe skanuj $PATH w poszukiwaniu nie-stub launcher (instalacja poza
#              ~/.local/bin i mise, np. /usr/bin/cursor-agent); domyślnie true
#              gdy ustawiono config, inaczej false
#
# JAK DODAĆ AGENTA:
#   1. Skopiuj blok [[agent]] poniżej i wypełnij pola (usuń niepotrzebne).
#   2. Sprawdź: bash scripts/setup-agents.sh --detect
#   3. Zintegruj: bash scripts/setup-agents.sh --apply <name>
# Presence: nie-stub w ~/.local/bin/<launcher>, nie-stub w $PATH/<launcher>,
# albo `mise where`. Cold stub (plik z "mise use -g") NIE liczy się jako instalacja.

# PRZYKŁAD 1 — agent z konfiguracją JSON w kontenerze "mcpServers" (większość CLI):
#
# [[agent]]
# name = "my-agent"
# launcher = "my-agent"
# config = "~/.my-agent/mcp.json"
#
# PRZYKŁAD 2 — agent tylko do wykrywania (brak znanej ścieżki konfiguracji):
#
# [[agent]]
# name = "some-agent"
# launcher = "some-agent"
#
# PRZYKŁAD 3 — agent z konfiguracją TOML (klucz jak w Codex):
#
# [[agent]]
# name = "toml-agent"
# launcher = "toml-agent"
# config = "~/.toml-agent/config.toml"
# format = "toml"
# container = "mcp_servers"
TOML

command -v python3 >/dev/null || { echo "ERROR: python3 required"; exit 1; }

# --- registry loading -------------------------------------------------------
# Emit one row per agent, fields joined by US (0x1f). A tab separator would be
# wrong: tab is IFS-whitespace, so `read` collapses the empty installer field
# and shifts every column. US is non-whitespace, so empty fields survive.
# Column order: name US launcher US package US installer US config US format US
# container US env_key US style. Defaults first; user registry overrides by name
# (default order kept), new names appended. A broken user registry is a loud
# error (fail-closed), never a silent empty list.
load_registry() {
  local tmp rc=0
  tmp="$(mktemp)"
  printf '%s' "$DEFAULT_REGISTRY" >"$tmp"
  python3 - "$tmp" "$USER_REGISTRY" <<'PY' || rc=$?
import os, re, sys, tomllib

default_path, user_path = sys.argv[1], sys.argv[2]
home_real = os.path.realpath(os.path.expanduser("~"))


def load(path):
    if not path or not os.path.exists(path):
        return []
    with open(path, "rb") as fh:
        data = tomllib.load(fh)
    agents = data.get("agent", [])
    if not isinstance(agents, list):
        raise SystemExit(f"{path}: 'agent' must be a list of [[agent]] blocks")
    return agents


order, by_name = [], {}
for source in (load(default_path), load(user_path)):
    for a in source:
        name = a.get("name")
        if not name:
            raise SystemExit("registry entry without a name")
        if name not in by_name:
            order.append(name)
        by_name[name] = a

for name in order:
    a = by_name[name]
    launcher = a.get("launcher") or ""
    pkg = a["package"] if "package" in a else launcher
    config = os.path.expanduser(a.get("config", ""))
    installer = a.get("installer", "")
    # A registry is data that drives two side effects, so both are constrained:
    #  (1) `installer` is executed (`<installer> --check`) — allow only the
    #      documented Omarchy installer namespace, never an arbitrary PATH binary.
    #  (2) `config` is a write target (`--apply`) — allow only paths inside HOME,
    #      so a hostile registry cannot make us create/overwrite files elsewhere.
    if installer and not re.fullmatch(r"omarchy-install-[a-z0-9-]+", installer):
        raise SystemExit(f"agent '{name}': installer must be an omarchy-install-* command, got {installer!r}")
    if config:
        real = os.path.realpath(config)
        if real != home_real and not real.startswith(home_real + os.sep):
            raise SystemExit(f"agent '{name}': config must be inside {home_real}, got {real!r}")
    # PATH probe finds installs outside ~/.local/bin and mise. Default ON only
    # for agents with a config (their launcher names are specific, e.g.
    # "cursor-agent"); generic detect-only names ("pi", "crush") stay off to
    # avoid matching an unrelated binary of the same name.
    path_probe = a.get("path_probe", bool(config))
    row = [
        name,
        launcher,
        pkg,
        installer,
        config,
        a.get("format", "json"),
        a.get("container", "mcpServers"),
        a.get("env_key", "env"),
        a.get("style", "claude"),
        "true" if path_probe else "false",
    ]
    for field in row:
        if "\x1f" in str(field) or "\n" in str(field):
            raise SystemExit(f"registry field with US/newline in agent '{name}'")
    print("\x1f".join(str(f) for f in row))
PY
  rm -f "$tmp"
  return $rc
}

registry_rows="$(load_registry)" || { echo "ERROR: invalid agents registry: $USER_REGISTRY" >&2; exit 1; }

declare -A A_LAUNCHER A_PACKAGE A_INSTALLER A_CONFIG A_FORMAT A_CONTAINER A_ENVKEY A_STYLE A_PATHPROBE
AGENTS_ORDER=()
while IFS=$'\x1f' read -r n launcher pkg installer config fmt container envkey style pathprobe; do
  [ -n "$n" ] || continue
  AGENTS_ORDER+=("$n")
  A_LAUNCHER["$n"]="$launcher"
  A_PACKAGE["$n"]="$pkg"
  A_INSTALLER["$n"]="$installer"
  A_CONFIG["$n"]="$config"
  A_FORMAT["$n"]="$fmt"
  A_CONTAINER["$n"]="$container"
  A_ENVKEY["$n"]="$envkey"
  A_STYLE["$n"]="$style"
  A_PATHPROBE["$n"]="$pathprobe"
done <<<"$registry_rows"

# name → config path (empty = detect-only / unknown).
agent_path() { printf '%s' "${A_CONFIG[$1]:-}"; }
agent_format() { printf '%s' "${A_FORMAT[$1]:-json}"; }
# Binary name under ~/.local/bin (cursor writer uses the Cursor CLI launcher).
agent_launcher() { printf '%s' "${A_LAUNCHER[$1]:-}"; }
# mise package id; empty → no mise where.
agent_package() { printf '%s' "${A_PACKAGE[$1]:-}"; }
agent_installer() { printf '%s' "${A_INSTALLER[$1]:-}"; }
agent_container() { printf '%s' "${A_CONTAINER[$1]:-mcpServers}"; }
agent_envkey() { printf '%s' "${A_ENVKEY[$1]:-env}"; }
agent_style() { printf '%s' "${A_STYLE[$1]:-claude}"; }
is_agent() { [ -n "${A_LAUNCHER[$1]+x}" ]; }

# Omarchy user_install: symlink-to-file or non-stub file (cold stub has `mise use -g`).
# Symlink-to-directory is not a launcher (stricter than Omarchy; avoids false present).
stub_or_missing() {
  # 0 = not a usable launcher (missing, dir, or cold stub); 1 = usable.
  local p="$1"
  [ -e "$p" ] || return 0
  if [ -L "$p" ]; then
    # A symlink-to-file is a real launcher: an Omarchy cold stub is a regular
    # file, and a mise shim is a symlink to the mise binary. Reading the target
    # for the "mise use -g" marker would follow it into the binary — which
    # contains that literal string as data — and misread the shim as a stub.
    [ -f "$p" ] && return 1 || return 0
  fi
  [ -f "$p" ] || return 0
  grep -q '^mise use -g' "$p" 2>/dev/null && return 0
  return 1
}

user_bin_installed() {
  local bin="$1"
  [ -n "$bin" ] || return 1
  ! stub_or_missing "$HOME/.local/bin/$bin"
}

# A non-stub launcher in any $BIN_DIRS entry (defaults to $PATH): the signal for
# installs outside ~/.local/bin (e.g. a cursor-agent at /usr/bin). Never executes.
bin_dirs_installed() {
  local bin="$1" d p
  [ -n "$bin" ] || return 1
  local IFS=:
  for d in $BIN_DIRS; do
    [ -n "$d" ] || continue
    p="$d/$bin"
    if ! stub_or_missing "$p"; then return 0; fi
  done
  return 1
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
  [ "${A_PATHPROBE[$name]:-false}" = true ] && bin_dirs_installed "$launcher" && return 0
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
# regex (internal/agentlabel). All writer names match by construction;
# anything else is a bug here, not user input, so refuse to write it.
agent_label() {
  local name="$1"
  [[ "$name" =~ ^[a-z0-9][a-z0-9._-]{0,31}$ ]] \
    || { echo "ERROR: invalid agent label: $name" >&2; return 1; }
  printf '%s' "$name"
}

inject_json() {
  # Shape is registry-driven: container, env_key and style (opencode vs claude).
  local file="$1" name="$2" label container env_key style
  label="$(agent_label "$name")"
  container="$(agent_container "$name")"
  env_key="$(agent_envkey "$name")"
  style="$(agent_style "$name")"
  [ -f "$file" ] || { mkdir -p "$(dirname "$file")"; echo '{}' >"$file"; }
  python3 - "$file" "$GATEWAY_BIN" "$SOCKET_PATH" "$SERVER_KEY" "$container" "$env_key" "$style" "$label" \
    "$RC_LABELED" "$RC_FOREIGN" <<'PY'
import json, os, sys, tempfile
path, binpath, sock, key, container, env_key, style, label = sys.argv[1:9]
rc_labeled, rc_foreign = int(sys.argv[9]), int(sys.argv[10])
# A config file may be a symlink into a dotfiles repo: os.replace on the link
# path would swap the link for a plain file. Write the target instead.
path = os.path.realpath(path)
with open(path) as fh:
    data = json.load(fh)
servers = data.setdefault(container, {})
entry = servers.get(key)
if entry is None:
    if style == "opencode":
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
    ours = (entry.get("command") == [binpath, "-mcp"] if style == "opencode"
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
  local file="$1" name="$2" label container
  label="$(agent_label "$name")"
  container="$(agent_container "$name")"
  python3 - "$file" "$GATEWAY_BIN" "$SOCKET_PATH" "$SERVER_KEY" "$container" "$label" \
    "$RC_LABELED" "$RC_FOREIGN" <<'PY'
import os, sys, tempfile, tomllib
path, binpath, sock, key, container, label = sys.argv[1:7]
rc_labeled, rc_foreign = int(sys.argv[7]), int(sys.argv[8])
path = os.path.realpath(path)  # dotfiles symlink: write the target, not the link
header = "[%s.%s]" % (container, key)


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
servers = data.get(container)
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
  # Deletes the key from the agent's native container; for the opencode style
  # also strips a legacy "mcpServers" entry (wrong container written by older
  # script). Shape-agnostic: only the key is removed, siblings untouched.
  local file="$1" name="$2" container style
  container="$(agent_container "$name")"
  style="$(agent_style "$name")"
  python3 - "$file" "$SERVER_KEY" "$container" "$style" <<'PY'
import json, os, sys, tempfile
path, key, container, style = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
containers = [container]
if style == "opencode" and container != "mcpServers":
    containers.append("mcpServers")
with open(path) as fh:
    data = json.load(fh)
found = False
for c in containers:
    servers = data.get(c, {})
    if key in servers:
        del servers[key]
        found = True
    if c in data and not data[c]:
        data.pop(c, None)   # drop the now-empty container
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
  local file="$1" name="$2" container
  container="$(agent_container "$name")"
  python3 - "$file" "$SERVER_KEY" "$container" <<'PY'
import os, sys, tempfile
path, key, container = sys.argv[1], sys.argv[2], sys.argv[3]
header = "[%s.%s]" % (container, key)
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
  is_agent "$name" || { echo "FAIL $name unknown-agent"; return 1; }
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
  is_agent "$name" || { echo "FAIL $name unknown-agent"; return 1; }
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
    toml) remove_toml "$file" "$name" || { restore "$bak" "$file"; echo "FAIL $name strip-failed"; return 1; } ;;
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
    for name in "${AGENTS_ORDER[@]}"; do
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

cmd_write_template() {
  printf '%s\n' "$REGISTRY_TEMPLATE"
}

cmd_interactive() {
  echo "=== x402 Gateway — AI agent integration ==="
  echo "MCP server to be added:"
  echo "  command: $GATEWAY_BIN -mcp"
  echo "  socket:  $SOCKET_PATH"
  echo
  local any=0 name file integrated
  for name in "${AGENTS_ORDER[@]}"; do
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
  for name in "${AGENTS_ORDER[@]}"; do
    file="$(agent_path "$name")"
    [ -n "$file" ] || continue
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
  --write-template) cmd_write_template ;;
  *)        cmd_interactive ;;
esac