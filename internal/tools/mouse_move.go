package tools

import (
	"context"
	"fmt"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mouseMoveHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		xf, err := request.RequireFloat("x")
		if err != nil {
			return mcpErrorResult("x is required and must be a number"), nil
		}
		yf, err := request.RequireFloat("y")
		if err != nil {
			return mcpErrorResult("y is required and must be a number"), nil
		}
		if err := validation.ValidateCoordinates(xf, yf); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		steps := int(request.GetFloat("steps", 1))
		if steps < 1 {
			steps = 1
		}
		pageCtx := getPageCtx(ctx)
		script := fmt.Sprintf(`(function(){
		var target = {x: %f, y: %f};
		var steps = %d;
		var currentX = window._lastMouseX || 0;
		var currentY = window._lastMouseY || 0;
		for (var i = 1; i <= steps; i++) {
			var progress = i / steps;
			var px = currentX + (target.x - currentX) * progress;
			var py = currentY + (target.y - currentY) * progress;
			document.dispatchEvent(new MouseEvent('mousemove', {bubbles: true, clientX: px, clientY: py}));
		}
		window._lastMouseX = target.x;
		window._lastMouseY = target.y;
		return {x: target.x, y: target.y, steps: steps};
		})()`, xf, yf, steps)
		var result map[string]any
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(script, &result),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse move failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Mouse moved to (%v, %v) in %v steps", result["x"], result["y"], result["steps"]))},
		}, nil
	}
}
