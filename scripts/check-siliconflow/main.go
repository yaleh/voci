package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	ttsURL = "https://api.siliconflow.cn/v1/audio/speech"
	asrURL = "https://api.siliconflow.cn/v1/audio/transcriptions"
	tmpWav = "/tmp/voci-tts-check.wav"
)

type config struct {
	SiliconFlowKey string `yaml:"siliconflow_api_key"`
}

func loadKey() (string, error) {
	if k := os.Getenv("SILICONFLOW_API_KEY"); k != "" {
		return k, nil
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".config", "voci", "config.yaml"))
	if err != nil {
		return "", fmt.Errorf("no key: set SILICONFLOW_API_KEY or update ~/.config/voci/config.yaml")
	}
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	if cfg.SiliconFlowKey == "" {
		return "", fmt.Errorf("siliconflow_api_key is empty in ~/.config/voci/config.yaml")
	}
	return cfg.SiliconFlowKey, nil
}

func tts(key string) error {
	body, _ := json.Marshal(map[string]any{
		"model":           "FunAudioLLM/CosyVoice2-0.5B",
		"input":           "TASK-1 voci 上下文感知语音改写验证",
		"voice":           "FunAudioLLM/CosyVoice2-0.5B:alex",
		"response_format": "wav",
	})
	req, _ := http.NewRequest("POST", ttsURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("TTS HTTP %d: %s", resp.StatusCode, b)
	}
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpWav, audio, 0644); err != nil {
		return err
	}
	fmt.Printf("TTS OK: %d bytes → %s\n", len(audio), tmpWav)
	return nil
}

func asr(key string) error {
	audio, err := os.ReadFile(tmpWav)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "check.wav")
	if err != nil {
		return err
	}
	if _, err := fw.Write(audio); err != nil {
		return err
	}
	_ = w.WriteField("model", "TeleAI/TeleSpeechASR")
	w.Close()

	req, _ := http.NewRequest("POST", asrURL, &buf)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ASR HTTP %d: %s", resp.StatusCode, b)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Text == "" {
		return fmt.Errorf("ASR returned empty text")
	}
	fmt.Printf("ASR OK: %q\n", result.Text)
	return nil
}

func main() {
	key, err := loadKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := tts(key); err != nil {
		fmt.Fprintf(os.Stderr, "TTS FAIL: %v\n", err)
		os.Exit(1)
	}
	if err := asr(key); err != nil {
		fmt.Fprintf(os.Stderr, "ASR FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ SiliconFlow 服务可用")
}
