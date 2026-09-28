// Package mcpserver exposes the gateway over the Model Context Protocol.
// It is a thin stdio→unix-socket bridge: credentials never leave the daemon
// process, so MCP clients cannot bypass policy or trigger token rotation
// races (CONTRACTS.md §5).
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures the bridge.
type Options struct {
	SocketPath string
	Version    string
}

type fetchInput struct {
	URL    string `json:"url" jsonschema:"the http(s) URL to fetch; x402 paywalls are paid automatically within the configured budget"`
	Method string `json:"method,omitempty" jsonschema:"HTTP method, default GET"`
	Body   string `json:"body,omitempty" jsonschema:"optional request body"`
}

type pauseInput struct {
	Paused bool `json:"paused" jsonschema:"true pauses all spending, false resumes"`
}

type emptyInput struct{}

// New builds an MCP server with the gateway tools wired to the daemon socket.
func New(ctx context.Context, opts Options) (*mcp.Server, error) {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", opts.SocketPath)
		},
	}
	client := &http.Client{Transport: transport}

	srv := mcp.NewServer(&mcp.Implementation{Name: "x402-gateway", Version: opts.Version}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fetch_with_payment",
		Description: "Fetch an http(s) URL; if the server answers 402 Payment Required (x402), pays automatically from the local wallet within the daily budget and returns the unlocked content.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in fetchInput) (*mcp.CallToolResult, any, error) {
		method := in.Method
		if method == "" {
			method = http.MethodGet
		}
		body := map[string]any{"url": in.URL, "method": method}
		if in.Body != "" {
			body["body"] = []byte(in.Body)
		}
		raw, err := callSocket(ctx, client, "/fetch", body)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gateway_status",
		Description: "Gateway state: wallet address, spend today vs budget, per-request cap, paused flag.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, any, error) {
		raw, err := callSocket(ctx, client, "/status", nil)
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "gateway_pause",
		Description: "Pause or resume all automatic spending. While paused every paid fetch fails fast.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in pauseInput) (*mcp.CallToolResult, any, error) {
		raw, err := callSocket(ctx, client, "/pause", map[string]any{"paused": in.Paused})
		if err != nil {
			return nil, nil, err
		}
		return textResult(raw), nil, nil
	})

	return srv, nil
}

// RunStdio serves MCP over stdin/stdout until the client disconnects.
func RunStdio(ctx context.Context, opts Options) error {
	srv, err := New(ctx, opts)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &mcp.StdioTransport{})
}

// callSocket POSTs a JSON body to a daemon endpoint over the unix socket.
func callSocket(ctx context.Context, client *http.Client, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = stringsReader(string(buf))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost"+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway unreachable (is the daemon running?): %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return raw, fmt.Errorf("gateway: %s", stringsTrimSpace(string(raw)))
	}
	return raw, nil
}

func textResult(raw []byte) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}
}

func stringsReader(s string) io.Reader { return strings.NewReader(s) }

func stringsTrimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
