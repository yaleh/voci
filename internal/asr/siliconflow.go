package asr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
)

const DefaultAPIURL = "https://api.siliconflow.cn/v1/audio/transcriptions"
const DefaultOpenRouterAPIURL = "https://openrouter.ai/api/v1/audio/transcriptions"
const DefaultOpenRouterModel = "openai/whisper-large-v3-turbo"

const modelTeleSpeech = `TeleAI/TeleSpeechASR`

var languageModel = map[string]string{
	"zh": modelTeleSpeech,
}

const defaultASRModel = "openai/whisper-large-v3"

type transcribeResponse struct {
	Text string `json:"text"`
}

func buildMultipartRequest(ctx context.Context, key, audioPath, apiURL, model string) (*http.Request, error) {
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	if err := w.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}

	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(audioData); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req, nil
}

func buildJSONRequest(ctx context.Context, key, audioPath, apiURL, model string) (*http.Request, error) {
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	audiob64 := base64.StdEncoding.EncodeToString(audioData)
	body, err := json.Marshal(map[string]interface{}{
		"model":       model,
		"input_audio": map[string]string{"data": audiob64, "format": "wav"},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// Transcribe sends an audio file to an ASR provider and returns the transcribed text.
// provider selects the request format: "openrouter" uses JSON+base64; others use multipart/form-data.
// apiURL defaults to the provider's default endpoint if empty.
// model overrides the provider default when non-empty; for siliconflow, language selects the default model.
// Returns empty string on error (errors are logged).
func Transcribe(ctx context.Context, key, audioPath, apiURL, language, provider, model string) string {
	var req *http.Request
	var err error

	if provider == "gemini" {
		return TranscribeGemini(ctx, key, audioPath, apiURL, language, model)
	} else if provider == "openrouter" {
		if apiURL == "" {
			apiURL = DefaultOpenRouterAPIURL
		}
		if model == "" {
			model = DefaultOpenRouterModel
		}
		req, err = buildJSONRequest(ctx, key, audioPath, apiURL, model)
	} else {
		if apiURL == "" {
			apiURL = DefaultAPIURL
		}
		if model == "" {
			model = defaultASRModel
			if m, ok := languageModel[language]; ok {
				model = m
			}
		}
		req, err = buildMultipartRequest(ctx, key, audioPath, apiURL, model)
	}

	if err != nil {
		log.Printf("asr: build request: %v", err)
		return ""
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("asr: http: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("asr: API error %d: %s", resp.StatusCode, bodyBytes)
		return ""
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("asr: decode response: %v", err)
		return ""
	}

	return result.Text
}
