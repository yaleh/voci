package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponseChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// Chat sends a chat request to the ollama API and returns the assistant's response.
// It handles streaming NDJSON responses.
func Chat(ctx context.Context, host, model string, messages []Message) (string, error) {
	url := strings.TrimRight(host, "/") + "/api/chat"

	reqBody := chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama error %d", resp.StatusCode)
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk chatResponseChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		sb.WriteString(chunk.Message.Content)
		if chunk.Done {
			break
		}
	}

	return sb.String(), nil
}
