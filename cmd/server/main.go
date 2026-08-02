package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/angoo/mcp-browser/internal/browser"
	"github.com/angoo/mcp-browser/internal/config"
	"github.com/angoo/mcp-browser/internal/logger"
	appserver "github.com/angoo/mcp-browser/internal/server"
	"github.com/angoo/mcp-browser/internal/tools"
	"github.com/angoo/mcp-browser/internal/watch"
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

	snapshotStore := watch.NewStore(cfg.MaxSnapshotsPerSession, log)
	liveHub := watch.NewHub(browserMgr, watch.HubOptions{
		Interval: cfg.LiveInterval,
		Quality:  cfg.LiveQuality,
		Logger:   log,
	})

	go startMetricsLogger(log, snapshotStore, browserMgr, liveHub)

	mcpSrv := server.NewMCPServer("mcp-browser", "1.0.0",
		server.WithLogging(),
	)
	mcpSrv.Use(tools.BrowserContextMiddleware(browserMgr, cfg.BrowserTimeout))
	tools.RegisterTools(mcpSrv, cfg.ScreenshotQuality, snapshotStore)
	log.Info("tools registered", "count", 16)

	mcpHTTP := server.NewStreamableHTTPServer(mcpSrv,
		server.WithStateful(true),
		server.WithSessionIdleTTL(cfg.SessionTimeout),
		server.WithEndpointPath("/"),
	)

	srv := appserver.New(cfg, log, mcpHTTP, snapshotStore, liveHub)
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

// metricsInterval controls how often the periodic diagnostics line is emitted.
const metricsInterval = 60 * time.Second

// startMetricsLogger emits a periodic line with memory usage, goroutine count,
// and in-process state so slow growth (e.g. toward an OOM) is visible in the
// logs instead of surfacing only as a silent container kill.
func startMetricsLogger(log *slog.Logger, snapshotStore *watch.Store, browserMgr *browser.BrowserManager, liveHub *watch.LiveHub) {
	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()
	for range ticker.C {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		sessions, snapBytes := snapshotStore.Stats()
		log.Info("metrics",
			"heap_alloc_mb", mem.Alloc/1024/1024,
			"heap_inuse_mb", mem.HeapInuse/1024/1024,
			"rss_mb", rssMB(),
			"chromium_rss_mb", chromiumTreeRSSMB(),
			"goroutines", runtime.NumGoroutine(),
			"snapshot_sessions", sessions,
			"snapshot_bytes_mb", snapBytes/1024/1024,
			"browser_pages", len(browserMgr.Sessions()),
			"live_streams", liveHub.StreamCount(),
		)
	}
}

// rssMB returns the process resident set size in MiB on Linux, reading
// /proc/self/statm (pages resident x page size). Returns 0 elsewhere.
func rssMB() int64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	var rssPages int64
	if _, err := fmt.Sscanf(fields[1], "%d", &rssPages); err != nil {
		return 0
	}
	return rssPages * int64(os.Getpagesize()) / 1024 / 1024
}

// chromiumTreeRSSMB returns the combined resident set size in MiB of every
// process in the container whose command line contains "chromium". The Go
// server's own rssMB() does not reflect the browser subprocesses, which are
// usually the dominant memory consumers in headed mode.
func chromiumTreeRSSMB() int64 {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		cmd, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		if !bytes.Contains(cmd, []byte("chromium")) {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/statm")
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			continue
		}
		var rssPages int64
		if _, err := fmt.Sscanf(fields[1], "%d", &rssPages); err != nil {
			continue
		}
		total += rssPages * int64(os.Getpagesize())
	}
	return total / 1024 / 1024
}
