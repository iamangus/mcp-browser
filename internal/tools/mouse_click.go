package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mouseClickHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		clickCount := int(request.GetFloat("clickCount", 1))
		if clickCount < 1 {
			clickCount = 1
		}
		buttonNum := mouseButtonToNumber(button)
		pageCtx := getPageCtx(ctx)
		var titleBefore, titleAfter, urlBefore, urlAfter string
		chromedp.Run(pageCtx,
			chromedp.Title(&titleBefore),
			chromedp.Evaluate(`window.location.href`, &urlBefore),
		)
		script := fmt.Sprintf(`(function(){
		var btn = %d;
		var count = %d;
		for (var i = 0; i < count; i++) {
			document.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, clientX: %f, clientY: %f, button: btn}));
			document.dispatchEvent(new MouseEvent('mouseup', {bubbles: true, clientX: %f, clientY: %f, button: btn}));
			document.dispatchEvent(new MouseEvent('click', {bubbles: true, clientX: %f, clientY: %f, button: btn, detail: count}));
		}
		window._lastMouseX = %f;
		window._lastMouseY = %f;
		return true;
		})()`, buttonNum, clickCount, xf, yf, xf, yf, xf, yf, xf, yf)
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(script, nil),
			chromedp.Sleep(300*time.Millisecond),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse click failed: %v", err)), nil
		}
		chromedp.Run(pageCtx,
			chromedp.Title(&titleAfter),
			chromedp.Evaluate(`window.location.href`, &urlAfter),
		)
		msg := fmt.Sprintf("Clicked at (%.0f, %.0f) with %s button (%d time%s)", xf, yf, button, clickCount, plural(clickCount))
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

func mouseButtonToNumber(button string) int {
	switch button {
	case "right":
		return 2
	case "middle":
		return 1
	case "back":
		return 3
	case "forward":
		return 4
	default:
		return 0
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
