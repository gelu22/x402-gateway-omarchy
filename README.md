# x402 Gateway — automatic x402 payments for AI agents

A desktop daemon that pays for x402 content (USDC on Base) on behalf of your
AI agents — within the daily budget you set. An Omarchy plugin adds a status
widget and a browser-free onboarding wizard.

```
AI agent ──► MCP / unix socket ──► Go daemon ──► x402 seller
                                      │  402 → sign → retry → content
                                      └─ budget: daily cap
```

> **Networks: Base Sepolia (testnet) and Base mainnet.** A fresh install defaults
> to Base Sepolia so you can try it with free test USDC. Base mainnet is
> supported and follows the same code path — switch to it in the plugin config
> and fund the wallet with real USDC (see [Funding your wallet](#funding-your-wallet)).
> Real funds are real money: you are responsible for what your agents spend.

## Install

The gateway is a bar widget plus a local daemon. Download the installer from a
**pinned release**, verify its signature, then run it — never pipe a remote
script straight into a shell:

```bash
VERSION=v0.1.19   # any released tag (see the Releases page)
TMP="$(mktemp -d)"
curl -fsSL -o "$TMP/install.sh" \
  "https://github.com/gelu22/x402-gateway-omarchy/releases/download/$VERSION/install.sh" \
&& gh attestation verify "$TMP/install.sh" --repo gelu22/x402-gateway-omarchy \
  --signer-workflow gelu22/x402-gateway-omarchy/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION" \
&& bash "$TMP/install.sh" "$VERSION"
```

Want to read the code first? Clone the **same pinned tag** (for inspection only)
and read it — then run the verified download above. Do **not** run the checkout's
copy: it is not attestation-checked, and a moved tag would silently run a
different installer.

```bash
git clone --branch v0.1.19 --depth 1 https://github.com/gelu22/x402-gateway-omarchy.git
less x402-gateway-omarchy/scripts/install.sh
```

The installer then checks **the daemon binary and the plugin bundle** (sha256
**and** a valid sigstore build attestation — the GitHub CLI `gh` must be
installed; without a valid signature it refuses to install). To skip the
signature check — offline, or at your own risk — prefix the run with
`GATEWAY_ALLOW_UNVERIFIED=1`. It installs:
- binary → `~/.local/bin/gateway`
- QML plugin → `~/.config/omarchy/plugins/gelu22.gateway` (Omarchy only;
  skipped with instructions elsewhere)
- helper script (agent setup) → `~/.local/share/x402-gateway/`
- plugin config → `~/.config/omarchy/x402-gateway/config.json` (seeded only
  when absent; your edits are kept)
- state dir `~/.local/state/x402-gateway` (0700)

Install always needs an explicit tag (`install.sh vX.Y.Z`) — there is no
floating `latest` / bare `install` alias. Other modes: `install.sh verify`
(check installation), `install.sh remove` (removes binary, plugin and scripts;
keeps state), `install.sh purge --yes` (full wipe).

### Update

There is no auto-update: the installer **copies** the binary and the plugin
once. To update, run the same pinned, verified download for the new tag:

```bash
VERSION=v0.1.19
TMP="$(mktemp -d)"
curl -fsSL -o "$TMP/install.sh" \
  "https://github.com/gelu22/x402-gateway-omarchy/releases/download/$VERSION/install.sh" \
&& gh attestation verify "$TMP/install.sh" --repo gelu22/x402-gateway-omarchy \
  --signer-workflow gelu22/x402-gateway-omarchy/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION" \
&& bash "$TMP/install.sh" "$VERSION"
```

Check what changed in [CHANGELOG.md](CHANGELOG.md).

### Uninstall

Interactive deinstaller — one self-contained chain (download → verify → run):

```bash
VERSION=v0.1.19
TMP="$(mktemp -d)"
curl -fsSL -o "$TMP/uninstall.sh" \
  "https://github.com/gelu22/x402-gateway-omarchy/releases/download/$VERSION/uninstall.sh" \
&& gh attestation verify "$TMP/uninstall.sh" --repo gelu22/x402-gateway-omarchy \
  --signer-workflow gelu22/x402-gateway-omarchy/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION" \
&& bash "$TMP/uninstall.sh"
```

Everything at once, no questions (stops the daemon, removes the binary, the
plugin, the helper, the agent MCP entries, your state **including the audit
log**, and the plugin config) — the same self-contained chain, with `--yes`:

```bash
VERSION=v0.1.19
TMP="$(mktemp -d)"
curl -fsSL -o "$TMP/uninstall.sh" \
  "https://github.com/gelu22/x402-gateway-omarchy/releases/download/$VERSION/uninstall.sh" \
&& gh attestation verify "$TMP/uninstall.sh" --repo gelu22/x402-gateway-omarchy \
  --signer-workflow gelu22/x402-gateway-omarchy/.github/workflows/release.yml \
  --source-ref "refs/tags/$VERSION" \
&& bash "$TMP/uninstall.sh" --yes
```

Every command above downloads and verifies **its own** copy into a fresh
`mktemp -d`; no step reuses a script from an earlier one.

`install.sh remove` is the non-destructive variant: it removes the binary, the
QML plugin and the helper script and **keeps your state and config** (session,
budgets, spend, audit log).

Neither mode touches your wallet: it stays in the CDP project, so remove it in
the CDP portal for a full cleanup.

## Development

The release installer **copies** both the daemon binary and the QML plugin once
— the shell uses `~/.config/omarchy/plugins/gelu22.gateway/` and the daemon runs
`~/.local/bin/gateway`, never the repo. After editing Go or `plugin/omarchy/`,
deploy both to the live shell:

```bash
bash scripts/dev-sync.sh   # build+install daemon, restart it, sync plugin
```

Reopen the panel (or `omarchy restart shell`) to reload. UI code only — budgets,
spend, sellers and remembered URLs are untouched.

## Onboarding (no browser)

1. Click the widget in the bar → enter email → enter the OTP code from mail
2. Set daily spending limit (required): daily cap in USD
3. Fund the wallet — the address is shown in the panel; click it to copy. A
   fresh install defaults to testnet, so you can fund it for free; see
   [Funding your wallet](#funding-your-wallet) just below.

## Funding your wallet

The panel shows a wallet address that starts with `0x…`. It is **one address
with two separate balances**: the same `0x…` holds test USDC on the test network
and real USDC on Base mainnet, and the two never mix. Testnet funds are play
money; mainnet funds are real.

### Test network — Base Sepolia (the default)

Free, no card required. Use it to try the gateway end to end.

1. Copy the `0x…` address from the panel (click it).
2. Open the Circle test faucet: **https://faucet.circle.com/**
3. Choose **Base Sepolia**, paste your address, click **Request tokens**.
4. The balance appears in the widget within a few seconds.

### Main network — Base mainnet (real money)

Switch the network first, then fund it — otherwise the money lands on the wrong
network.

1. In the panel open **Open config** and change `paymentNetwork` from
   `eip155:84532` to `eip155:8453`. The daemon restarts on mainnet.
2. Add real USDC to the **same** `0x…` address, on the **Base** network:
   - Buy with a card, Apple Pay or Google Pay through an on-ramp such as
     [Coinbase Onramp](https://pay.coinbase.com/), [MoonPay](https://www.moonpay.com/)
     or [Kraken](https://www.kraken.com/) — pick **USDC on Base**.
   - Or withdraw from an exchange you already use (Coinbase, Binance, Kraken, …):
     buy USDC, choose **Withdraw**, paste the `0x…` address, and select the
     **Base** network.

> **Pick the Base network, not Ethereum.** USDC sent on the wrong network, or to
> a wrong address, cannot be recovered. Check the network and the address twice
> before you send.

## Spend limits (two layers)

**1. Daily budget — yours.** One cap (default $5, set in the panel; `0` = always
ask). It is the only control on the *sum* you spend in a day:

- **This layer is local.** The cap and today's spent counter live in the daemon's
  own state files on this machine. A process running as your user can change
  them, so this budget guards against a mistaken or overeager agent — not against
  malware. The hard part is layer 2.
- **Within budget: everything auto-pays** — that is the point of the product.
- **Above budget: the panel asks.** The panel opens automatically when a payment
  needs approval; you get the amount, the seller and the URL ("Remember this
  URL", Cancel, Pay). Spend may exceed the cap until midnight, and every further
  over-budget payment asks again.
- **Remember this URL** pre-authorizes one endpoint (even above budget). The
  remembered list lives in the plugin config file, not inline in the panel.

**2. Hard per-payment ceiling — enforced by Coinbase (`X`, 50 USDC in this deployment).**
When the operator has configured it, a rule in Coinbase's Policy Engine, enforced
inside Coinbase's TEE *before* any signature, caps what a **single** payment may
move. It exists precisely because the daily budget is a file on your machine: the
ceiling is not, so tampering with that file cannot lift it.

- **Pick `X` close to your daily budget.** `X` is the most one *single* payment
  can move; the default budget is $5/day, so an `X` of about **$5–10** is the
  natural fit. This deployment ships `X = $50`, deliberately loose — lower it if
  you do not expect expensive purchases.
- A single payment can never exceed the ceiling, whatever your daily budget says.
- **Keep the daily budget ≤ the ceiling.** A higher budget still works, but the
  ceiling clamps every single payment, so the excess can only leave in several
  payments.
- A payment above the ceiling is refused by Coinbase (`policy_violation`) and
  **cannot be approved from the panel** — the panel shows the reason.
- The ceiling is a **per-signature** limit, not a daily one: Coinbase's policy
  engine has no daily sum. The number of payments per day is still bounded by
  layer 1 — which is local and tamperable, so against stolen credentials `X`
  bounds **each** signature and your wallet balance bounds the total.
- The ceiling is **project-wide**: it is one rule for the whole CDP project this
  deployment uses, so it applies to every user of it (and project quotas are
  shared). It is a safety ceiling, not a per-user budget. What that means for
  availability (and how to isolate with your own `CDP_PROJECT_ID`) is threat
  **T11** in the project's threat model.
- The ceiling is a deployment setting: the operator sets it in the CDP Portal
  (or with an API key), and this build does not carry the value. Nothing on your
  computer can change it — see the release notes for what your operator deployed.


## Supported payment rail

What the gateway will sign for — and what it will not. This is a product
contract, not a preference.

**What it accepts (all must hold):**

- scheme `exact`
- network on the allowlist (`eip155:84532` Base Sepolia, `eip155:8453` Base mainnet)
- asset = the pinned ERC-20 that supports **EIP-3009** (USDC), settleable via CDP,
  and covered by the per-payment ceiling
- `payTo` a valid hex address
- `amount > 0` (canonical decimal)

**The loop:** `402 (accepts[]) → checks (scheme/network/asset → policy → seller →
budget) → sign (CDP TEE, EIP-3009) → retry with Payment-Signature → content`.

**Bounds per payment:** daily budget (local), per-domain sub-cap (local),
**per-payment ceiling (Coinbase — not liftable from your machine)**, wallet balance.

**Out of scope (by design):** any non-`exact` scheme and any non-EVM rail
(e.g. Nano/XNO). The seller advertises the rail; a client cannot choose it. A
different rail family needs its own scheme, facilitator and trust model — a
companion project, not a config change.

## MFA TOTP (optional, recommended)

Enable TOTP in the panel (QR code generated by the daemon, secret key backed up
by the user). CDP exposes no API to reset MFA — if you lose the secret key,
reset through the CDP portal (link in the panel). `mfa_required` blocks payments
(fail-closed). No recovery codes.

## Plugin config

The plugin reads `~/.config/omarchy/x402-gateway/config.json` (JSONC, comments
allowed). Manage it with **"Open config"** in the panel (opens your editor via
`omarchy launch config editor`):

```jsonc
{
  // "eip155:84532" (Base Sepolia) or "eip155:8453" (Base Mainnet)
  "paymentNetwork": "eip155:84532",
  // URLs that pay without asking (added from the approval dialog)
  "rememberedUrls": []
}
```

Changing `paymentNetwork` restarts the daemon on the new network (a missing or
corrupt config keeps the running network — fail-safe). Budgets are NOT here:
they stay in the daemon's `~/.local/state/x402-gateway/policy.json`.

## Agent setup

**MCP** (Claude Code / opencode / Cursor / Codex / Gemini) — auto-detect and
one-click **Integrate** per agent in the panel ("AI agents" section), or
manually:

```json
{"mcp": {"x402-gateway": {
  "type": "local",
  "command": ["/home/you/.local/bin/gateway", "-mcp"],
  "environment": {"GATEWAY_SOCKET_PATH": "/home/you/.local/state/x402-gateway/gw.sock"},
  "enabled": true
}}}
```

Tools: `fetch_with_payment` (pays 402 automatically), `gateway_status`,
`gateway_pause`.

**HTTP gateway** (any client):

```bash
curl --unix-socket ~/.local/state/x402-gateway/gw.sock -X POST \
  http://localhost/fetch -H 'Content-Type: application/json' \
  -d '{"url":"https://seller.example/paid-article"}'
```

Over-budget payments return `budget_exceeded` with `can_override: true`;
approved retries go through `/fetch-override`.

## Money audit log

Every payment attempt appends one JSON line to
`~/.local/state/x402-gateway/audit.log` (0600): `amount_micro`, `domain`
(never full URL), `outcome` (`paid` / `failed:<code>`), `override`, timestamp.
Query it with
`grep '"outcome":"paid"' ~/.local/state/x402-gateway/audit.log`.
Daemon persists there too: policy-cap POSTs (`policy caps` old→new) and pause
transitions (`paused`); no-op writes stay silent. Panel UI decisions (approval
clicks, Save clicks) log to the journal instead
(`journalctl --user | grep gelu22.gateway`).

## Monitoring

One command shows daemon state, spend vs caps, recent audit lines, and
socket/config health (no writes to state files, no restarts; creates the empty
state dir if missing):

```bash
gateway --status
```

Meaning of the output: `daemon:` is the live state (`active`, `paused`,
`offline` when the socket is unreachable); `spend:` is today's spend vs the
daily cap; `audit:` lists the last 10 `audit.log` lines
(`no entries yet` on a fresh install); `config:` shows the payment network
and remembered-URL count. A non-zero exit means something needs attention
(dead daemon, unreadable state/config file) — the message names the file.

Lower level, same data piecemeal:

```bash
journalctl --user | grep gelu22.gateway   # panel UI actions + daemon lifecycle
tail ~/.local/state/x402-gateway/audit.log  # every payment attempt (JSONL)
curl --unix-socket ~/.local/state/x402-gateway/gw.sock http://localhost/status
```

## Security

- Private keys never leave the device (CDP MPC + device-generated secrets)
- Daily budget (mandatory at startup) **plus a hard per-payment ceiling enforced
  at Coinbase**, which cannot be lifted from this machine; single-instance lock —
  a duplicate daemon exits instead of risking session revocation
- Unix socket 0600/0700 — your user only
- Fail-closed: no network = no payment; one-click pause
- **Enable NTP**: EIP-3009 authorizations are valid in a ±5 minute window, so a
  wrong clock makes payments fail (the panel warns when the skew exceeds 60 s)
- Security policy and known limits: [SECURITY.md](SECURITY.md)

## Backend (not part of the product)

An earlier Cloudflare Worker (device registration, budget sync and telemetry) is
**not built, not deployed and not published** — it is parked as future work. The
shipped product talks to Coinbase CDP only.

## Architecture

| Component | Technology |
|---|---|
| Daemon | Go 1.25+, static binary |
| Plugin | Quickshell QML (Omarchy) |
| Backend | Cloudflare Workers + D1 (optional) |
| Payments | x402 v2, CDP signing, ERC-8021 builder-code attribution (`s`) |

## License

MIT — see [LICENSE](LICENSE).
