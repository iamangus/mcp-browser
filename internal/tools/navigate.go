package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func navigateHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawURL, err := request.RequireString("url")
		if err != nil {
			return mcpErrorResult("url is required"), nil
		}
		waitUntil := "domcontentloaded"
		if v := request.GetString("waitUntil", ""); v != "" {
			switch v {
			case "load", "domcontentloaded", "networkidle":
				waitUntil = v
			default:
				return mcpErrorResult(fmt.Sprintf("invalid waitUntil %q (want load, domcontentloaded, or networkidle)", v)), nil
			}
		}
		pageCtx := getPageCtx(ctx)
		navCtx, cancel := context.WithTimeout(pageCtx, getBrowserTimeout(ctx))
		defer cancel()

		var title, finalURL string
		err = chromedp.Run(navCtx,
			// page.Navigate (the raw CDP command) only initiates the
			// navigation; waitUntilAction then blocks until the requested
			// readiness condition. chromedp.Navigate would fire the same
			// command but never wait for the page to reach a usable state,
			// so the waitUntil option was previously ignored.
			chromedp.ActionFunc(func(ctx context.Context) error {
				_, _, errorText, _, err := page.Navigate(rawURL).Do(ctx)
				if err != nil {
					return err
				}
				if errorText != "" {
					return fmt.Errorf("page load error %s", errorText)
				}
				return nil
			}),
			waitUntilAction(waitUntil),
			chromedp.Title(&title),
			chromedp.Evaluate(`window.location.href`, &finalURL),
		)
		if err != nil {
			slog.Warn("navigation failed",
				"session", getSessionID(ctx),
				"url", rawURL,
				"wait_until", waitUntil,
				"error", err,
			)
			// The navigation may have begun even though the requested
			// readiness condition was not reached (e.g. a page that never
			// fires load). Return useful partial state so the agent can
			// screenshot the page instead of treating the session as dead.
			partial := partialNavigationState(navCtx, &title, &finalURL)
			return mcpErrorResult(fmt.Sprintf("navigation failed: %v%s", err, partial)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Navigated to %s\nTitle: %s", finalURL, title))},
		}, nil
	}
}

// waitUntilAction blocks until the page reaches the requested readiness
// condition. "domcontentloaded" returns as soon as the DOM is interactive —
// the right default for inspecting pages (e.g. a Cloudflare challenge) without
// waiting for every resource to finish. "load" and "networkidle" wait for the
// full load event.
func waitUntilAction(waitUntil string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if waitUntil != "domcontentloaded" {
			return waitForLoad(ctx)
		}
		return waitForReadyState(ctx, func(rs string) bool { return rs != "loading" })
	})
}

func waitForLoad(ctx context.Context) error {
	// A resource-heavy page can take a long time to reach complete; only wait
	// for the load lifecycle event so this honors the caller's timeout.
	return waitForReadyState(ctx, func(rs string) bool { return rs == "complete" })
}

func waitForReadyState(ctx context.Context, ready func(string) bool) error {
	for {
		var rs string
		if err := chromedp.Evaluate(`document.readyState`, &rs).Do(ctx); err != nil {
			return err
		}
		if ready(rs) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// partialNavigationState best-effort captures the current URL and title after
// a failed navigation so the agent gets the page's state rather than just an
// error. Returns "" when the page is unreachable.
func partialNavigationState(ctx context.Context, title, finalURL *string) string {
	var t, u string
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = chromedp.Run(probe,
		chromedp.Title(&t),
		chromedp.Evaluate(`window.location.href`, &u),
	)
	if t != "" || u != "" {
		*title = t
		*finalURL = u
		return fmt.Sprintf(" (page loaded at %s title %q)", u, t)
	}
	return ""
}
