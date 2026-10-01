// fetch_sign.go — signing, retry with signature, and result handling.
package gateway

import (
	"context"
	"fmt"
	"net/http"

	"gateway/internal/cdp"
	"gateway/internal/chains"
	"gateway/internal/x402"
)

// signAndRetry signs the payment with the user's TWS key and retries the
// request with the Payment-Signature header. budgetToken is the Authorize
// reservation: Release only on failures BEFORE the header is sent; once
// Payment-Signature leaves the process, every outcome (2xx, non-2xx, 402,
// transport error, content_too_large) Commits — the seller may already have
// redeemed (HANCORE a / 44.3). MarkSigned persists before send so a crash
// before Commit still promotes via TTL (HANCORE b / 44.4).
// Domain spend is no longer written here: the Commit moves the per-domain total
// inside the budget authority, which is the only place the per-domain cap is
// read from (47.1). OnPayment stays 2xx-only (telemetry / balance invalidate).
func (g *Gateway) signAndRetry(ctx context.Context, method, target string, body []byte, headers map[string]string, pr *x402.PaymentRequired, req *x402.PaymentRequirements, overrideAmountMicro int64, key string, amountMicro int64, amountErr error, budgetToken string) (*FetchResult, error) {
	release := func() {
		if budgetToken == "" || g.Budget == nil {
			return
		}
		if rerr := g.Budget.Release(budgetToken); rerr != nil && g.Logger != nil {
			g.Logger.Error("budget release", "err", rerr)
		}
	}
	commit := func() {
		if budgetToken == "" || g.Budget == nil {
			return
		}
		if cerr := g.Budget.Commit(budgetToken); cerr != nil && g.Logger != nil {
			g.Logger.Error("budget commit", "err", cerr)
		}
	}
	// commitPostSig: daily Commit, which also moves the per-domain total inside
	// the same transaction (47.1). Never Release.
	commitPostSig := func() {
		commit()
	}
	if amountErr != nil {
		release()
		err := fmt.Errorf("%w: %v", ErrSigner, amountErr)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	auth, err := x402.BuildAuthorization(g.Signer.Address(), req)
	if err != nil {
		release()
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	ws, err := g.Signer.WalletSecret()
	if err != nil {
		release()
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	token, err := g.Signer.AccessToken()
	if err != nil {
		release()
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	sig, err := x402.SignAuthorizationViaCDP(ctx, g.Client, ws, g.Signer.UserID(), token,
		g.Signer.Address(), chains.ChainID(req.Network), req, auth)
	if err != nil {
		release()
		if cdp.IsMFARequired(err) {
			g.setLastFetchError("mfa_required", amountMicro, false, target, err.Error())
			return nil, &PolicyError{Code: "mfa_required", AmountMicro: amountMicro, CanOverride: false}
		}
		if cdp.IsPolicyViolation(err) {
			g.setLastFetchError("policy_violation", amountMicro, false, target, err.Error())
			return nil, &PolicyError{Code: "policy_violation", AmountMicro: amountMicro, CanOverride: false}
		}
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	var extensions map[string]any
	if code := g.CurrentPolicy().BuilderCode; code != "" {
		ext, berr := x402.BuildBuilderExtension([]string{code}, x402.ServerAppCode(pr.Extensions))
		if berr != nil {
			release()
			err := fmt.Errorf("builder extension: %w", berr)
			g.setLastFetchError("signer_error", 0, false, target, err.Error())
			return nil, err
		}
		extensions = map[string]any{x402.BuilderCodeExtensionKey: ext}
	}
	headerValue, err := x402.EncodePaymentSignatureHeader(pr.Resource, req, auth, sig, extensions)
	if err != nil {
		release()
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}

	// Durable "sig is leaving" before HTTP (HANCORE b / 44.4). Fail-closed:
	// if MarkSigned cannot persist, do not send Payment-Signature — Release.
	if budgetToken != "" && g.Budget != nil {
		if merr := g.Budget.MarkSigned(budgetToken); merr != nil {
			release()
			err := fmt.Errorf("%w: budget mark signed: %v", ErrUpstream, merr)
			g.setLastFetchError("upstream_error", amountMicro, false, target, err.Error())
			return nil, err
		}
	}

	g.markSigned(key)
	paid, err := g.doRequestWithHeader(ctx, method, target, body, headers, "Payment-Signature", headerValue)
	if err != nil {
		commitPostSig() // header sent — never Release (seller may have redeemed)
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer paid.Body.Close()
	if paid.StatusCode == http.StatusPaymentRequired {
		commitPostSig() // header sent — never Release
		g.setLastFetchError("upstream_error", 0, false, target, "payment rejected after signature")
		return nil, fmt.Errorf("%w: payment rejected after signature", ErrUpstream)
	}
	g.mu.Lock()
	g.lastFetchError = nil
	g.mu.Unlock()

	if paid.StatusCode >= 200 && paid.StatusCode < 300 {
		if g.Blocks != nil {
			g.Blocks.Clear()
		}
		// Commit + domain charge before OnPayment (telemetry must not own the ledger).
		commitPostSig()
		if g.OnPayment != nil {
			g.OnPayment(amountMicro, normSellerDomain(target))
		}
		res, rerr := toResult(paid)
		if rerr != nil {
			// Settled but oversized: money left — no Release.
			g.setLastFetchError("content_too_large", amountMicro, false, target, rerr.Error())
			return nil, fmt.Errorf("%w: %v", ErrContentTooLarge, rerr)
		}
		LogPayment(g.Logger, amountMicro, target, "paid", overrideAmountMicro > 0)
		return res, nil
	}
	commitPostSig() // header sent — never Release on non-2xx
	g.setLastFetchError("upstream_error", 0, false, target, fmt.Sprintf("seller status %d after signature", paid.StatusCode))
	return nil, fmt.Errorf("%w: seller status %d after signature", ErrUpstream, paid.StatusCode)
}

// chargeDomain records per-seller day spend after a post-sig Commit so the
// 20% domain sub-cap cannot be bypassed by redeem-then-non-2xx (NEW-P1-3).
