package middleware

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

type AuthMiddleware struct {
	apiKey string
	logger *slog.Logger
}

func NewAuth(apiKey string, logger *slog.Logger) *AuthMiddleware {
	return &AuthMiddleware{apiKey: apiKey, logger: logger}
}

func (a *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := extractBearerToken(r)
		if key == "" {
			a.sendUnauthorized(w, "missing authorization header")
			return
		}
		if !constantTimeEqual(a.apiKey, key) {
			a.sendUnauthorized(w, "invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func constantTimeEqual(a, b string) bool {
	la, lb := len(a), len(b)
	if la != lb {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (a *AuthMiddleware) sendUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
	a.logger.Warn("auth failed", "message", msg)
}
