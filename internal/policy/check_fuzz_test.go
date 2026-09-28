package policy

import (
	"gateway/internal/x402"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

// FuzzPolicyCheck validates the Check money-gate invariants (THREAT-MODEL T1:
// budget before sign) using a minimal Default() policy. No disk access.
func FuzzPolicyCheck(f *testing.F) {
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "1000000", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "5000000", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "5000001", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "0", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "-1", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "9223372036854775807", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "1e9", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("upto", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "1000000", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:1", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "1000000", "0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6")
	f.Add("exact", "eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e", "1000000", "bad")

	f.Fuzz(func(t *testing.T, scheme, network, asset, amountStr, payTo string) {
		req := &x402.PaymentRequirements{
			Scheme:  scheme,
			Network: network,
			Asset:   asset,
			Amount:  amountStr,
			PayTo:   payTo,
		}
		p := Default()

		// Negative spend must deny, never loosen.
		if err := p.Check(req, -1); err == nil {
			t.Fatal("Check with negative spend = nil, want error")
		}

		amountInt, parseErr := strconv.ParseInt(strings.TrimSpace(amountStr), 10, 64)
		amountBig, bigOk := new(big.Int).SetString(strings.TrimSpace(amountStr), 10)

		if parseErr != nil || (bigOk && amountBig.Sign() <= 0) || amountInt <= 0 {
			if err := p.Check(req, 0); err == nil {
				t.Fatalf("Check with amount %q = nil, want invalid_amount", amountStr)
			}
			return
		}
		if scheme != "exact" {
			if err := p.Check(req, 0); err == nil {
				t.Fatalf("Check with scheme %q = nil, want network_denied", scheme)
			}
			return
		}
		if !p.networkAllowed(network) {
			if err := p.Check(req, 0); err == nil {
				t.Fatalf("Check with network %q = nil, want network_denied", network)
			}
			return
		}
		pinned, ok := p.PinnedAssets[network]
		if !ok || !strings.EqualFold(asset, pinned) {
			if err := p.Check(req, 0); err == nil {
				t.Fatal("Check with unpinned asset = nil, want network_denied")
			}
			return
		}
		if len(payTo) != 42 || payTo[:2] != "0x" {
			if err := p.Check(req, 0); err == nil {
				t.Fatalf("Check with payTo %q = nil, want network_denied", payTo)
			}
			return
		}
		for _, c := range payTo[2:] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				if err := p.Check(req, 0); err == nil {
					t.Fatalf("Check with non-hex payTo %q = nil, want network_denied", payTo)
				}
				return
			}
		}

		capDefault := big.NewInt(DefaultDailyCapMicro)
		if bigOk && amountBig.Cmp(capDefault) > 0 {
			if err := p.Check(req, 0); err == nil {
				t.Fatalf("Check with amount %q above cap = nil, want budget_exceeded", amountStr)
			}
			return
		}
		if err := p.Check(req, 0); err != nil {
			t.Fatalf("Check with valid req = %v, want nil", err)
		}
	})
}
