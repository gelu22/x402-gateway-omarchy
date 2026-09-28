package chains

import (
	"fmt"
)

// ChainInfo holds metadata for a supported EVM chain.
type ChainInfo struct {
	CAIP2        string // e.g., "eip155:84532"
	ChainID      int64  // EVM chain ID (84532, 8453)
	USDCContract string // Pinned USDC contract address (checksummed)
	RPCURL       string // Public RPC endpoint for balance queries
	Name         string // Human-readable name
}

// Supported chains (THREAT-MODEL T1: pinned assets, allowlisted networks).
//
// The list is closed on purpose: the network whitelist and the USDC contract
// addresses are a security control, not a setting. `policy.json` may only
// narrow it (`AllowedNetworks`/`PinnedAssets`), never extend it — otherwise the
// ceiling would live in a file the same user (or malware acting as them) can
// rewrite (THREAT-MODEL T3). Adding a chain is therefore a reviewed code change
// with the contract address checked at the source, not a configuration edit.
var byCAIP2 = map[string]ChainInfo{
	"eip155:84532": {
		CAIP2:        "eip155:84532",
		ChainID:      84532,
		USDCContract: "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
		RPCURL:       "https://sepolia.base.org",
		Name:         "Base Sepolia",
	},
	"eip155:8453": {
		CAIP2:        "eip155:8453",
		ChainID:      8453,
		USDCContract: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
		RPCURL:       "https://mainnet.base.org",
		Name:         "Base",
	},
}

// ByCAIP2 returns chain info for the given CAIP-2 network identifier.
// Returns zero ChainInfo and false if not supported.
func ByCAIP2(caip2 string) (ChainInfo, bool) {
	info, ok := byCAIP2[caip2]
	return info, ok
}

// MustByCAIP2 returns chain info or panics if unsupported.
// Use only for config-time validation where unsupported = bug.
func MustByCAIP2(caip2 string) ChainInfo {
	info, ok := ByCAIP2(caip2)
	if !ok {
		panic(fmt.Sprintf("chains: unsupported network %q", caip2))
	}
	return info
}

// SupportedCAIP2s returns all supported CAIP-2 identifiers.
func SupportedCAIP2s() []string {
	out := make([]string, 0, len(byCAIP2))
	for k := range byCAIP2 {
		out = append(out, k)
	}
	return out
}

// USDCContract returns the pinned USDC contract address for the network.
// Returns empty string if network not supported.
func USDCContract(caip2 string) string {
	info, ok := ByCAIP2(caip2)
	if !ok {
		return ""
	}
	return info.USDCContract
}

// ChainID returns the EVM chain ID for the network.
// Returns 0 if network not supported.
func ChainID(caip2 string) int64 {
	info, ok := ByCAIP2(caip2)
	if !ok {
		return 0
	}
	return info.ChainID
}

// RPCURL returns the public RPC URL for the network.
// Returns empty string if network not supported.
func RPCURL(caip2 string) string {
	info, ok := ByCAIP2(caip2)
	if !ok {
		return ""
	}
	return info.RPCURL
}

// Name returns the human-readable name for the network.
// Returns the CAIP-2 identifier if network not supported.
func Name(caip2 string) string {
	info, ok := ByCAIP2(caip2)
	if !ok {
		return caip2
	}
	return info.Name
}

// IsSupported reports whether the network is supported.
func IsSupported(caip2 string) bool {
	_, ok := ByCAIP2(caip2)
	return ok
}
