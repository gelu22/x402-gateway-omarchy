# Security policy

x402 Gateway signs x402 payments for AI agents from a local daemon on your
machine. It handles a spending budget, a session with Coinbase CDP, and a
signing credential (TWS) that lives only in RAM. Security reports are welcome
and taken seriously.

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x (current pre-release) | ✅ |
| older | ❌ |

0.1.x is a **testnet-first pre-release** (default network: Base Sepolia).

## Reporting a vulnerability

Use GitHub's **private vulnerability reporting**: open this repository →
**Security** → **Report a vulnerability**. That channel keeps the report
private, gives us a place to discuss and to credit you, and lets us request a
CVE when it is warranted.

Please include: what you did, what you expected, what happened, the version
(`gateway --version` and `plugin/omarchy/manifest.json`), and any logs
(`~/.local/state/x402-gateway/audit.log` is the money audit; **never** include
tokens, TOTP codes or the wallet secret).

We aim to acknowledge within 3 business days and to agree on a disclosure
timeline with you. Please do not open a public issue for a security bug before
we have a fix or a mitigation.

## What is in scope

- the Go daemon (`cmd/gateway`, `internal/*`): socket authorization, policy
  checks, signing flow, session/TWS handling, error envelopes
- the Omarchy QML plugin (`plugin/omarchy/*`) and the MCP bridge
- `scripts/install.sh` and the release artifacts (checksums, sigstore
  attestations)
- the wire contract itself: the unix socket is 0700 with peer-credential checks,
  and every request/response envelope is documented in the API contract

## What is out of scope

- **Coinbase CDP and its TEE** — wallet creation, signing, MFA challenges and
  the Policy Engine are Coinbase's systems, not ours
- **Omarchy / Quickshell** and the notification service
- **x402 sellers** — a seller asking a price, or delivering nothing after being
  paid, is a seller-side risk (see the budget and approval model)
- compromise of your **Coinbase account or email** — an attacker who logs into
  Coinbase directly is outside anything a local plugin can see

## Known limits we do not hide

- **The daily budget is local.** `policy.json`, `spend.json` and `sellers.json`
  live in your state directory and are hot-reloaded; a process running as your
  user can change them. The daily budget guards against a mistaken or overeager
  agent, not against malware.
- **The hard per-payment ceiling is not local.** When the operator has
  configured it, Coinbase's Policy Engine refuses any single signature above the
  ceiling. It caps **one payment**; Coinbase's policy engine has no daily sum.
- **Device compromise is credential compromise.** If malware reads the TWS and
  the refresh token, it can sign directly at CDP and bypass this daemon. The
  primary controls are the ptrace/core-dump block, the short TWS lifetime and
  Coinbase-side MFA.
- **MFA has no recovery codes.** Losing the authenticator means resetting MFA in
  the CDP portal.
- **The clock matters.** EIP-3009 authorizations are valid in a ±5 minute
  window; a machine with a wrong clock will see signatures rejected. Enable NTP.
- **PATH trust boundary.** The installer resolves `gh` and `curl` from your
  PATH. An attacker who controls your PATH already executes code as you (out of
  scope). Mitigations: `type -P` rejects functions/aliases; attestation is
  verified against a pinned signer workflow + tag (not just an exit code);
  sha256 cross-check is always required. An out-of-band anchor (signed checksums
  with a pinned public key) would close this and is not implemented yet.

## Disclaimer

This is an independent project. It is **not affiliated with, endorsed by, or
supported by Coinbase**. "Coinbase", "CDP" and "Base" are trademarks of their
respective owners.
