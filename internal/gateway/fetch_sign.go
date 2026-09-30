// fetch_sign.go — signing, retry with signature, and result handling.
package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"gateway/internal/cdp"
	"gateway/internal/chains"
	"gateway/internal/x402"
)

// signAndRetry signs the payment with the user's TWS key and retries the
// request with the Payment-Signature header. budgetToken is the Authorize
// reservation: Commit on 2xx settle, Release on any failure before settle.
// content_too_large after settle does NOT Release (money already left).
func (g *Gateway) signAndRetry(ctx context.Context, method, target string, body []byte, headers map[string]string, pr *x402.PaymentRequired, req *x402.PaymentRequirements, overrideAmountMicro int64, key string, amountMicro int64, amountErr error, budgetToken string) (*FetchResult, error) {
	release := func() {
		if budgetToken == "" || g.Budget == nil {
			return
		}
		if rerr := g.Budget.Release(budgetToken); rerr != nil && g.Logger != nil {
			g.Logger.Error("budget release", "err", rerr)
		}
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

	g.markSigned(key)
	paid, err := g.doRequestWithHeader(ctx, method, target, body, headers, "Payment-Signature", headerValue)
	if err != nil {
		release()
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer paid.Body.Close()
	if paid.StatusCode == http.StatusPaymentRequired {
		release()
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
		// Commit before OnPayment: telemetry must not depend on an uncommitted charge.
		if budgetToken != "" && g.Budget != nil {
			if cerr := g.Budget.Commit(budgetToken); cerr != nil && g.Logger != nil {
				g.Logger.Error("budget commit", "err", cerr)
			}
		}
		if g.OnPayment != nil {
			domain := ""
			if u, perr2 := url.Parse(target); perr2 == nil {
				domain = u.Hostname()
			}
			g.OnPayment(amountMicro, domain)
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
	release()
	g.setLastFetchError("upstream_error", 0, false, target, fmt.Sprintf("seller status %d after signature", paid.StatusCode))
	return nil, fmt.Errorf("%w: seller status %d after signature", ErrUpstream, paid.StatusCode)
}
