package server

import (
	"bytes"
	"net/http"
	"testing"

	"gateway/internal/gateway"
)

func permsServer(t *testing.T, verified bool) (*gateway.Gateway, string) {
	t.Helper()
	gw := newTestGateway(t)
	gw.Permissions = gateway.NewPermissionStore(t.TempDir(), nil)
	sock := startTestServer(t, gw, &mockMFAServer{verified: verified, enrolled: true})
	return gw, sock
}

// TestPermissionsAddRequiresSudo (49.3): adding a grant raises spending
// authority, so an unverified owner gets 403 and nothing is stored.
func TestPermissionsAddRequiresSudo(t *testing.T) {
	gw, sock := permsServer(t, false)
	body := bytes.NewReader([]byte(`{"url":"https://a.example/x","limit_micro":1000}`))
	resp, err := testClient(sock).Post("http://localhost/permissions", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if len(gw.Permissions.List()) != 0 {
		t.Fatal("nothing must be stored when sudo fails")
	}
}

// TestPermissionsAddThenList (49.3): a verified owner adds a grant, and GET
// (read-only, no sudo) shows it.
func TestPermissionsAddThenList(t *testing.T) {
	gw, sock := permsServer(t, true)
	body := bytes.NewReader([]byte(`{"url":"https://a.example/x","limit_micro":1000}`))
	resp, err := testClient(sock).Post("http://localhost/permissions", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add status = %d, want 200", resp.StatusCode)
	}
	if !gw.Permissions.Allows("https://a.example/x", 1000) {
		t.Fatal("the grant must be effective")
	}
	get, err := testClient(sock).Get("http://localhost/permissions")
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", get.StatusCode)
	}
}

// TestPermissionsAddRejectsBadURL (49.3): a malformed grant is a 400, not stored.
func TestPermissionsAddRejectsBadURL(t *testing.T) {
	gw, sock := permsServer(t, true)
	body := bytes.NewReader([]byte(`{"url":"ftp://a.example","limit_micro":1000}`))
	resp, err := testClient(sock).Post("http://localhost/permissions", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(gw.Permissions.List()) != 0 {
		t.Fatal("invalid grant must not be stored")
	}
}

// TestPermissionsRemove (49.3): a verified owner removes a grant.
func TestPermissionsRemove(t *testing.T) {
	gw, sock := permsServer(t, true)
	if err := gw.Permissions.Add("https://a.example/x", 1000, false, 0); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodDelete, "http://localhost/permissions?url=https://a.example/x", nil)
	resp, err := testClient(sock).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gw.Permissions.Allows("https://a.example/x", 1) {
		t.Fatal("removed grant must not allow")
	}
}
