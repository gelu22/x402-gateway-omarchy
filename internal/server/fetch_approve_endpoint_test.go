package server

import (
	"bytes"
	"net/http"
	"testing"
)

// TestFetchApproveGuards (49.4-daemon): a malformed approve is 400 and an
// unknown id is 404 — before any sudo gate or signing.
func TestFetchApproveGuards(t *testing.T) {
	_, sock := permsServer(t, true)
	// missing id
	resp, err := testClient(sock).Post("http://localhost/fetch-approve", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing id: status = %d, want 400", resp.StatusCode)
	}
	// unknown id
	resp2, err := testClient(sock).Post("http://localhost/fetch-approve", "application/json", bytes.NewReader([]byte(`{"id":"nope"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: status = %d, want 404", resp2.StatusCode)
	}
}
