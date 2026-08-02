package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func navigateHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawURL, err := request.RequireString("url")
		if err != nil {
			return mcpErrorResult("url is required"), nil
		}
		pageCtx := getPageCtx(ctx)
		navCtx, cancel := context.WithTimeout(pageCtx, getBrowserTimeout(ctx))
		defer cancel()
		var title, finalURL string
		err = chromedp.Run(navCtx,
			chromedp.Navigate(rawURL),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.Title(&title),
			chromedp.Evaluate(`window.location.href`, &finalURL),
		)
		if err != nil {
			// The middleware only logs the tool error when the handler returns
			// a non-nil error; here the failure is folded into the result, so
			// surface the real cause (e.g. a dead renderer target) ourselves.
			slog.Warn("navigation failed",
				"session", getSessionID(ctx),
				"url", rawURL,
				"error", err,
			)
			return mcpErrorResult(fmt.Sprintf("navigation failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Navigated to %s\nTitle: %s", finalURL, title))},
		}, nil
	}
}
