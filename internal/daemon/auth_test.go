package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }

func TestBearerMiddleware_AllowsWhenTokenEmpty(t *testing.T) {
	h := BearerMiddleware("", http.HandlerFunc(okHandler))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/voice/emit", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", w.Code)
	}
}

func TestBearerMiddleware_AllowsCorrectToken(t *testing.T) {
	h := BearerMiddleware("secret42", http.HandlerFunc(okHandler))
	req := httptest.NewRequest("POST", "/api/voice/emit", nil)
	req.Header.Set("Authorization", "Bearer secret42")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", w.Code)
	}
}

func TestBearerMiddleware_Rejects401WhenTokenWrong(t *testing.T) {
	h := BearerMiddleware("secret42", http.HandlerFunc(okHandler))
	req := httptest.NewRequest("POST", "/api/voice/emit", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestBearerMiddleware_Rejects401WhenHeaderMissing(t *testing.T) {
	h := BearerMiddleware("secret42", http.HandlerFunc(okHandler))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/voice/emit", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestBearerMiddleware_Rejects401WhenSchemeMismatch(t *testing.T) {
	h := BearerMiddleware("secret42", http.HandlerFunc(okHandler))
	req := httptest.NewRequest("POST", "/api/voice/emit", nil)
	req.Header.Set("Authorization", "Basic secret42")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestGenerateToken_Is64HexChars(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Errorf("want 64 chars, got %d: %q", len(tok), tok)
	}
	for _, c := range tok {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("non-hex char %q in token", c)
		}
	}
}

func TestGenerateToken_UniqueEachCall(t *testing.T) {
	a, _ := GenerateToken()
	b, _ := GenerateToken()
	if a == b {
		t.Error("two tokens should differ")
	}
}
