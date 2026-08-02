package browser

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/chromedp/chromedp"
)

// TestSessionPagesShareOneBrowser guards against a regression where every MCP
// session launched its own Chromium process: page contexts must be created from
// the long-lived browser context (a tab) and not from the allocator context (a
// whole new browser). Each extra browser process costs hundreds of MB and, in a
// container, repeats the GPU/EGL init crash-loop that kills page renderers.
//
// Requires a local Chromium, so it is opt-in: BROWSER_E2E=1 go test ./internal/browser
func TestSessionPagesShareOneBrowser(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1 to run tests that launch Chromium")
	}
	cfg := &config.Config{
		Headless:           true,
		NoSandbox:          true,
		Stealth:            true,
		MaxConcurrentPages: 10,
		ScreenshotWidth:    1280,
		ScreenshotHeight:   720,
		SessionTimeout:     30 * time.Minute,
	}
	m := NewManager(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown()

	afterStart := childProcessCount(t)
	if afterStart != 1 {
		t.Fatalf("expected exactly 1 browser process after Start, got %d", afterStart)
	}

	for _, id := range []string{"session-a", "session-b", "session-c"} {
		if _, err := m.GetOrCreatePage(id); err != nil {
			t.Fatalf("GetOrCreatePage(%s): %v", id, err)
		}
	}

	if got := childProcessCount(t); got != 1 {
		t.Fatalf("expected sessions to share 1 browser process, got %d browser processes", got)
	}
}

// TestSessionPageSurvivesNavigation guards the production symptom that page
// contexts were cancelled the instant GetOrCreatePage returned, making every
// navigation fail with "context canceled" and every live screenshot capture
// return no frame.
//
// Requires a local Chromium: BROWSER_E2E=1 go test ./internal/browser
func TestSessionPageSurvivesNavigation(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1 to run tests that launch Chromium")
	}
	cfg := &config.Config{
		Headless:           true,
		NoSandbox:          true,
		Stealth:            true,
		MaxConcurrentPages: 10,
		ScreenshotWidth:    1280,
		ScreenshotHeight:   720,
		SessionTimeout:     30 * time.Minute,
	}
	m := NewManager(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown()

	pageCtx, err := m.GetOrCreatePage("nav-session")
	if err != nil {
		t.Fatalf("GetOrCreatePage: %v", err)
	}
	if err := pageCtx.Err(); err != nil {
		t.Fatalf("page context already cancelled after creation: %v", err)
	}

	var title string
	if err := chromedp.Run(pageCtx,
		chromedp.Navigate("http://example.com/"),
		chromedp.Title(&title),
	); err != nil {
		t.Fatalf("navigate on session page: %v", err)
	}
	if title == "" {
		t.Fatal("expected a non-empty page title")
	}

	// The live streamer captures frames off the same page context; a dead
	// target is what made /watch show nothing.
	var frame []byte
	if err := chromedp.Run(pageCtx, chromedp.CaptureScreenshot(&frame)); err != nil {
		t.Fatalf("screenshot on session page: %v", err)
	}
	if len(frame) == 0 {
		t.Fatal("expected a non-empty screenshot frame")
	}
}

// childProcessCount returns the number of Chromium processes that are direct
// children of the test binary. chromedp starts each browser as a direct child,
// so this is an exact count of launched browser processes.
func childProcessCount(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("ps", "-o", "comm=", "--ppid", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(strings.ToLower(strings.TrimSpace(line)), "chrom") {
			n++
		}
	}
	return n
}

// TestSessionsAreIsolated checks that sharing one Chromium process does not
// leak cookies between MCP sessions: each session tab is created in its own
// browser context, which is the isolation guarantee that previously came from
// giving every session a whole browser.
//
// Requires a local Chromium: BROWSER_E2E=1 go test ./internal/browser
func TestSessionsAreIsolated(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1 to run tests that launch Chromium")
	}
	cfg := &config.Config{
		Headless:           true,
		NoSandbox:          true,
		Stealth:            true,
		MaxConcurrentPages: 10,
		ScreenshotWidth:    1280,
		ScreenshotHeight:   720,
		SessionTimeout:     30 * time.Minute,
	}
	m := NewManager(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown()

	const url = "http://example.com/"
	writer, err := m.GetOrCreatePage("session-writer")
	if err != nil {
		t.Fatalf("GetOrCreatePage(writer): %v", err)
	}
	if err := chromedp.Run(writer,
		chromedp.Navigate(url),
		chromedp.Evaluate(`document.cookie = "mcpb_isolation=1"; true`, nil),
	); err != nil {
		t.Fatalf("set cookie: %v", err)
	}

	reader, err := m.GetOrCreatePage("session-reader")
	if err != nil {
		t.Fatalf("GetOrCreatePage(reader): %v", err)
	}
	var cookies string
	if err := chromedp.Run(reader,
		chromedp.Navigate(url),
		chromedp.Evaluate(`document.cookie`, &cookies),
	); err != nil {
		t.Fatalf("read cookie: %v", err)
	}
	if strings.Contains(cookies, "mcpb_isolation") {
		t.Fatalf("cookie leaked across sessions: %q", cookies)
	}
	if got := childProcessCount(t); got != 1 {
		t.Fatalf("expected isolation without extra browser processes, got %d", got)
	}
}
