package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Event represents a single voice pipeline result written to the event log.
type Event struct {
	Timestamp     string  `json:"timestamp"`
	Rewritten     string  `json:"rewritten"`
	Kind          string  `json:"kind"`
	RawTranscript string  `json:"raw_transcript"`
	Confidence    float64 `json:"confidence"`
}

// AppendEvent appends ev as a single JSON line to the file at path.
// Parent directories are created if they do not exist.
func AppendEvent(path string, ev Event) error {
	if ev.Timestamp == "" {
		ev.Timestamp = time.Now().Format(time.RFC3339)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	_, err = f.Write(append(data, '\n'))
	return err
}
