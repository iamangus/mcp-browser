package tools

import (
	"context"
	"fmt"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mouseDownHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		button := request.GetString("button", "left")
		if err := validation.ValidateMouseButton(button); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		btnNum := mouseButtonToNumber(button)
		pageCtx := getPageCtx(ctx)
		script := fmt.Sprintf(`(function(){
		document.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, clientX: %f, clientY: %f, button: %d}));
		window._lastMouseX = %f;
		window._lastMouseY = %f;
		return true;
		})()`, xf, yf, btnNum, xf, yf)
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(script, nil),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse down failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Mouse %s button pressed at (%.0f, %.0f)", button, xf, yf))},
		}, nil
	}
}
