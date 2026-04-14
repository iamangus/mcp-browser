package tools

import (
	"context"
	"fmt"
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
		var title, finalURL string
		err = chromedp.Run(pageCtx,
			chromedp.Navigate(rawURL),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.Title(&title),
			chromedp.Evaluate(`window.location.href`, &finalURL),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("navigation failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Navigated to %s\nTitle: %s", finalURL, title))},
		}, nil
	}
}
