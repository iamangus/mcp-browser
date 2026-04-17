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
		script := fmt.Sprintf(`(function(){
		var el = document.querySelector(%q);
		if (!el) throw new Error('Element not found: ' + %q);
		el.scrollIntoView({behavior: 'smooth', block: 'center'});
		var rect = el.getBoundingClientRect();
		var x = rect.left + rect.width / 2;
		var y = rect.top + rect.height / 2;
		el.dispatchEvent(new MouseEvent('mouseover', {bubbles: true, clientX: x, clientY: y}));
		el.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true, clientX: x, clientY: y}));
		return {x: Math.round(x), y: Math.round(y), width: Math.round(rect.width), height: Math.round(rect.height)};
		})()`, selector, selector)
		var result map[string]any
		err = runWithTimeout(pageCtx, timeout,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Evaluate(script, &result),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			if isTimeoutError(err) {
				return mcpErrorResult(fmt.Sprintf("Timeout after %v: element '%s' not found or not visible. Try taking a screenshot to see the current page state.", timeout, selector)), nil
			}
			return mcpErrorResult(fmt.Sprintf("hover failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Hovered over %s at position (%v, %v) with size %vx%v",
				selector, result["x"], result["y"], result["width"], result["height"]))},
		}, nil
	}
}
