package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/cdproto/cdp"
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
		if err := validation.ValidateURL(rawURL); err != nil {
			return mcpErrorResult(err.Error()), nil
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
		if pageCtx == nil {
			return mcpErrorResult("no browser page for session"), nil
		}
		sessionID := getSessionID(ctx)
		navCtx, cancel := context.WithTimeout(pageCtx, getBrowserTimeout(ctx))
		defer cancel()

		// Collect Page lifecycle events so waitUntil can be honored from
		// Chromium's own signal instead of polling document.readyState every
		// 100ms (which sends up to 300 JS commands during a slow navigation and
		// itself contends with the renderer). The channel is fresh per call, so
		// events from an earlier navigation cannot leak in.
		lifecycleCh := make(chan *page.EventLifecycleEvent, 16)
		chromedp.ListenTarget(navCtx, func(ev any) {
			if e, ok := ev.(*page.EventLifecycleEvent); ok {
				select {
				case lifecycleCh <- e:
				default:
				}
			}
		})

		var title, finalURL string
		var loaderID cdp.LoaderID
		start := time.Now()
		err = chromedp.Run(navCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				if err := page.Enable().Do(ctx); err != nil {
					return err
				}
				if err := page.SetLifecycleEventsEnabled(true).Do(ctx); err != nil {
					return err
				}
				var err error
				var errorText string
				_, loaderID, errorText, _, err = page.Navigate(rawURL).Do(ctx)
				if err != nil {
					return err
				}
				if errorText != "" {
					return fmt.Errorf("page load error %s", errorText)
				}
				return nil
			}),
			chromedp.ActionFunc(func(ctx context.Context) error {
				// Read loaderID at execution time (after page.Navigate above
				// assigned it), so the wait correlates to THIS navigation.
				return waitForReadiness(ctx, loaderID, lifecycleCh, waitUntil)
			}),
			chromedp.Title(&title),
			chromedp.Evaluate(`window.location.href`, &finalURL),
		)
		if err != nil {
			// Drain whatever lifecycle events arrived before the timeout so the
			// log shows how far the page actually got.
			var seen []string
		drain:
			for {
				select {
				case e := <-lifecycleCh:
					seen = append(seen, e.Name)
				default:
					break drain
				}
			}
			slog.Warn("navigation failed",
				"session", sessionID,
				"url", rawURL,
				"wait_until", waitUntil,
				"loader_id", loaderID,
				"lifecycle_events", strings.Join(seen, ","),
				"error", err,
			)
			// Probe partial state on the long-lived page context, not navCtx,
			// which is already canceled at this point.
			partial := partialNavigationState(pageCtx, &title, &finalURL)
			return mcpErrorResult(fmt.Sprintf("navigation failed: %v%s", err, partial)), nil
		}
		slog.Info("navigation ok",
			"session", sessionID,
			"url", rawURL,
			"final_url", finalURL,
			"title", title,
			"wait_until", waitUntil,
			"loader_id", loaderID,
			"duration", time.Since(start).Round(time.Millisecond),
		)
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Navigated to %s\nTitle: %s", finalURL, title))},
		}, nil
	}
}

// waitForReadiness blocks until the page reaches the requested readiness
// condition. It short-circuits via document.readyState when the condition is
// already satisfied (e.g. same-document navigation), then waits for the
// lifecycle event, filtering to the navigation's loader so stale events from a
// previous document (same target, same frame) cannot satisfy the wait.
func waitForReadiness(ctx context.Context, loaderID cdp.LoaderID, ch <-chan *page.EventLifecycleEvent, want string) error {
	if want != "networkidle" {
		var rs string
		if err := chromedp.Evaluate(`document.readyState`, &rs).Do(ctx); err == nil {
			if want == "domcontentloaded" && rs != "loading" {
				return nil
			}
			if want == "load" && rs == "complete" {
				return nil
			}
		}
	}
	event := "DOMContentLoaded"
	switch want {
	case "load":
		event = "load"
	case "networkidle":
		event = "networkIdle"
	}
	for {
		select {
		case e := <-ch:
			if (loaderID == "" || e.LoaderID == loaderID) && e.Name == event {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// partialNavigationState best-effort captures the current URL, title, and
// readyState after a failed navigation so the agent gets the page's state
// rather than just an error. It probes the long-lived page context with its
// own short timeout. Returns "" when the page is unreachable.
func partialNavigationState(pageCtx context.Context, title, finalURL *string) string {
	probe, cancel := context.WithTimeout(pageCtx, 2*time.Second)
	defer cancel()
	var t, u, rs string
	err := chromedp.Run(probe,
		chromedp.Title(&t),
		chromedp.Evaluate(`window.location.href`, &u),
		chromedp.Evaluate(`document.readyState`, &rs),
	)
	if err != nil {
		return ""
	}
	*title = t
	*finalURL = u
	return fmt.Sprintf(" (page loaded at %s title %q ready_state %q)", u, t, rs)
}
