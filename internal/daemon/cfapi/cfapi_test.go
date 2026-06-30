package cfapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cfResponse builds a minimal Cloudflare v4 success envelope with the given result.
func cfResponse(result any) any {
	b, _ := json.Marshal(result)
	return map[string]any{"success": true, "result": json.RawMessage(b)}
}

func cfErrorResponse(code int, msg string) any {
	return map[string]any{
		"success": false,
		"errors":  []map[string]any{{"code": code, "message": msg}},
		"result":  nil,
	}
}

func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		APIToken:  "test-token",
		AccountID: "acct-123",
		ZoneID:    "zone-456",
		BaseURL:   srv.URL,
	}
}

// ── CreateTunnel ──────────────────────────────────────────────────────────────

func TestCreateTunnel_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/cfd_tunnel") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(cfResponse(map[string]string{"id": "tunnel-abc"}))
	}))
	defer srv.Close()

	info, err := newTestClient(srv).CreateTunnel("voci-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.TunnelID != "tunnel-abc" {
		t.Errorf("TunnelID = %q, want tunnel-abc", info.TunnelID)
	}
}

func TestCreateTunnel_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(cfErrorResponse(1003, "invalid name"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateTunnel("bad-name")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "422") && !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("error missing status/message: %v", err)
	}
}

// ── GetTunnelToken ────────────────────────────────────────────────────────────

func TestGetTunnelToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/token") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(cfResponse("super-secret-token"))
	}))
	defer srv.Close()

	token, err := newTestClient(srv).GetTunnelToken("tunnel-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "super-secret-token" {
		t.Errorf("token = %q, want super-secret-token", token)
	}
}

func TestGetTunnelToken_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(cfErrorResponse(1003, "not found"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetTunnelToken("no-such-tunnel")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

// ── CreateDNSRecord ───────────────────────────────────────────────────────────

func TestCreateDNSRecord_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/dns_records") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(cfResponse(map[string]string{"id": "dns-rec-789"}))
	}))
	defer srv.Close()

	rec, err := newTestClient(srv).CreateDNSRecord("voci-test.example.com", "tunnel-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.RecordID != "dns-rec-789" {
		t.Errorf("RecordID = %q, want dns-rec-789", rec.RecordID)
	}
}

func TestCreateDNSRecord_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(cfErrorResponse(81058, "record already exists"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateDNSRecord("dup.example.com", "tunnel-abc")
	if err == nil {
		t.Fatal("expected error for conflict, got nil")
	}
	if !strings.Contains(fmt.Sprint(err), "409") && !strings.Contains(fmt.Sprint(err), "already exists") {
		t.Errorf("error missing conflict details: %v", err)
	}
}

// ── DeleteTunnel ──────────────────────────────────────────────────────────────

func TestDeleteTunnel_Success(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(cfResponse(map[string]string{"id": "tunnel-abc"}))
	}))
	defer srv.Close()

	err := newTestClient(srv).DeleteTunnel("tunnel-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected DELETE request, none received")
	}
}

func TestDeleteTunnel_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(cfErrorResponse(1003, "not found"))
	}))
	defer srv.Close()

	// 404 must be treated as success (idempotent)
	err := newTestClient(srv).DeleteTunnel("no-such-tunnel")
	if err != nil {
		t.Fatalf("DeleteTunnel on 404 should return nil, got: %v", err)
	}
}

// ── DeleteDNSRecord ───────────────────────────────────────────────────────────

func TestDeleteDNSRecord_Success(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		json.NewEncoder(w).Encode(cfResponse(map[string]string{"id": "dns-rec-789"}))
	}))
	defer srv.Close()

	err := newTestClient(srv).DeleteDNSRecord("dns-rec-789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected DELETE request, none received")
	}
}

func TestDeleteDNSRecord_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(cfErrorResponse(1003, "not found"))
	}))
	defer srv.Close()

	// 404 must be treated as success (idempotent)
	err := newTestClient(srv).DeleteDNSRecord("no-such-record")
	if err != nil {
		t.Fatalf("DeleteDNSRecord on 404 should return nil, got: %v", err)
	}
}

// ── Client BaseURL override ───────────────────────────────────────────────────

func TestClient_BaseURLOverride(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		json.NewEncoder(w).Encode(cfResponse(map[string]string{"id": "tunnel-x"}))
	}))
	defer srv.Close()

	c := &Client{APIToken: "tok", AccountID: "acct", ZoneID: "zone", BaseURL: srv.URL}
	c.CreateTunnel("override-test")

	if gotHost != strings.TrimPrefix(srv.URL, "http://") {
		t.Errorf("request hit wrong host: %q (want %q)", gotHost, srv.URL)
	}
}
