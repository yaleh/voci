// Package cfapi provides a minimal Cloudflare REST API client for managing
// Named Tunnels and DNS records used by voci serve --share.
package cfapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const defaultBaseURL = "https://api.cloudflare.com"

// Client calls the Cloudflare v4 API using an API token.
// Set BaseURL to override the API host (e.g. for tests with httptest.Server).
type Client struct {
	APIToken  string
	AccountID string
	ZoneID    string
	BaseURL   string
}

// baseURL returns the configured base URL or the default.
func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

// TunnelInfo holds the result of a successful CreateTunnel call.
type TunnelInfo struct {
	TunnelID string
	Token    string
}

// DNSRecord holds the result of a successful CreateDNSRecord call.
type DNSRecord struct {
	RecordID string
}

// apiResponse is the common Cloudflare v4 response envelope.
type apiResponse struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Errors  []apiError      `json:"errors"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e apiError) Error() string {
	return fmt.Sprintf("CF API error %d: %s", e.Code, e.Message)
}

// do performs a JSON HTTP request and decodes the response envelope.
// On HTTP ≥400 it returns an error containing the status code.
// 404 is NOT silenced here; callers that need idempotent deletes should check.
func (c *Client) do(method, url string, body any) (*apiResponse, int, error) {
	var bodyR io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		bodyR = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, bodyR)
	if err != nil {
		return nil, 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var env apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 400 {
		if len(env.Errors) > 0 {
			return nil, resp.StatusCode, fmt.Errorf("status %d: %w", resp.StatusCode, env.Errors[0])
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return &env, resp.StatusCode, nil
}

// CreateTunnel creates a new Named Tunnel under the configured account.
// Returns the tunnel ID (for DNS CNAME target) and a management token.
func (c *Client) CreateTunnel(name string) (TunnelInfo, error) {
	url := fmt.Sprintf("%s/client/v4/accounts/%s/cfd_tunnel", c.baseURL(), c.AccountID)
	payload := map[string]any{
		"name":         name,
		"tunnel_secret": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	}
	env, _, err := c.do("POST", url, payload)
	if err != nil {
		return TunnelInfo{}, fmt.Errorf("CreateTunnel: %w", err)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return TunnelInfo{}, fmt.Errorf("CreateTunnel: parse result: %w", err)
	}
	if result.ID == "" {
		return TunnelInfo{}, fmt.Errorf("CreateTunnel: empty tunnel ID in response")
	}
	return TunnelInfo{TunnelID: result.ID}, nil
}

// GetTunnelToken retrieves the connector token for the given tunnel.
// The token is passed to `cloudflared tunnel run --token` to connect.
func (c *Client) GetTunnelToken(tunnelID string) (string, error) {
	url := fmt.Sprintf("%s/client/v4/accounts/%s/cfd_tunnel/%s/token", c.baseURL(), c.AccountID, tunnelID)
	env, _, err := c.do("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("GetTunnelToken: %w", err)
	}
	var token string
	if err := json.Unmarshal(env.Result, &token); err != nil {
		return "", fmt.Errorf("GetTunnelToken: parse token: %w", err)
	}
	if token == "" {
		return "", fmt.Errorf("GetTunnelToken: empty token in response")
	}
	return token, nil
}

// CreateDNSRecord creates a CNAME record pointing subdomain → tunnelID.cfargotunnel.com.
func (c *Client) CreateDNSRecord(subdomain, tunnelID string) (DNSRecord, error) {
	url := fmt.Sprintf("%s/client/v4/zones/%s/dns_records", c.baseURL(), c.ZoneID)
	payload := map[string]any{
		"type":    "CNAME",
		"name":    subdomain,
		"content": fmt.Sprintf("%s.cfargotunnel.com", tunnelID),
		"proxied": true,
	}
	env, _, err := c.do("POST", url, payload)
	if err != nil {
		return DNSRecord{}, fmt.Errorf("CreateDNSRecord: %w", err)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return DNSRecord{}, fmt.Errorf("CreateDNSRecord: parse result: %w", err)
	}
	if result.ID == "" {
		return DNSRecord{}, fmt.Errorf("CreateDNSRecord: empty record ID in response")
	}
	return DNSRecord{RecordID: result.ID}, nil
}

// DeleteTunnel deletes the named tunnel by ID. Returns nil if the tunnel
// does not exist (idempotent).
func (c *Client) DeleteTunnel(tunnelID string) error {
	url := fmt.Sprintf("%s/client/v4/accounts/%s/cfd_tunnel/%s", c.baseURL(), c.AccountID, tunnelID)
	_, status, err := c.do("DELETE", url, nil)
	if err != nil && status == 404 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("DeleteTunnel: %w", err)
	}
	return nil
}

// DeleteDNSRecord deletes the DNS record by ID. Returns nil if the record
// does not exist (idempotent).
func (c *Client) DeleteDNSRecord(recordID string) error {
	url := fmt.Sprintf("%s/client/v4/zones/%s/dns_records/%s", c.baseURL(), c.ZoneID, recordID)
	_, status, err := c.do("DELETE", url, nil)
	if err != nil && status == 404 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("DeleteDNSRecord: %w", err)
	}
	return nil
}
