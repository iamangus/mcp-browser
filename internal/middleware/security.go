package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		cleanPath := strings.ReplaceAll(r.URL.Path, "\n", "")
		cleanPath = strings.ReplaceAll(cleanPath, "\r", "")
		// #nosec G706 -- path is sanitized above
		slog.Info("request",
			"method", r.Method,
			"path", cleanPath,
			"remote", r.RemoteAddr,
			"duration", time.Since(start),
		)
	})
}
