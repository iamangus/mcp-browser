package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func clickHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, err := request.RequireString("selector")
		if err != nil {
			return mcpErrorResult("selector is required"), nil
		}
		if err := validation.ValidateSelector(selector); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		pageCtx := getPageCtx(ctx)
		var titleBefore, titleAfter, urlBefore, urlAfter string
		err = chromedp.Run(pageCtx,
			chromedp.Title(&titleBefore),
			chromedp.Evaluate(`window.location.href`, &urlBefore),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("failed to get page state before click: %v", err)), nil
		}
		err = chromedp.Run(pageCtx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Click(selector, chromedp.ByQuery),
			chromedp.Sleep(500*time.Millisecond),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("click failed: %v", err)), nil
		}
		_ = chromedp.Run(pageCtx,
			chromedp.Title(&titleAfter),
			chromedp.Evaluate(`window.location.href`, &urlAfter),
		)
		msg := fmt.Sprintf("Clicked element: %s", selector)
		if titleBefore != titleAfter {
			msg += fmt.Sprintf("\nTitle changed: %s -> %s", titleBefore, titleAfter)
		}
		if urlBefore != urlAfter {
			msg += fmt.Sprintf("\nURL changed: %s -> %s", urlBefore, urlAfter)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(msg)},
		}, nil
	}
}
