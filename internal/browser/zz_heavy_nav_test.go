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

// TestHeavyNavWithConcurrentCapture reproduces the production failure where the
// /watch live streamer's screenshot commands starved an in-flight navigation.
// The capture loop now goes through the page lease (TryPageCapture), so it
// yields to a tool call holding the lease and the navigation must complete
// instead of hitting its 30s timeout.
func TestHeavyNavWithConcurrentCapture(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(2 * time.Second)
			w.Header().Set("Content-Type", "image/gif")
			fmt.Fprint(w, "GIF89a")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Test Page</title></head><body>hello<img src="/slow"></body></html>`)
	}))
	defer srv.Close()

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

	const sid = "heavy"
	pageCtx, err := m.GetOrCreatePage(sid)
	if err != nil {
		t.Fatalf("GetOrCreatePage: %v", err)
	}

	var title string
	if err := chromedp.Run(pageCtx, chromedp.Navigate(srv.URL), chromedp.Title(&title)); err != nil {
		t.Fatalf("baseline nav: %v", err)
	}

	// Emulate the live streamer: repeatedly try to take a screenshot through
	// the page lease, exactly as LiveHub.captureAndTrack does.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if acquired, _ := m.TryPageCapture(sid); acquired {
				ctx, cancel := context.WithTimeout(pageCtx, 5*time.Second)
				var buf []byte
				_ = chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf))
				cancel()
				m.ReleasePageCapture(sid)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// The tool call holds the page lease for the whole navigation, so the
	// capture loop above must skip and the navigation must succeed.
	release, _, err := m.BeginPageOp(context.Background(), sid)
	if err != nil {
		t.Fatalf("BeginPageOp: %v", err)
	}
	start := time.Now()
	navErr := chromedp.Run(pageCtx, chromedp.Navigate(srv.URL+"/slowpage"), chromedp.Title(&title))
	elapsed := time.Since(start)
	release()
	close(stop)
	<-done
	if navErr != nil {
		t.Fatalf("nav under concurrent capture failed: %v (elapsed=%s)", navErr, elapsed)
	}
	t.Logf("nav under concurrent capture ok: elapsed=%s title=%q", elapsed, title)
}
