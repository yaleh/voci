package daemon

import (
	"testing"
)

func TestParseTunnelURL_ExtractsHTTPS(t *testing.T) {
	input := "2024-01-01T00:00:00Z INF |  https://abc123.trycloudflare.com  |\n"
	got := ParseTunnelURL(input)
	if got != "https://abc123.trycloudflare.com" {
		t.Errorf("want https://abc123.trycloudflare.com, got %q", got)
	}
}

func TestParseTunnelURL_ReturnsEmptyWhenNoMatch(t *testing.T) {
	got := ParseTunnelURL("no url here")
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
