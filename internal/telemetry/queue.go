// Telemetry queue: LoadQueue, Enqueue, Flush, queuePath, persistLocked.
package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	queueFileName = "telemetry_queue.jsonl"
	maxQueued     = 500
	batchLimit    = 100
)

// LoadQueue restores persisted offline events (call once at startup).
func (c *Client) LoadQueue() {
	if !c.Enabled() {
		return
	}
	raw, err := os.ReadFile(c.queuePath())
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Event
		if json.Unmarshal([]byte(line), &e) == nil {
			c.queue = append(c.queue, e)
		}
	}
	if len(c.queue) > maxQueued {
		c.queue = c.queue[len(c.queue)-maxQueued:]
	}
}

// Enqueue appends an event to the offline queue (cap maxQueued, drop oldest)
// and persists it as a JSONL line.
func (c *Client) Enqueue(e Event) {
	if !c.Enabled() {
		return
	}
	if e.Ts == "" {
		e.Ts = time.Now().UTC().Format(time.RFC3339)
	}
	c.mu.Lock()
	c.queue = append(c.queue, e)
	if len(c.queue) > maxQueued {
		c.queue = c.queue[len(c.queue)-maxQueued:]
	}
	persistLocked(c.queuePath(), c.queue)
	c.mu.Unlock()
}

// Flush posts up to batchLimit queued events. Returns number sent.
func (c *Client) Flush(deviceToken string) (int, error) {
	if !c.Enabled() {
		return 0, nil
	}
	if deviceToken == "" {
		c.mu.Lock()
		deviceToken = c.deviceToken
		c.mu.Unlock()
	}
	if deviceToken == "" {
		return 0, nil
	}
	c.mu.Lock()
	n := len(c.queue)
	if n > batchLimit {
		n = batchLimit
	}
	batch := make([]Event, n)
	copy(batch, c.queue[:n])
	c.mu.Unlock()
	if n == 0 {
		return 0, nil
	}

	payload, _ := json.Marshal(map[string]any{"events": batch})
	req, err := http.NewRequest(http.MethodPost, c.BackendURL+"/v1/telemetry/events",
		bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("telemetry: flush: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 400 {
		return 0, fmt.Errorf("telemetry: flush status %d", res.StatusCode)
	}
	c.mu.Lock()
	c.queue = c.queue[n:]
	c.mu.Unlock()
	return n, nil
}

func (c *Client) queuePath() string { return filepath.Join(c.StateDir, queueFileName) }

func persistLocked(path string, queue []Event) {
	var buf bytes.Buffer
	for _, e := range queue {
		b, _ := json.Marshal(e)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, buf.Bytes(), 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}
