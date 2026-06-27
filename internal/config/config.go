package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds runtime configuration for voci.
type Config struct {
	SiliconFlowKey string
	OllamaHost     string
}

type fileConfig struct {
	SiliconFlowAPIKey string `yaml:"siliconflow_api_key"`
	OllamaHost        string `yaml:"ollama_host"`
}

// LoadConfig reads configuration from environment variables first,
// then falls back to ~/.config/voci/config.yaml.
// The API key is never printed to stdout/stderr.
func LoadConfig() (Config, error) {
	cfg := Config{}

	// Read SILICONFLOW_API_KEY from env
	cfg.SiliconFlowKey = os.Getenv("SILICONFLOW_API_KEY")

	// Read OLLAMA_HOST from env
	cfg.OllamaHost = os.Getenv("OLLAMA_HOST")

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
			}
		}
	}

	// Default ollama host
	if cfg.OllamaHost == "" {
		cfg.OllamaHost = "http://localhost:11434"
	}

	if cfg.SiliconFlowKey == "" {
		return cfg, fmt.Errorf("SiliconFlow API key not found: set SILICONFLOW_API_KEY env or add siliconflow_api_key to ~/.config/voci/config.yaml")
	}

	return cfg, nil
}
