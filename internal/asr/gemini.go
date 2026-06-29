package asr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

const DefaultGeminiModel = "gemini-2.5-flash"
const DefaultGeminiAPIURLTemplate = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"

// Request structs

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

// Response structs

type geminiResponsePart struct {
	Text string `json:"text"`
}

type geminiResponseContent struct {
	Parts []geminiResponsePart `json:"parts"`
}

type geminiCandidate struct {
	Content geminiResponseContent `json:"content"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

func buildGeminiRequest(ctx context.Context, key, audioPath, apiURL, model string) (*http.Request, error) {
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	payload := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: "Transcribe the following audio."},
					{InlineData: &geminiInlineData{
						MimeType: "audio/wav",
						Data:     base64.StdEncoding.EncodeToString(audioData),
					}},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", key)
	return req, nil
}

// TranscribeGemini sends an audio file to the Gemini generateContent API and returns the transcription.
// apiURL defaults to DefaultGeminiAPIURLTemplate with model substituted if empty.
// model defaults to DefaultGeminiModel if empty.
// Auth uses x-goog-api-key header; key is never written to the URL.
func TranscribeGemini(ctx context.Context, key, audioPath, apiURL, language, model string) string {
	if model == "" {
		model = DefaultGeminiModel
	}
	if apiURL == "" {
		apiURL = strings.ReplaceAll(DefaultGeminiAPIURLTemplate, "{model}", model)
	} else {
		// If a custom base URL is provided (e.g. httptest), append the model path segment.
		apiURL = apiURL + "/v1beta/models/" + model + ":generateContent"
	}

	req, err := buildGeminiRequest(ctx, key, audioPath, apiURL, model)
	if err != nil {
		log.Printf("asr: gemini: build request: %v", err)
		return ""
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("asr: gemini: http: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("asr: gemini: API error %d: %s", resp.StatusCode, bodyBytes)
		return ""
	}

	var result geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("asr: gemini: decode response: %v", err)
		return ""
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		log.Printf("asr: gemini: empty candidates in response")
		return ""
	}

	return result.Candidates[0].Content.Parts[0].Text
}
