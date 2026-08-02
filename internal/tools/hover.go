package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func hoverHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, err := request.RequireString("selector")
		if err != nil {
			return mcpErrorResult("selector is required"), nil
		}
		if err := validation.ValidateSelector(selector); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		pageCtx := getPageCtx(ctx)
		timeout := getBrowserTimeout(ctx)
		var pos struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}
		err = runWithTimeout(pageCtx, timeout,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Evaluate(elementCenterScript(selector), &pos),
			moveTo(func() (float64, float64) { return pos.X, pos.Y }),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			if isTimeoutError(err) {
				return mcpErrorResult(fmt.Sprintf("Timeout after %v: element '%s' not found or not visible. Try taking a screenshot to see the current page state.", timeout, selector)), nil
			}
			return mcpErrorResult(fmt.Sprintf("hover failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Hovered over %s at position (%.0f, %.0f)",
				selector, pos.X, pos.Y))},
		}, nil
	}
}
