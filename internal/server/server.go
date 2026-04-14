package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/angoo/mcp-browser/internal/middleware"
)

type Server struct {
	router     *chi.Mux
	cfg        *config.Config
	logger     *slog.Logger
	mcpHandler http.Handler
}

func New(cfg *config.Config, logger *slog.Logger, mcpHandler http.Handler) *Server {
	s := &Server{
		cfg:        cfg,
		logger:     logger,
		mcpHandler: mcpHandler,
	}
	s.router = chi.NewRouter()
	s.setupMiddleware()
	s.setupRoutes()
	return s
}

func (s *Server) setupMiddleware() {
	s.router.Use(chimw.RealIP)
	s.router.Use(chimw.Recoverer)
	s.router.Use(middleware.SecurityHeaders)
	s.router.Use(middleware.RequestLogger)
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.parseCorsOrigins(),
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Mcp-Session-Id"},
		ExposedHeaders:   []string{"Mcp-Session-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	auth := middleware.NewAuth(s.cfg.APIKey, s.logger)
	rateLimiter := middleware.NewRateLimiter(s.cfg.RateLimitMax, s.cfg.RateLimitWindow, s.logger)
	s.router.Get("/health", s.handleHealth)
	if s.cfg.DisableAuth {
		s.logger.Warn("authentication is DISABLED")
		s.router.With(rateLimiter.Middleware).Mount("/mcp", s.mcpHandler)
	} else {
		s.router.With(auth.RequireAuth, rateLimiter.Middleware).Mount("/mcp", s.mcpHandler)
	}
}

func (s *Server) setupRoutes() {
	s.router.Get("/", s.handleIndex)
}

func (s *Server) parseCorsOrigins() []string {
	if s.cfg.CorsOrigin == "*" {
		return []string{"*"}
	}
	return []string{s.cfg.CorsOrigin}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{
  "name": "mcp-browser",
  "version": "1.0.0",
  "transport": "streamable-http",
  "endpoints": {
    "mcp": "POST/GET/DELETE /mcp (Streamable HTTP)",
    "health": "GET /health"
  }
}`)); err != nil {
		s.logger.Error("failed to write index response", "error", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		s.logger.Error("failed to write health response", "error", err)
	}
}
