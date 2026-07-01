package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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

	// Tuning parameters (B-class: server-side context/ASR behavior; D-class:
	// frontend VAD signal-processing params served read-only via /api/config).
	// All default to the values that were previously hardcoded constants.
	MaxProseTurns          int
	MaxProseCharsPerTurn   int
	MaxProseCharsTotal     int
	SessionLines           int
	ContextCacheTTLSeconds int
	EntityTokenCap         int
	EntityMinTokenLen      int
	VADThreshold           float64
	MinAudioMs             int
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

	MaxProseTurns          int     `yaml:"max_prose_turns"`
	MaxProseCharsPerTurn   int     `yaml:"max_prose_chars_per_turn"`
	MaxProseCharsTotal     int     `yaml:"max_prose_chars_total"`
	SessionLines           int     `yaml:"session_lines"`
	ContextCacheTTLSeconds int     `yaml:"context_cache_ttl_seconds"`
	EntityTokenCap         int     `yaml:"entity_token_cap"`
	EntityMinTokenLen      int     `yaml:"entity_min_token_len"`
	VADThreshold           float64 `yaml:"vad_threshold"`
	MinAudioMs             int     `yaml:"min_audio_ms"`
}

// envInt sets *out from the named env var if it parses as an int. A missing
// or malformed value is ignored (tuning knobs must never fail startup).
func envInt(name string, out *int) {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*out = n
		}
	}
}

// envFloat sets *out from the named env var if it parses as a float64.
func envFloat(name string, out *float64) {
	if v := os.Getenv(name); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			*out = f
		}
	}
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

	envInt("VOCI_MAX_PROSE_TURNS", &cfg.MaxProseTurns)
	envInt("VOCI_MAX_PROSE_CHARS_PER_TURN", &cfg.MaxProseCharsPerTurn)
	envInt("VOCI_MAX_PROSE_CHARS_TOTAL", &cfg.MaxProseCharsTotal)
	envInt("VOCI_SESSION_LINES", &cfg.SessionLines)
	envInt("VOCI_CONTEXT_CACHE_TTL_SECONDS", &cfg.ContextCacheTTLSeconds)
	envInt("VOCI_ENTITY_TOKEN_CAP", &cfg.EntityTokenCap)
	envInt("VOCI_ENTITY_MIN_TOKEN_LEN", &cfg.EntityMinTokenLen)
	envFloat("VOCI_VAD_THRESHOLD", &cfg.VADThreshold)
	envInt("VOCI_MIN_AUDIO_MS", &cfg.MinAudioMs)

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
				if cfg.MaxProseTurns == 0 && fc.MaxProseTurns != 0 {
					cfg.MaxProseTurns = fc.MaxProseTurns
				}
				if cfg.MaxProseCharsPerTurn == 0 && fc.MaxProseCharsPerTurn != 0 {
					cfg.MaxProseCharsPerTurn = fc.MaxProseCharsPerTurn
				}
				if cfg.MaxProseCharsTotal == 0 && fc.MaxProseCharsTotal != 0 {
					cfg.MaxProseCharsTotal = fc.MaxProseCharsTotal
				}
				if cfg.SessionLines == 0 && fc.SessionLines != 0 {
					cfg.SessionLines = fc.SessionLines
				}
				if cfg.ContextCacheTTLSeconds == 0 && fc.ContextCacheTTLSeconds != 0 {
					cfg.ContextCacheTTLSeconds = fc.ContextCacheTTLSeconds
				}
				if cfg.EntityTokenCap == 0 && fc.EntityTokenCap != 0 {
					cfg.EntityTokenCap = fc.EntityTokenCap
				}
				if cfg.EntityMinTokenLen == 0 && fc.EntityMinTokenLen != 0 {
					cfg.EntityMinTokenLen = fc.EntityMinTokenLen
				}
				if cfg.VADThreshold == 0 && fc.VADThreshold != 0 {
					cfg.VADThreshold = fc.VADThreshold
				}
				if cfg.MinAudioMs == 0 && fc.MinAudioMs != 0 {
					cfg.MinAudioMs = fc.MinAudioMs
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
	if cfg.MaxProseTurns == 0 {
		cfg.MaxProseTurns = 6
	}
	if cfg.MaxProseCharsPerTurn == 0 {
		cfg.MaxProseCharsPerTurn = 500
	}
	if cfg.MaxProseCharsTotal == 0 {
		cfg.MaxProseCharsTotal = 3000
	}
	if cfg.SessionLines == 0 {
		cfg.SessionLines = 100
	}
	if cfg.ContextCacheTTLSeconds == 0 {
		cfg.ContextCacheTTLSeconds = 60
	}
	if cfg.EntityTokenCap == 0 {
		cfg.EntityTokenCap = 30
	}
	if cfg.EntityMinTokenLen == 0 {
		cfg.EntityMinTokenLen = 4
	}
	if cfg.VADThreshold == 0 {
		cfg.VADThreshold = 0.01
	}
	if cfg.MinAudioMs == 0 {
		cfg.MinAudioMs = 300
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
