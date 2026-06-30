package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/yaleh/voci/internal/ollama"
)

// RunHintedVariantA uses output-only JSON format: natural language input,
// JSON {"corrected": "..."} output. The system prompt enforces JSON-only output
// to prevent the LLM from treating the user message as a conversational query.
func RunHintedVariantA(ctx context.Context, raw, hint string, chatFn ChatFn) (string, error) {
	var sys strings.Builder
	sys.WriteString("You are an ASR correction assistant.\n\n")
	sys.WriteString("## Instructions\n")
	sys.WriteString("Correct ASR transcription errors using the hint context below.\n")
	sys.WriteString("For each entry in '## Known Entities' formatted as `spoken-form: canonical-form`,\n")
	sys.WriteString("replace occurrences of the spoken-form with the exact canonical spelling.\n")
	sys.WriteString("Apply all substitutions first, then fix remaining grammar.\n")
	sys.WriteString("When multiple candidates of the same kind could match a phrase, choose the candidate whose spoken form most closely matches the exact words in the transcription.\n")
	sys.WriteString("Only substitute a package path such as 'internal/xxx' when the transcription explicitly refers to a Go package or import path.\n")
	sys.WriteString("\n## Output Format\n")
	sys.WriteString("Return ONLY a JSON object with this exact structure:\n")
	sys.WriteString(`{"corrected": "<corrected transcription here>"}`)
	sys.WriteString("\nDo not include any other text, explanation, or markdown.\n")
	if hint != "" {
		sys.WriteString("\n")
		sys.WriteString(hint)
	}

	messages := []ollama.Message{
		{Role: "system", Content: sys.String()},
		{Role: "user", Content: fmt.Sprintf("Transcription: %s", raw)},
	}

	resp, err := chatFn(ctx, messages)
	if err != nil {
		return "", err
	}
	return parseJSONCorrection(resp, raw)
}

// RunHintedVariantB uses full JSON input+output format (recommended): the raw
// transcript is wrapped as a data field {"raw_transcript": ..., "context": ...},
// eliminating any conversational framing that would cause the LLM to answer
// rather than correct.
func RunHintedVariantB(ctx context.Context, raw, hint string, chatFn ChatFn) (string, error) {
	var sys strings.Builder
	sys.WriteString("You are an ASR correction assistant.\n\n")
	sys.WriteString("## Instructions\n")
	sys.WriteString("You receive a JSON object with a 'raw_transcript' field (the ASR output to correct)\n")
	sys.WriteString("and a 'context' field (the correction hint with Known Entities).\n")
	sys.WriteString("For each entry in Known Entities formatted as `spoken-form: canonical-form`,\n")
	sys.WriteString("replace occurrences of the spoken-form in raw_transcript with the exact canonical spelling.\n")
	sys.WriteString("Apply all substitutions first, then fix remaining grammar.\n")
	sys.WriteString("When multiple candidates of the same kind could match a phrase, choose the candidate whose spoken form most closely matches the exact words in raw_transcript.\n")
	sys.WriteString("Only substitute a package path such as 'internal/xxx' when raw_transcript explicitly refers to a Go package or import path.\n")
	sys.WriteString("\n## Output Format\n")
	sys.WriteString("Return ONLY a JSON object with this exact structure:\n")
	sys.WriteString(`{"corrected": "<corrected transcription here>"}`)
	sys.WriteString("\nDo not include any other text, explanation, or markdown.\n")

	inputJSON, _ := json.Marshal(map[string]string{
		"raw_transcript": raw,
		"context":        hint,
	})

	messages := []ollama.Message{
		{Role: "system", Content: sys.String()},
		{Role: "user", Content: string(inputJSON)},
	}

	resp, err := chatFn(ctx, messages)
	if err != nil {
		return "", err
	}
	return parseJSONCorrection(resp, raw)
}

// RunHintedVariantC uses XML tag delimiters: input is wrapped in
// <raw_transcript> and <context> tags, output is expected inside <corrected>.
// This gives the LLM a clear structural signal that the transcript is data,
// not a conversational message to respond to.
func RunHintedVariantC(ctx context.Context, raw, hint string, chatFn ChatFn) (string, error) {
	var sys strings.Builder
	sys.WriteString("You are an ASR correction assistant.\n\n")
	sys.WriteString("## Instructions\n")
	sys.WriteString("You receive input wrapped in XML tags:\n")
	sys.WriteString("- <raw_transcript>: the ASR output to correct\n")
	sys.WriteString("- <context>: the correction hint with Known Entities\n")
	sys.WriteString("For each entry in Known Entities formatted as `spoken-form: canonical-form`,\n")
	sys.WriteString("replace occurrences of the spoken-form in raw_transcript with the exact canonical spelling.\n")
	sys.WriteString("Apply all substitutions first, then fix remaining grammar.\n")
	sys.WriteString("When multiple candidates of the same kind could match a phrase, choose the candidate whose spoken form most closely matches the exact words in raw_transcript.\n")
	sys.WriteString("Only substitute a package path such as 'internal/xxx' when raw_transcript explicitly refers to a Go package or import path.\n")
	sys.WriteString("\n## Output Format\n")
	sys.WriteString("Return ONLY a single XML element with this exact structure:\n")
	sys.WriteString("<corrected>corrected transcription here</corrected>\n")
	sys.WriteString("Do not include any other text, explanation, or markdown.\n")

	var userMsg strings.Builder
	userMsg.WriteString("<raw_transcript>")
	userMsg.WriteString(raw)
	userMsg.WriteString("</raw_transcript>")
	if hint != "" {
		userMsg.WriteString("\n<context>")
		userMsg.WriteString(hint)
		userMsg.WriteString("</context>")
	}

	messages := []ollama.Message{
		{Role: "system", Content: sys.String()},
		{Role: "user", Content: userMsg.String()},
	}

	resp, err := chatFn(ctx, messages)
	if err != nil {
		return "", err
	}
	return parseXMLCorrection(resp, raw)
}

var xmlCorrectedRe = regexp.MustCompile(`(?s)<corrected>(.*?)</corrected>`)

// parseJSONCorrection extracts the "corrected" field from a JSON response.
// Falls back to the raw response string if parsing fails.
func parseJSONCorrection(resp, fallback string) (string, error) {
	resp = strings.TrimSpace(resp)
	// Strip markdown code fences if present
	if strings.HasPrefix(resp, "```") {
		resp = strings.Trim(resp, "`")
		if idx := strings.Index(resp, "\n"); idx >= 0 {
			resp = resp[idx+1:]
		}
	}
	var result struct {
		Corrected string `json:"corrected"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err == nil && result.Corrected != "" {
		return result.Corrected, nil
	}
	// Fallback: return raw response
	return strings.TrimSpace(resp), nil
}

// parseXMLCorrection extracts content inside <corrected>...</corrected>.
// Falls back to the raw response string if parsing fails.
func parseXMLCorrection(resp, fallback string) (string, error) {
	m := xmlCorrectedRe.FindStringSubmatch(resp)
	if m != nil {
		return strings.TrimSpace(m[1]), nil
	}
	return strings.TrimSpace(resp), nil
}
