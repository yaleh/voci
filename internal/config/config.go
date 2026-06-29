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
}

type fileConfig struct {
	SiliconFlowAPIKey string `yaml:"siliconflow_api_key"`
	OllamaHost        string `yaml:"ollama_host"`
	Language          string `yaml:"language"`
	ASRProvider       string `yaml:"asr_provider"`
	ASRAPIKey         string `yaml:"asr_api_key"`
	ASRAPIURL         string `yaml:"asr_api_url"`
	ASRModel          string `yaml:"asr_model"`
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

	// Try to load from file
	home, err := os.UserHomeDir()
	if err == nil {
		cfgPath := filepath.Join(home, ".config", "voci", "config.yaml")
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
