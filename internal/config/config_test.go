package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "sk-env-test")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SiliconFlowKey != "sk-env-test" {
		t.Errorf("expected sk-env-test, got %q", cfg.SiliconFlowKey)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	// Unset env to force file read
	t.Setenv("SILICONFLOW_API_KEY", "")

	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, ".config", "voci")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("siliconflow_api_key: sk-test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Override home directory by patching UserHomeDir behaviour via temp approach.
	// Since we can't easily mock os.UserHomeDir, we test via the env override path here.
	// We set HOME to tmpDir so os.UserHomeDir uses it.
	t.Setenv("HOME", tmpDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SiliconFlowKey != "sk-test" {
		t.Errorf("expected sk-test, got %q", cfg.SiliconFlowKey)
	}
}

func TestLoadConfigMissingKey(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "")
	// Point HOME to a temp dir with no config file
	t.Setenv("HOME", t.TempDir())

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if msg := err.Error(); len(msg) == 0 {
		t.Fatal("error message is empty")
	}
}
