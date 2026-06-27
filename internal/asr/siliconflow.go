package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

const DefaultAPIURL = "https://api.siliconflow.cn/v1/audio/transcriptions"

type transcribeResponse struct {
	Text string `json:"text"`
}

// Transcribe sends an audio file to SiliconFlow ASR and returns the transcribed text.
// apiURL defaults to the SiliconFlow endpoint if empty.
func Transcribe(ctx context.Context, key, audioPath, apiURL string) (string, error) {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}

	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("read audio: %w", err)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	// Add model field
	if err := w.WriteField("model", "TeleAI/TeleSpeechASR"); err != nil {
		return "", fmt.Errorf("write model field: %w", err)
	}

	// Add file field
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(audioData); err != nil {
		return "", fmt.Errorf("write audio data: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, &body)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, bodyBytes)
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.Text, nil
}
