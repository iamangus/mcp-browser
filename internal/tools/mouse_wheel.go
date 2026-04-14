package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mouseWheelHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		deltaX := request.GetFloat("deltaX", 0)
		deltaY := request.GetFloat("deltaY", -120)
		pageCtx := getPageCtx(ctx)
		script := fmt.Sprintf(`(function(){
		var scrollBefore = {x: window.scrollX, y: window.scrollY};
		window._lastMouseX = %f;
		window._lastMouseY = %f;
		document.dispatchEvent(new WheelEvent('wheel', {
			bubbles: true,
			clientX: %f,
			clientY: %f,
			deltaX: %f,
			deltaY: %f
		}));
		return {scrollBefore: scrollBefore, scrollAfter: {x: window.scrollX, y: window.scrollY}};
		})()`, xf, yf, xf, yf, deltaX, deltaY)
		var result map[string]any
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(script, &result),
			chromedp.Sleep(300*time.Millisecond),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse wheel failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Mouse wheel at (%.0f, %.0f) deltaX=%.0f deltaY=%.0f\nScroll result: %v", xf, yf, deltaX, deltaY, result))},
		}, nil
	}
}
