package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
)

const DefaultAPIURL = "https://api.siliconflow.cn/v1/audio/transcriptions"

const modelTeleSpeech = `TeleAI/TeleSpeechASR`

var languageModel = map[string]string{
	"zh": modelTeleSpeech,
}

const defaultASRModel = "openai/whisper-large-v3"

type transcribeResponse struct {
	Text string `json:"text"`
}

// Transcribe sends an audio file to SiliconFlow ASR and returns the transcribed text.
// apiURL defaults to the SiliconFlow endpoint if empty.
// language selects the ASR model: "zh" uses TeleSpeechASR, all others use whisper-large-v3.
// Returns empty string on error (errors are logged).
func Transcribe(ctx context.Context, key, audioPath, apiURL, language string) string {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}

	model := defaultASRModel
	if m, ok := languageModel[language]; ok {
		model = m
	}

	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		log.Printf("asr: read audio: %v", err)
		return ""
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	// Add model field
	if err := w.WriteField("model", model); err != nil {
		log.Printf("asr: write model field: %v", err)
		return ""
	}

	// Add file field
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		log.Printf("asr: create form file: %v", err)
		return ""
	}
	if _, err := fw.Write(audioData); err != nil {
		log.Printf("asr: write audio data: %v", err)
		return ""
	}

	if err := w.Close(); err != nil {
		log.Printf("asr: close multipart: %v", err)
		return ""
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, &body)
	if err != nil {
		log.Printf("asr: new request: %v", err)
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", w.FormDataContentType())

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
