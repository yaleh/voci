package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/intent"
)

// Phase A: static resource serving via go:embed + FileServerFS

func TestHandler_ServesIndexHTML(t *testing.T) {
	srv := makeEmitServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: got %q, want text/html prefix", ct)
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(body.String(), "<title>voci</title>") {
		s := body.String()
		if len(s) > 200 {
			s = s[:200]
		}
		t.Errorf("body missing <title>voci</title>; got %q", s)
	}
}

func TestHandler_ServesRecorderJS(t *testing.T) {
	srv := makeEmitServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/recorder.js")
	if err != nil {
		t.Fatalf("GET /recorder.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type: got %q, want to contain 'javascript'", ct)
	}
}

func TestHandler_APIRoutesNotShadowed(t *testing.T) {
	var buf bytes.Buffer
	srv := makeEmitServerWithWriter(t, &buf)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/voice/transcribe", "application/octet-stream", bytes.NewReader([]byte("fake audio")))
	if err != nil {
		t.Fatalf("POST /api/voice/transcribe: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var proposal intent.ActionProposal
	if err := json.NewDecoder(resp.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode ActionProposal: %v", err)
	}
}

// Phase C: embedded asset content assertions

func TestEmbeddedAssets_NonEmpty(t *testing.T) {
	idx, err := embeddedFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read web/index.html: %v", err)
	}
	if len(idx) == 0 {
		t.Error("web/index.html is empty")
	}

	rec, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read web/recorder.js: %v", err)
	}
	if len(rec) == 0 {
		t.Error("web/recorder.js is empty")
	}
}

func TestEmbeddedIndex_ReferencesRecorderAndFields(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read web/index.html: %v", err)
	}
	body := string(data)

	checks := []string{"recorder.js", "voci-compose", "voci-dialogue"}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
}

func TestEmbeddedRecorder_UsesContract(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read web/recorder.js: %v", err)
	}
	body := string(data)

	checks := []string{
		"/api/voice/transcribe",
		"/api/voice/emit",
		"MediaRecorder",
		"composeEl",
		"/api/context",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("recorder.js missing %q", want)
		}
	}
}

func TestEmbeddedRecorder_HasAuthHeader(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read recorder.js: %v", err)
	}
	if !strings.Contains(string(data), "Authorization") {
		t.Error("recorder.js missing Authorization header")
	}
}

func TestEmbeddedRecorder_HasLocalStorageToken(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read recorder.js: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "localStorage") {
		t.Error("recorder.js missing localStorage")
	}
	if !strings.Contains(body, "voci_token") {
		t.Error("recorder.js missing voci_token")
	}
}

func TestEmbeddedIndex_HasTokenInputUI(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(data), "voci-token") {
		t.Error("index.html missing voci-token element")
	}
}

// helpers

func makeEmitServer(t *testing.T) *Server {
	t.Helper()
	s, _, _ := makeServer(t, "")
	return s
}

func makeEmitServerWithWriter(t *testing.T, w *bytes.Buffer) *Server {
	t.Helper()
	s, _, _ := makeServer(t, "")
	s.EventWriter = w
	return s
}

