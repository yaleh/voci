package daemon

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendEvent_WritesOneJSONLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.log")

	ev := Event{
		Rewritten:     "add feature X",
		Kind:          "direct_prompt",
		RawTranscript: "add featur X",
		Confidence:    0.9,
	}

	if err := AppendEvent(path, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Event
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Rewritten != ev.Rewritten {
		t.Errorf("Rewritten: got %q, want %q", got.Rewritten, ev.Rewritten)
	}
	if got.Kind != ev.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, ev.Kind)
	}
	if got.RawTranscript != ev.RawTranscript {
		t.Errorf("RawTranscript: got %q, want %q", got.RawTranscript, ev.RawTranscript)
	}
}

func TestAppendEvent_AppendsNotTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.log")

	ev1 := Event{Rewritten: "first", Kind: "direct_prompt", RawTranscript: "first raw"}
	ev2 := Event{Rewritten: "second", Kind: "query", RawTranscript: "second raw"}

	if err := AppendEvent(path, ev1); err != nil {
		t.Fatalf("AppendEvent 1: %v", err)
	}
	if err := AppendEvent(path, ev2); err != nil {
		t.Fatalf("AppendEvent 2: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	var lines []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		lines = append(lines, ev)
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Rewritten != "first" {
		t.Errorf("first line Rewritten: got %q, want %q", lines[0].Rewritten, "first")
	}
	if lines[1].Rewritten != "second" {
		t.Errorf("second line Rewritten: got %q, want %q", lines[1].Rewritten, "second")
	}
}

func TestAppendEvent_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "events.log")

	ev := Event{Rewritten: "test", Kind: "direct_prompt", RawTranscript: "test"}
	if err := AppendEvent(path, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestAppendEvent_EventHasTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.log")

	ev := Event{Rewritten: "test", Kind: "direct_prompt", RawTranscript: "test"}
	if err := AppendEvent(path, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Event
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Timestamp == "" {
		t.Error("Timestamp is empty")
	}

	// Verify it's a valid RFC3339 timestamp
	if _, err := time.Parse(time.RFC3339, got.Timestamp); err != nil {
		t.Errorf("Timestamp %q is not valid RFC3339: %v", got.Timestamp, err)
	}
}
