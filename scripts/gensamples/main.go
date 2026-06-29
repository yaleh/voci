package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/yaleh/voci/internal/config"
)

const apiURL = "https://api.siliconflow.cn/v1/audio/speech"

func main() {
	cases, err := LoadCases("testdata/testcases.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load cases error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll("testdata", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir error: %v\n", err)
		os.Exit(1)
	}

	samples := make([]struct {
		filename string
		text     string
		voice    string
	}, len(cases))
	for i, c := range cases {
		samples[i].filename = c.ID + ".wav"
		samples[i].text = c.TTSInput
		samples[i].voice = c.Voice
	}

	for _, s := range samples {
		outPath := "testdata/" + s.filename
		if _, err := os.Stat(outPath); err == nil {
			fmt.Printf("skip %s (already exists)\n", outPath)
			continue
		}

		fmt.Printf("generating %s ...\n", outPath)
		audioData, err := generateSpeech(cfg.SiliconFlowKey, s.text, s.voice)
		if err != nil {
			fmt.Fprintf(os.Stderr, "TTS error for %s: %v\n", s.filename, err)
			os.Exit(1)
		}

		if err := os.WriteFile(outPath, audioData, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  wrote %d bytes to %s\n", len(audioData), outPath)
	}

	fmt.Println("done")
}

type speechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed"`
}

const defaultVoice = "FunAudioLLM/CosyVoice2-0.5B:claire"

func generateSpeech(apiKey, text, voice string) ([]byte, error) {
	if voice == "" {
		voice = defaultVoice
	}
	reqBody := speechRequest{
		Model:          "FunAudioLLM/CosyVoice2-0.5B",
		Input:          text,
		Voice:          voice,
		ResponseFormat: "wav",
		Speed:          1.0,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, body)
	}

	return io.ReadAll(resp.Body)
}
