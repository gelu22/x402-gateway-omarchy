package gateway

import (
	"strconv"
	"strings"
)

// Status is the /status response (CONTRACTS §1).
type Status struct {
	Version           string          `json:"version"`
	State             string          `json:"state,omitempty"` // logged_out | active (from session)
	WalletAddress     string          `json:"wallet_address"`
	Network           string          `json:"network"`
	SpendTodayUSDC    float64         `json:"spend_today_usdc"`
	BudgetDailyUSDC   float64         `json:"budget_daily_usdc"`
	Paused            bool            `json:"paused"`
	SignerOK          bool            `json:"signer_ok"`
	MfaEnrolled       bool            `json:"mfa_enrolled"`
	MfaMethod         string          `json:"mfa_method,omitempty"`
	ClockSkewMS       int64           `json:"clock_skew_ms"`
	WalletBalanceUSDC float64         `json:"wallet_balance_usdc"`
	PaymentNetwork    string          `json:"payment_network,omitempty"`
	LastBlock         *BlockRecord    `json:"last_block,omitempty"`
	LastFetchError    *FetchErrorInfo `json:"last_fetch_error,omitempty"`
}

// FetchErrorInfo carries details of the last fetch error for UI consumption.
type FetchErrorInfo struct {
	Code        string  `json:"code"`
	AmountMicro int64   `json:"amount_micro,omitempty"`
	AmountUSDC  float64 `json:"amount_usdc,omitempty"`
	CanOverride bool    `json:"can_override"`
	TargetURL   string  `json:"target_url,omitempty"`
	Timestamp   string  `json:"timestamp"` // RFC3339Nano
}

// Status assembles the current snapshot from policy, spend and signer.
func (g *Gateway) Status(version string) (*Status, error) {
	var spend int64
	var err error
	if g.Budget != nil {
		spend, err = g.Budget.Today()
	} else if g.Spend != nil {
		spend, err = g.Spend.Today()
	}
	if err != nil {
		return nil, err
	}
	pol := g.CurrentPolicy()
	st := &Status{
		Version:         version,
		WalletAddress:   g.Signer.Address(),
		Network:         strings.Join(pol.AllowedNetworks, ","),
		SpendTodayUSDC:  microToUSDC(spend),
		BudgetDailyUSDC: microToUSDC(pol.DailyCapMicro),
		Paused:          g.Paused.Load(),
		SignerOK:        true,
	}
	if g.SessionState != nil {
		st.State = g.SessionState()
	}
	if g.MFAState != nil {
		enrolled, method := g.MFAState()
		st.MfaEnrolled = enrolled
		st.MfaMethod = method
	}
	if _, err := g.Signer.AccessToken(); err != nil {
		st.SignerOK = false
	}
	if g.Blocks != nil {
		st.LastBlock = g.Blocks.Current()
	}
	if g.Balance != nil {
		// 0 means "unknown", not "empty": the panel must not render a failed
		// RPC as an empty wallet, and no payment decision reads this field
		// (the pre-sign gate in fetch.go has its own advisory comment).
		if v, berr := g.Balance.Fetch(g.Signer.Address()); berr == nil {
			st.WalletBalanceUSDC = v
		}
	}
	if g.PaymentNetwork != "" {
		st.PaymentNetwork = g.PaymentNetwork
	}
	// Clock skew comes from the CDP response Date header (cdp/clock.go); 0 means
	// "no fresh observation" and the panel stays silent (THREAT-MODEL T6).
	if g.Client != nil {
		st.ClockSkewMS = g.Client.ClockSkewMS()
	}
	if e := g.lastError(); e != nil {
		st.LastFetchError = e
	}
	return st, nil
}

func parseAmountMicro(s string) (int64, error) {
	// Strict canonical decimal (same as the money path in gateway.go and
	// policy.go): Sscanf %d silently truncated "1e9"->1, "0x1000"->0
	// (016.6a). Anything ParseInt rejects is a hard error, never a guess.
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}
