package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaleh/voci/internal/intent"
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

	idx := strings.Index(body, "lastDialogueHtml")
	if idx < 0 {
		t.Fatal("lastDialogueHtml not found")
	}
	surroundingContext := body[idx : min(len(body), idx+200)]
	if !strings.Contains(surroundingContext, "return") {
		t.Errorf("lastDialogueHtml guard does not appear to use early-return pattern; surrounding: %q", surroundingContext)
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

// TestEmbeddedRecorder_SendTextRendersLocalMessages verifies that sendText()
// re-renders the dialogue immediately after updating localMessages, without
// waiting for hint changes from /api/context. The fix is that sendText() must
// call renderContext(lastHint) directly after pushing to localMessages.
func TestEmbeddedRecorder_SendTextRendersLocalMessages(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read recorder.js: %v", err)
	}
	body := string(data)
	idx := strings.Index(body, "function sendText")
	if idx < 0 {
		t.Fatal("sendText function not found in recorder.js")
	}
	// Find the end of sendText (next top-level function or closing brace pattern)
	fnBody := body[idx:min(len(body), idx+1100)]
	// sendText must call renderContext after updating localMessages so the
	// dialogue updates immediately — not deferred behind a hint-change guard.
	if !strings.Contains(fnBody, "renderContext") {
		t.Errorf("sendText() does not call renderContext() after updating localMessages; "+
			"dialogue will not update when hint is unchanged.\nFunction body: %q", fnBody)
	}
}

// TestSkillGrepPatternMatchesEventJSON is the contract test that bit us:
// the voci-listen skill grep pattern must use the lowercase key "rewritten"
// (the actual json tag on Event.Rewritten), not "Rewritten" (uppercase).
// When the case is wrong the grep filter never matches and the Monitor is silent.
func TestSkillGrepPatternMatchesEventJSON(t *testing.T) {
	skillPath := filepath.Join("..", "..", ".claude", "skills", "voci-listen", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Skipf("SKILL.md not accessible from test path (%s): %v", skillPath, err)
	}
	body := string(data)
	if strings.Contains(body, `"Rewritten"`) {
		t.Error(`voci-listen SKILL.md grep pattern contains "Rewritten" (uppercase R); ` +
			`Event JSON key is "rewritten" (lowercase) — Monitor will never fire`)
	}
	if !strings.Contains(body, `"rewritten"`) {
		t.Error(`voci-listen SKILL.md does not contain '"rewritten"' in grep pattern; ` +
			`Monitor will not match events emitted by /api/voice/emit`)
	}
}

// TestEmbeddedIndex_OverlayIsOpaque verifies the token-setup overlay uses a fully
// opaque background so that session content cannot bleed through when auth is required
// (--share mode exposes the server over Cloudflare; semi-transparent overlay leaks context).
func TestEmbeddedIndex_OverlayIsOpaque(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read web/index.html: %v", err)
	}
	if strings.Contains(string(data), "rgba(0,0,0,0.7)") {
		t.Error("token overlay uses semi-transparent background rgba(0,0,0,0.7); must be fully opaque to prevent session content bleed-through in --share mode")
	}
}

// TestEmbeddedRecorder_AuthRequiredFlag verifies recorder.js maintains an
// authRequired state variable that is set when the server probes with 401.
func TestEmbeddedRecorder_AuthRequiredFlag(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read recorder.js: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "authRequired") {
		t.Error("recorder.js missing authRequired state variable; required to block API calls when server demands Bearer token")
	}
	if !strings.Contains(body, "401") {
		t.Error("recorder.js does not check for HTTP 401 response; required for auth probe on init")
	}
}

// TestEmbeddedRecorder_RefreshContextGuardedByAuth verifies refreshContext() bails
// out early when auth is required but no token is stored, preventing data from being
// loaded and rendered into the DOM behind the overlay.
func TestEmbeddedRecorder_RefreshContextGuardedByAuth(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read recorder.js: %v", err)
	}
	body := string(data)
	idx := strings.Index(body, "function refreshContext")
	if idx < 0 {
		t.Fatal("refreshContext function not found in recorder.js")
	}
	fnBody := body[idx:min(len(body), idx+400)]
	if !strings.Contains(fnBody, "authRequired") {
		t.Errorf("refreshContext() does not check authRequired before fetching; session data will load behind the auth overlay.\nFunction body (first 400 chars): %q", fnBody)
	}
}

// TestEmbeddedRecorder_SaveTokenKickstartsPolling verifies that saveToken() calls
// refreshContext() so context polling starts as soon as the user enters the token.
func TestEmbeddedRecorder_SaveTokenKickstartsPolling(t *testing.T) {
	data, err := embeddedFS.ReadFile("web/recorder.js")
	if err != nil {
		t.Fatalf("read recorder.js: %v", err)
	}
	body := string(data)
	idx := strings.Index(body, "function saveToken")
	if idx < 0 {
		t.Fatal("saveToken function not found in recorder.js")
	}
	fnBody := body[idx:min(len(body), idx+400)]
	if !strings.Contains(fnBody, "refreshContext") {
		t.Errorf("saveToken() does not call refreshContext() after token is saved; polling will not start after auth.\nFunction body (first 400 chars): %q", fnBody)
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

