package tools

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

	"github.com/angoo/mcp-browser/internal/browser"
	"github.com/angoo/mcp-browser/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestNavigateWaitUntil verifies that browser_navigate honors its waitUntil
// option by correlating Page lifecycle events with the navigation: with a page
// whose load event waits on a slow resource, domcontentloaded must return
// before the resource finishes while load and networkidle must wait for it.
func TestNavigateWaitUntil(t *testing.T) {
	if os.Getenv("BROWSER_E2E") != "1" {
		t.Skip("set BROWSER_E2E=1")
	}
	const slowDelay = 2500 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(slowDelay)
			w.Header().Set("Content-Type", "image/gif")
			fmt.Fprint(w, "GIF89a")
			return
		}
		c := r.URL.Query().Get("c")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Test Page</title></head><body>hello<img src="/slow?c=%s"></body></html>`, c)
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
	m := browser.NewManager(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Shutdown()

	pageCtx, err := m.GetOrCreatePage("navtest")
	if err != nil {
		t.Fatalf("GetOrCreatePage: %v", err)
	}
	rctx := context.WithValue(context.Background(), BrowserKey, pageCtx)
	rctx = context.WithValue(rctx, TimeoutKey, 30*time.Second)

	handler := navigateHandler()
	call := func(waitUntil string, n int) (time.Duration, bool) {
		start := time.Now()
		res, err := handler(rctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name:      "browser_navigate",
			Arguments: map[string]any{"url": srv.URL + "?c=" + fmt.Sprint(n), "waitUntil": waitUntil},
		}})
		if err != nil {
			t.Fatalf("navigate(%s): %v", waitUntil, err)
		}
		return time.Since(start), res.IsError
	}

	elapsed, isErr := call("domcontentloaded", 1)
	if isErr {
		t.Fatal("domcontentloaded returned an error result")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("domcontentloaded took %s, expected to return before the slow resource", elapsed)
	}

	elapsed, isErr = call("load", 2)
	if isErr {
		t.Fatal("load returned an error result")
	}
	if elapsed < 2*time.Second {
		t.Fatalf("load returned too early (%s), expected to wait for the slow resource", elapsed)
	}

	elapsed, isErr = call("networkidle", 3)
	if isErr {
		t.Fatal("networkidle returned an error result")
	}
	if elapsed < 2*time.Second {
		t.Fatalf("networkidle returned too early (%s)", elapsed)
	}
}
