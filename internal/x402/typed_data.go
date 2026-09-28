// EIP-712 typed data: buildTypedData, typedData, isHexAddress, checksumOrLower.
package x402

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// typedData is the EIP-712 envelope sent to the CDP sign endpoint.
type typedData struct {
	Domain      map[string]any `json:"domain"`
	Types       map[string]any `json:"types"`
	PrimaryType string         `json:"primaryType"`
	Message     map[string]any `json:"message"`
}

func buildTypedData(chainID int64, req *PaymentRequirements, auth *Authorization) (*typedData, error) {
	name, _ := req.Extra["name"].(string)
	version, _ := req.Extra["version"].(string)
	if name == "" || version == "" {
		return nil, fmt.Errorf("x402: requirements.extra.name/version missing (EIP-712 domain)")
	}
	bigValue, ok := new(big.Int).SetString(auth.Value, 10)
	if !ok {
		return nil, fmt.Errorf("x402: amount %q is not decimal", auth.Value)
	}
	va, okA := new(big.Int).SetString(auth.ValidAfter, 10)
	vb, okB := new(big.Int).SetString(auth.ValidBefore, 10)
	if !okA || !okB {
		return nil, fmt.Errorf("x402: validity windows are not decimal")
	}
	return &typedData{
		Domain: map[string]any{
			"name":              name,
			"version":           version,
			"chainId":           chainID,
			"verifyingContract": checksumOrLower(req.Asset),
		},
		Types: map[string]any{
			// CDP requires the EIP712Domain struct to be spelled out.
			"EIP712Domain": []map[string]string{
				{"name": "name", "type": "string"},
				{"name": "version", "type": "string"},
				{"name": "chainId", "type": "uint256"},
				{"name": "verifyingContract", "type": "address"},
			},
			"TransferWithAuthorization": []map[string]string{
				{"name": "from", "type": "address"},
				{"name": "to", "type": "address"},
				{"name": "value", "type": "uint256"},
				{"name": "validAfter", "type": "uint256"},
				{"name": "validBefore", "type": "uint256"},
				{"name": "nonce", "type": "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Message: map[string]any{
			"from":        auth.From,
			"to":          auth.To,
			"value":       bigValue.String(),
			"validAfter":  va.String(),
			"validBefore": vb.String(),
			"nonce":       auth.Nonce,
		},
	}, nil
}

func isHexAddress(s string) bool {
	if !strings.HasPrefix(s, "0x") || len(s) != 42 {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

func checksumOrLower(addr string) string { return strings.ToLower(addr) }
