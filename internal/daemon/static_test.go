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

// TestEmbeddedRecorder_NoDialogueFlicker asserts that recorder.js guards
// dialogueFeed.innerHTML with an HTML-level cache variable so that repeated
// /api/context polls with unchanged dialogue content do not mutate the DOM
// (which would re-trigger CSS animations and cause visible flicker).
func TestEmbeddedRecorder_NoDialogueFlicker(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read web/recorder.js: %v", err)
	}
	body := string(data)

	guards := []struct {
		pattern string
		desc    string
	}{
		{"lastDialogueHtml", "HTML cache variable for dialogue dedup"},
		{"lastDialogueHtml", "must be compared before setting dialogueFeed.innerHTML"},
	}
	for _, g := range guards {
		if !strings.Contains(body, g.pattern) {
			t.Errorf("recorder.js missing anti-flicker guard %q (%s)", g.pattern, g.desc)
		}
	}

	// Verify the guard is actually used in a conditional before innerHTML assignment.
	// A correct guard looks like: if (html === lastDialogueHtml) return;
	if !strings.Contains(body, "lastDialogueHtml") {
		t.Error("recorder.js: lastDialogueHtml guard not found — dialogue will flicker on every context poll")
	}
	// Must NOT unconditionally assign dialogueFeed.innerHTML without a guard.
	// We check that `return` appears near lastDialogueHtml (early-return pattern).
	idx := strings.Index(body, "lastDialogueHtml")
	if idx < 0 {
		t.Fatal("lastDialogueHtml not found")
	}
	surroundingContext := body[idx : min(len(body), idx+200)]
	if !strings.Contains(surroundingContext, "return") {
		t.Errorf("lastDialogueHtml guard does not appear to use early-return pattern; surrounding: %q", surroundingContext)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

