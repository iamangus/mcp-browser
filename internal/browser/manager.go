package browser

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/chromedp/chromedp"
)

func chromedpErrorf(format string, args ...any) {
	if strings.Contains(format, "could not unmarshal event") && len(args) > 0 {
		if err, ok := args[len(args)-1].(error); ok && strings.Contains(err.Error(), "unknown IPAddressSpace value") {
			return
		}
	}
	log.Printf(format, args...)
}

type PageSession struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type BrowserManager struct {
	cfg         *config.Config
	logger      *slog.Logger
	allocCtx    context.Context
	allocCancel context.CancelFunc
	mu          sync.RWMutex
	pages       map[string]*PageSession
	startedAt   time.Time
}

func NewManager(cfg *config.Config, logger *slog.Logger) *BrowserManager {
	return &BrowserManager{
		cfg:    cfg,
		logger: logger,
		pages:  make(map[string]*PageSession),
	}
}

func (m *BrowserManager) Start() error {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", m.cfg.Headless),
		chromedp.Flag("no-sandbox", m.cfg.NoSandbox),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-translate", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("safebrowsing-disable-auto-update", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-features", "TranslateUI"),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.WindowSize(m.cfg.ScreenshotWidth, m.cfg.ScreenshotHeight),
	)
	if m.cfg.ChromiumPath != "" {
		opts = append(opts, chromedp.ExecPath(m.cfg.ChromiumPath))
	}
	m.allocCtx, m.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(m.allocCtx, chromedp.WithErrorf(chromedpErrorf))
	defer cancel()
	if err := chromedp.Run(ctx); err != nil {
		return fmt.Errorf("failed to start browser: %w", err)
	}
	m.startedAt = time.Now()
	m.logger.Info("browser started", "chromium_path", m.cfg.ChromiumPath, "headless", m.cfg.Headless)
	go m.cleanupLoop()
	return nil
}

func (m *BrowserManager) GetOrCreatePage(sessionID string) (context.Context, error) {
	m.mu.RLock()
	if p, ok := m.pages[sessionID]; ok {
		m.mu.RUnlock()
		return p.ctx, nil
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pages[sessionID]; ok {
		return p.ctx, nil
	}
	if len(m.pages) >= m.cfg.MaxConcurrentPages {
		return nil, fmt.Errorf("maximum concurrent pages (%d) reached", m.cfg.MaxConcurrentPages)
	}
	ctx, cancel := chromedp.NewContext(m.allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	m.pages[sessionID] = &PageSession{ctx: ctx, cancel: cancel}
	m.logger.Info("page created", "session", sessionID, "total_pages", len(m.pages))
	return ctx, nil
}

func (m *BrowserManager) GetPage(sessionID string) (context.Context, error) {
	return m.GetOrCreatePage(sessionID)
}

func (m *BrowserManager) ClosePage(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pages[sessionID]; ok {
		p.cancel()
		delete(m.pages, sessionID)
		m.logger.Info("page closed", "session", sessionID, "total_pages", len(m.pages))
	}
}

func (m *BrowserManager) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(m.allocCtx, 10*time.Second)
	defer cancel()
	var result string
	err := chromedp.Run(ctx, chromedp.Evaluate(`navigator.userAgent`, &result))
	if err != nil {
		m.logger.Warn("browser health check failed", "error", err)
		return false
	}
	return true
}

func (m *BrowserManager) Stats() map[string]any {
	m.mu.RLock()
	pageCount := len(m.pages)
	m.mu.RUnlock()
	return map[string]any{
		"status":    "healthy",
		"pageCount": pageCount,
		"maxPages":  m.cfg.MaxConcurrentPages,
		"uptime":    time.Since(m.startedAt).String(),
		"headless":  m.cfg.Headless,
	}
}

func (m *BrowserManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.RLock()
		count := len(m.pages)
		m.mu.RUnlock()
		m.logger.Debug("browser cleanup check", "active_pages", count)
	}
}

func (m *BrowserManager) Shutdown() {
	m.mu.Lock()
	for id, p := range m.pages {
		p.cancel()
		delete(m.pages, id)
	}
	m.mu.Unlock()
	if m.allocCancel != nil {
		m.allocCancel()
	}
	m.logger.Info("browser shut down")
}
