package browser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
	"github.com/chromedp/chromedp"
)

// TestDialogHandlerAutoAcceptsAlert guards against the production failure where
// a page's alert() blocks the renderer's main thread in headed mode: the
// navigation never reaches DOMContentLoaded, later navigations get no loader,
// and screenshots fail — the tab is wedged until the dialog is answered.
// installDialogHandler must answer it, so the navigation completes and the
// script after the alert runs.
func TestDialogHandlerAutoAcceptsAlert(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>unset</title>
<script>alert('please confirm');</script>
<script>document.title = 'after-alert';</script>
</head><body>hello</body></html>`)
	}))
	defer srv.Close()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(io.Discard, &logBuf), nil))
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

	pageCtx, err := m.GetOrCreatePage("dlg")
	if err != nil {
		t.Fatalf("GetOrCreatePage: %v", err)
	}

	// Without the dialog handler this navigation would hang on the alert()
	// until the context deadline below.
	navCtx, cancel := context.WithTimeout(pageCtx, 20*time.Second)
	defer cancel()
	var title string
	start := time.Now()
	if err := chromedp.Run(navCtx, chromedp.Navigate(srv.URL), chromedp.Title(&title)); err != nil {
		t.Fatalf("navigation blocked by dialog: %v (elapsed=%s)", err, time.Since(start))
	}
	if title != "after-alert" {
		t.Fatalf("script after alert() did not run, title=%q", title)
	}
	t.Logf("navigation completed in %s", time.Since(start))
	if !strings.Contains(logBuf.String(), "javascript dialog opened") {
		t.Logf("note: no dialog log captured (headless may have suppressed the dialog); log=%q", logBuf.String())
	}
}
