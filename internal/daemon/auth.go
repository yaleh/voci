package daemon

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
)

// GenerateToken returns a 6-digit numeric token, easy to read and share.
func GenerateToken() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(b[:]) % 1_000_000
	return fmt.Sprintf("%06d", n), nil
}

// BearerMiddleware wraps next with Bearer token authentication.
// When token is empty, all requests pass through unchanged.
func BearerMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
