package tools

import (
	"context"
	"fmt"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mouseDragHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		startX, err := request.RequireFloat("startX")
		if err != nil {
			return mcpErrorResult("startX is required"), nil
		}
		startY, err := request.RequireFloat("startY")
		if err != nil {
			return mcpErrorResult("startY is required"), nil
		}
		endX, err := request.RequireFloat("endX")
		if err != nil {
			return mcpErrorResult("endX is required"), nil
		}
		endY, err := request.RequireFloat("endY")
		if err != nil {
			return mcpErrorResult("endY is required"), nil
		}
		if err := validation.ValidateCoordinates(startX, startY); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		if err := validation.ValidateCoordinates(endX, endY); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		steps := int(request.GetFloat("steps", 10))
		if steps < 1 {
			steps = 1
		}
		delay := int(request.GetFloat("delay", 10))
		if delay < 1 {
			delay = 1
		}
		pageCtx := getPageCtx(ctx)
		script := fmt.Sprintf(`(function(){
		return new Promise(function(resolve) {
			var steps = %d;
			var delay = %d;
			var sx = %f, sy = %f, ex = %f, ey = %f;
			document.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, clientX: sx, clientY: sy, button: 0}));
			var i = 1;
			function step() {
				if (i > steps) {
					document.dispatchEvent(new MouseEvent('mouseup', {bubbles: true, clientX: ex, clientY: ey, button: 0}));
					document.dispatchEvent(new MouseEvent('click', {bubbles: true, clientX: ex, clientY: ey, button: 0}));
					window._lastMouseX = ex;
					window._lastMouseY = ey;
					resolve({steps: steps, delay: delay, from: {x: sx, y: sy}, to: {x: ex, y: ey}});
					return;
				}
				var progress = i / steps;
				var px = sx + (ex - sx) * progress;
				var py = sy + (ey - sy) * progress;
				document.dispatchEvent(new MouseEvent('mousemove', {bubbles: true, clientX: px, clientY: py}));
				i++;
				setTimeout(step, delay);
			}
			setTimeout(step, delay);
		});
		})()`, steps, delay, startX, startY, endX, endY)
		var result map[string]any
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(script, &result),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse drag failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Drag completed: %v", result))},
		}, nil
	}
}
