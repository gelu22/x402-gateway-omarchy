// Model.js tests using node:test
// This file tests the pure JavaScript functions from Model.js

import { describe, it, beforeEach } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";

// Load and evaluate Model.js
function loadModelJS() {
  const modelPath = path.join(path.dirname(new URL(import.meta.url).pathname), "..", "Model.js");
  const content = fs.readFileSync(modelPath, "utf-8");
  // Remove .pragma library line which is QML-specific
  const jsContent = content
    .split("\n")
    .filter(line => !line.trim().startsWith(".pragma library"))
    .join("\n");
  // Evaluate in a module scope and return the functions
  const module = { exports: {} };
  eval(jsContent + `
    module.exports = {
      defaultSocketPath,
      buildCommand,
      stateLabel,
      shortAddress,
      networkLabel,
      parseStatus,
      parseOverrideError,
      parseLogoutResponse: (typeof parseLogoutResponse !== "undefined") ? parseLogoutResponse : undefined,
      statusRole,
      heroState,
      overBudgetAlert,
      MICRO_USDC: (typeof MICRO_USDC !== "undefined") ? MICRO_USDC : undefined,
      CURL_TIMEOUT_S: (typeof CURL_TIMEOUT_S !== "undefined") ? CURL_TIMEOUT_S : undefined,
      CURL_TIMEOUT_WAIT_S: (typeof CURL_TIMEOUT_WAIT_S !== "undefined") ? CURL_TIMEOUT_WAIT_S : undefined,
      LOG_TAG: (typeof LOG_TAG !== "undefined") ? LOG_TAG : undefined,
      PLUGIN_ID: (typeof PLUGIN_ID !== "undefined") ? PLUGIN_ID : undefined,
      localBinPath: (typeof localBinPath !== "undefined") ? localBinPath : undefined,
      shareFilePath: (typeof shareFilePath !== "undefined") ? shareFilePath : undefined,
      gatewayConfigPath: (typeof gatewayConfigPath !== "undefined") ? gatewayConfigPath : undefined,
      isSupportedNetwork: (typeof isSupportedNetwork !== "undefined") ? isSupportedNetwork : undefined,
      isValidRememberedUrl: (typeof isValidRememberedUrl !== "undefined") ? isValidRememberedUrl : undefined,
      rememberedLimitMicro: (typeof rememberedLimitMicro !== "undefined") ? rememberedLimitMicro : undefined,
      needsApproval: (typeof needsApproval !== "undefined") ? needsApproval : undefined,
      parseBlocked: (typeof parseBlocked !== "undefined") ? parseBlocked : undefined,
      parseHistory: (typeof parseHistory !== "undefined") ? parseHistory : undefined,
      historyRows: (typeof historyRows !== "undefined") ? historyRows : undefined,
      agentSpendRows: (typeof agentSpendRows !== "undefined") ? agentSpendRows : undefined,
      agentGroups: (typeof agentGroups !== "undefined") ? agentGroups : undefined,
      capList: (typeof capList !== "undefined") ? capList : undefined,
      DEFAULT_AVAILABLE_LIMIT: (typeof DEFAULT_AVAILABLE_LIMIT !== "undefined") ? DEFAULT_AVAILABLE_LIMIT : undefined,
      agentCapsBody: (typeof agentCapsBody !== "undefined") ? agentCapsBody : undefined,
      agentLimitCaption: (typeof agentLimitCaption !== "undefined") ? agentLimitCaption : undefined,
      errorLabel: (typeof errorLabel !== "undefined") ? errorLabel : undefined,
      outcomeLabel: (typeof outcomeLabel !== "undefined") ? outcomeLabel : undefined,
      relativeWhen: (typeof relativeWhen !== "undefined") ? relativeWhen : undefined,
      blockedAmountText: (typeof blockedAmountText !== "undefined") ? blockedAmountText : undefined,
      approveBody: (typeof approveBody !== "undefined") ? approveBody : undefined,
      permissionBody: (typeof permissionBody !== "undefined") ? permissionBody : undefined,
      validatePermission: (typeof validatePermission !== "undefined") ? validatePermission : undefined,
      overrideReasonText: (typeof overrideReasonText !== "undefined") ? overrideReasonText : undefined,
      parseJsonc: (typeof parseJsonc !== "undefined") ? parseJsonc : undefined,
      parseGatewayConfig: (typeof parseGatewayConfig !== "undefined") ? parseGatewayConfig : undefined,
      serializeGatewayConfig: (typeof serializeGatewayConfig !== "undefined") ? serializeGatewayConfig : undefined,
      DEFAULT_NETWORK: (typeof DEFAULT_NETWORK !== "undefined") ? DEFAULT_NETWORK : undefined,
      MAX_REMEMBERED_URLS: (typeof MAX_REMEMBERED_URLS !== "undefined") ? MAX_REMEMBERED_URLS : undefined,
      ENV_NETWORK: (typeof ENV_NETWORK !== "undefined") ? ENV_NETWORK : undefined,
      State: (typeof State !== "undefined") ? State : undefined,
      Endpoint: (typeof Endpoint !== "undefined") ? Endpoint : undefined,
      NET_SEPOLIA: (typeof NET_SEPOLIA !== "undefined") ? NET_SEPOLIA : undefined,
      NET_MAINNET: (typeof NET_MAINNET !== "undefined") ? NET_MAINNET : undefined,
      Precision: (typeof Precision !== "undefined") ? Precision : undefined,
      formatUsd: (typeof formatUsd !== "undefined") ? formatUsd : undefined,
      formatUsdExact: (typeof formatUsdExact !== "undefined") ? formatUsdExact : undefined,
      formatUsdcExact: (typeof formatUsdcExact !== "undefined") ? formatUsdcExact : undefined,
      budgetFraction: (typeof budgetFraction !== "undefined") ? budgetFraction : undefined,
      budgetRemaining: (typeof budgetRemaining !== "undefined") ? budgetRemaining : undefined,
      buildInfoLabel: (typeof buildInfoLabel !== "undefined") ? buildInfoLabel : undefined,
      pluginVersionLabel: (typeof pluginVersionLabel !== "undefined") ? pluginVersionLabel : undefined,
      footerVersionLabel: (typeof footerVersionLabel !== "undefined") ? footerVersionLabel : undefined,
      historyFilePath: (typeof historyFilePath !== "undefined") ? historyFilePath : undefined,
      historyDocument: (typeof historyDocument !== "undefined") ? historyDocument : undefined,
      openEditorCommand: (typeof openEditorCommand !== "undefined") ? openEditorCommand : undefined,
      panelDebugEnabled: (typeof panelDebugEnabled !== "undefined") ? panelDebugEnabled : undefined,
      clipboardCommand: (typeof clipboardCommand !== "undefined") ? clipboardCommand : undefined,
      clipboardStdin: (typeof clipboardStdin !== "undefined") ? clipboardStdin : undefined,
      secretClipboardCommand: (typeof secretClipboardCommand !== "undefined") ? secretClipboardCommand : undefined,
      stdinPayload: (typeof stdinPayload !== "undefined") ? stdinPayload : undefined,
      USDC: (typeof USDC !== "undefined") ? USDC : undefined,
      USD_SYMBOL: (typeof USD_SYMBOL !== "undefined") ? USD_SYMBOL : undefined,
      Method: (typeof Method !== "undefined") ? Method : undefined,
      formatUsdc: (typeof formatUsdc !== "undefined") ? formatUsdc : undefined,
      ICON_WALLET: (typeof ICON_WALLET !== "undefined") ? ICON_WALLET : undefined,
      mfaRole: (typeof mfaRole !== "undefined") ? mfaRole : undefined,
      mfaLabel: (typeof mfaLabel !== "undefined") ? mfaLabel : undefined,
      mfaTooltip: (typeof mfaTooltip !== "undefined") ? mfaTooltip : undefined,
      walletCopyValue: (typeof walletCopyValue !== "undefined") ? walletCopyValue : undefined,
      mfaVerifyReason: (typeof mfaVerifyReason !== "undefined") ? mfaVerifyReason : undefined,
      usdToMicro: (typeof usdToMicro !== "undefined") ? usdToMicro : undefined,
      microToUsd: (typeof microToUsd !== "undefined") ? microToUsd : undefined,
      blockedText: (typeof blockedText !== "undefined") ? blockedText : undefined,
      clockSkewWarning: (typeof clockSkewWarning !== "undefined") ? clockSkewWarning : undefined,
      CLOCK_SKEW_WARN_MS: (typeof CLOCK_SKEW_WARN_MS !== "undefined") ? CLOCK_SKEW_WARN_MS : undefined,
      mfaNagKey: (typeof mfaNagKey !== "undefined") ? mfaNagKey : undefined,
      shouldSurfaceMfa: (typeof shouldSurfaceMfa !== "undefined") ? shouldSurfaceMfa : undefined,
      MFA_REPROMPT_COOLDOWN_MS: (typeof MFA_REPROMPT_COOLDOWN_MS !== "undefined") ? MFA_REPROMPT_COOLDOWN_MS : undefined,
      mfaGateFromError: (typeof mfaGateFromError !== "undefined") ? mfaGateFromError : undefined,
      mfaSudoReason: (typeof mfaSudoReason !== "undefined") ? mfaSudoReason : undefined,
      urlHost: (typeof urlHost !== "undefined") ? urlHost : undefined,
      isMfaCodeValid: (typeof isMfaCodeValid !== "undefined") ? isMfaCodeValid : undefined,
      copyDoneLabel: (typeof copyDoneLabel !== "undefined") ? copyDoneLabel : undefined,
      openUrlCommand: (typeof openUrlCommand !== "undefined") ? openUrlCommand : undefined,
      MFA_RESET_URL: (typeof MFA_RESET_URL !== "undefined") ? MFA_RESET_URL : undefined,
      AGENT_SCRIPT_MISSING: (typeof AGENT_SCRIPT_MISSING !== "undefined") ? AGENT_SCRIPT_MISSING : undefined,
      LOG_TRIM_RESULT: (typeof LOG_TRIM_RESULT !== "undefined") ? LOG_TRIM_RESULT : undefined,
      LOG_TRIM_PROC: (typeof LOG_TRIM_PROC !== "undefined") ? LOG_TRIM_PROC : undefined,
      daemonOffline: (typeof daemonOffline !== "undefined") ? daemonOffline : undefined,
      errorCode: (typeof errorCode !== "undefined") ? errorCode : undefined,
      errorDetail: (typeof errorDetail !== "undefined") ? errorDetail : undefined,
      resolveStep: (typeof resolveStep !== "undefined") ? resolveStep : undefined
    };
  `);
  return module.exports;
}

let Model;

describe("Model.js", () => {
  beforeEach(() => {
    Model = loadModelJS();
  });

  describe("defaultSocketPath", () => {
    it("should return correct path", () => {
      assert.strictEqual(Model.defaultSocketPath("/home/user"), "/home/user/.local/state/x402-gateway/gw.sock");
      assert.strictEqual(Model.defaultSocketPath(""), "/.local/state/x402-gateway/gw.sock");
    });
  });

  describe("buildCommand", () => {
    it("should build GET command without body", () => {
      const cmd = Model.buildCommand("/tmp/sock", "/status", "GET", "");
      assert.deepStrictEqual(cmd, [
        "curl", "-sS", "-m", "30", "--unix-socket", "/tmp/sock",
        "-X", "GET", "-H", "Content-Type: application/json",
        "http://localhost/status"
      ]);
    });

    it("should build POST command with body on stdin, not argv", () => {
      const body = '{"key":"value"}';
      const cmd = Model.buildCommand("/tmp/sock", "/pause", "POST", body);
      assert.deepStrictEqual(cmd, [
        "curl", "-sS", "-m", "30", "--unix-socket", "/tmp/sock",
        "-X", "POST", "-H", "Content-Type: application/json",
        "--data-binary", "@-",
        "http://localhost/pause"
      ]);
      assert.strictEqual(Model.stdinPayload(body), body);
      assert.ok(!cmd.includes(body));
    });

    it("50.1: mfa_code and otp never appear in argv", () => {
      const cases = [
        JSON.stringify({ mfa_code: "123456" }),
        JSON.stringify({ otp: "654321" }),
      ];
      for (const body of cases) {
        const cmd = Model.buildCommand("gw.sock", "/submit", "POST", body);
        assert.ok(cmd.includes("--data-binary"), body);
        assert.ok(cmd.includes("@-"), body);
        for (const arg of cmd) {
          assert.ok(!arg.includes("123456"), arg);
          assert.ok(!arg.includes("654321"), arg);
          assert.ok(!arg.includes("mfa_code"), arg);
          assert.ok(!arg.includes("otp"), arg);
        }
        assert.strictEqual(Model.stdinPayload(body), body);
      }
      assert.strictEqual(Model.stdinPayload(""), "");
      assert.strictEqual(Model.stdinPayload(null), "");
      assert.strictEqual(Model.stdinPayload(undefined), "");
      assert.deepStrictEqual(Model.secretClipboardCommand(), ["wl-copy"]);
      assert.ok(!Model.secretClipboardCommand().join("\0").includes("TOTPSECRETVALUE"));
    });
  });

  describe("stateLabel", () => {
    it("should map all known states", () => {
      assert.strictEqual(Model.stateLabel("active"), "Active");
      assert.strictEqual(Model.stateLabel("paused"), "Paused");
      assert.strictEqual(Model.stateLabel("exhausted"), "Over budget");
      assert.strictEqual(Model.stateLabel("logged_out"), "Sign-in required");
      assert.strictEqual(Model.stateLabel("error"), "Error");
      assert.strictEqual(Model.stateLabel("offline"), "Offline");
      assert.strictEqual(Model.stateLabel("unknown"), "Offline");
    });
  });

  describe("shortAddress", () => {
    it("should shorten full address to 0x1234…abcd", () => {
      assert.strictEqual(Model.shortAddress("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F"), "0x3caa…630F");
      assert.strictEqual(Model.shortAddress("0x1234567890abcdef"), "0x1234…cdef");
    });

    it("should handle short/empty addresses", () => {
      assert.strictEqual(Model.shortAddress("0x123"), "0x123");
      assert.strictEqual(Model.shortAddress(""), "");
      assert.strictEqual(Model.shortAddress(null), "");
      assert.strictEqual(Model.shortAddress(undefined), "");
    });
  });

  describe("networkLabel", () => {
    it("should map known networks", () => {
      assert.strictEqual(Model.networkLabel("eip155:84532"), "Base Sepolia");
      assert.strictEqual(Model.networkLabel("eip155:8453"), "Base Mainnet");
    });

    it("should return raw string for unknown networks", () => {
      assert.strictEqual(Model.networkLabel("eip155:9999"), "eip155:9999");
      assert.strictEqual(Model.networkLabel("unknown"), "unknown");
    });
  });

  describe("parseStatus", () => {
    it("should parse active state", () => {
      const raw = JSON.stringify({
        state: "active",
        wallet_address: "0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F",
        paused: false,
        spend_today_usdc: 1.23,
        budget_daily_usdc: 5.00,
        wallet_balance_usdc: 19.99,
        payment_network: "eip155:84532",
        last_block: null
      });
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.ok, true);
      assert.strictEqual(result.state, "active");
      assert.strictEqual(result.balance_usd, "19.99");
      assert.strictEqual(result.spend_today, "1.23");
      assert.strictEqual(result.budget_daily, "5.00");
      assert.strictEqual(result.address, "0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F");
      assert.strictEqual(result.payment_network, "eip155:84532");
    });

    it("should parse paused state", () => {
      const raw = JSON.stringify({ paused: true, wallet_address: "" });
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.state, "paused");
    });

    it("should parse logged_out state", () => {
      const raw = JSON.stringify({ state: "logged_out", wallet_address: "" });
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.state, "logged_out");
    });

    it("should parse exhausted state with budget_exceeded", () => {
      const raw = JSON.stringify({
        last_block: { reason: "budget_exceeded" },
        wallet_address: "0x123"
      });
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.state, "exhausted");
      assert.strictEqual(result.block_text, "Over daily budget — new payments will ask for approval");
    });

    it("should parse exhausted state with invalid_amount", () => {
      const raw = JSON.stringify({
        last_block: { reason: "invalid_amount" },
        wallet_address: "0x123"
      });
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.block_text, "Single request rejected (invalid amount) — unpaid");
    });

    it("should parse error state (signer_ok false)", () => {
      const raw = JSON.stringify({ signer_ok: false, wallet_address: "0x123" });
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.state, "error");
    });

    it("should handle missing/zero fields gracefully", () => {
      const raw = JSON.stringify({});
      const result = Model.parseStatus(raw);
      assert.strictEqual(result.ok, true);
      assert.strictEqual(result.state, "logged_out");
      assert.strictEqual(result.balance_usd, "0.00");
      assert.strictEqual(result.spend_today, "0.00");
      assert.strictEqual(result.budget_daily, "0.00");
    });

    it("should handle malformed JSON", () => {
      const result = Model.parseStatus("not json");
      assert.strictEqual(result.ok, false);
      assert.strictEqual(result.state, "offline");
      assert.strictEqual(result.balance_usd, "0.00");
    });
  });

  describe("parseOverrideError (ad 1: price_changed)", () => {
    it("should recognize a price_changed re-approval request", () => {
      const raw = JSON.stringify({
        last_fetch_error: {
          code: "price_changed",
          amount_micro: 10000000,
          can_override: true,
          target_url: "https://seller.example/book",
          timestamp: "2026-09-11T00:00:00Z"
        }
      });
      const ov = Model.parseOverrideError(raw);
      assert.strictEqual(ov.isOverride, true);
      assert.strictEqual(ov.code, "price_changed");
      assert.strictEqual(ov.amountMicro, 10000000);
      assert.strictEqual(ov.canOverride, true);
      assert.strictEqual(ov.targetUrl, "https://seller.example/book");
    });

    it("should ignore non-overridable errors", () => {
      const raw = JSON.stringify({
        last_fetch_error: { code: "upstream_error", can_override: false }
      });
      assert.strictEqual(Model.parseOverrideError(raw).isOverride, false);
    });
  });

  describe("rememberedLimitMicro (ad 1: remember caps at the approved amount)", () => {
    it("should return the approved amount for a remembered URL", () => {
      const list = [{ url: "https://a.example/x", added: "t", approvedMicro: 10000 }];
      assert.strictEqual(Model.rememberedLimitMicro(list, "https://a.example/x"), 10000);
    });

    it("should return 0 for a legacy entry without approvedMicro (forces re-approval)", () => {
      const list = [{ url: "https://a.example/x", added: "t" }];
      assert.strictEqual(Model.rememberedLimitMicro(list, "https://a.example/x"), 0);
    });

    it("should return 0 for hostile approvedMicro (Infinity is the fix case; negatives were already rejected by `> 0`)", () => {
      for (const bad of [-5, 1e999, NaN]) {
        const list = [{ url: "https://a.example/x", added: "t", approvedMicro: bad }];
        assert.strictEqual(Model.rememberedLimitMicro(list, "https://a.example/x"), 0, String(bad));
      }
    });

    it("should sanitize hostile approvedMicro coming from a hand-edited file", () => {
      const raw = '{"paymentNetwork":"eip155:84532","rememberedUrls":[' +
        '{"url":"https://a.example/x","added":"t","approvedMicro":1e999},' +
        '{"url":"https://b.example/y","added":"t","approvedMicro":-5}]}';
      const cfg = Model.parseGatewayConfig(raw);
      assert.strictEqual(cfg.rememberedUrls[0].approvedMicro, 0);
      assert.strictEqual(cfg.rememberedUrls[1].approvedMicro, 0);
      assert.strictEqual(Model.needsApproval(
        { targetUrl: "https://a.example/x", amountMicro: 1, code: "budget_exceeded" },
        cfg.rememberedUrls).autoPay, false);
    });

    it("should resolve duplicate JSON keys deterministically (last wins)", () => {
      const raw = '{"paymentNetwork":"eip155:84532","paymentNetwork":"eip155:8453","rememberedUrls":[]}';
      const cfg = Model.parseGatewayConfig(raw);
      assert.strictEqual(cfg.paymentNetwork, "eip155:8453");
    });

    it("should return 0 for an unknown URL", () => {
      assert.strictEqual(Model.rememberedLimitMicro([], "https://a.example/x"), 0);
    });
  });

  describe("needsApproval (ad 1: remembered URL cannot silently pay a raise)", () => {
    const rem = (micro) => [{ url: "https://a.example/x", added: "t", approvedMicro: micro }];

    it("should auto-pay a remembered URL at or below the approved amount", () => {
      const d = Model.needsApproval({ targetUrl: "https://a.example/x", amountMicro: 10000, code: "budget_exceeded" }, rem(10000));
      assert.strictEqual(d.autoPay, true);
      assert.strictEqual(d.priceChanged, false);
    });

    it("should NOT auto-pay when a fresh fetch raises the price above the approved amount", () => {
      const d = Model.needsApproval({ targetUrl: "https://a.example/x", amountMicro: 500000, code: "budget_exceeded" }, rem(10000));
      assert.strictEqual(d.autoPay, false);
      assert.strictEqual(d.priceChanged, true);
      assert.strictEqual(d.previousMicro, 10000);
    });

    it("should force approval for a legacy remembered entry (no approved amount)", () => {
      const d = Model.needsApproval({ targetUrl: "https://a.example/x", amountMicro: 5000, code: "budget_exceeded" },
        [{ url: "https://a.example/x", added: "t" }]);
      assert.strictEqual(d.autoPay, false);
      assert.strictEqual(d.priceChanged, true);
      assert.strictEqual(d.previousMicro, 0);
    });

    it("should force approval on a gateway price_changed regardless of remember", () => {
      const d = Model.needsApproval({ targetUrl: "https://a.example/x", amountMicro: 10000, code: "price_changed" }, rem(10000));
      assert.strictEqual(d.autoPay, false);
      assert.strictEqual(d.priceChanged, true);
    });

    it("should not auto-pay an unknown (non-remembered) URL (fail-closed)", () => {
      const d = Model.needsApproval({ targetUrl: "https://n.example/y", amountMicro: 5000, code: "budget_exceeded" }, rem(10000));
      assert.strictEqual(d.autoPay, false);
      assert.strictEqual(d.remembered, false);
    });

    it("should force dialog for unknown_seller even when remembered (013.2)", () => {
      const d = Model.needsApproval({ targetUrl: "https://a.example/x", amountMicro: 5000, code: "unknown_seller" }, rem(10000));
      assert.strictEqual(d.autoPay, false);
    });

    it("should fail closed on hostile override objects", () => {
      for (const ov of [null, undefined, {}, { targetUrl: 42 }, { amountMicro: "lots" },
                        { targetUrl: "https://a.example/x", amountMicro: -5 },
                        { targetUrl: "https://a.example/x", amountMicro: NaN },
                        { targetUrl: "https://a.example/x", amountMicro: 1e999 }]) {
        const d = Model.needsApproval(ov, rem(10000));
        assert.strictEqual(d.autoPay, false, JSON.stringify(ov));
      }
      for (const list of [null, undefined, "x", [{ url: "https://a.example/x" }]]) {
        const d = Model.needsApproval({ targetUrl: "https://a.example/x", amountMicro: 1, code: "budget_exceeded" }, list);
        assert.strictEqual(d.autoPay, false);
      }
    });
  });

  describe("overrideReasonText (013.2: seller-trust dialog copy)", () => {
    it("should name the domain for first-payment approval", () => {
      assert.strictEqual(
        Model.overrideReasonText("unknown_seller", "https://New-Seller.Example.com:8443/pay"),
        "First payment to new-seller.example.com — approve this seller?"
      );
    });

    it("should explain the per-seller share breach", () => {
      assert.strictEqual(
        Model.overrideReasonText("domain_cap_exceeded", "https://a.example/x"),
        "Over this seller's share of today's budget."
      );
    });

    it("should render IPv6 and strip userinfo/port", () => {
      assert.strictEqual(
        Model.overrideReasonText("unknown_seller", "https://[::1]:8080/x"),
        "First payment to ::1 — approve this seller?"
      );
      assert.strictEqual(
        Model.overrideReasonText("unknown_seller", "https://user:pass@h.example:1/x"),
        "First payment to h.example — approve this seller?"
      );
    });

    it("should stay silent for codes with dedicated UI", () => {
      assert.strictEqual(Model.overrideReasonText("budget_exceeded", "https://a.example/x"), "");
      assert.strictEqual(Model.overrideReasonText("price_changed", "https://a.example/x"), "");
      assert.strictEqual(Model.overrideReasonText("bogus", "https://a.example/x"), "");
      assert.strictEqual(Model.overrideReasonText("unknown_seller", ""), "First payment to this seller — approve it?");
    });

    it("should survive hostile reason inputs without throwing", () => {
      for (const args of [[null, null], [42, 42], ["unknown_seller", null],
                          ["unknown_seller", 42], [null, "https://a.example/x"],
                          ["unknown_seller", "not a url"], ["unknown_seller", "ftp://x/y"]]) {
        const text = Model.overrideReasonText(args[0], args[1]);
        assert.strictEqual(typeof text, "string", JSON.stringify(args));
      }
      assert.strictEqual(Model.overrideReasonText("unknown_seller", "not a url"), "First payment to this seller — approve it?");
    });
  });

  describe("gateway config remember round-trip (ad 1)", () => {
    it("should persist and re-read approvedMicro", () => {
      const cfg = {
        paymentNetwork: Model.NET_SEPOLIA,
        rememberedUrls: [{ url: "https://a.example/x", added: "t", approvedMicro: 250000 }]
      };
      const back = Model.parseGatewayConfig(Model.serializeGatewayConfig(cfg));
      assert.strictEqual(back.rememberedUrls.length, 1);
      assert.strictEqual(back.rememberedUrls[0].approvedMicro, 250000);
    });

    it("should sanitize hostile approvedMicro on write (never persist Infinity)", () => {
      const cfg = {
        paymentNetwork: Model.NET_SEPOLIA,
        rememberedUrls: [
          { url: "https://a.example/x", added: "t", approvedMicro: 1e999 },
          { url: "https://b.example/y", added: "t", approvedMicro: -5 }
        ]
      };
      const back = Model.parseGatewayConfig(Model.serializeGatewayConfig(cfg));
      assert.strictEqual(back.rememberedUrls[0].approvedMicro, 0);
      assert.strictEqual(back.rememberedUrls[1].approvedMicro, 0);
    });
  });

  describe("theme roles (Model returns names; Palette.qml resolves colors)", () => {
    it("statusRole maps every known state and fails safe", () => {
      assert.strictEqual(Model.statusRole("active"), "ok");
      assert.strictEqual(Model.statusRole("paused"), "paused");
      assert.strictEqual(Model.statusRole("exhausted"), "warn");
      assert.strictEqual(Model.statusRole("logged_out"), "info");
      assert.strictEqual(Model.statusRole("error"), "error");
      assert.strictEqual(Model.statusRole("offline"), "offline");
      assert.strictEqual(Model.statusRole("bogus"), "offline");
    });

    it("mfaRole is ok/offline", () => {
      assert.strictEqual(Model.mfaRole(true), "ok");
      assert.strictEqual(Model.mfaRole(false), "offline");
      assert.strictEqual(Model.mfaRole(undefined), "offline");
    });

    it("Model.js carries no color literals", () => {
      assert.strictEqual(typeof Model.Palette, "undefined");
      assert.strictEqual(typeof Model.statusColor, "undefined");
      assert.strictEqual(typeof Model.mfaBadge, "undefined");
    });
  });

  describe("shared constants", () => {
    it("should expose money/time/log constants", () => {
      assert.strictEqual(Model.MICRO_USDC, 1000000);
      assert.strictEqual(Model.CURL_TIMEOUT_S, 30);
      assert.strictEqual(Model.LOG_TAG, "[gelu22.gateway]");
    });

    it("should expose 015.1 literals (single source, no magic values)", () => {
      assert.strictEqual(Model.ICON_WALLET, "\uF09D");
      assert.strictEqual(Model.AGENT_SCRIPT_MISSING, "Agent setup script not found");
      assert.strictEqual(Model.LOG_TRIM_RESULT, 300);
      assert.strictEqual(Model.LOG_TRIM_PROC, 1500);
    });

    it("daemonOffline() should build the exact daemon-offline payload", () => {
      assert.strictEqual(Model.daemonOffline(), '{"error":"daemon_offline"}');
      assert.deepStrictEqual(JSON.parse(Model.daemonOffline()), { error: "daemon_offline" });
    });

    it("errorCode/errorDetail should read the canonical pair", () => {
      const o = { error: "mfa_required", detail: "verify now" };
      assert.strictEqual(Model.errorCode(o), "mfa_required");
      assert.strictEqual(Model.errorDetail(o), "verify now");
    });

    it("errorCode/errorDetail should ignore the cut deprecated pair", () => {
      const o = { code: "mfa_required", message: "verify now" };
      assert.strictEqual(Model.errorCode(o), "");
      assert.strictEqual(Model.errorDetail(o), "");
    });

    it("errorCode/errorDetail should return empty string when no pair", () => {
      assert.strictEqual(Model.errorCode({}), "");
      assert.strictEqual(Model.errorDetail({}), "");
      assert.strictEqual(Model.errorCode(null), "");
      assert.strictEqual(Model.errorDetail(null), "");
      assert.strictEqual(Model.errorCode(JSON.parse(Model.daemonOffline())), "daemon_offline");
    });

    it("parseOverrideError should read the live last_fetch_error code", () => {
      const raw = JSON.stringify({ last_fetch_error: {
        code: "price_changed", can_override: true, amount_micro: 1000,
        target_url: "https://x.example/p", timestamp: "t" } });
      const ov = Model.parseOverrideError(raw);
      assert.strictEqual(ov.isOverride, true);
      assert.strictEqual(ov.code, "price_changed");
    });

    it("errorCode/errorDetail should agree with the server on the shared fixture", () => {      // Shared envelope fixture with internal/server/socket_test.go (018.5):
      // canonical pair only — the deprecated pair is cut on both sides.
      const env = { error: "policy_error", detail: "denied" };
      assert.strictEqual(Model.errorCode(env), "policy_error");
      assert.strictEqual(Model.errorDetail(env), "denied");
      assert.strictEqual(Model.errorCode(env), env.error);
      assert.strictEqual(Model.errorDetail(env), env.detail);
    });

    it("parseLogoutResponse should accept logged_out", () => {
      const r = Model.parseLogoutResponse('{"state":"logged_out"}');
      assert.strictEqual(r.ok, true);
    });

    it("parseLogoutResponse should reject an error envelope with text", () => {
      const r = Model.parseLogoutResponse('{"error":"pair_logout_error","detail":"boom"}');
      assert.strictEqual(r.ok, false);
      assert.strictEqual(r.text, "boom");
    });

    it("parseLogoutResponse should reject garbage without hanging", () => {
      const r = Model.parseLogoutResponse("not json{{{");
      assert.strictEqual(r.ok, false);
      assert.strictEqual(r.text, "not json{{{");
    });

    it("should expose plugin identity and install-layout paths", () => {
      assert.strictEqual(Model.PLUGIN_ID, "gelu22.gateway");
      assert.strictEqual(Model.localBinPath("/home/user"), "/home/user/.local/bin/gateway");
      assert.strictEqual(Model.shareFilePath("/home/user", "setup-agents.sh"), "/home/user/.local/share/x402-gateway/setup-agents.sh");
    });
  });

  describe("heroState (009.1: honest hero line)", () => {
    const base = { paused: false, online: true, session: "active", wallet: "0xabc",
                   signerOk: true, spend: 0.1, cap: 1 };
    it("should show Active when all green", () => {
      const hs = Model.heroState(base);
      assert.strictEqual(hs.label, "Active");
      assert.strictEqual(hs.over, false);
    });
    it("should prioritize paused over everything", () => {
      const hs = Model.heroState({ ...base, paused: true, spend: 5 });
      assert.strictEqual(hs.label, "Paused");
    });
    it("should show Offline when unreachable", () => {
      const hs = Model.heroState({ ...base, online: false });
      assert.strictEqual(hs.label, "Offline");
    });
    it("should show Sign-in required when logged out or walletless", () => {
      assert.strictEqual(Model.heroState({ ...base, session: "logged_out" }).label, "Sign-in required");
      assert.strictEqual(Model.heroState({ ...base, wallet: "" }).label, "Sign-in required");
    });
    it("should show Error on signer failure with wallet", () => {
      const hs = Model.heroState({ ...base, signerOk: false });
      assert.strictEqual(hs.label, "Error");
    });
    it("should show Over budget without alarmism", () => {
      const hs = Model.heroState({ ...base, spend: 1.5 });
      assert.strictEqual(hs.label, "Over budget");
      assert.strictEqual(hs.over, true);
    });
    it("should assert roles per state (visual contract)", () => {
      const cases = [
        [{ ...base, paused: true }, "paused"],
        [{ ...base, online: false }, "offline"],
        [{ ...base, session: "logged_out" }, "info"],
        [{ ...base, signerOk: false }, "error"],
        [{ ...base, spend: 1.5 }, "warn"],
        [base, "ok"]
      ];
      for (const [input, role] of cases)
        assert.strictEqual(Model.heroState(input).role, role);
    });
    it("should treat spend == cap as within budget (strict >)", () => {
      const hs = Model.heroState({ ...base, spend: 1, cap: 1 });
      assert.strictEqual(hs.label, "Active");
      assert.strictEqual(hs.over, false);
    });
    it("should treat missing signerOk as OK and walletless as logged out", () => {
      const noField = Model.heroState({ ...base, signerOk: undefined });
      assert.strictEqual(noField.label, "Active");
      const noWallet = Model.heroState({ ...base, session: "", wallet: "" });
      assert.strictEqual(noWallet.label, "Sign-in required");
    });
    it("should ignore a zero cap (always-ask mode, not over)", () => {
      const hs = Model.heroState({ ...base, spend: 1, cap: 0 });
      assert.strictEqual(hs.over, false);
    });
  });

  describe("overBudgetAlert", () => {
    it("renders a single neutral line only when over", () => {
      assert.strictEqual(Model.overBudgetAlert(true), "⚠ Over budget — new payments will ask for approval.");
      assert.strictEqual(Model.overBudgetAlert(false), "");
    });
  });


  describe("gateway config (009.5)", () => {
    it("gatewayConfigPath points under ~/.config/omarchy", () => {
      assert.strictEqual(Model.gatewayConfigPath("/home/u"), "/home/u/.config/omarchy/x402-gateway/config.json");
    });

    it("ENV_NETWORK matches the daemon's env contract", () => {
      assert.strictEqual(Model.ENV_NETWORK, "GATEWAY_NETWORK");
    });

    it("isSupportedNetwork allows both Base networks only", () => {
      assert.strictEqual(Model.isSupportedNetwork("eip155:84532"), true);
      assert.strictEqual(Model.isSupportedNetwork("eip155:8453"), true);
      assert.strictEqual(Model.isSupportedNetwork("eip155:1"), false);
      assert.strictEqual(Model.isSupportedNetwork(""), false);
    });

    it("isValidRememberedUrl accepts http(s) with host, rejects junk", () => {
      assert.strictEqual(Model.isValidRememberedUrl("https://a.example/x?y=1"), true);
      assert.strictEqual(Model.isValidRememberedUrl("http://a.example"), true);
      assert.strictEqual(Model.isValidRememberedUrl("ftp://a.example"), false);
      assert.strictEqual(Model.isValidRememberedUrl("not a url"), false);
      assert.strictEqual(Model.isValidRememberedUrl(""), false);
    });

    it("parseJsonc strips whole-line comments and trailing commas", () => {
      const raw = `{
        // a comment
        "a": 1,
      }`;
      assert.deepStrictEqual(JSON.parse(Model.parseJsonc(raw)), { a: 1 });
    });

    it("parseGatewayConfig returns defaults for empty/missing text", () => {
      for (const t of ["", "   ", null, undefined]) {
        const c = Model.parseGatewayConfig(t);
        assert.strictEqual(c.paymentNetwork, Model.DEFAULT_NETWORK);
        assert.deepStrictEqual(c.rememberedUrls, []);
      }
    });

    it("parseGatewayConfig reads JSONC with comments and URLs", () => {
      const raw = `{
        // network
        "paymentNetwork": "eip155:8453",
        // list
        "rememberedUrls": [
          { "url": "https://x.example/a", "added": "2026-09-10T00:00:00Z" },
        ],
      }`;
      const c = Model.parseGatewayConfig(raw);
      assert.strictEqual(c.ok, true);
      assert.strictEqual(c.paymentNetwork, "eip155:8453");
      assert.strictEqual(c.rememberedUrls.length, 1);
      assert.strictEqual(c.rememberedUrls[0].url, "https://x.example/a");
      assert.strictEqual(c.rememberedUrls[0].added, "2026-09-10T00:00:00Z");
    });

    it("parseGatewayConfig falls back to default on unsupported network", () => {
      const c = Model.parseGatewayConfig('{"paymentNetwork":"eip155:1"}');
      assert.strictEqual(c.paymentNetwork, Model.DEFAULT_NETWORK);
      assert.match(c.error, /unsupported network/);
    });

    it("parseGatewayConfig drops invalid URLs and tolerates corrupt JSON", () => {
      const c = Model.parseGatewayConfig('{"rememberedUrls":[{"url":"nope"},{"url":"https://ok.example"}]}');
      assert.strictEqual(c.rememberedUrls.length, 1);
      assert.strictEqual(c.rememberedUrls[0].url, "https://ok.example");
      const bad = Model.parseGatewayConfig("{ not json");
      assert.strictEqual(bad.ok, false);
      assert.strictEqual(bad.paymentNetwork, Model.DEFAULT_NETWORK);
    });

    it("serializeGatewayConfig round-trips through parseGatewayConfig", () => {
      const cfg = {
        paymentNetwork: "eip155:8453",
        rememberedUrls: [
          { url: "https://x.example/a", added: "2026-09-10T00:00:00Z", approvedMicro: 0 },
          { url: "https://y.example/b", added: "", approvedMicro: 0 }
        ]
      };
      const text = Model.serializeGatewayConfig(cfg);
      assert.match(text, /\/\//); // keeps header comments
      const back = Model.parseGatewayConfig(text);
      assert.strictEqual(back.paymentNetwork, "eip155:8453");
      assert.deepStrictEqual(back.rememberedUrls, cfg.rememberedUrls);
    });

    it("serializeGatewayConfig handles empty list and bad network", () => {
      const text = Model.serializeGatewayConfig({ paymentNetwork: "bogus", rememberedUrls: [] });
      assert.match(text, /"rememberedUrls": \[\]/);
      const back = Model.parseGatewayConfig(text);
      assert.strictEqual(back.paymentNetwork, Model.DEFAULT_NETWORK);
      assert.deepStrictEqual(back.rememberedUrls, []);
    });

    it("parseJsonc is string-aware: does not corrupt '],' inside a URL", () => {
      const raw = '{"rememberedUrls":[{"url":"https://a.example/x,]"}]}';
      const c = Model.parseGatewayConfig(raw);
      assert.strictEqual(c.rememberedUrls.length, 1);
      assert.strictEqual(c.rememberedUrls[0].url, "https://a.example/x,]");
    });

    it("round-trips URLs with quotes, backslashes and unicode", () => {
      const cfg = {
        paymentNetwork: "eip155:8453",
        rememberedUrls: [
          { url: 'https://a.example/q?x="1"&y=\\z', added: "2026-09-10T00:00:00Z", approvedMicro: 0 },
          { url: "https://b.example/ścieżka", added: "", approvedMicro: 0 }
        ]
      };
      const back = Model.parseGatewayConfig(Model.serializeGatewayConfig(cfg));
      assert.deepStrictEqual(back.rememberedUrls, cfg.rememberedUrls);
    });

    it("serializeGatewayConfig drops invalid entries and caps at MAX", () => {
      const many = [];
      for (let i = 0; i < Model.MAX_REMEMBERED_URLS + 5; i++)
        many.push({ url: "https://x.example/" + i, added: "" });
      const back = Model.parseGatewayConfig(Model.serializeGatewayConfig({ rememberedUrls: many }));
      assert.strictEqual(back.rememberedUrls.length, Model.MAX_REMEMBERED_URLS);
      const mixed = Model.parseGatewayConfig(Model.serializeGatewayConfig({ rememberedUrls: [null, { url: "nope" }, { url: "https://ok.example" }] }));
      assert.deepStrictEqual(mixed.rememberedUrls, [{ url: "https://ok.example", added: "", approvedMicro: 0 }]);
    });

    it("serializeGatewayConfig tolerates undefined/null and non-array list", () => {
      for (const cfg of [undefined, null, {}, { rememberedUrls: "nope" }]) {
        const back = Model.parseGatewayConfig(Model.serializeGatewayConfig(cfg));
        assert.strictEqual(back.paymentNetwork, Model.DEFAULT_NETWORK);
        assert.deepStrictEqual(back.rememberedUrls, []);
      }
    });

    it("parseGatewayConfig reports ok semantics for bad network", () => {
      const c = Model.parseGatewayConfig('{"paymentNetwork":"eip155:1"}');
      assert.strictEqual(c.ok, true); // parsed, usable with fallback
      assert.match(c.error, /unsupported network/);
    });

    it("shipped template equals serializeGatewayConfig(defaults)", () => {
      const tplPath = path.join(path.dirname(new URL(import.meta.url).pathname), "..", "..", "..", "config", "gateway-config.json");
      const tpl = fs.readFileSync(tplPath, "utf-8");
      assert.strictEqual(tpl, Model.serializeGatewayConfig({ paymentNetwork: Model.DEFAULT_NETWORK, rememberedUrls: [] }));
      assert.strictEqual(Model.parseGatewayConfig(tpl).ok, true);
    });
  });

  describe("enums/helpers (010.1)", () => {
    it("State keys match the daemon contract", () => {
      assert.deepStrictEqual(Model.State, {
        ACTIVE: "active", PAUSED: "paused", EXHAUSTED: "exhausted",
        LOGGED_OUT: "logged_out", OFFLINE: "offline", ERROR: "error"
      });
    });

    it("Endpoint paths match CONTRACTS §1", () => {
      assert.deepStrictEqual(Model.Endpoint, {
        STATUS: "/status", POLICY: "/policy", PAUSE: "/pause",
        PAIR_INIT: "/pair/init", PAIR_VERIFY: "/pair/verify", PAIR_LOGOUT: "/pair/logout", FETCH_OVERRIDE: "/fetch-override",
        FETCH_APPROVE: "/fetch-approve", PERMISSIONS: "/permissions",
        MFA_ENROLL_INIT: "/mfa/enroll/init", MFA_ENROLL_SUBMIT: "/mfa/enroll/submit",
        MFA_VERIFY_INIT: "/mfa/verify/init", MFA_VERIFY_SUBMIT: "/mfa/verify/submit",
        HISTORY: "/history"
      });
    });

    it("Networks are pinned", () => {
      assert.strictEqual(Model.NET_SEPOLIA, "eip155:84532");
      assert.strictEqual(Model.NET_MAINNET, "eip155:8453");
      assert.strictEqual(Model.DEFAULT_NETWORK, Model.NET_SEPOLIA);
    });

    it("formatUsd is NaN-safe and honors precision", () => {
      assert.strictEqual(Model.formatUsd(1.5), "1.50");
      assert.strictEqual(Model.formatUsd(1.5, Model.Precision.SPEND), "1.500");
      assert.strictEqual(Model.formatUsd(1.5, Model.Precision.BALANCE), "1.5000");
      assert.strictEqual(Model.formatUsd(undefined), "0.00");
      assert.strictEqual(Model.formatUsd(null), "0.00");
      assert.strictEqual(Model.formatUsd("abc"), "0.00");
      assert.strictEqual(Model.formatUsd(NaN), "0.00");
      assert.strictEqual(Model.formatUsd(""), "0.00");
      assert.deepStrictEqual(Model.Precision, { MONEY: 2, SPEND: 3, BALANCE: 4 });
    });

    it("formatUsdExact keeps sub-cent x402 amounts visible", () => {
      // Regression: a flat 2-decimal format showed a real $0.002 as "0.00".
      assert.strictEqual(Model.formatUsdExact(0.002), "0.002");
      assert.strictEqual(Model.formatUsdExact(0.001), "0.001");
      assert.strictEqual(Model.formatUsdExact(0.0006), "0.0006");
      assert.strictEqual(Model.formatUsdExact(0.01), "0.01");
      assert.strictEqual(Model.formatUsdExact(0.02), "0.02");
      assert.strictEqual(Model.formatUsdExact(0), "0.00");
      assert.strictEqual(Model.formatUsdExact(5), "5.00");
      assert.strictEqual(Model.formatUsdExact(1.234567), "1.234567");
      assert.strictEqual(Model.formatUsdExact(-0.002), "-0.002");
      // NaN-safe, same contract as formatUsd.
      assert.strictEqual(Model.formatUsdExact(undefined), "0.00");
      assert.strictEqual(Model.formatUsdExact("abc"), "0.00");
      assert.strictEqual(Model.formatUsdExact(""), "0.00");
      // Idempotent: re-formatting its own output must not lose precision.
      assert.strictEqual(Model.formatUsdExact(Model.formatUsdExact(0.002)), "0.002");
      assert.strictEqual(Model.formatUsdcExact(0.002), "0.002 USDC");
    });

    it("NEW-P2-1: parseStatus uses formatUsdExact (micro spend/balance stay visible)", () => {
      // Regression: formatUsd(..., SPEND=3) turned 0.0004 into "0.000".
      const st = Model.parseStatus(JSON.stringify({
        wallet_address: "0xabc",
        spend_today_usdc: 0.0004,
        budget_daily_usdc: 5,
        wallet_balance_usdc: 0.0006,
      }));
      assert.strictEqual(st.ok, true);
      assert.strictEqual(st.spend_today, "0.0004");
      assert.strictEqual(st.balance_usd, "0.0006");
      assert.strictEqual(st.budget_daily, "5.00");
      // Fixed-decimal path must NOT be what parseStatus uses for these fields.
      assert.notStrictEqual(st.spend_today, Model.formatUsd(0.0004, Model.Precision.SPEND));
    });

    it("clipboardCommand keeps hostile payloads off argv and on stdin", () => {
      // Real path: a seller-controlled URL reaches the clipboard through
      // OverrideConfirmDialog -> clipboardStdin -> wl-copy stdin. There is
      // no shell, so these strings are data, not a script.
      const hostile = [
        "$(id)", "`id`", "a\nb", "a;rm -rf ~", "a|wl-copy", "a&b", "a>b",
        "a'b", "a'\\''b", "\\", '"', "a b", "--help", "-x", "'",
        "zażółć", "🛡️", "x".repeat(10000),
      ];
      for (const payload of hostile) {
        const argv = Model.clipboardCommand(payload);
        assert.deepStrictEqual(argv, ["wl-copy"], payload);
        assert.ok(!argv.some((part) => String(part).includes(payload)), payload);
        assert.strictEqual(Model.clipboardStdin(payload), payload);
      }
    });

    it("clipboardCommand returns a safe argv", () => {
      assert.strictEqual(Model.clipboardCommand(""), null);
      assert.strictEqual(Model.clipboardCommand(null), null);
      assert.strictEqual(Model.clipboardStdin(""), "");
      assert.strictEqual(Model.clipboardStdin(null), "");
      assert.deepStrictEqual(Model.clipboardCommand("https://a.example/x"), ["wl-copy"]);
      assert.strictEqual(Model.clipboardStdin("https://a.example/x"), "https://a.example/x");
      assert.deepStrictEqual(Model.clipboardCommand("a'b"), ["wl-copy"]);
      assert.strictEqual(Model.clipboardStdin("a'b"), "a'b");
    });

    it("statusRole/stateLabel return exact per-state values", () => {
      assert.strictEqual(Model.stateLabel(Model.State.ACTIVE), "Active");
      assert.strictEqual(Model.stateLabel(Model.State.PAUSED), "Paused");
      assert.strictEqual(Model.stateLabel(Model.State.EXHAUSTED), "Over budget");
      assert.strictEqual(Model.stateLabel(Model.State.LOGGED_OUT), "Sign-in required");
      assert.strictEqual(Model.stateLabel(Model.State.ERROR), "Error");
      assert.strictEqual(Model.stateLabel("bogus"), "Offline");
      assert.strictEqual(Model.statusRole(Model.State.ACTIVE), "ok");
      assert.strictEqual(Model.statusRole(Model.State.PAUSED), "paused");
      assert.strictEqual(Model.statusRole(Model.State.EXHAUSTED), "warn");
      assert.strictEqual(Model.statusRole(Model.State.LOGGED_OUT), "info");
      assert.strictEqual(Model.statusRole(Model.State.ERROR), "error");
      assert.strictEqual(Model.statusRole("bogus"), "offline");
    });

    it("formatUsdc appends the unit; USDC/USD_SYMBOL/Method pinned", () => {
      assert.strictEqual(Model.formatUsdc(1.5), "1.50 USDC");
      assert.strictEqual(Model.formatUsdc(1.5, Model.Precision.SPEND), "1.500 USDC");
      assert.strictEqual(Model.formatUsdc("abc"), "0.00 USDC");
      assert.strictEqual(Model.formatUsdc("0.05"), "0.05 USDC"); // already-formatted input
      assert.strictEqual(Model.USDC, "USDC");
      assert.strictEqual(Model.USD_SYMBOL, "$");
      assert.deepStrictEqual(Model.Method, { GET: "GET", POST: "POST", DELETE: "DELETE" });
    });
  });

  // 016.6c — property test: autoPay is never true when any guard fails
  describe("needsApproval property", () => {
    it("autoPay ⇒ known, sane amount, within limit, not unknown_seller", () => {
      const { needsApproval } = Model;
      const rng = makeRng(42);
      const codes = ["per_request_cap", "budget_exceeded", "price_changed", "unknown_seller", ""];
      const url = "https://example.com/v1/x402-test";
      const knownUrls = [
        { url: "https://example.com/v1/x402-test", added: Date.now(), approvedMicro: 1000000 },
        { url: "https://other.com/v1", added: Date.now(), approvedMicro: 500000 },
        { url: "https://example.com/v1/x402-test", added: Date.now(), approvedMicro: 0 }, // legacy=always ask
      ];
      for (let i = 0; i < 5000; i++) {
        const code = codes[rng() % codes.length];
        const isInf = rng() > 0.85;
        const amt = isInf ? (rng() > 0.5 ? Infinity : -Infinity)
          : rng() * 100000000;
        const target = rng() > 0.1 ? url : (rng() % 2 === 0 ? "" : "https://different.com/path");
        const sel = rng() % 5;
        let remembered;
        if (sel === 0) remembered = knownUrls;
        else if (sel === 1) remembered = [knownUrls[0]];
        else if (sel === 2) remembered = [knownUrls[1]];
        else if (sel === 3) remembered = [knownUrls[2]]; // legacy=0
        else remembered = [];
        // occasional weird entries
        if (rng() % 10 === 0) remembered = [undefined, {}, knownUrls[0], null];
        const ov = { code, targetUrl: target, amountMicro: amt };
        const res = needsApproval(ov, remembered);
        // structural check
        assert.strictEqual(typeof res.remembered, "boolean");
        assert.strictEqual(typeof res.priceChanged, "boolean");
        assert.strictEqual(typeof res.previousMicro, "number");
        assert.strictEqual(typeof res.autoPay, "boolean");
        if (!res.autoPay) continue;
        // autoPay is true — all guards must hold:
        // 1. known URL in rememberedUrls
        const found = Array.isArray(remembered) && target !== "" &&
          remembered.some(r => r && r.url === target);
        assert.strictEqual(found, true, `autoPay=true but target not in rememberedUrls`);
        // 2. finite, positive amount
        assert.strictEqual(isFinite(amt), true, `autoPay=true but non-finite amount`);
        assert.ok(amt > 0, `autoPay=true but amount<=0`);
        // 3. amount <= limit
        const limit = Model.rememberedLimitMicro(remembered, target);
        assert.ok(amt <= limit, `autoPay=true but ${amt} > limit ${limit}`);
        // 4. legacy=0 → not autoPay (must ask)
        if (sel === 3) assert.fail("autoPay with legacy=0 must be false");
        // 5. code != unknown_seller
        assert.notStrictEqual(code, "unknown_seller", "autoPay with unknown_seller");
      }
    });
  });

  describe("MFA helpers (017.11)", () => {
    it("isMfaCodeValid accepts exactly 6 digits", () => {
      assert.strictEqual(Model.isMfaCodeValid("123456"), true);
      assert.strictEqual(Model.isMfaCodeValid("000000"), true);
      for (const bad of ["12345", "1234567", "12 456", "abcdef", "", null, undefined]) {
        assert.strictEqual(Model.isMfaCodeValid(bad), false, `should reject ${JSON.stringify(bad)}`);
      }
    });

    it("mfaLabel is a friendly on/off status line", () => {
      assert.strictEqual(Model.mfaLabel(true), "Two-factor protection: on");
      assert.strictEqual(Model.mfaLabel(false), "Two-factor protection: off");
    });

    it("mfaTooltip explains the state (method in plain words, never TOTP)", () => {
      assert.match(Model.mfaTooltip(true, "totp"), /authenticator app/);
      assert.match(Model.mfaTooltip(true, "totp"), /Click to change or reset/);
      assert.doesNotMatch(Model.mfaTooltip(true, "totp"), /TOTP/);
      assert.match(Model.mfaTooltip(false, ""), /click to add an authenticator app/);
    });

    it("walletCopyValue reduces to hex/x, empty when none", () => {
      assert.strictEqual(
        Model.walletCopyValue("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F"),
        "0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F"
      );
      // Spaces, "…" and any non hex/x characters are stripped.
      assert.strictEqual(Model.walletCopyValue("0x3caa…bb CC"), "0x3caabbCC");
      assert.strictEqual(Model.walletCopyValue(""), "");
      assert.strictEqual(Model.walletCopyValue("!!! zzz"), "");
      assert.strictEqual(Model.walletCopyValue(undefined), "");
    });

    it("mfaRole maps on/off to ok/offline", () => {
      assert.strictEqual(Model.mfaRole(true), "ok");
      assert.strictEqual(Model.mfaRole(false), "offline");
      assert.strictEqual(Model.mfaRole(undefined), "offline");
    });

    it("copyDoneLabel returns the exact transient caption", () => {
      assert.strictEqual(Model.copyDoneLabel(), "Copied ✓");
      assert.strictEqual(Model.copyDoneLabel(), Model.copyDoneLabel());
    });

    it("openUrlCommand builds an xdg-open argv", () => {
      assert.deepStrictEqual(Model.openUrlCommand("https://x.example/y"), ["xdg-open", "https://x.example/y"]);
      assert.strictEqual(Model.openUrlCommand(""), null);
      assert.strictEqual(Model.openUrlCommand(null), null);
    });

    it("openUrlCommand rejects anything that is not http(s) (argument injection)", () => {
      // xdg-open would treat a leading "-" as an option; non-http schemes are
      // never a portal URL. Fail-closed: null means "open nothing".
      for (const bad of ["-x", "--help", "file:///etc/passwd", "javascript:alert(1)",
                         "ftp://x", "not a url", "https://", " -x"]) {
        assert.strictEqual(Model.openUrlCommand(bad), null, String(bad));
      }
      assert.deepStrictEqual(Model.openUrlCommand(Model.MFA_RESET_URL),
        ["xdg-open", Model.MFA_RESET_URL]);
    });

    it("MFA_RESET_URL is a non-empty https portal URL", () => {
      assert.strictEqual(typeof Model.MFA_RESET_URL, "string");
      assert.ok(Model.MFA_RESET_URL.startsWith("https://"));
    });

    it("parseStatus surfaces mfa_enrolled/mfa_method", () => {
      const on = Model.parseStatus(JSON.stringify({ wallet_address: "0x1", mfa_enrolled: true, mfa_method: "totp" }));
      assert.strictEqual(on.mfa_enrolled, true);
      assert.strictEqual(on.mfa_method, "totp");
      const off = Model.parseStatus(JSON.stringify({ wallet_address: "0x1" }));
      assert.strictEqual(off.mfa_enrolled, false);
      assert.strictEqual(off.mfa_method, "");
      const broken = Model.parseStatus("not json");
      assert.strictEqual(broken.mfa_enrolled, false);
    });
  });

  describe("mfaVerifyReason (24.4: verify dialog context)", () => {
    const lfe = (over = {}) => ({
      code: "mfa_required",
      amount_micro: 2000,
      can_override: false,
      target_url: "https://seller.example/book",
      timestamp: "2026-09-17T00:00:00Z",
      ...over
    });

    it("returns no lines without a last_fetch_error (bare code prompt)", () => {
      for (const bad of [null, undefined, "x", 42]) {
        assert.deepStrictEqual(Model.mfaVerifyReason(bad, 5, 10), [], String(bad));
      }
    });

    it("renders amount, seller host, cap and balance for a full context", () => {
      const lines = Model.mfaVerifyReason(lfe(), 5, 19.99);
      assert.ok(lines.some(l => l.includes("Amount: 0.002 USDC")), lines.join(" | "));
      assert.ok(lines.some(l => l.includes("Seller: seller.example")), lines.join(" | "));
      assert.ok(lines.some(l => l.includes("Daily budget: 5.00 USDC")), lines.join(" | "));
      assert.ok(lines.some(l => l.includes("Wallet balance: 19.99 USDC")), lines.join(" | "));
      assert.ok(lines.some(l => /completes this pending payment/.test(l)), lines.join(" | "));
      assert.ok(lines.some(l => /nothing is paid/.test(l)), lines.join(" | "));
    });

    it("keeps a sub-cent amount exact (never 0.00)", () => {
      const lines = Model.mfaVerifyReason(lfe({ amount_micro: 2000 }), 5, 10);
      const amountLine = lines.find(l => l.startsWith("Amount:"));
      assert.match(amountLine, /0\.002/);
      assert.ok(!amountLine.includes("0.00 USDC"), amountLine);
    });

    it("says auto-pay is off when cap is zero (no fake over-budget line)", () => {
      const lines = Model.mfaVerifyReason(lfe(), 0, 10);
      assert.ok(lines.some(l => /Auto-pay is off/.test(l)), lines.join(" | "));
      assert.ok(!lines.some(l => /Daily budget/.test(l)), lines.join(" | "));
    });

    it("omits the balance line when the balance is missing or zero", () => {
      for (const bal of [0, undefined, null, NaN, "abc", -1]) {
        const lines = Model.mfaVerifyReason(lfe(), 5, bal);
        assert.ok(!lines.some(l => /balance/i.test(l)), `bal=${bal}: ` + lines.join(" | "));
      }
    });

    it("omits the seller line when target_url is missing or unparseable", () => {
      for (const url of ["", undefined, null, "not a url", "ftp://x/y"]) {
        const lines = Model.mfaVerifyReason(lfe({ target_url: url }), 5, 10);
        assert.ok(!lines.some(l => /Seller:/.test(l)), `url=${url}: ` + lines.join(" | "));
      }
    });

    it("always explains the consequence (the code completes this payment)", () => {
      const lines = Model.mfaVerifyReason(lfe(), 5, 10);
      assert.ok(lines.some(l => /completes this pending payment/.test(l)), lines.join(" | "));
      assert.ok(lines.some(l => /nothing is paid/.test(l)), lines.join(" | "));
    });

    it("urlHost handles case, IPv6, userinfo and port", () => {
      assert.strictEqual(Model.urlHost("https://New-Seller.Example.com:8443/pay"), "new-seller.example.com");
      assert.strictEqual(Model.urlHost("https://[::1]:8080/x"), "::1");
      assert.strictEqual(Model.urlHost("https://user:pass@h.example:1/x"), "h.example");
      assert.strictEqual(Model.urlHost("not a url"), "");
      assert.strictEqual(Model.urlHost(""), "");
    });
  });

  describe("resolveStep", () => {
    it("fresh panel resolves from daemon truth", () => {
      assert.strictEqual(Model.resolveStep(-1, Model.State.LOGGED_OUT), 0);
      assert.strictEqual(Model.resolveStep(-1, Model.State.ACTIVE), 3);
    });
    it("dropped session with no wizard returns to email step", () => {
      assert.strictEqual(Model.resolveStep(3, Model.State.LOGGED_OUT), 0);
      assert.strictEqual(Model.resolveStep(2, Model.State.LOGGED_OUT), 0);
    });
    it("never interrupts in-flow OTP steps", () => {
      assert.strictEqual(Model.resolveStep(0, Model.State.LOGGED_OUT), 0);
      assert.strictEqual(Model.resolveStep(1, Model.State.LOGGED_OUT), 1);
    });
    it("keeps current step when session is alive", () => {
      assert.strictEqual(Model.resolveStep(3, Model.State.ACTIVE), 3);
      assert.strictEqual(Model.resolveStep(2, Model.State.ACTIVE), 2);
    });
  });
});

function makeRng(seed) {
  let s = seed;
  return function () {
    s = (s * 16807) % 2147483647;
    return s;
  };
}

describe("mfaGateFromError (26.1/26.2: sudo denial routing)", () => {
  it("routes mfa_not_enrolled to the enroll dialog", () => {
    const g = Model.mfaGateFromError('{"error":"mfa_not_enrolled","detail":"enroll TOTP"}');
    assert.strictEqual(g.code, "mfa_not_enrolled");
    assert.strictEqual(g.mode, "enroll");
    assert.strictEqual(g.detail, "enroll TOTP");
  });
  it("routes mfa_stale to the verify dialog", () => {
    assert.strictEqual(Model.mfaGateFromError('{"error":"mfa_stale"}').mode, "verify");
  });
  it("routes mfa_required (CDP, at signing) to the same verify dialog", () => {
    // 30.2a: the override path must finish after the code instead of failing.
    const g = Model.mfaGateFromError('{"error":"mfa_required","detail":"MFA required"}');
    assert.strictEqual(g.code, "mfa_required");
    assert.strictEqual(g.mode, "verify");
  });
  it("leaves other codes without a mode (plain error path)", () => {
    for (const raw of ['{"error":"mfa_unavailable"}', '{"error":"bad_request"}', '{"ok":true}'])
      assert.strictEqual(Model.mfaGateFromError(raw).mode, "", raw);
  });
  it("never throws on non-JSON or empty input", () => {
    for (const raw of ["daemon offline", "", null, undefined, 42])
      assert.deepStrictEqual(Model.mfaGateFromError(raw), { code: "", mode: "", detail: "" });
  });
  it("does not treat a 403 body as success (the 26.2 regression)", () => {
    // curl exits 0 on HTTP 403, so JSON.parse alone used to look like a saved cap.
    assert.notStrictEqual(Model.mfaGateFromError('{"error":"mfa_stale"}').mode, "");
  });
});

describe("mfaSudoReason (26.2: why the code is asked)", () => {
  it("names the cap change and the CDP window when enrolled", () => {
    const lines = Model.mfaSudoReason("policy", 5, 50, true);
    assert.ok(lines[0].includes("5.00") && lines[0].includes("50.00"), lines[0]);
    const all = lines.join(" ");
    assert.ok(all.includes("authenticator"));
    assert.ok(all.includes("keep running without one"), "must state the real CDP window");
  });
  it("reads as 'enable MFA first' when not enrolled", () => {
    const all = Model.mfaSudoReason("policy", 5, 50, false).join(" ");
    assert.ok(all.includes("no authenticator code"));
    assert.ok(!all.includes("keep running without one"));
  });
  it("labels an override payment instead of a cap change", () => {
    const lines = Model.mfaSudoReason("override", 0, 2, true);
    assert.ok(lines[0].includes("exceeds the daily limit"), lines[0]);
  });
  it("stays readable without valid numbers", () => {
    const lines = Model.mfaSudoReason("policy", undefined, NaN, true);
    assert.ok(lines.length >= 1);
    assert.ok(!lines.join(" ").includes("NaN"));
  });
});

describe("mfaVerifyReason balance warning (26.3)", () => {
  const lfe = (amountMicro) => ({ amount_micro: amountMicro, target_url: "https://seller.example/x" });
  const warn = (lines) => lines.filter((l) => l.startsWith("Warning:")).join(" | ");
  it("stays silent below the threshold (49%)", () => {
    const lines = Model.mfaVerifyReason(lfe(4_900_000), 100, 10);
    assert.strictEqual(warn(lines), "");
  });
  it("warns at the threshold (50%) with the share", () => {
    const lines = Model.mfaVerifyReason(lfe(5_000_000), 100, 10);
    assert.ok(warn(lines).includes("50%"), warn(lines));
    assert.ok(warn(lines).includes("10.00"), warn(lines));
  });
  it("warns at the whole balance (100%)", () => {
    const lines = Model.mfaVerifyReason(lfe(10_000_000), 100, 10);
    assert.ok(warn(lines).includes("100%"), warn(lines));
  });
  it("uses 'larger than' wording above the balance, never a >100% figure", () => {
    const lines = Model.mfaVerifyReason(lfe(5_000_000), 100, 1);
    assert.ok(warn(lines).includes("larger than"), warn(lines));
    assert.ok(!warn(lines).includes("%"), warn(lines));
  });
  it("stays silent without a usable balance", () => {
    for (const b of [0, undefined, null, NaN])
      assert.strictEqual(warn(Model.mfaVerifyReason(lfe(5_000_000), 100, b)), "", String(b));
  });
  it("keeps the warning between balance and the explanation lines", () => {
    const lines = Model.mfaVerifyReason(lfe(10_000_000), 100, 10);
    const iBalance = lines.findIndex((l) => l.startsWith("Wallet balance:"));
    const iWarn = lines.findIndex((l) => l.startsWith("Warning:"));
    const iExplain = lines.findIndex((l) => l.startsWith("Your code completes"));
    assert.ok(iBalance < iWarn && iWarn < iExplain, lines.join(" | "));
  });
});

describe("usdToMicro / microToUsd (28.2: one place for money conversion)", () => {
  it("converts whole USDC to micro-USDC", () => {
    assert.strictEqual(Model.usdToMicro(5), 5_000_000);
    assert.strictEqual(Model.usdToMicro(0), 0);
  });
  it("keeps sub-cent amounts visible (no rounding to zero)", () => {
    assert.strictEqual(Model.usdToMicro(0.000001), 1);
    assert.strictEqual(Model.usdToMicro(0.0000004), 0); // below one micro
  });
  it("accepts numeric strings from the UI", () => {
    assert.strictEqual(Model.usdToMicro("5"), 5_000_000);
  });
  it("returns 0 for non-finite input instead of NaN", () => {
    for (const v of [undefined, null, NaN, "abc", Infinity])
      assert.strictEqual(Model.usdToMicro(v), 0, String(v));
  });
  it("converts back and round-trips", () => {
    assert.strictEqual(Model.microToUsd(5_000_000), 5);
    assert.strictEqual(Model.microToUsd(1), 0.000001);
    assert.strictEqual(Model.microToUsd(undefined), 0);
    assert.strictEqual(Model.microToUsd(Model.usdToMicro(2.5)), 2.5);
  });
});

describe("mfaNagKey / shouldSurfaceMfa (30.1: no popup nagging on retry)", () => {
  // Model is created per test (beforeEach), so helpers that touch it must live
  // inside the `it` bodies — not in the describe scope.
  const denial = (over) => Object.assign({ code: "mfa_required", target_url: "https://seller.example/x", amount_micro: 2000 }, over || {});

  it("identifies a denial by code, seller and amount (not by time)", () => {
    const a = Model.mfaNagKey(denial());
    const b = Model.mfaNagKey(denial({ timestamp: "2026-09-22T09:00:00Z" }));
    assert.strictEqual(a, b, "a new timestamp is the same denial");
    assert.notStrictEqual(a, Model.mfaNagKey(denial({ amount_micro: 5000 })));
    assert.notStrictEqual(a, Model.mfaNagKey(denial({ target_url: "https://other.example/x" })));
    assert.notStrictEqual(a, Model.mfaNagKey(denial({ code: "budget_exceeded" })));
  });

  it("has no identity without a denial", () => {
    for (const bad of [null, undefined, {}, { code: "" }, "nope"]) {
      assert.strictEqual(Model.mfaNagKey(bad), "", String(bad));
      assert.strictEqual(Model.shouldSurfaceMfa("", 0, "", 1000, Model.MFA_REPROMPT_COOLDOWN_MS), false);
    }
  });

  it("surfaces a retry loop exactly once", () => {
    let surfaced = 0;
    let prevKey = "";
    let prevAt = 0;
    const key = Model.mfaNagKey(denial());
    const cooldown = Model.MFA_REPROMPT_COOLDOWN_MS;
    // agent retries once a second for 4 minutes (inside the cooldown)
    for (let t = 0; t < 240_000; t += 1000) {
      if (Model.shouldSurfaceMfa(prevKey, prevAt, key, t, cooldown)) {
        surfaced++;
        prevKey = key;
        prevAt = t;
      }
    }
    assert.strictEqual(surfaced, 1, "240 identical attempts must surface once");
  });

  it("surfaces again after the cooldown, or for a different denial", () => {
    const cooldown = Model.MFA_REPROMPT_COOLDOWN_MS;
    const key = Model.mfaNagKey(denial());
    assert.strictEqual(Model.shouldSurfaceMfa(key, 0, key, cooldown, cooldown), true, "cooldown elapsed");
    assert.strictEqual(Model.shouldSurfaceMfa(key, 0, key, cooldown - 1, cooldown), false, "still inside");
    assert.strictEqual(Model.shouldSurfaceMfa(key, 0, Model.mfaNagKey(denial({ amount_micro: 9000 })), 1, cooldown), true, "new problem");
  });
});

describe("buildCommand timeouts (30.2b)", () => {
  it("defaults to the generic socket budget", () => {
    const cmd = Model.buildCommand("/tmp/x.sock", "/status", "GET", "");
    assert.strictEqual(cmd[cmd.indexOf("-m") + 1], String(Model.CURL_TIMEOUT_S));
  });
  it("lets a waiting call ask for a longer budget", () => {
    const cmd = Model.buildCommand("/tmp/x.sock", "/fetch-override", "POST", "{}", Model.CURL_TIMEOUT_WAIT_S);
    assert.strictEqual(cmd[cmd.indexOf("-m") + 1], String(Model.CURL_TIMEOUT_WAIT_S));
    assert.ok(Model.CURL_TIMEOUT_WAIT_S > Model.CURL_TIMEOUT_S, "wait budget must exceed the generic one");
  });
});

describe("blockedText (per-payment ceiling and other silent denials)", () => {
  it("explains the Coinbase ceiling and that it cannot be approved", () => {
    const t = Model.blockedText({ code: "policy_violation", amount_micro: 90000000 });
    assert.ok(/Coinbase/.test(t) && /cannot be approved/.test(t), t);
  });
  it("stays silent for codes that already have their own dialog or popup", () => {
    for (const code of ["budget_exceeded", "unknown_seller", "price_changed", "domain_cap_exceeded", "mfa_required"])
      assert.strictEqual(Model.blockedText({ code }), "", code);
  });
  it("has nothing to say without a denial", () => {
    for (const bad of [null, undefined, {}, { code: "" }, "nope"])
      assert.strictEqual(Model.blockedText(bad), "", String(bad));
  });
  it("explains an empty wallet (33.2)", () => {
    const t = Model.blockedText({ code: "insufficient_funds" });
    assert.ok(/USDC/.test(t) && /top it up/.test(t), t);
  });
});

describe("clockSkewWarning (33.2: wrong clock breaks EIP-3009 signatures)", () => {
  it("stays silent within the tolerance window", () => {
    for (const ms of [0, 1000, -1000, 59999, -59999])
      assert.strictEqual(Model.clockSkewWarning(ms), "", String(ms));
  });
  it("warns with whole seconds once past the threshold", () => {
    assert.ok(/60 s/.test(Model.clockSkewWarning(60000)), Model.clockSkewWarning(60000));
    assert.ok(/120 s/.test(Model.clockSkewWarning(-120000)), Model.clockSkewWarning(-120000));
    assert.ok(/NTP/.test(Model.clockSkewWarning(61000)), Model.clockSkewWarning(61000));
  });
  it("stays silent when the skew is unknown or not a number", () => {
    for (const bad of [undefined, null, NaN, Infinity, "60000", {}])
      assert.strictEqual(Model.clockSkewWarning(bad), "", String(bad));
  });
  it("uses the exported threshold", () => {
    assert.strictEqual(Model.CLOCK_SKEW_WARN_MS, 60000);
    assert.strictEqual(Model.clockSkewWarning(Model.CLOCK_SKEW_WARN_MS - 1), "");
    assert.notStrictEqual(Model.clockSkewWarning(Model.CLOCK_SKEW_WARN_MS), "");
  });
});

describe("budgetFraction (40.1: hero spend/cap meter)", () => {
  it("returns 0 when nothing is spent", () => {
    assert.strictEqual(Model.budgetFraction(0, 5), 0);
  });
  it("returns the share for a normal spend", () => {
    assert.strictEqual(Model.budgetFraction(2.5, 5), 0.5);
    assert.strictEqual(Model.budgetFraction(1, 4), 0.25);
  });
  it("clamps spend above the cap to 1", () => {
    assert.strictEqual(Model.budgetFraction(6, 5), 1);
    assert.strictEqual(Model.budgetFraction(100, 5), 1);
  });
  it("clamps negative spend to 0", () => {
    assert.strictEqual(Model.budgetFraction(-1, 5), 0);
  });
  it("returns 0 when the cap is 0 or negative (auto-pay off)", () => {
    assert.strictEqual(Model.budgetFraction(3, 0), 0);
    assert.strictEqual(Model.budgetFraction(3, -5), 0);
  });
  it("is NaN-safe", () => {
    for (const [s, c] of [[NaN, 5], [3, NaN], [NaN, NaN], [undefined, 5], [3, undefined]])
      assert.strictEqual(Model.budgetFraction(s, c), 0, s + "," + c);
  });
});

describe("budgetRemaining (40.2: hero remaining pillar)", () => {
  it("returns cap minus spend for a normal day", () => {
    assert.strictEqual(Model.budgetRemaining(1, 5), 4);
    assert.strictEqual(Model.budgetRemaining(0, 5), 5);
    assert.strictEqual(Model.budgetRemaining(5, 5), 0);
  });
  it("clamps an over-cap day to 0 (never negative)", () => {
    assert.strictEqual(Model.budgetRemaining(6, 5), 0);
    assert.strictEqual(Model.budgetRemaining(100, 5), 0);
  });
  it("returns 0 when the cap is 0 or negative (auto-pay off)", () => {
    assert.strictEqual(Model.budgetRemaining(3, 0), 0);
    assert.strictEqual(Model.budgetRemaining(3, -5), 0);
  });
  it("is NaN-safe", () => {
    for (const [s, c] of [[NaN, 5], [3, NaN], [NaN, NaN], [undefined, 5], [3, undefined]])
      assert.strictEqual(Model.budgetRemaining(s, c), 0, s + "," + c);
  });
});

describe("buildInfoLabel (41.3: release stamp in the panel footer)", () => {
  it("formats version + short sha", () => {
    assert.strictEqual(
      Model.buildInfoLabel('{"version":"0.1.0","git_sha":"abcdef1234567890"}'),
      "plugin v0.1.0 (abcdef1)");
  });
  it("omits the sha when absent", () => {
    assert.strictEqual(Model.buildInfoLabel('{"version":"0.1.0"}'), "plugin v0.1.0");
  });
  it("returns empty for a missing/blank file (older installs)", () => {
    for (const bad of [undefined, null, "", "   "])
      assert.strictEqual(Model.buildInfoLabel(bad), "", String(bad));
  });
  it("returns empty for malformed JSON or a missing version (fail-safe, no crash)", () => {
    for (const bad of ["not json", "{", "[]", "null", "{}", '{"git_sha":"abc"}'])
      assert.strictEqual(Model.buildInfoLabel(bad), "", bad);
  });
});

describe("pluginVersionLabel (52.16: short SETUP stamp without sha)", () => {
  it("formats version without sha even when git_sha is present", () => {
    assert.strictEqual(
      Model.pluginVersionLabel('{"version":"0.1.20","git_sha":"abcdef1234567890"}'),
      "plugin v0.1.20");
  });
  it("formats version alone", () => {
    assert.strictEqual(Model.pluginVersionLabel('{"version":"0.1.20"}'), "plugin v0.1.20");
  });
  it("returns empty for missing/blank/malformed input", () => {
    for (const bad of [undefined, null, "", "   ", "not json", "{}", '{"git_sha":"abc"}'])
      assert.strictEqual(Model.pluginVersionLabel(bad), "", String(bad));
  });
});

describe("footerVersionLabel / history file helpers", () => {
  const M = loadModelJS();
  it("footerVersionLabel prefers live daemon version", () => {
    assert.strictEqual(M.footerVersionLabel("plugin v0.1.20", "0.1.26"), "gateway v0.1.26");
    assert.strictEqual(M.footerVersionLabel("plugin v0.1.20", ""), "plugin v0.1.20");
    assert.strictEqual(M.footerVersionLabel("", "dev"), "gateway vdev");
  });
  it("historyFilePath is under state dir", () => {
    assert.strictEqual(M.historyFilePath("/home/u"), "/home/u/.local/state/x402-gateway/history.txt");
    assert.strictEqual(M.historyFilePath(""), "");
    assert.strictEqual(M.historyFilePath("/home/../etc"), "");
  });
  it("historyDocument formats readable lines", () => {
    const doc = M.historyDocument([{
      agent: "codex", domain: "seller.example", amount_micro: 2000,
      outcome: "paid", time: "2026-10-06T12:00:00Z", override: false,
    }], Date.parse("2026-10-06T12:05:00Z"));
    assert.match(doc, /codex/);
    assert.match(doc, /seller\.example/);
    assert.match(doc, /Paid/);
  });
  it("openEditorCommand rejects traversal", () => {
    assert.deepStrictEqual(
      M.openEditorCommand("/home/u/.local/state/x402-gateway/history.txt"),
      ["omarchy", "launch", "config", "editor", "/home/u/.local/state/x402-gateway/history.txt"]);
    assert.strictEqual(M.openEditorCommand("../x"), null);
    assert.strictEqual(M.openEditorCommand(""), null);
  });
});

describe("panelDebugEnabled (43.4: version stamp fail-closed)", () => {
  it("is true only for the exact string \"1\"", () => {
    assert.strictEqual(Model.panelDebugEnabled("1"), true);
  });
  it("is false for missing/empty/other values", () => {
    for (const v of [undefined, null, "", "0", "true", "yes", "1 ", " 1", 1])
      assert.strictEqual(Model.panelDebugEnabled(v), false, String(v));
  });
});

describe("parseBlocked (49.4)", () => {
  const M = loadModelJS();
  it("returns [] for a status without blocked", () => {
    assert.deepStrictEqual(M.parseBlocked({ raw: {} }), []);
    assert.deepStrictEqual(M.parseBlocked(null), []);
  });
  it("skips entries without id or url (fail-closed)", () => {
    const rows = M.parseBlocked({ raw: { blocked: [
      { id: "b1", url: "https://a.example/x", amount_micro: 1000, reason: "mfa_required" },
      { id: "", url: "https://b.example/x" },
      { id: "b3" },
      null,
    ] } });
    assert.strictEqual(rows.length, 1);
    assert.strictEqual(rows[0].id, "b1");
    assert.strictEqual(rows[0].host, "a.example");
    assert.strictEqual(rows[0].reason, "mfa_required");
  });
  it("coerces a missing amount to 0 and defaults method", () => {
    const rows = M.parseBlocked({ raw: { blocked: [{ id: "b1", url: "https://a.example/x" }] } });
    assert.strictEqual(rows[0].amountMicro, 0);
    assert.strictEqual(rows[0].method, "GET");
  });
});

describe("permission payload (49.4)", () => {
  const M = loadModelJS();
  it("validatePermission rejects bad url and zero limit", () => {
    assert.notStrictEqual(M.validatePermission("ftp://a.example", 1), "");
    assert.notStrictEqual(M.validatePermission("https://a.example/x", 0), "");
    assert.strictEqual(M.validatePermission("https://a.example/x", 10), "");
  });
  it("permissionBody marks temporary and carries ttl only then", () => {
    const perm = JSON.parse(M.permissionBody("https://a.example/x", 10, false, 0));
    assert.strictEqual(perm.temporary, false);
    assert.strictEqual(perm.ttl_seconds, 0);
    assert.strictEqual(perm.limit_micro, 10_000_000);
    const tmp = JSON.parse(M.permissionBody("https://a.example/x", 5, true, 900));
    assert.strictEqual(tmp.temporary, true);
    assert.strictEqual(tmp.ttl_seconds, 900);
  });
  it("approveBody carries the id", () => {
    assert.deepStrictEqual(JSON.parse(M.approveBody("b7")), { id: "b7" });
  });
});

describe("parseHistory / historyRows (54.8)", () => {
  const M = loadModelJS();
  it("parseHistory rejects bad JSON", () => {
    assert.strictEqual(M.parseHistory("{").ok, false);
    assert.strictEqual(M.parseHistory("").ok, false);
  });
  it("parseHistory accepts entries + truncated", () => {
    const p = M.parseHistory(JSON.stringify({
      entries: [{ time: "2026-01-01T00:00:00Z", amount_micro: 1500, domain: "a.example", outcome: "paid", override: false, agent: "" }],
      truncated: true
    }));
    assert.strictEqual(p.ok, true);
    assert.strictEqual(p.truncated, true);
    assert.strictEqual(p.entries.length, 1);
  });
  it("historyRows maps empty agent and failed outcome", () => {
    const now = Date.parse("2026-01-01T00:10:00Z");
    const rows = M.historyRows([{
      time: "2026-01-01T00:05:00Z", amount_micro: 1000, domain: "a.example",
      outcome: "failed:budget_exceeded", override: true, agent: ""
    }], now);
    assert.strictEqual(rows.length, 1);
    assert.strictEqual(rows[0].agent, "—");
    assert.strictEqual(rows[0].outcome, "Budget exceeded");
    assert.strictEqual(rows[0].when, "5 min ago");
    assert.strictEqual(rows[0].override, true);
    assert.strictEqual(rows[0].amount, "0.001");
  });
  it("buildCommand HISTORY is GET without body", () => {
    const cmd = M.buildCommand("gw.sock", M.Endpoint.HISTORY + "?limit=20", "GET", "");
    assert.ok(cmd.includes("http://localhost/history?limit=20"));
    assert.ok(!cmd.includes("--data-binary"));
  });
});

describe("agentSpendRows / agentCapsBody / errorLabel (55.7)", () => {
  const M = loadModelJS();
  it("agentSpendRows matches label to agent name", () => {
    const status = { raw: { agents: [
      { label: "codex", spent_today_micro: 1_500_000, cap_micro: 2_000_000 },
      { label: "claude", spent_today_micro: 0, cap_micro: 0 },
    ] } };
    const agents = [{ name: "codex", integrated: true }, { name: "claude", integrated: false, connectable: true }];
    const rows = M.agentSpendRows(status, agents);
    assert.strictEqual(rows.length, 2);
    assert.strictEqual(rows[0].name, "codex");
    assert.strictEqual(rows[0].spentText, "1.50");
    assert.strictEqual(rows[0].limitText, "2.00");
    assert.strictEqual(rows[1].limitText, "asks every time");
  });
  it("agentLimitCaption: default 0 is no limit", () => {
    const status = { raw: { agents: [{ label: "a", spent_today_micro: 0, cap_micro: 0 }] } };
    const rows = M.agentSpendRows(status, [{ name: "a" }]);
    assert.strictEqual(rows[0].limitText, "no limit");
  });
  it("agentSpendRows appends read-only unlabeled for empty label", () => {
    const status = { raw: { agents: [
      { label: "codex", spent_today_micro: 0, cap_micro: 1_000_000 },
      { label: "", spent_today_micro: 500_000, cap_micro: 0 },
    ] } };
    const rows = M.agentSpendRows(status, [{ name: "codex" }]);
    assert.strictEqual(rows.length, 2);
    assert.strictEqual(rows[1].name, "unlabeled");
    assert.strictEqual(rows[1].readOnly, true);
    assert.strictEqual(rows[1].spentText, "0.50");
    assert.strictEqual(rows[1].connectable, false);
  });
  it("agentGroups splits connected / available / unlabeled", () => {
    const status = { raw: { agents: [
      { label: "codex", spent_today_micro: 0, cap_micro: 0 },
      { label: "", spent_today_micro: 1, cap_micro: 0 },
    ] } };
    const agents = [
      { name: "codex", integrated: true },
      { name: "cursor", integrated: false, connectable: true },
    ];
    const g = M.agentGroups(status, agents);
    assert.deepStrictEqual(g.connected.map((r) => r.name), ["codex"]);
    assert.deepStrictEqual(g.available.map((r) => r.name), ["cursor"]);
    assert.deepStrictEqual(g.unlabeled.map((r) => r.name), ["unlabeled"]);
  });
  it("capList shows first N and the hidden count", () => {
    const list = [1, 2, 3, 4, 5];
    assert.deepStrictEqual(M.capList(list, 2), { shown: [1, 2], hidden: 3 });
    assert.deepStrictEqual(M.capList(list, 5), { shown: [1, 2, 3, 4, 5], hidden: 0 });
    assert.deepStrictEqual(M.capList(list, 9), { shown: [1, 2, 3, 4, 5], hidden: 0 });
    assert.deepStrictEqual(M.capList([], 3), { shown: [], hidden: 0 });
    assert.strictEqual(M.DEFAULT_AVAILABLE_LIMIT, 8);
  });
  it("agentCapsBody builds full map in micro", () => {
    const built = M.agentCapsBody({ other: 1000 }, "codex", 2.5);
    assert.strictEqual(built.error, "");
    const o = JSON.parse(built.body);
    assert.strictEqual(o.agent_caps_micro_usdc.codex, 2_500_000);
    assert.strictEqual(o.agent_caps_micro_usdc.other, 1000);
  });
  it("agentCapsBody rejects bad label and negative", () => {
    assert.notStrictEqual(M.agentCapsBody({}, "Bad", 1).error, "");
    assert.notStrictEqual(M.agentCapsBody({}, "codex", -1).error, "");
  });
  it("errorLabel names agent_cap_exceeded", () => {
    assert.match(M.errorLabel("agent_cap_exceeded"), /agent/i);
  });
});
