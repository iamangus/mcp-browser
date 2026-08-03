package browser

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/chromedp/chromedp"
)

// TestRendererCrashRecovery covers the production failure where a page's
// renderer crashed while a command was in flight: the tab's target stays
// alive, so without recovery the page would wedge forever (commands hang
// against the dead renderer, the page lease is never released, and every
// later tool call on the session fails). The crash listener must close the
// page so the next GetOrCreatePage recreates a working tab.
func TestRendererCrashRecovery(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>alive</title></head><body>ok</body></html>`)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Headless:           true,
		NoSandbox:          true,
		Stealth:            true,
		MaxConcurrentPages: 10,
		ScreenshotWidth:    1280,
		ScreenshotHeight:   720,
		SessionTimeout:     30 * time.Minute,
	}
	m := NewManager(cfg, logger)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown()

	pageCtx, err := m.GetOrCreatePage("crash")
	if err != nil {
		t.Fatalf("GetOrCreatePage: %v", err)
	}
	navCtx, cancel := context.WithTimeout(pageCtx, 15*time.Second)
	defer cancel()
	var title string
	if err := chromedp.Run(navCtx, chromedp.Navigate(srv.URL), chromedp.Title(&title)); err != nil {
		t.Fatalf("initial navigation: %v", err)
	}
	if title != "alive" {
		t.Fatalf("unexpected title %q", title)
	}

	// Crash the renderer on purpose. The navigation may return an error or
	// time out depending on how fast the renderer dies; either is fine.
	crashCtx, crashCancel := context.WithTimeout(pageCtx, 5*time.Second)
	defer crashCancel()
	_ = chromedp.Run(crashCtx, chromedp.Navigate("chrome://crash"))

	// The crash listener must close the page promptly.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := m.LookupPage("crash"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crashed page was not closed")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The session must recover: a fresh page is created and works.
	pageCtx2, err := m.GetOrCreatePage("crash")
	if err != nil {
		t.Fatalf("GetOrCreatePage after crash: %v", err)
	}
	navCtx2, cancel2 := context.WithTimeout(pageCtx2, 15*time.Second)
	defer cancel2()
	title = ""
	if err := chromedp.Run(navCtx2, chromedp.Navigate(srv.URL), chromedp.Title(&title)); err != nil {
		t.Fatalf("navigation after crash recovery: %v", err)
	}
	if title != "alive" {
		t.Fatalf("unexpected title after recovery %q", title)
	}
}
