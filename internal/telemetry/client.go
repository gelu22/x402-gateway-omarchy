// Telemetry: Event, Client, New, Enabled, Register, hostname, truncate.
package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// httpTimeout bounds a single request to the future backend. Telemetry is
// best-effort: 15 s is the ceiling after which the queue gives up instead of
// holding daemon work.
const httpTimeout = 15 * time.Second

// maxErrorBody caps how many bytes of an upstream error body reach the log via
// truncate. Enough to diagnose, small enough to never dump a payload.
const maxErrorBody = 200

// Event is a single telemetry record (CONTRACTS §2).
type Event struct {
	Type        string `json:"type"` // payment | budget_exhausted
	Domain      string `json:"domain,omitempty"`
	AmountMicro int64  `json:"amount_micro,omitempty"`
	Ts          string `json:"ts,omitempty"`
}

// Client registers the device and flushes queued events to the backend.
// Empty BackendURL disables everything (no-op, zero errors).
type Client struct {
	BackendURL string
	StateDir   string
	HTTP       *http.Client

	mu          sync.Mutex
	queue       []Event
	deviceToken string
}

func New(backendURL, stateDir string) *Client {
	return &Client{
		BackendURL: backendURL,
		StateDir:   stateDir,
		HTTP:       &http.Client{Timeout: httpTimeout},
	}
}

// Enabled reports whether telemetry is configured.
func (c *Client) Enabled() bool { return c != nil && c.BackendURL != "" }

// Register exchanges the CDP access token for a device token. The backend
// validates the token with CDP and binds the device to that user.
func (c *Client) Register(accessToken string) error {
	if !c.Enabled() || accessToken == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{
		"access_token": accessToken,
		"name":         hostname(),
	})
	req, err := http.NewRequest(http.MethodPost, c.BackendURL+"/v1/devices/register",
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("telemetry: register: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("telemetry: register status %d: %s", res.StatusCode, truncate(raw))
	}
	var out struct {
		DeviceToken string `json:"device_token"`
	}
	if json.Unmarshal(raw, &out) != nil || out.DeviceToken == "" {
		return fmt.Errorf("telemetry: register decode: %s", truncate(raw))
	}
	c.mu.Lock()
	c.deviceToken = out.DeviceToken
	c.mu.Unlock()
	return nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > maxErrorBody {
		return s[:maxErrorBody]
	}
	return s
}
