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
// request with the Payment-Signature header.
func (g *Gateway) signAndRetry(ctx context.Context, method, target string, body []byte, headers map[string]string, pr *x402.PaymentRequired, req *x402.PaymentRequirements, overrideAmountMicro int64, key string, amountMicro int64, amountErr error) (*FetchResult, error) {
	if amountErr != nil {
		// No signature without an accounted amount: paying and skipping the
		// ledger entry would be silent money. Unreachable today (Check and
		// CheckOverride reject non-canonical amounts first), pinned anyway.
		err := fmt.Errorf("%w: %v", ErrSigner, amountErr)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	auth, err := x402.BuildAuthorization(g.Signer.Address(), req)
	if err != nil {
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	ws, err := g.Signer.WalletSecret()
	if err != nil {
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	token, err := g.Signer.AccessToken()
	if err != nil {
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	sig, err := x402.SignAuthorizationViaCDP(ctx, g.Client, ws, g.Signer.UserID(), token,
		g.Signer.Address(), chains.ChainID(req.Network), req, auth)
	if err != nil {
		if cdp.IsMFARequired(err) {
			g.setLastFetchError("mfa_required", amountMicro, false, target, err.Error())
			return nil, &PolicyError{
				Code:        "mfa_required",
				AmountMicro: amountMicro,
				CanOverride: false,
			}
		}
		if cdp.IsPolicyViolation(err) {
			// The CDP Policy Engine refused to sign: an explicit ceiling, not a
			// malfunction, and nothing to override — the user cannot approve
			// their way past a TEE rule.
			g.setLastFetchError("policy_violation", amountMicro, false, target, err.Error())
			return nil, &PolicyError{
				Code:        "policy_violation",
				AmountMicro: amountMicro,
				CanOverride: false,
			}
		}
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}
	var extensions map[string]any
	if code := g.CurrentPolicy().BuilderCode; code != "" {
		ext, berr := x402.BuildBuilderExtension([]string{code}, x402.ServerAppCode(pr.Extensions))
		if berr != nil {
			err := fmt.Errorf("builder extension: %w", berr)
			g.setLastFetchError("signer_error", 0, false, target, err.Error())
			return nil, err
		}
		extensions = map[string]any{x402.BuilderCodeExtensionKey: ext}
	}
	headerValue, err := x402.EncodePaymentSignatureHeader(pr.Resource, req, auth, sig, extensions)
	if err != nil {
		err := fmt.Errorf("%w: %v", ErrSigner, err)
		g.setLastFetchError("signer_error", 0, false, target, err.Error())
		return nil, err
	}

	// Dedup stays pre-retry (T4 anti-hammer)
	g.markSigned(key)
	paid, err := g.doRequestWithHeader(ctx, method, target, body, headers, "Payment-Signature", headerValue)
	if err != nil {
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer paid.Body.Close()
	if paid.StatusCode == http.StatusPaymentRequired {
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
		if amountErr == nil && g.OnPayment != nil {
			domain := ""
			if u, perr2 := url.Parse(target); perr2 == nil {
				domain = u.Hostname()
			}
			g.OnPayment(amountMicro, domain)
		}
		res, rerr := toResult(paid)
		if rerr != nil {
			// The seller settled but sent more than we can hold: the money left
			// the wallet (spend stays recorded above), so fail loudly instead of
			// delivering truncated content.
			g.setLastFetchError("content_too_large", amountMicro, false, target, rerr.Error())
			return nil, fmt.Errorf("%w: %v", ErrContentTooLarge, rerr)
		}
		LogPayment(g.Logger, amountMicro, target, "paid", overrideAmountMicro > 0)
		return res, nil
	}
	g.setLastFetchError("upstream_error", 0, false, target, fmt.Sprintf("seller status %d after signature", paid.StatusCode))
	return nil, fmt.Errorf("%w: seller status %d after signature", ErrUpstream, paid.StatusCode)
}
