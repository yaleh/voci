package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for voci.
// Language string contains the language code (e.g. "zh", "en") read from VOCI_LANGUAGE env.
type Config struct {
	SiliconFlowKey string
	OllamaHost     string
	Language       string
	ASRProvider    string
	ASRAPIKey      string
	ASRAPIURL      string
	ASRModel       string

	// Cloudflare managed tunnel (optional). When all four are set, voci serve --share
	// uses a stable Named Tunnel instead of a Quick Tunnel.
	CloudflareAPIToken     string
	CloudflareAccountID    string
	CloudflareZoneID       string
	CloudflareTunnelDomain string
}

type fileConfig struct {
	SiliconFlowAPIKey string `yaml:"siliconflow_api_key"`
	OllamaHost        string `yaml:"ollama_host"`
	Language          string `yaml:"language"`
	ASRProvider       string `yaml:"asr_provider"`
	ASRAPIKey         string `yaml:"asr_api_key"`
	ASRAPIURL         string `yaml:"asr_api_url"`
	ASRModel          string `yaml:"asr_model"`

	CloudflareAPIToken     string `yaml:"cloudflare_api_token"`
	CloudflareAccountID    string `yaml:"cloudflare_account_id"`
	CloudflareZoneID       string `yaml:"cloudflare_zone_id"`
	CloudflareTunnelDomain string `yaml:"cloudflare_tunnel_domain"`
}

// LoadConfig reads configuration from environment variables first,
// then falls back to ~/.config/voci/config.yaml.
// The API key is never printed to stdout/stderr.
func LoadConfig() (Config, error) {
	cfg := Config{}

	// Read env vars
	cfg.SiliconFlowKey = os.Getenv("SILICONFLOW_API_KEY")
	cfg.OllamaHost = os.Getenv("OLLAMA_HOST")
	cfg.Language = os.Getenv("VOCI_LANGUAGE")
	cfg.ASRProvider = os.Getenv("ASR_PROVIDER")
	cfg.ASRAPIKey = os.Getenv("ASR_API_KEY")
	cfg.ASRAPIURL = os.Getenv("ASR_API_URL")
	cfg.ASRModel = os.Getenv("ASR_MODEL")
	cfg.CloudflareAPIToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	cfg.CloudflareAccountID = os.Getenv("CF_ACCOUNT_ID")
	cfg.CloudflareZoneID = os.Getenv("CF_ZONE_ID")
	cfg.CloudflareTunnelDomain = os.Getenv("CF_TUNNEL_DOMAIN")

	// Try to load from file; VOCI_CONFIG overrides the default path.
	cfgPath := os.Getenv("VOCI_CONFIG")
	if cfgPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfgPath = filepath.Join(home, ".config", "voci", "config.yaml")
		}
	}
	if cfgPath != "" {
		data, err := os.ReadFile(cfgPath)
		if err == nil {
			var fc fileConfig
			if err := yaml.Unmarshal(data, &fc); err == nil {
				if cfg.SiliconFlowKey == "" && fc.SiliconFlowAPIKey != "" {
					cfg.SiliconFlowKey = fc.SiliconFlowAPIKey
				}
				if cfg.OllamaHost == "" && fc.OllamaHost != "" {
					cfg.OllamaHost = fc.OllamaHost
				}
				if cfg.Language == "" && fc.Language != "" {
					cfg.Language = fc.Language
				}
				if cfg.ASRProvider == "" && fc.ASRProvider != "" {
					cfg.ASRProvider = fc.ASRProvider
				}
				if cfg.ASRAPIKey == "" && fc.ASRAPIKey != "" {
					cfg.ASRAPIKey = fc.ASRAPIKey
				}
				if cfg.ASRAPIURL == "" && fc.ASRAPIURL != "" {
					cfg.ASRAPIURL = fc.ASRAPIURL
				}
				if cfg.ASRModel == "" && fc.ASRModel != "" {
					cfg.ASRModel = fc.ASRModel
				}
				if cfg.CloudflareAPIToken == "" && fc.CloudflareAPIToken != "" {
					cfg.CloudflareAPIToken = fc.CloudflareAPIToken
				}
				if cfg.CloudflareAccountID == "" && fc.CloudflareAccountID != "" {
					cfg.CloudflareAccountID = fc.CloudflareAccountID
				}
				if cfg.CloudflareZoneID == "" && fc.CloudflareZoneID != "" {
					cfg.CloudflareZoneID = fc.CloudflareZoneID
				}
				if cfg.CloudflareTunnelDomain == "" && fc.CloudflareTunnelDomain != "" {
					cfg.CloudflareTunnelDomain = fc.CloudflareTunnelDomain
				}
			}
		}
	}

	// Defaults
	if cfg.OllamaHost == "" {
		cfg.OllamaHost = "http://localhost:11434"
	}
	if cfg.Language == "" {
		cfg.Language = "zh"
	}
	if cfg.ASRProvider == "" {
		cfg.ASRProvider = "siliconflow"
	}

	// Backward-compat: if ASRAPIKey not set, fall back to SiliconFlowKey
	if cfg.ASRAPIKey == "" && cfg.SiliconFlowKey != "" {
		cfg.ASRAPIKey = cfg.SiliconFlowKey
	}

	if cfg.ASRAPIKey == "" {
		return cfg, fmt.Errorf("ASR API key not found: set ASR_API_KEY or SILICONFLOW_API_KEY env, or add asr_api_key/siliconflow_api_key to ~/.config/voci/config.yaml")
	}

	return cfg, nil
}
