package browser

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/chromedp/chromedp"
)

func TestHeavyNavWithConcurrentCapture(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1")
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

	pageCtx, err := m.GetOrCreatePage("heavy")
	if err != nil {
		t.Fatalf("GetOrCreatePage: %v", err)
	}

	var title string
	if err := chromedp.Run(pageCtx, chromedp.Navigate("http://example.com/"), chromedp.Title(&title)); err != nil {
		t.Fatalf("baseline nav: %v", err)
	}
	t.Logf("baseline example.com nav ok")

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
			ctx, cancel := context.WithTimeout(pageCtx, 5*time.Second)
			var buf []byte
			_ = chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf))
			cancel()
			time.Sleep(100 * time.Millisecond)
		}
	}()

	start := time.Now()
	navErr := chromedp.Run(pageCtx, chromedp.Navigate("https://www.gamenerdz.com"), chromedp.Title(&title))
	elapsed := time.Since(start)
	close(stop)
	<-done
	t.Logf("gamenerdz nav under concurrent capture: err=%v elapsed=%s title=%q", navErr, elapsed, title)
}
