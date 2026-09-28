package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"gateway/internal/chains"
)

// USDC balance via public RPC eth_call balanceOf (read-only, no keys).
// Cached 60 s so bar polling never spams the RPC.
type BalanceFetcher struct {
	RPCURL  string
	Network string // CAIP-2
	HTTP    *http.Client

	now func() time.Time // injectable clock (tests)

	mu       sync.Mutex
	address  string
	cached   float64
	cachedAt time.Time
}

// SetAddress updates the queried wallet address (invalidates cache).
func (b *BalanceFetcher) SetAddress(addr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.address != addr {
		b.address = addr
		b.cachedAt = time.Time{}
	}
}

// Invalidate clears the cached balance (forces fresh fetch on next call).
func (b *BalanceFetcher) Invalidate() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cachedAt = time.Time{}
}

const balanceTTL = 60 * time.Second

// NewBalanceFetcher creates a fetcher for the given CAIP-2 network.
// RPC URL is derived from the network (can be overridden via RPCURL field for tests).
func NewBalanceFetcher(network string) *BalanceFetcher {
	info, ok := chains.ByCAIP2(network)
	if !ok {
		info = chains.MustByCAIP2("eip155:84532") // fallback to Sepolia
	}
	return &BalanceFetcher{RPCURL: info.RPCURL, Network: network,
		HTTP: &http.Client{Timeout: 8 * time.Second}}
}

// Fetch returns cached USDC balance (human units) for the given address.
func (b *BalanceFetcher) Fetch(address string) (float64, error) {
	b.SetAddress(address)
	b.mu.Lock()
	if b.now != nil {
		if b.now().Sub(b.cachedAt) < balanceTTL {
			v := b.cached
			b.mu.Unlock()
			return v, nil
		}
	} else {
		if time.Since(b.cachedAt) < balanceTTL {
			v := b.cached
			b.mu.Unlock()
			return v, nil
		}
	}
	b.mu.Unlock()
	info, ok := chains.ByCAIP2(b.Network)
	if !ok {
		return 0, fmt.Errorf("unsupported network %q", b.Network)
	}
	usdc := info.USDCContract
	addr := strings.ToLower(b.address)
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		return 0, fmt.Errorf("invalid address %q", b.address)
	}

	data := "0x70a08231000000000000000000000000" + strings.TrimPrefix(addr, "0x")
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_call",
		"params": []any{map[string]string{"to": usdc, "data": data}, "latest"},
	}
	buf, _ := json.Marshal(payload)
	res, err := b.HTTP.Post(b.RPCURL, "application/json", bytesReader(buf))
	if err != nil {
		return 0, fmt.Errorf("rpc: %w", err)
	}
	defer res.Body.Close()
	var out struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return 0, err
	}
	if out.Error != nil {
		return 0, fmt.Errorf("rpc error: %s", out.Error.Message)
	}
	if strings.HasPrefix(out.Result, "0x") && len(out.Result) > 2 {
		v := new(big.Int)
		if _, ok := v.SetString(out.Result[2:], 16); !ok {
			return 0, fmt.Errorf("rpc: invalid hex in result %q", truncateStr(out.Result))
		}
		b.cached = float64(v.Int64()) / 1_000_000
		if b.now != nil {
			b.cachedAt = b.now()
		} else {
			b.cachedAt = time.Now()
		}
		return b.cached, nil
	}
	return 0, fmt.Errorf("rpc: unexpected result %q", truncateStr(out.Result))
}

func truncateStr(s string) string {
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

type bytesReaderAlias = bytes.Reader

func bytesReader(b []byte) *bytesReaderAlias { return bytes.NewReader(b) }
