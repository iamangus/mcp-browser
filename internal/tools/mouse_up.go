package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mouseUpHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		var titleBefore, titleAfter, urlBefore, urlAfter string
		_ = chromedp.Run(pageCtx,
			chromedp.Title(&titleBefore),
			chromedp.Evaluate(`window.location.href`, &urlBefore),
		)
		script := fmt.Sprintf(`(function(){
		document.dispatchEvent(new MouseEvent('mouseup', {bubbles: true, clientX: %f, clientY: %f, button: %d}));
		document.dispatchEvent(new MouseEvent('click', {bubbles: true, clientX: %f, clientY: %f, button: %d}));
		window._lastMouseX = %f;
		window._lastMouseY = %f;
		return true;
		})()`, xf, yf, btnNum, xf, yf, btnNum, xf, yf)
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(script, nil),
			chromedp.Sleep(300*time.Millisecond),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse up failed: %v", err)), nil
		}
		_ = chromedp.Run(pageCtx,
			chromedp.Title(&titleAfter),
			chromedp.Evaluate(`window.location.href`, &urlAfter),
		)
		msg := fmt.Sprintf("Mouse %s button released at (%.0f, %.0f)", button, xf, yf)
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
