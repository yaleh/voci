package tunnel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yaleh/voci/internal/daemon/cfapi"
)

// fakeCFServer returns an httptest.Server that handles CF API calls with canned responses.
// tunnelID, token, dnsRecordID are the values returned by Create* endpoints.
func fakeCFServer(t *testing.T, tunnelID, token, dnsRecordID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/cfd_tunnel") && !strings.Contains(r.URL.Path, "/token"):
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": tunnelID}})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/token"):
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": token})
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/dns_records"):
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": dnsRecordID}})
		case r.Method == "DELETE":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": "deleted"}})
		default:
			t.Logf("unexpected CF API call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": 1003, "message": "not found"}}})
		}
	}))
}

func testManagedConfig(cfSrv *httptest.Server) ManagedTunnelConfig {
	return ManagedTunnelConfig{
		APIToken:     "tok",
		AccountID:    "acct",
		ZoneID:       "zone",
		TunnelDomain: "voci.example.com",
		TTL:          20 * time.Hour,
	}
}

func testCFClient(cfSrv *httptest.Server) *cfapi.Client {
	return &cfapi.Client{
		APIToken:  "tok",
		AccountID: "acct",
		ZoneID:    "zone",
		BaseURL:   cfSrv.URL,
	}
}

func TestStartManagedTunnel_FreshState(t *testing.T) {
	withFakeHome(t, func(home string) {
		cfSrv := fakeCFServer(t, "tun-new", "connector-token", "dns-new")
		defer cfSrv.Close()

		client := testCFClient(cfSrv)
		cfg := testManagedConfig(cfSrv)

		state, err := createNewTunnel(context.Background(), client, cfg)
		if err != nil {
			t.Fatalf("createNewTunnel: %v", err)
		}
		if state.TunnelID != "tun-new" {
			t.Errorf("TunnelID = %q, want tun-new", state.TunnelID)
		}
		if state.Token != "connector-token" {
			t.Errorf("Token = %q, want connector-token", state.Token)
		}
		if !strings.HasPrefix(state.PublicURL, "https://voci-") {
			t.Errorf("PublicURL = %q, want https://voci-*.voci.example.com", state.PublicURL)
		}
		if !strings.HasSuffix(state.PublicURL, ".voci.example.com") {
			t.Errorf("PublicURL = %q, want suffix .voci.example.com", state.PublicURL)
		}
		if state.DNSRecordID != "dns-new" {
			t.Errorf("DNSRecordID = %q, want dns-new", state.DNSRecordID)
		}

		// State file must be written
		persisted, readErr := ReadActiveTunnel()
		if readErr != nil {
			t.Fatalf("ReadActiveTunnel: %v", readErr)
		}
		if persisted == nil || persisted.TunnelID != "tun-new" {
			t.Errorf("persisted state missing or wrong TunnelID: %+v", persisted)
		}
	})
}

func TestStartManagedTunnel_ReuseState(t *testing.T) {
	withFakeHome(t, func(home string) {
		// Preset a fresh state
		existing := &TunnelState{
			TunnelID:    "tun-existing",
			Token:       "tok-existing",
			PublicURL:   "https://voci-existing.voci.example.com",
			DNSRecordID: "dns-existing",
			CreatedAt:   time.Now().Add(-1 * time.Hour), // 1h old, within 20h TTL
		}
		if err := WriteActiveTunnel(existing); err != nil {
			t.Fatalf("WriteActiveTunnel: %v", err)
		}

		apiCalled := false
		cfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiCalled = true
			w.WriteHeader(500)
		}))
		defer cfSrv.Close()

		client := testCFClient(cfSrv)
		cfg := testManagedConfig(cfSrv)

		state, err := readOrCreateState(context.Background(), client, cfg)
		if err != nil {
			t.Fatalf("readOrCreateState: %v", err)
		}
		if state.TunnelID != "tun-existing" {
			t.Errorf("expected reuse of existing state, got TunnelID = %q", state.TunnelID)
		}
		if apiCalled {
			t.Error("CF API must NOT be called when state is still valid")
		}
	})
}

func TestStartManagedTunnel_ExpiredState(t *testing.T) {
	withFakeHome(t, func(home string) {
		// Write an expired state
		stale := &TunnelState{
			TunnelID:    "tun-stale",
			Token:       "tok-stale",
			PublicURL:   "https://voci-stale.voci.example.com",
			DNSRecordID: "dns-stale",
			CreatedAt:   time.Now().Add(-25 * time.Hour), // expired
		}
		if err := WriteActiveTunnel(stale); err != nil {
			t.Fatalf("WriteActiveTunnel: %v", err)
		}

		deletedTunnelIDs := []string{}
		deletedDNSIDs := []string{}
		cfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/cfd_tunnel"):
				deletedTunnelIDs = append(deletedTunnelIDs, "tun-stale")
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": nil})
			case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/dns_records"):
				deletedDNSIDs = append(deletedDNSIDs, "dns-stale")
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": nil})
			case r.Method == "POST" && strings.Contains(r.URL.Path, "/cfd_tunnel") && !strings.Contains(r.URL.Path, "/token"):
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": "tun-new"}})
			case r.Method == "GET" && strings.Contains(r.URL.Path, "/token"):
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": "tok-new"})
			case r.Method == "POST" && strings.Contains(r.URL.Path, "/dns_records"):
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": "dns-new"}})
			default:
				w.WriteHeader(404)
			}
		}))
		defer cfSrv.Close()

		client := testCFClient(cfSrv)
		cfg := testManagedConfig(cfSrv)

		state, err := readOrCreateState(context.Background(), client, cfg)
		if err != nil {
			t.Fatalf("readOrCreateState: %v", err)
		}
		if state.TunnelID != "tun-new" {
			t.Errorf("expected new tunnel, got TunnelID = %q", state.TunnelID)
		}
		if len(deletedTunnelIDs) == 0 {
			t.Error("expected stale tunnel to be deleted via API")
		}
		if len(deletedDNSIDs) == 0 {
			t.Error("expected stale DNS record to be deleted via API")
		}
	})
}

func TestStartManagedTunnel_MissingBinary(t *testing.T) {
	// Override PATH to empty so cloudflared is not found.
	t.Setenv("PATH", "")
	cfg := ManagedTunnelConfig{
		APIToken: "tok", AccountID: "acct", ZoneID: "zone", TunnelDomain: "example.com",
	}
	_, _, err := StartManagedTunnel(context.Background(), cfg, 9474, io.Discard)
	if err == nil {
		t.Fatal("expected error when cloudflared not in PATH")
	}
	if !strings.Contains(err.Error(), "cloudflared not found") {
		t.Errorf("error should mention 'cloudflared not found', got: %v", err)
	}
}

func TestManagedTunnelConfig_DefaultTTL(t *testing.T) {
	cfg := ManagedTunnelConfig{TTL: 0}
	got := cfg.ttl()
	want := 20 * time.Hour
	if got != want {
		t.Errorf("ttl() = %v, want %v", got, want)
	}
}

func TestManagedTunnelConfig_CustomTTL(t *testing.T) {
	cfg := ManagedTunnelConfig{TTL: 5 * time.Hour}
	got := cfg.ttl()
	want := 5 * time.Hour
	if got != want {
		t.Errorf("ttl() = %v, want %v", got, want)
	}
}
