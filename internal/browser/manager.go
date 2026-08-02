package browser

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/chromedp/chromedp"
)

func chromedpErrorf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if strings.Contains(message, "could not unmarshal event") && strings.Contains(message, "unknown IPAddressSpace value") {
		return
	}
	log.Printf(format, args...)
}

type PageSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	lastUsed time.Time
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
	m.allocCtx, m.allocCancel = chromedp.NewExecAllocator(context.Background(), m.execAllocatorOptions()...)
	ctx, cancel := chromedp.NewContext(m.allocCtx, chromedp.WithErrorf(chromedpErrorf))
	defer cancel()
	// Bound the launch: a stuck browser (e.g. missing D-Bus or a GPU init
	// crash-loop in a container) would otherwise hang this call forever and
	// prevent the HTTP server from ever starting.
	startCtx, startCancel := context.WithTimeout(ctx, browserStartTimeout)
	defer startCancel()
	if err := chromedp.Run(startCtx); err != nil {
		return fmt.Errorf("failed to start browser (timed out after %s): %w", browserStartTimeout, err)
	}
	m.startedAt = time.Now()
	m.logger.Info("browser started", "chromium_path", m.cfg.ChromiumPath, "headless", m.cfg.Headless, "stealth", m.cfg.Stealth)
	go m.cleanupLoop()
	return nil
}

// browserStartTimeout bounds how long we wait for the Chromium process to come
// up before failing with an actionable error instead of hanging.
const browserStartTimeout = 60 * time.Second

// execAllocatorOptions builds the Chromium launch flags. In stealth mode the
// automation markers added by chromedp's defaults (--enable-automation) and by
// our own suppression flags are countered or dropped, and headed mode launches
// a clean near-default flag set so the browser fingerprints like a normal one.
func (m *BrowserManager) execAllocatorOptions() []chromedp.ExecAllocatorOption {
	if !m.cfg.Stealth {
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
			chromedp.Flag("disk-cache-dir", "/dev/null"),
			chromedp.WindowSize(m.cfg.ScreenshotWidth, m.cfg.ScreenshotHeight),
		)
		if m.cfg.ChromiumPath != "" {
			opts = append(opts, chromedp.ExecPath(m.cfg.ChromiumPath))
		}
		return opts
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", m.cfg.Headless),
		chromedp.Flag("no-sandbox", m.cfg.NoSandbox),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disk-cache-dir", "/dev/null"),
		chromedp.WindowSize(m.cfg.ScreenshotWidth, m.cfg.ScreenshotHeight),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("lang", "en-US,en"),
	}
	if m.cfg.ChromiumPath != "" {
		opts = append(opts, chromedp.ExecPath(m.cfg.ChromiumPath))
	}
	if m.cfg.StealthUserAgent != "" {
		opts = append(opts, chromedp.UserAgent(m.cfg.StealthUserAgent))
	}
	if m.cfg.Headless {
		opts = append(opts,
			chromedp.Flag("disable-extensions", true),
			chromedp.Flag("hide-scrollbars", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("disable-dev-shm-usage", true),
		)
	} else {
		// Headed mode under a virtual display (Xvfb): there is no GPU in the
		// container, so force software rendering up front. Otherwise Chromium's
		// GPU process crash-loops through Vulkan/EGL init and can stall startup.
		opts = append(opts,
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("use-angle", "swiftshader"),
			chromedp.Flag("enable-unsafe-swiftshader", true),
		)
	}
	return opts
}

func (m *BrowserManager) GetOrCreatePage(sessionID string) (context.Context, error) {
	m.mu.RLock()
	if p, ok := m.pages[sessionID]; ok {
		m.mu.RUnlock()
		m.touchPage(sessionID)
		return p.ctx, nil
	}
	m.mu.RUnlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pages[sessionID]; ok {
		p.lastUsed = time.Now()
		return p.ctx, nil
	}
	if len(m.pages) >= m.cfg.MaxConcurrentPages {
		return nil, fmt.Errorf("maximum concurrent pages (%d) reached", m.cfg.MaxConcurrentPages)
	}
	ctx, cancel := chromedp.NewContext(m.allocCtx)
	firstRun := func(ctx context.Context) error {
		if m.cfg.Stealth {
			if err := installStealthScript(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	createCtx, createCancel := context.WithTimeout(ctx, pageCreateTimeout)
	defer createCancel()
	if err := chromedp.Run(createCtx, chromedp.ActionFunc(firstRun)); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create page (timed out after %s): %w", pageCreateTimeout, err)
	}
	m.pages[sessionID] = &PageSession{ctx: ctx, cancel: cancel, lastUsed: time.Now()}
	m.logger.Info("page created", "session", sessionID, "total_pages", len(m.pages))
	return ctx, nil
}

const pageCreateTimeout = 30 * time.Second

func (m *BrowserManager) touchPage(sessionID string) {
	m.mu.Lock()
	if p, ok := m.pages[sessionID]; ok {
		p.lastUsed = time.Now()
	}
	m.mu.Unlock()
}

func (m *BrowserManager) GetPage(sessionID string) (context.Context, error) {
	return m.GetOrCreatePage(sessionID)
}

// LookupPage returns an existing page context for the session without creating
// a new one. It is used by the live streamer so that opening the watch UI does
// not spawn browser pages for sessions that have no browser activity.
func (m *BrowserManager) LookupPage(sessionID string) (context.Context, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pages[sessionID]
	if !ok {
		return nil, false
	}
	return p.ctx, true
}

func (m *BrowserManager) Sessions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.pages))
	for id := range m.pages {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
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
		var stale []string
		now := time.Now()
		m.mu.RLock()
		idleTimeout := m.cfg.SessionTimeout
		if idleTimeout <= 0 {
			idleTimeout = pageIdleTimeout
		}
		for sessionID, p := range m.pages {
			if now.Sub(p.lastUsed) >= idleTimeout {
				stale = append(stale, sessionID)
			}
		}
		count := len(m.pages)
		m.mu.RUnlock()
		for _, sessionID := range stale {
			m.ClosePage(sessionID)
		}
		m.logger.Debug("browser cleanup check", "active_pages", count, "closed_stale_pages", len(stale))
	}
}

const pageIdleTimeout = 30 * time.Minute

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
