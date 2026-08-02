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
	cfg    *config.Config
	logger *slog.Logger
	// allocCtx owns the Chromium process pool. browserCtx is the long-lived
	// context for the single browser instance; every session page is created as
	// a tab of it (chromedp.NewContext on browserCtx creates a tab, whereas
	// NewContext on allocCtx would launch a whole new Chromium process).
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	mu            sync.RWMutex
	pages         map[string]*PageSession
	leaseMu       sync.Mutex
	leases        map[string]*pageLease
	startedAt     time.Time
	stopping      bool
	chromiumOut   *chromiumOutput
}

func NewManager(cfg *config.Config, logger *slog.Logger) *BrowserManager {
	return &BrowserManager{
		cfg:    cfg,
		logger: logger,
		pages:  make(map[string]*PageSession),
		leases: make(map[string]*pageLease),
	}
}

// pageLease serializes access to a session's page between tool calls and the
// live capture streamer. sem holds a single permit: a tool call takes it for
// the whole tool execution, and the live streamer takes it only for a single
// screenshot. waiters counts tool calls blocked waiting for the permit so the
// streamer yields to them instead of racing for the token.
type pageLease struct {
	mu      sync.Mutex
	sem     chan struct{}
	waiters int
}

func newPageLease() *pageLease {
	l := &pageLease{sem: make(chan struct{}, 1)}
	l.sem <- struct{}{}
	return l
}

func (m *BrowserManager) getLease(sessionID string) *pageLease {
	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()
	l, ok := m.leases[sessionID]
	if !ok {
		l = newPageLease()
		m.leases[sessionID] = l
	}
	return l
}

// BeginPageOp grants a tool call exclusive use of the session's page, waiting
// until any in-flight live capture finishes. It returns a release function and
// how long the call had to wait. The live streamer uses TryPageCapture, which
// yields to a waiting tool, so the wait is bounded by one capture.
func (m *BrowserManager) BeginPageOp(ctx context.Context, sessionID string) (release func(), wait time.Duration, err error) {
	l := m.getLease(sessionID)
	l.mu.Lock()
	l.waiters++
	l.mu.Unlock()
	start := time.Now()
	select {
	case <-l.sem:
		l.mu.Lock()
		l.waiters--
		l.mu.Unlock()
		return func() { l.sem <- struct{}{} }, time.Since(start), nil
	case <-ctx.Done():
		l.mu.Lock()
		l.waiters--
		l.mu.Unlock()
		return nil, time.Since(start), ctx.Err()
	}
}

// TryPageCapture reports whether the live streamer may take a screenshot of the
// session's page right now. It fails when a tool call holds or waits for the
// page; toolWaiting reports which case, for diagnostics.
func (m *BrowserManager) TryPageCapture(sessionID string) (acquired, toolWaiting bool) {
	l := m.getLease(sessionID)
	l.mu.Lock()
	waiters := l.waiters
	l.mu.Unlock()
	if waiters > 0 {
		return false, true
	}
	select {
	case <-l.sem:
		// A tool may have started waiting between the check above and taking
		// the token; yield immediately so the tool is not delayed a capture.
		l.mu.Lock()
		waiters = l.waiters
		l.mu.Unlock()
		if waiters > 0 {
			l.sem <- struct{}{}
			return false, true
		}
		return true, false
	default:
		return false, false
	}
}

// ReleasePageCapture returns the page to the tool-lease pool after the live
// streamer finishes a screenshot.
func (m *BrowserManager) ReleasePageCapture(sessionID string) {
	m.getLease(sessionID).sem <- struct{}{}
}

func (m *BrowserManager) Start() error {
	chromiumOut := newChromiumOutput(m.logger)
	m.chromiumOut = chromiumOut
	m.allocCtx, m.allocCancel = chromedp.NewExecAllocator(context.Background(), append(m.execAllocatorOptions(),
		chromedp.CombinedOutput(chromiumOut))...)
	// This context must outlive Start: it owns the browser that every session
	// tab is created from. Cancelling it here would kill the browser and force
	// each session to launch its own Chromium process.
	m.browserCtx, m.browserCancel = chromedp.NewContext(m.allocCtx, chromedp.WithErrorf(chromedpErrorf))
	// Bound the launch: a stuck browser (e.g. missing D-Bus or a GPU init
	// crash-loop in a container) would otherwise hang this call forever and
	// prevent the HTTP server from ever starting.
	//
	// The timeout MUST NOT be applied by deriving a context for chromedp.Run:
	// chromedp allocates the Chromium process against the context handed to the
	// allocating Run, so a derived timeout context would own the browser's
	// lifetime and kill it as soon as this function returned.
	runErr := make(chan error, 1)
	go func() { runErr <- chromedp.Run(m.browserCtx) }()
	select {
	case err := <-runErr:
		if err != nil {
			m.browserCancel()
			m.allocCancel()
			return fmt.Errorf("failed to start browser: %w", err)
		}
	case <-time.After(browserStartTimeout):
		m.browserCancel()
		m.allocCancel()
		return fmt.Errorf("failed to start browser: timed out after %s", browserStartTimeout)
	}
	m.startedAt = time.Now()
	m.logger.Info("browser started",
		"chromium_path", m.cfg.ChromiumPath,
		"headless", m.cfg.Headless,
		"stealth", m.cfg.Stealth,
	)
	m.logGPUInfo(m.browserCtx)
	go m.watchBrowserExit(chromiumOut)
	go m.cleanupLoop(chromiumOut)
	return nil
}

// browserStartTimeout bounds how long we wait for the Chromium process to come
// up before failing with an actionable error instead of hanging.
const browserStartTimeout = 60 * time.Second

// logGPUInfo inspects the compositor's WebGL renderer string so we can see
// which renderer is in use at startup. In headed mode under a virtual display
// rendering is pure software (--disable-gpu), so WebGL may be unavailable and
// the renderer empty — that is expected and not an error. A non-empty renderer
// still tells us whether hardware acceleration engaged elsewhere. The
// SystemInfo CDP domain is browser-target-only and not reachable from
// chromedp's page context, so we read the renderer string from a page instead.
func (m *BrowserManager) logGPUInfo(ctx context.Context) {
	infoCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var info struct {
		Renderer string `json:"renderer"`
		Vendor   string `json:"vendor"`
	}
	err := chromedp.Run(infoCtx,
		chromedp.Evaluate(`
			(() => {
				const gl = document.createElement('canvas').getContext('webgl');
				if (!gl) return { renderer: '', vendor: '' };
				const ext = gl.getExtension('WEBGL_debug_renderer_info');
				if (!ext) return { renderer: '', vendor: '' };
				return {
					renderer: String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL)),
					vendor: String(gl.getParameter(ext.UNMASKED_VENDOR_WEBGL)),
				};
			})()
		`, &info),
	)
	if err != nil {
		m.logger.Warn("gpu info unavailable", "error", err)
		return
	}
	attrs := []any{"vendor", info.Vendor, "renderer", info.Renderer}
	if strings.Contains(strings.ToLower(info.Renderer), "swiftshader") {
		attrs = append(attrs, "accel", "software")
	} else if info.Renderer != "" {
		attrs = append(attrs, "accel", "hardware")
	} else {
		attrs = append(attrs, "accel", "none")
	}
	m.logger.Info("gpu info", attrs...)
}

// watchBrowserExit logs the reason the browser process went away. chromedp
// cancels the allocator context whenever the browser loses connection (e.g. the
// process crashed or was killed), so watching m.browserCtx.Done lets us surface
// the browser-side failure with the tail of Chromium's own stderr — the missing
// diagnostic when a page target dies.
func (m *BrowserManager) watchBrowserExit(chromiumOut *chromiumOutput) {
	// browserCtx is cancelled both when the Chromium process dies and when
	// chromedp tears the allocator down on a lost connection, so it is the
	// earliest signal that the browser is gone.
	<-m.browserCtx.Done()
	m.mu.Lock()
	stopping := m.stopping
	m.mu.Unlock()
	if stopping {
		// Shutdown() cancelled the browser and we're exiting cleanly.
		return
	}
	attrs := []any{"error", context.Cause(m.browserCtx)}
	if tail := chromiumOut.Tail(); tail != "" {
		attrs = append(attrs, "chromium_stderr_tail", tail)
	}
	m.logger.Error("browser process exited", attrs...)
	// The browser is gone; every session's page context is now dead. Drop them
	// all so the live UI reports the sessions as inactive immediately instead
	// of waiting for the next cleanup tick.
	m.mu.Lock()
	for id, p := range m.pages {
		p.cancel()
		delete(m.pages, id)
	}
	m.mu.Unlock()
	m.logger.Warn("browser process exited: cleared all session pages", "cleared", "all")
}

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
		chromedp.Flag("enable-logging", "stderr"),
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
		// Headed mode under a virtual display (Xvfb). Xvfb is a software
		// framebuffer with no DRI3 extension, so Chromium's GPU process cannot
		// present frames through any ANGLE backend. SwiftShader software
		// rendering is the only path that keeps the renderer alive while a real
		// page composites: without --use-angle=swiftshader and
		// --enable-unsafe-swiftshader the GPU process's EGL init crashes and
		// takes the page's renderer with it (surface context is canceled ~0.4s
		// into the first navigation).
		opts = append(opts, softwareGLFlags()...)
	}
	return opts
}

// softwareGLFlags forces SwiftShader software rendering in headed mode under a
// virtual display (Xvfb). Xvfb has no DRI3, so no ANGLE backend can present;
// --use-angle=swiftshader routes compositing through SwiftShader and
// --enable-unsafe-swiftshader unblocks it (it is "unsafe" because it is a
// software renderer, which is exactly what we want here).
func softwareGLFlags() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("use-angle", "swiftshader"),
		chromedp.Flag("enable-unsafe-swiftshader", true),
	}
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
	// Create a tab of the existing browser, in its own browser context so the
	// session keeps isolated cookies/storage. Deriving from m.allocCtx instead
	// would launch an entire new Chromium process per session.
	ctx, cancel := chromedp.NewContext(m.browserCtx, chromedp.WithNewBrowserContext())
	firstRun := func(ctx context.Context) error {
		if m.cfg.Stealth {
			if err := installStealthScript(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	// As in Start, the timeout must not come from a context derived for
	// chromedp.Run: the tab's CDP session and its message pump are bound to the
	// context handed to the Run that creates the target, so a derived timeout
	// context would kill the session as soon as this function returned. Every
	// later command on the page would then block forever or fail with
	// "context canceled".
	runErr := make(chan error, 1)
	go func() { runErr <- chromedp.Run(ctx, chromedp.ActionFunc(firstRun)) }()
	select {
	case err := <-runErr:
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create page: %w", err)
		}
	case <-time.After(pageCreateTimeout):
		cancel()
		return nil, fmt.Errorf("failed to create page: timed out after %s", pageCreateTimeout)
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
	if p, ok := m.pages[sessionID]; ok {
		p.cancel()
		delete(m.pages, sessionID)
		m.logger.Info("page closed", "session", sessionID, "total_pages", len(m.pages))
	}
	m.mu.Unlock()
}

func (m *BrowserManager) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(m.browserCtx, 10*time.Second)
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

// cleanupLoop runs every 30 seconds. It closes pages whose underlying context
// has been cancelled (the chromedp target was destroyed, e.g. a crashed
// renderer) so a dead tab cannot linger as a phantom "live" session while
// retaining its per-target memory, and closes pages that have been idle past
// the session timeout.
func (m *BrowserManager) cleanupLoop(chromiumOut *chromiumOutput) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var dead, stale []string
		now := time.Now()
		idleTimeout := m.cfg.SessionTimeout
		if idleTimeout <= 0 {
			idleTimeout = pageIdleTimeout
		}
		m.mu.RLock()
		for sessionID, p := range m.pages {
			if p.ctx.Err() != nil {
				dead = append(dead, sessionID)
				continue
			}
			if now.Sub(p.lastUsed) >= idleTimeout {
				stale = append(stale, sessionID)
			}
		}
		count := len(m.pages)
		m.mu.RUnlock()
		for _, sessionID := range dead {
			attrs := []any{"session", sessionID}
			if tail := chromiumOut.Tail(); tail != "" {
				attrs = append(attrs, "chromium_stderr_tail", tail)
			}
			m.logger.Warn("closing dead browser page", attrs...)
			m.ClosePage(sessionID)
		}
		for _, sessionID := range stale {
			m.ClosePage(sessionID)
		}
		if len(dead) > 0 || len(stale) > 0 {
			m.logger.Info("browser cleanup", "closed_dead_pages", len(dead), "closed_stale_pages", len(stale), "remaining_pages", count-len(dead)-len(stale))
		} else {
			m.logger.Debug("browser cleanup check", "active_pages", count)
		}
	}
}

const pageIdleTimeout = 30 * time.Minute

func (m *BrowserManager) Shutdown() {
	m.mu.Lock()
	m.stopping = true
	for id, p := range m.pages {
		p.cancel()
		delete(m.pages, id)
	}
	m.mu.Unlock()
	if m.browserCancel != nil {
		m.browserCancel()
	}
	if m.allocCancel != nil {
		m.allocCancel()
	}
	m.logger.Info("browser shut down")
}
