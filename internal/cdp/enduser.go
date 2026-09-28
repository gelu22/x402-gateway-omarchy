package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// EvmAccount is an end user's EVM account (address + timestamps).
type EvmAccount struct {
	Address   string `json:"address"`
	CreatedAt string `json:"createdAt"`
}

// CreateEvmAccount provisions an EVM account for the end user. Requires
// X-Wallet-Auth signed with the user's TWS private key; the body carries the
// matching walletSecretId. Returns the newly created account address.
func (c *Client) CreateEvmAccount(ctx context.Context, userID, accessToken string, ws *WalletSecret) (*EvmAccount, error) {
	path := "/v2/embedded-wallet-api/end-users/" + userID + "/evm"
	payload, err := json.Marshal(map[string]string{"walletSecretId": ws.ID})
	if err != nil {
		return nil, err
	}
	jwt, err := ws.XWalletAuth(http.MethodPost, APIHost, "/platform"+path, payload)
	if err != nil {
		return nil, err
	}
	raw, _, err := c.do(ctx, c.HTTP, http.MethodPost, path, json.RawMessage(payload), map[string]string{
		"Authorization": "Bearer " + accessToken,
		"X-Wallet-Auth": jwt,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		EvmAccountObjects []EvmAccount `json:"evmAccountObjects"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cdp: evm account decode: %w (%s)", err, truncate(raw))
	}
	if len(out.EvmAccountObjects) == 0 {
		return nil, fmt.Errorf("cdp: evm account: none in response (%s)", truncate(raw))
	}
	return &out.EvmAccountObjects[len(out.EvmAccountObjects)-1], nil
}

// GetEndUser returns the end user record; primary use in the spike is
// discovering the EVM account address that must sign EIP-3009 authorizations.
func (c *Client) GetEndUser(ctx context.Context, userID, accessToken string) ([]EvmAccount, error) {
	raw, _, err := c.do(ctx, c.HTTP, http.MethodGet,
		"/v2/embedded-wallet-api/end-users/"+userID, nil,
		map[string]string{"Authorization": "Bearer " + accessToken})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		EvmAccountObjects []EvmAccount `json:"evmAccountObjects"`
		EvmAccounts       []string     `json:"evmAccounts"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("cdp: end user decode: %w (%s)", err, truncate(raw))
	}
	if len(parsed.EvmAccountObjects) > 0 {
		return parsed.EvmAccountObjects, nil
	}
	out := make([]EvmAccount, 0, len(parsed.EvmAccounts))
	for _, a := range parsed.EvmAccounts {
		out = append(out, EvmAccount{Address: a})
	}
	return out, nil
}
