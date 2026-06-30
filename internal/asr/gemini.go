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

	intentmodel "github.com/yaleh/voci/internal/intent/model"
)

// mergedPromptTemplate is the system instruction for the merged pipeline call.
// {ENTITIES_PLACEHOLDER} is replaced at runtime with the joined entity list.
const mergedPromptTemplate = `You are a voice assistant pipeline. The following technical terms must appear EXACTLY as listed (case-sensitive) in the transcription.

Example: if the audio contains the phrase "我们用 Sentry 来监控" and the known term is "Sentry", the correct transcript is:
"我们用 Sentry 来监控"

Similarly, if the audio says "run the vocal command" and known terms include "voci", transcribe as "voci", not "vocal".

Known technical terms: {ENTITIES_PLACEHOLDER}

Complete these three steps and return ONLY this JSON (no other text):
1. Transcribe the audio, preserving ALL known technical terms EXACTLY as listed (case-sensitive)
2. Rewrite the transcript into a clean, well-formed instruction (same language; do NOT translate; do NOT add unstated content; do NOT pick specific targets the speaker did not name; if genuinely too vague, start with [ambiguous])
3. Classify the rewritten instruction into exactly one of: direct_prompt / backlog_action / query / ambiguous

Return ONLY this JSON:
{"transcript": "...", "rewritten": "...", "kind": "...", "confidence": 0.0}`

// geminiMergedTestBaseURL, when non-empty, overrides the Gemini API base URL
// for TranscribeMerged. Only set this in tests.
var geminiMergedTestBaseURL string

const DefaultGeminiModel = "gemini-2.5-flash"
const DefaultGeminiAPIURLTemplate = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"

// geminiGenConfig holds generation configuration for a Gemini request.
type geminiGenConfig struct {
	ResponseMimeType string `json:"response_mime_type"`
}

// geminiMergedRequest is the request payload for the merged pipeline call.
// It includes generationConfig to force JSON output.
type geminiMergedRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

// geminiMergedResult is the inner JSON returned by the merged Gemini call.
type geminiMergedResult struct {
	Transcript string  `json:"transcript"`
	Rewritten  string  `json:"rewritten"`
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence"`
}

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
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
}

// geminiChatContent is the per-turn content used for chat (has a role).
type geminiChatContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiChatRequest struct {
	Contents          []geminiChatContent `json:"contents"`
	SystemInstruction *geminiContent      `json:"systemInstruction,omitempty"`
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

func buildGeminiRequest(ctx context.Context, key, audioPath, apiURL, model string, entities []string) (*http.Request, error) {
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	var promptText string
	if len(entities) > 0 {
		promptText = "Transcribe the following audio. Below is an example of correct output format:\n\n" +
			"Example — if the audio contains the phrase \"我们用 Sentry 来监控\" and the " +
			"known term is \"Sentry\", the correct transcript is:\n" +
			"\"我们用 Sentry 来监控\"\n\n" +
			"Known technical terms: " + strings.Join(entities, ", ") + "\n\n" +
			"Now transcribe the actual audio:"
	} else {
		promptText = "Transcribe the following audio."
	}

	payload := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: promptText},
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
// entities, when non-empty, enables Config C few-shot prompt format with the given technical terms.
func TranscribeGemini(ctx context.Context, key, audioPath, apiURL, language, model string, entities []string) string {
	if model == "" {
		model = DefaultGeminiModel
	}
	if apiURL == "" {
		apiURL = strings.ReplaceAll(DefaultGeminiAPIURLTemplate, "{model}", model)
	} else {
		// If a custom base URL is provided (e.g. httptest), append the model path segment.
		apiURL = apiURL + "/v1beta/models/" + model + ":generateContent"
	}

	req, err := buildGeminiRequest(ctx, key, audioPath, apiURL, model, entities)
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

// ExtractEntities parses a hint string and returns canonical entity names from
// "## Known Entities" or "## Known Entities (dynamic)" sections.
// Each line of the form "- spoken: Canonical" contributes the right-hand side (canonical form).
// Returns nil if no entities are found.
func ExtractEntities(hint string) []string {
	lines := strings.Split(hint, "\n")
	inSection := false
	seen := map[string]struct{}{}
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Known Entities") {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "## ") {
				// Next section header: stop
				break
			}
			if strings.HasPrefix(trimmed, "- ") {
				entry := strings.TrimPrefix(trimmed, "- ")
				// Format: "spoken: Canonical" — take right-hand side
				if idx := strings.Index(entry, ": "); idx >= 0 {
					canonical := strings.TrimSpace(entry[idx+2:])
					if canonical != "" {
						if _, exists := seen[canonical]; !exists {
							seen[canonical] = struct{}{}
							result = append(result, canonical)
						}
					}
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// TranscribeMerged performs a single Gemini Audio API call that combines
// transcription, hinted correction, and intent classification into one request.
// The merged system prompt is filled with entities, the audio is sent inline,
// and the response is expected to be JSON with transcript/rewritten/kind/confidence.
// model defaults to DefaultGeminiModel if empty.
func TranscribeMerged(ctx context.Context, key, audioPath, hint, language, model string, entities []string) (intentmodel.ActionProposal, error) {
	if model == "" {
		model = DefaultGeminiModel
	}

	var apiURL string
	if geminiMergedTestBaseURL != "" {
		apiURL = geminiMergedTestBaseURL + "/v1beta/models/" + model + ":generateContent"
	} else {
		apiURL = strings.ReplaceAll(DefaultGeminiAPIURLTemplate, "{model}", model)
	}

	// Fill the prompt template.
	entityStr := strings.Join(entities, ", ")
	prompt := strings.ReplaceAll(mergedPromptTemplate, "{ENTITIES_PLACEHOLDER}", entityStr)

	// Read audio file.
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: read audio: %w", err)
	}

	payload := geminiMergedRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: prompt}},
		},
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{InlineData: &geminiInlineData{
						MimeType: "audio/wav",
						Data:     base64.StdEncoding.EncodeToString(audioData),
					}},
				},
			},
		},
		GenerationConfig: &geminiGenConfig{
			ResponseMimeType: "application/json",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: API error %d: %s", resp.StatusCode, bodyBytes)
	}

	var gemResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: decode response: %w", err)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: empty candidates in response")
	}

	text := gemResp.Candidates[0].Content.Parts[0].Text

	var result geminiMergedResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return intentmodel.ActionProposal{}, fmt.Errorf("asr: merged: unmarshal inner JSON %q: %w", text, err)
	}

	return intentmodel.ActionProposal{
		RawTranscript: result.Transcript,
		Rewritten:     result.Rewritten,
		Kind:          intentmodel.Kind(result.Kind),
		Confidence:    result.Confidence,
	}, nil
}

// GeminiChat sends a multi-turn chat to the Gemini generateContent API.
// roles and contents must be the same length. Accepted roles: "user", "assistant", "system".
// "assistant" is mapped to Gemini's "model" role; "system" turns become systemInstruction.
// model defaults to DefaultGeminiModel if empty.
func GeminiChat(ctx context.Context, key, model string, roles, contents []string) (string, error) {
	if model == "" {
		model = DefaultGeminiModel
	}
	apiURL := strings.ReplaceAll(DefaultGeminiAPIURLTemplate, "{model}", model)

	var sysInstruction *geminiContent
	var turns []geminiChatContent
	for i, role := range roles {
		text := ""
		if i < len(contents) {
			text = contents[i]
		}
		switch role {
		case "system":
			sysInstruction = &geminiContent{Parts: []geminiPart{{Text: text}}}
		case "assistant":
			turns = append(turns, geminiChatContent{Role: "model", Parts: []geminiPart{{Text: text}}})
		default:
			turns = append(turns, geminiChatContent{Role: "user", Parts: []geminiPart{{Text: text}}})
		}
	}

	payload := geminiChatRequest{Contents: turns, SystemInstruction: sysInstruction}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("gemini chat: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini chat: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini chat: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini chat: API error %d: %s", resp.StatusCode, bodyBytes)
	}

	var result geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("gemini chat: decode: %w", err)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini chat: empty response")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}
