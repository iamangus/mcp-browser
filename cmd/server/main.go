package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/angoo/mcp-browser/internal/browser"
	"github.com/angoo/mcp-browser/internal/config"
	"github.com/angoo/mcp-browser/internal/logger"
	appserver "github.com/angoo/mcp-browser/internal/server"
	"github.com/angoo/mcp-browser/internal/tools"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logger.Init(cfg.LogLevel)
	slog.SetDefault(log)
	log.Info("starting mcp-browser",
		"port", cfg.Port,
		"host", cfg.Host,
		"log_level", cfg.LogLevel,
		"headless", cfg.Headless,
	)
	browserMgr := browser.NewManager(cfg, log)
	if err := browserMgr.Start(); err != nil {
		return fmt.Errorf("browser: %w", err)
	}
	defer browserMgr.Shutdown()

	mcpSrv := server.NewMCPServer("mcp-browser", "1.0.0",
		server.WithLogging(),
	)
	mcpSrv.Use(tools.BrowserContextMiddleware(browserMgr))
	tools.RegisterTools(mcpSrv, cfg.ScreenshotQuality)
	log.Info("tools registered", "count", 16)

	mcpHTTP := server.NewStreamableHTTPServer(mcpSrv,
		server.WithStateful(true),
		server.WithSessionIdleTTL(cfg.SessionTimeout),
		server.WithEndpointPath("/"),
	)

	srv := appserver.New(cfg, log, mcpHTTP)
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		log.Info("server listening", "addr", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Info("shutting down", "signal", sig.String())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error("shutdown error", "error", err)
	}
	log.Info("server stopped")
	return nil
}
