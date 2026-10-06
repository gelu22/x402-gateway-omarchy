// First-request + 402 parsing: parse402Response, doRequest, doRequestWithHeader.
package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gateway/internal/x402"
)

func (g *Gateway) parse402Response(ctx context.Context, method, target string, body []byte, headers map[string]string, key, agent string) (*http.Response, *x402.PaymentRequired, *x402.PaymentRequirements, error) {
	first, err := g.doRequest(ctx, method, target, body, headers)
	if err != nil {
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if first.StatusCode != http.StatusPaymentRequired {
		return first, nil, nil, nil
	}

	prHeader := first.Header.Get("Payment-Required")
	_ = first.Body.Close()
	if prHeader == "" {
		err := fmt.Errorf("%w: 402 without PAYMENT-REQUIRED", ErrNoRequirements)
		g.setLastFetchError("no_requirements", 0, false, target, err.Error())
		return nil, nil, nil, err
	}
	pr, err := x402.ParsePaymentRequired(prHeader)
	if err != nil {
		err := fmt.Errorf("%w: %v", ErrNoRequirements, err)
		g.setLastFetchError("no_requirements", 0, false, target, err.Error())
		return nil, nil, nil, err
	}
	req, err := pr.SelectRequirements()
	if err != nil {
		err := &PolicyError{Code: "network_denied"}
		g.setLastFetchError("network_denied", 0, false, target, err.Error())
		LogPayment(g.Logger, PaymentLine{
			AmountMicro: 0, Target: target,
			Outcome: policyOutcome("network_denied"), Agent: agent,
		})
		return nil, nil, nil, err
	}
	return first, pr, req, nil
}

func (g *Gateway) doRequest(ctx context.Context, method, target string, body []byte, headers map[string]string) (*http.Response, error) {
	return g.doRequestWithHeader(ctx, method, target, body, headers, "", "")
}

func (g *Gateway) doRequestWithHeader(ctx context.Context, method, target string, body []byte, headers map[string]string, hKey, hVal string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if hKey != "" {
		req.Header.Set(hKey, hVal)
	}
	client := g.HTTP
	if client == nil {
		client = &http.Client{Timeout: FetchTimeout}
	}
	return client.Do(req)
}
