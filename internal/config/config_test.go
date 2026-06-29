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
	t.Setenv("ASR_API_KEY", "")
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

func TestLoadConfigASRProviderFromEnv(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("ASR_PROVIDER", "openrouter")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRProvider != "openrouter" {
		t.Errorf("want openrouter, got %q", cfg.ASRProvider)
	}
}

func TestLoadConfigASRAPIKeyFromEnv(t *testing.T) {
	t.Setenv("ASR_API_KEY", "sk-or-test")
	t.Setenv("SILICONFLOW_API_KEY", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRAPIKey != "sk-or-test" {
		t.Errorf("want sk-or-test, got %q", cfg.ASRAPIKey)
	}
}

func TestLoadConfigASRAPIURLFromEnv(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("ASR_API_URL", "https://custom.example/v1")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRAPIURL != "https://custom.example/v1" {
		t.Errorf("want https://custom.example/v1, got %q", cfg.ASRAPIURL)
	}
}

func TestLoadConfigASRModelFromEnv(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("ASR_MODEL", "alibaba/qwen3-asr-flash")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRModel != "alibaba/qwen3-asr-flash" {
		t.Errorf("want alibaba/qwen3-asr-flash, got %q", cfg.ASRModel)
	}
}

func TestLoadConfigASRAPIKeyFallsBackToSiliconFlowKey(t *testing.T) {
	t.Setenv("VOCI_CONFIG", "")
	t.Setenv("HOME", t.TempDir()) // no config.yaml in temp dir
	t.Setenv("ASR_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "sk-sf")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRAPIKey != "sk-sf" {
		t.Errorf("want sk-sf, got %q", cfg.ASRAPIKey)
	}
}

func TestLoadConfigASRProviderDefaultsSiliconflow(t *testing.T) {
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("ASR_PROVIDER", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRProvider != "siliconflow" {
		t.Errorf("want siliconflow, got %q", cfg.ASRProvider)
	}
}

func TestLoadConfigMissingKeyNewFields(t *testing.T) {
	t.Setenv("ASR_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadConfigVociConfigEnvOverridesPath(t *testing.T) {
	t.Setenv("ASR_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("ASR_PROVIDER", "")

	f, err := os.CreateTemp(t.TempDir(), "voci-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("asr_provider: gemini\nasr_api_key: gem-key\n")
	f.Close()

	t.Setenv("VOCI_CONFIG", f.Name())
	// HOME points somewhere without a config.yaml — proves VOCI_CONFIG is used, not HOME
	t.Setenv("HOME", t.TempDir())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ASRProvider != "gemini" {
		t.Errorf("ASRProvider: want gemini, got %q", cfg.ASRProvider)
	}
	if cfg.ASRAPIKey != "gem-key" {
		t.Errorf("ASRAPIKey: want gem-key, got %q", cfg.ASRAPIKey)
	}
}

func TestLoadConfigASRFieldsFromFile(t *testing.T) {
	t.Setenv("ASR_PROVIDER", "")
	t.Setenv("ASR_API_KEY", "")
	t.Setenv("ASR_API_URL", "")
	t.Setenv("ASR_MODEL", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	os.MkdirAll(dir+"/.config/voci", 0755)
	yamlContent := "asr_provider: openrouter\nasr_api_key: sk-from-file\nasr_api_url: https://file.example/v1\nasr_model: alibaba/qwen3-asr-flash\n"
	os.WriteFile(dir+"/.config/voci/config.yaml", []byte(yamlContent), 0644)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ASRProvider != "openrouter" {
		t.Errorf("ASRProvider: want openrouter, got %q", cfg.ASRProvider)
	}
	if cfg.ASRAPIKey != "sk-from-file" {
		t.Errorf("ASRAPIKey: want sk-from-file, got %q", cfg.ASRAPIKey)
	}
	if cfg.ASRAPIURL != "https://file.example/v1" {
		t.Errorf("ASRAPIURL: want https://file.example/v1, got %q", cfg.ASRAPIURL)
	}
	if cfg.ASRModel != "alibaba/qwen3-asr-flash" {
		t.Errorf("ASRModel: want alibaba/qwen3-asr-flash, got %q", cfg.ASRModel)
	}
}
