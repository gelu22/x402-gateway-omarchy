# Changelog

All notable changes to this project are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows the plugin manifest (`plugin/omarchy/manifest.json`).

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
