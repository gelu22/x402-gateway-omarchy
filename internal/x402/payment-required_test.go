package x402

import (
	"encoding/base64"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestParsePaymentRequiredAcceptsOnlyV2(t *testing.T) {
	accepts := `"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0x19c1d70Df1F5179CfD015A88Acc7371E203B092C","maxTimeoutSeconds":60}]`
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	if _, err := ParsePaymentRequired(b64(`{"x402Version":2,` + accepts + `}`)); err != nil {
		t.Fatalf("v2: %v", err)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{"v1", `{"x402Version":1,` + accepts + `}`},
		{"v3", `{"x402Version":3,` + accepts + `}`},
		{"missing", `{` + accepts + `}`},
	} {
		if _, err := ParsePaymentRequired(b64(tc.body)); err == nil {
			t.Fatalf("%s: want error", tc.name)
		}
	}
}

// exactReq builds minimal valid requirements with the given timeout.
func exactReq(timeout int) *PaymentRequired {
	return &PaymentRequired{
		X402Version: 2,
		Accepts: []PaymentRequirements{{
			Scheme:            "exact",
			Network:           "eip155:84532",
			Asset:             "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
			Amount:            "100",
			PayTo:             "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C",
			MaxTimeoutSeconds: timeout,
		}},
	}
}

func TestSelectRequirementsClampsTimeout(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int
	}{
		{60, 60},
		{300, 300},
		{301, 300},
		{600, 300},
		{math.MaxInt, 300},
	} {
		req, err := exactReq(tc.in).SelectRequirements()
		if err != nil {
			t.Fatalf("timeout %d: %v", tc.in, err)
		}
		if req.MaxTimeoutSeconds != tc.want {
			t.Fatalf("timeout %d: got %d, want %d", tc.in, req.MaxTimeoutSeconds, tc.want)
		}
	}
}

// 36.6 (A-P3-3): the USDC/network pin lives in code (chains.USDCContract),
// not in any policy file — a permissive config must not widen it. Negative
// assertion: NOTHING is selected (not just "the first accept failed").
func TestSelectRequirementsRejectsUnpinnedChainAndAsset(t *testing.T) {
	unknown := exactReq(60)
	unknown.Accepts[0].Network = "eip155:137" // not pinned, whatever policy.json says
	unknown.Accepts[0].Asset = "0x0000000000000000000000000000000000000001"
	if req, err := unknown.SelectRequirements(); err == nil || req != nil {
		t.Fatalf("unpinned chain: req=%+v err=%v, want rejection with no requirements", req, err)
	}
	scam := exactReq(60)
	scam.Accepts[0].Asset = "0x0000000000000000000000000000000000000001" // not the pinned USDC
	if req, err := scam.SelectRequirements(); err == nil || req != nil {
		t.Fatalf("scam asset on a pinned chain: req=%+v err=%v, want rejection with no requirements", req, err)
	}
}

func TestSelectRequirementsRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []int{0, -1} {
		if _, err := exactReq(timeout).SelectRequirements(); err == nil {
			t.Fatalf("timeout %d: want error", timeout)
		}
	}
}

func TestBuildAuthorizationClampsValidity(t *testing.T) {
	before := time.Now().Unix()
	auth, err := BuildAuthorization(
		"0xe6D2863Eb960a980eC3714f85568f974d03cE04E",
		&PaymentRequirements{
			PayTo:             "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C",
			Amount:            "100",
			MaxTimeoutSeconds: math.MaxInt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := strconv.ParseInt(auth.ValidBefore, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if vb > before+MaxAuthorizationTTLSeconds {
		t.Fatalf("ValidBefore %d exceeds now+300", vb)
	}
	if vb <= before {
		t.Fatalf("ValidBefore %d not in the future (overflow?)", vb)
	}
}

func TestBuildAuthorizationRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []int{0, -1} {
		_, err := BuildAuthorization(
			"0xe6D2863Eb960a980eC3714f85568f974d03cE04E",
			&PaymentRequirements{
				PayTo:             "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C",
				Amount:            "100",
				MaxTimeoutSeconds: timeout,
			},
		)
		if err == nil {
			t.Fatalf("timeout %d: want error", timeout)
		}
	}
}
