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

func TestLoadConfigLanguageFromEnv(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("VOCI_LANGUAGE", "en")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" {
		t.Errorf("want en, got %q", cfg.Language)
	}
}

func TestLoadConfigLanguageFromFile(t *testing.T) {
	// Clear env, write yaml with language: fr, set HOME to tmpdir
	t.Setenv("VOCI_LANGUAGE", "")
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	os.MkdirAll(dir+"/.config/voci", 0755)
	os.WriteFile(dir+"/.config/voci/config.yaml", []byte("language: fr\n"), 0644)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "fr" {
		t.Errorf("want fr, got %q", cfg.Language)
	}
}

func TestLoadConfigLanguageDefault(t *testing.T) {
	t.Setenv("VOCI_LANGUAGE", "")
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "zh" {
		t.Errorf("want zh, got %q", cfg.Language)
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
