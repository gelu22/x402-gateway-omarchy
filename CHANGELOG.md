# Changelog

All notable changes to this project are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows the plugin manifest (`plugin/omarchy/manifest.json`).

## [0.1.14] — 2026-09-30

### Changed

- **Panel compact hierarchy**: Status is one row (large balance + status chip +
  power toggle); Daily Budget is a quieter single section; Account is collapsed
  by default (network · short address · MFA in the header; Open config / MFA /
  Logout in the body); AI Agents and Remembered overrides sit under one
  Advanced disclosure (no nested agent chevron). The `plugin v… · daemon v…`
  stamp is hidden unless `GATEWAY_PANEL_DEBUG=1` (fail-closed).

### Security

- **Security-class gate harden**: money/lifecycle path coverage for
  `internal/budget` and `internal/install`; stricter mutable `/tmp` detection;
  `security_class_test.sh` wired into preflight.

## [0.1.13] — 2026-09-30

### Security

- **Atomic budget authority** (`internal/budget`): `Authorize` / `Commit` /
  `Release` in one mutex+persist transaction. Daily cap and per-domain sub-cap
  are reserved atomically — concurrent requests can no longer each pass against
  the same remaining budget. Charge happens **before** signing; a write failure
  means no authorization (fail-closed). Crash leaves the amount reserved until
  TTL (budget gets tighter, never looser). `OnPayment` no longer calls
  `Spend.Add` (telemetry/cache only).
- **Lifecycle mutations in Go** (`internal/install`): installs/removes use
  `Openat(O_NOFOLLOW)` + `Renameat` / `Unlinkat`. Bash is a thin verified
  downloader that delegates to `gateway install` / `gateway self-remove`.
- **Trust-anchor layers**: `resolve_tool` + `type -P` reject PATH functions/
  aliases for `curl`/`gh`; attestation + sha256 both required (unless a
  documented opt-out). `SECURITY.md` states the PATH trust boundary; ADR-001 D11
  records the future out-of-band anchor.
- **Security-class gate**: `scripts/check-security-classes.sh` in preflight plus
  required `knowledge/reviews/<promptId>.md` for money/lifecycle/session changes.

### Added

- README section **Supported payment rail** and ADR-001 **D10** (EIP-3009 on
  Base; non-goal non-EVM).

## [0.1.12] — 2026-09-29

### Security

- Every documented install/update/uninstall command is now a **self-contained
  fail-closed chain** (download → attestation verify → run) into a fresh
  `mktemp -d`. Nothing is reused from an earlier step and no predictable `/tmp`
  path is used, so a stale or replaced script cannot perform the destructive
  purge without verified provenance.
- The deinstaller's no-terminal hint follows the same pattern.

## [0.1.11] — 2026-09-29

### Security

- The README no longer offers an unverified checkout execution path: cloning a
  tag is documented for **inspection only**, and the single documented way to run
  the installer is the pinned download plus attestation check. A moved tag can no
  longer silently run a different installer than the reviewed release.
- Added `scripts/preflight.sh` (the pre-push gate) and wired the container test
  into CI.

## [0.1.10] — 2026-09-28

### Security

- The README's documented install/update/uninstall version is now substituted
  automatically at publish time, so the instructions always point at the release
  that includes the latest fixes (no stale tags).
- The deinstaller's no-terminal hint is a single fail-closed `&&` chain (download,
  verify with `--source-ref`, run) — a failed download cannot leave a stale script
  to be executed.

## [0.1.9] — 2026-09-28

### Security

- **Fixed a critical pipefail bug in the bundle safety check**: under
  `set -euo pipefail`, `tar | grep -q` would SIGPIPE tar when grep exited early,
  and the unsafe-path/symlink rejection would silently pass. The check now
  captures the listing first (no pipe), so a malicious bundle is always rejected.
- The installer now fails loudly when `omarchy plugin validate` rejects the
  plugin (instead of silently continuing).
- The deinstaller's no-terminal hint points at a pinned release with an
  attestation check (no more `releases/latest`).
- Interactive deinstaller prompts use standard English `[Y/n]`/`[y/N]` defaults;
  removing data (including the audit log) now defaults to **no**.
- README install/update/uninstall are single `&&` chains: download, verify, run.

## [0.1.8] — 2026-09-28

### Security

- Installer and deinstaller take a state lock, so two concurrent runs cannot
  interleave and corrupt the install.
- The plugin bundle is rejected if it carries absolute, `..`, or symlink members,
  and it is extracted with `--no-same-owner --no-same-permissions`.
- The daemon is stopped by matching `/proc/<pid>/exe`, not a `pkill -f` pattern
  over the command line; agent-config backups are `0600` and removed on success.
- README bootstraps from a **pinned release** and verifies `install.sh` /
  `uninstall.sh` (sigstore attestation, pinned signer workflow + tag) **before**
  running them.

## [0.1.7] — 2026-09-28

### Security

- No downloaded or sibling script is executed any more: `install.sh purge` wipes
  inline instead of fetching `uninstall.sh` from a mutable branch (or running a
  foreign `/tmp/uninstall.sh`). `install.sh`/`uninstall.sh` now ship as signed
  release assets, and the README uses those release URLs instead of `master`.
- The installer pins the signer workflow and the tag when verifying the sigstore
  attestation, and `GATEWAY_RELEASE_BASE` is accepted only as a `file://` offline
  source (a remote override needs the explicit `GATEWAY_ALLOW_UNVERIFIED=1`).
- Target directories are refused when they are symlinks; ownership checks run
  before any side effect; the ownership registry handles a `$HOME` with spaces;
  the deinstaller’s destructive default is now “no”.
- Release workflow uses `persist-credentials: false`; the publish script takes the
  public workflows from HEAD and runs a fail-closed secret scan before pushing.
- Agent setup rolls back and removes its backup when a write fails validation.

## [0.1.6] — 2026-09-28

### Security

- The installer no longer overwrites a foreign `setup-agents.sh` (it checks the
  registry first, like the binary), removes the retired `remember-override.sh`
  only when it is ours, and the deinstaller stops the daemon only after
  confirming the binary is ours and removes only the files it owns from the
  shared directory — never the whole directory.

## [0.1.5] — 2026-09-28

### Security

- The installer and deinstaller no longer overwrite or delete paths they did not
  create. A sha256 registry under the private state directory records what was
  installed, and install/update/remove refuse a foreign `gateway` binary, a
  plugin directory owned by another plugin, or a foreign setup helper (set
  `GATEWAY_FORCE=1` to override deliberately).

## [0.1.4] — 2026-09-28

### Changed

- Repository: new issues are labeled `triage` and acknowledged automatically, and
  security-sensitive reports are pointed at private vulnerability reporting.

## [0.1.3] — 2026-09-28

### Changed

- The public repository now ships the daemon source (`cmd/`, `internal/`,
  `go.mod`, `go.sum`) and builds releases from it there, so the shipped binary is
  reviewable against its exact source.
- `install.sh` now requires a valid sigstore build attestation (fail-closed): a
  missing `gh` or an unverifiable release aborts the install. Set
  `GATEWAY_ALLOW_UNVERIFIED=1` to install sha256-only (offline/dev, not
  recommended).

## [0.1.2] — 2026-09-28

### Changed

- README: the Coinbase per-payment ceiling is documented as `X` — what it is,
  why it is the only limit that cannot be lifted from your machine, and how to
  choose it (keep `X` close to the daily budget, ~$5–10, rather than the loose
  $50 this deployment ships).

## [0.1.1] — 2026-09-28

### Added

- Interactive deinstaller `scripts/uninstall.sh` (and `install.sh purge`): asks
  whether to remove everything without further questions or to pick groups
  (agent MCP entries, program, data); `--yes` for a scripted full wipe; refuses
  and changes nothing without a terminal and without `--yes`.
- `preview.png` at the repository root for the Omarchy plugin catalogue.
- Install/update/uninstall no longer pipe a remote script into a shell
  (`curl … | bash`): the README and `install.sh purge` download the script and
  run it, or install from a clone.

### Changed

- README: new “Funding your wallet” guide (Circle test faucet for Base Sepolia,
  on-ramp options for Base mainnet) and a corrected network note — Base mainnet
  is supported; a fresh install still defaults to Base Sepolia.
- The panel footer now shows the installed plugin version and commit
  (`plugin vX.Y.Z (sha)`) next to the daemon version.

### Distribution

- Releases are built, signed (sigstore attestation) and published in the public
  plugin repository `gelu22/x402-gateway-omarchy`; installs and updates read from
  there.

## [0.1.0] — 2026-09-22

First public pre-release. **Testnet-first**: a fresh install defaults to Base
Sepolia (`eip155:84532`); switch to Base mainnet in the plugin config.

### Added

- Automatic x402 payments for local AI agents from an Omarchy bar widget: fetch
  a paywalled URL, the daemon pays within your daily budget and returns the
  content.
- Two spend limits: a **daily budget** you set in the panel, and a hard
  **per-payment ceiling** enforced by Coinbase's Policy Engine (set by the
  operator, not changeable from your machine).
- Onboarding without a browser: email OTP through Coinbase CDP, wallet address
  shown in the panel for funding.
- MFA TOTP step-up: raising spending authority (cap change, approving a seller,
  over-budget override) requires a fresh code attested by Coinbase.
- A pending payment waits for the MFA code (up to 180 s) and completes in the
  same response; a retry within 5 minutes is served from memory without a second
  charge.
- Money audit log (`~/.local/state/x402-gateway/audit.log`) and a JSON error
  envelope on the socket.
- MCP bridge so agents can call `fetch_with_payment`.

### Security

- Fail-closed everywhere: an auth, signing or settlement failure is an explicit
  error, never a silent skip.
- The signing credential (TWS) lives only in RAM: process hardened with
  `PR_SET_DUMPABLE=0` and `RLIMIT_CORE=0`, the scalar buffer `mlock`-ed and
  wiped byte-by-byte on logout and shutdown.
- Atomic per-request key reservation prevents paying twice when identical
  requests race.
- Private keys never leave the device; the session store is `0600`, the state
  directory `0700`.
- Release artifacts ship with sha256 checksums and sigstore build attestations.
