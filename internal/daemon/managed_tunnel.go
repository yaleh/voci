package daemon

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"time"

	"github.com/yaleh/voci/internal/daemon/cfapi"
)

// ManagedTunnelConfig holds credentials and settings for a Cloudflare Named Tunnel.
type ManagedTunnelConfig struct {
	APIToken     string
	AccountID    string
	ZoneID       string
	TunnelDomain string        // e.g. "voci.example.com"
	TTL          time.Duration // default 20h when zero
}

func (cfg ManagedTunnelConfig) ttl() time.Duration {
	if cfg.TTL > 0 {
		return cfg.TTL
	}
	return 20 * time.Hour
}

// StartManagedTunnel orchestrates the full Named Tunnel lifecycle:
//  1. If a valid active-tunnel.json exists within TTL, reuse it.
//  2. Otherwise, delete any stale Cloudflare resources and create a new tunnel.
//  3. Start cloudflared with the stored token, wait for a ready signal.
//
// Returns the running cloudflared *exec.Cmd and the stable public HTTPS URL.
func StartManagedTunnel(ctx context.Context, cfg ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, "", fmt.Errorf("cloudflared not found in PATH: %w\nInstall: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/", err)
	}

	client := &cfapi.Client{
		APIToken:  cfg.APIToken,
		AccountID: cfg.AccountID,
		ZoneID:    cfg.ZoneID,
	}

	state, err := readOrCreateState(ctx, client, cfg)
	if err != nil {
		return nil, "", fmt.Errorf("tunnel state: %w", err)
	}

	cmd := exec.CommandContext(ctx, bin,
		"tunnel", "run",
		"--token", state.Token,
		"--url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	applyChildAttrs(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", fmt.Errorf("pipe stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("start cloudflared: %w", err)
	}

	// Drain stderr for diagnostics; managed tunnels don't emit a URL on stderr
	// (the URL is fixed from state), so we just log all output.
	go func() {
		urlCh := make(chan string, 1)
		drainStderr(stderr, logW, urlCh)
	}()

	return cmd, state.PublicURL, nil
}

// readOrCreateState returns an existing valid TunnelState or creates a new one
// by calling the Cloudflare API.
func readOrCreateState(ctx context.Context, client *cfapi.Client, cfg ManagedTunnelConfig) (*TunnelState, error) {
	existing, err := ReadActiveTunnel()
	if err == nil && existing != nil && existing.IsWithinTTL(cfg.ttl()) {
		return existing, nil
	}

	// Clean up stale resources before creating new ones.
	if existing != nil {
		if existing.TunnelID != "" {
			client.DeleteTunnel(existing.TunnelID)
		}
		if existing.DNSRecordID != "" {
			client.DeleteDNSRecord(existing.DNSRecordID)
		}
	}

	return createNewTunnel(ctx, client, cfg)
}

// createNewTunnel calls the CF API to provision a new Named Tunnel + DNS CNAME,
// writes the result to active-tunnel.json, and returns the populated state.
func createNewTunnel(_ context.Context, client *cfapi.Client, cfg ManagedTunnelConfig) (*TunnelState, error) {
	suffix := randomSuffix(6)
	name := "voci-" + suffix
	subdomain := name + "." + cfg.TunnelDomain

	info, err := client.CreateTunnel(name)
	if err != nil {
		return nil, fmt.Errorf("create tunnel: %w", err)
	}

	token, err := client.GetTunnelToken(info.TunnelID)
	if err != nil {
		client.DeleteTunnel(info.TunnelID)
		return nil, fmt.Errorf("get token: %w", err)
	}

	rec, err := client.CreateDNSRecord(subdomain, info.TunnelID)
	if err != nil {
		client.DeleteTunnel(info.TunnelID)
		return nil, fmt.Errorf("create DNS record: %w", err)
	}

	state := &TunnelState{
		TunnelID:    info.TunnelID,
		Token:       token,
		PublicURL:   "https://" + subdomain,
		DNSRecordID: rec.RecordID,
		CreatedAt:   time.Now(),
	}
	if err := WriteActiveTunnel(state); err != nil {
		return nil, fmt.Errorf("write state: %w", err)
	}
	return state, nil
}

const letters = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomSuffix(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
