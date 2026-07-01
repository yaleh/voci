// Command experiment-asr-context runs a controlled experiment to test whether
// adding Claude Code session context to the Gemini ASR prompt improves
// recognition accuracy for technical vocabulary.
//
// Three prompt variants are tested:
//
//	V0 (baseline): entities list only
//	V1 (structured context): entities + file paths + commands (no prose)
//	V2 (full context): entities + file paths + commands + last 3 prose turns
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Prompt templates
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

const v0Prompt = `You are a voice assistant. Transcribe the following audio exactly as spoken, preserving ALL known technical terms EXACTLY as listed (case-sensitive).

Example: if the audio contains "打开 internal/asr/gemini.go" and the known term is "gemini.go", the correct transcript must use "gemini.go" exactly.

Known technical terms: {ENTITIES}

Return ONLY this JSON:
{"transcript": "..."}`

const v1Prompt = `You are a voice assistant. Transcribe the following audio exactly as spoken, preserving ALL known technical terms EXACTLY as listed (case-sensitive).

Known technical terms: {ENTITIES}

## Claude Code Session Context
### Recently Edited Files
{FILES}

### Recent Commands
{COMMANDS}

Return ONLY this JSON:
{"transcript": "..."}`

const v2Prompt = `You are a voice assistant. Transcribe the following audio exactly as spoken, preserving ALL known technical terms EXACTLY as listed (case-sensitive).

Known technical terms: {ENTITIES}

## Claude Code Session Context
### Recently Edited Files
{FILES}

### Recent Commands
{COMMANDS}

### Recent Conversation
{PROSE}

Return ONLY this JSON:
{"transcript": "..."}`

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Data types
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

// GroundTruthEntry is one line of ground_truth.jsonl.
type GroundTruthEntry struct {
	Audio       string `json:"audio"`
	Category    string `json:"category"`
	GroundTruth string `json:"ground_truth"`
}

// ExperimentResult is one line of results.jsonl.
type ExperimentResult struct {
	Audio      string `json:"audio"`
	Category   string `json:"category"`
	Variant    string `json:"variant"`
	Transcript string `json:"transcript"`
	ExactMatch bool   `json:"exact_match"`
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	LatencyMs    int64 `json:"latency_ms"`
}

// SessionHint holds the parsed session_hint.txt content.
type SessionHint struct {
	Entities string
	Files    string
	Commands string
	Prose    string
}

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Gemini API types (self-contained, not imported from internal/asr)
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

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

type geminiGenConfig struct {
	ResponseMimeType string `json:"response_mime_type"`
}

type geminiExperimentRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiResponsePart struct {
	Text string `json:"text"`
}

type geminiResponseContent struct {
	Parts []geminiResponsePart `json:"parts"`
}

type geminiCandidate struct {
	Content geminiResponseContent `json:"content"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiExperimentResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiTranscriptResult struct {
	Transcript string `json:"transcript"`
}

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Normalisation
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

var punctRE = regexp.MustCompile(`[，。！？、；：""''（）【】《》\.,!?;:'"()\[\]{}<> ]+`)

// normalise removes punctuation and whitespace for exact-match comparison.
func normalise(s string) string {
	s = strings.TrimSpace(s)
	s = punctRE.ReplaceAllString(s, "")
	return strings.ToLower(s)
}

// isExactMatch compares transcript to ground truth ignoring punctuation, whitespace, and case.
func isExactMatch(transcript, groundTruth string) bool {
	return normalise(transcript) == normalise(groundTruth)
}

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Session hint parsing
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

func parseSessionHint(path string) (*SessionHint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session hint: %w", err)
	}
	text := string(data)

	hint := &SessionHint{}

	// Extract entities section.
	if idx := strings.Index(text, "## Known Entities"); idx >= 0 {
		section := text[idx:]
		if end := strings.Index(section, "\n## "); end > 0 {
			section = section[:end]
		}
		var lines []string
		for _, line := range strings.Split(section, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") {
				entry := strings.TrimPrefix(line, "- ")
				if colonIdx := strings.Index(entry, ": "); colonIdx >= 0 {
					canonical := strings.TrimSpace(entry[colonIdx+2:])
					if canonical != "" {
						lines = append(lines, canonical)
					}
				}
			}
		}
		hint.Entities = strings.Join(lines, ", ")
	}

	// Extract files from "### Recently Edited Files" section.
	hint.Files = extractSection(text, "### Recently Edited Files")

	// Extract commands from "### Recent Commands" section.
	hint.Commands = extractSection(text, "### Recent Commands")

	// Extract prose from "### Recent Conversation" section.
	hint.Prose = extractSection(text, "### Recent Conversation")

	return hint, nil
}

func extractSection(text, header string) string {
	idx := strings.Index(text, header)
	if idx < 0 {
		return ""
	}
	section := text[idx+len(header):]
	// Stop at next "### " or "## " header.
	nextHeader := strings.Index(section, "\n### ")
	nextHeader2 := strings.Index(section, "\n## ")
	end := len(section)
	if nextHeader >= 0 && nextHeader < end {
		end = nextHeader
	}
	if nextHeader2 >= 0 && nextHeader2 < end {
		end = nextHeader2
	}
	return strings.TrimSpace(section[:end])
}

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Gemini API call
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

const defaultGeminiModel = "gemini-2.5-flash"
const geminiAPIURLTemplate = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"

func callGeminiForTranscript(ctx context.Context, apiKey, model, apiURL, systemPrompt, audioPath string) (string, int, int, error) {
	if model == "" {
		model = defaultGeminiModel
	}
	if apiURL == "" {
		apiURL = strings.ReplaceAll(geminiAPIURLTemplate, "{model}", model)
	}

	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", 0, 0, fmt.Errorf("read audio: %w", err)
	}

	payload := geminiExperimentRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
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
		return "", 0, 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", 0, 0, fmt.Errorf("API error %d: %s", resp.StatusCode, bodyBytes)
	}

	var gemResp geminiExperimentResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return "", 0, 0, fmt.Errorf("decode response: %w", err)
	}

	inputTokens := 0
	outputTokens := 0
	if gemResp.UsageMetadata != nil {
		inputTokens = gemResp.UsageMetadata.PromptTokenCount
		outputTokens = gemResp.UsageMetadata.CandidatesTokenCount
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", inputTokens, outputTokens, fmt.Errorf("empty candidates")
	}

	text := gemResp.Candidates[0].Content.Parts[0].Text

	var result geminiTranscriptResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return "", inputTokens, outputTokens, fmt.Errorf("unmarshal inner JSON %q: %w", text, err)
	}

	return result.Transcript, inputTokens, outputTokens, nil
}

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Prompt building
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

func buildPrompt(variant string, hint *SessionHint) string {
	entities := hint.Entities
	if entities == "" {
		entities = "(none)"
	}

	switch variant {
	case "V0":
		return strings.ReplaceAll(v0Prompt, "{ENTITIES}", entities)
	case "V1":
		p := strings.ReplaceAll(v1Prompt, "{ENTITIES}", entities)
		p = strings.ReplaceAll(p, "{FILES}", hint.Files)
		p = strings.ReplaceAll(p, "{COMMANDS}", hint.Commands)
		return p
	case "V2":
		p := strings.ReplaceAll(v2Prompt, "{ENTITIES}", entities)
		p = strings.ReplaceAll(p, "{FILES}", hint.Files)
		p = strings.ReplaceAll(p, "{COMMANDS}", hint.Commands)
		prose := hint.Prose
		if prose == "" {
			prose = "(none)"
		}
		p = strings.ReplaceAll(p, "{PROSE}", prose)
		return p
	default:
		log.Fatalf("unknown variant: %s (expected V0, V1, or V2)", variant)
		return ""
	}
}

// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――
// Main
// ―――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――――

func main() {
	audioDir := flag.String("audio-dir", "", "Path to directory containing audio files")
	groundTruth := flag.String("ground-truth", "", "Path to ground_truth.jsonl")
	sessionHint := flag.String("session-hint", "", "Path to session_hint.txt")
	apiKey := flag.String("api-key", "", "Gemini API key (or set ASR_API_KEY env var)")
	variantsStr := flag.String("variants", "V0,V1,V2", "Comma-separated list of variants")
	output := flag.String("output", "scripts/experiment-asr-context/results.jsonl", "Results output path")
	delayMs := flag.Int("delay", 1000, "Delay between API calls in ms")
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("ASR_API_KEY")
	}
	if *apiKey == "" {
		log.Fatal("--api-key is required or set ASR_API_KEY env var")
	}
	if *audioDir == "" {
		log.Fatal("--audio-dir is required")
	}
	if *groundTruth == "" {
		log.Fatal("--ground-truth is required")
	}
	if *sessionHint == "" {
		log.Fatal("--session-hint is required")
	}

	// Parse variants.
	variants := strings.Split(*variantsStr, ",")
	for i := range variants {
		variants[i] = strings.TrimSpace(variants[i])
		if variants[i] != "V0" && variants[i] != "V1" && variants[i] != "V2" {
			log.Fatalf("invalid variant %q (expected V0, V1, or V2)", variants[i])
		}
	}

	// Read ground truth.
	gtFile, err := os.Open(*groundTruth)
	if err != nil {
		log.Fatalf("open ground truth: %v", err)
	}
	defer gtFile.Close()

	var entries []GroundTruthEntry
	scanner := bufio.NewScanner(gtFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry GroundTruthEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Fatalf("parse ground truth line: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("read ground truth: %v", err)
	}

	// Parse session hint.
	hint, err := parseSessionHint(*sessionHint)
	if err != nil {
		log.Fatalf("parse session hint: %v", err)
	}

	// Open output file for appending.
	outFile, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("open output: %v", err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)

	// Sort entries for consistent ordering.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Audio < entries[j].Audio })

	total := len(entries) * len(variants)
	done := 0
	startTime := time.Now()

	for _, entry := range entries {
		audioPath := filepath.Join(*audioDir, entry.Audio)

		for _, variant := range variants {
			prompt := buildPrompt(variant, hint)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			t0 := time.Now()
			transcript, inputTokens, outputTokens, err := callGeminiForTranscript(
				ctx, *apiKey, defaultGeminiModel, "", prompt, audioPath,
			)
			cancel()
			latency := time.Since(t0).Milliseconds()

			result := ExperimentResult{
				Audio:      entry.Audio,
				Category:   entry.Category,
				Variant:    variant,
				Transcript: transcript,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				LatencyMs:    latency,
			}

			if err != nil {
				log.Printf("WARN: %s/%s: %v", entry.Audio, variant, err)
				result.Transcript = fmt.Sprintf("ERROR: %v", err)
				result.ExactMatch = false
			} else {
				result.ExactMatch = isExactMatch(transcript, entry.GroundTruth)
			}

			if err := encoder.Encode(result); err != nil {
				log.Fatalf("write result: %v", err)
			}

			done++
			elapsed := time.Since(startTime).Round(time.Second)
			log.Printf("[%d/%d] %s %s match=%v latency=%dms (elapsed %v)",
				done, total, entry.Audio, variant, result.ExactMatch, latency, elapsed)

			// Delay between calls to avoid rate limits.
			if done < total {
				time.Sleep(time.Duration(*delayMs) * time.Millisecond)
			}
		}
	}

	log.Printf("Experiment complete. Results written to %s (%d lines)", *output, done)
}
