package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/cdproto/input"
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
		pageCtx := getPageCtx(ctx)
		timeout := getBrowserTimeout(ctx)
		var titleBefore, titleAfter, urlBefore, urlAfter string
		_ = runWithTimeout(pageCtx, timeout,
			chromedp.Title(&titleBefore),
			chromedp.Evaluate(`window.location.href`, &urlBefore),
		)
		err = runWithTimeout(pageCtx, timeout,
			chromedp.ActionFunc(func(ctx context.Context) error {
				if err := input.DispatchMouseEvent(input.MouseReleased, xf, yf).
					WithButton(cdpMouseButton(button)).WithClickCount(1).Do(ctx); err != nil {
					return err
				}
				writeMousePos(ctx, xf, yf)
				return nil
			}),
			chromedp.Sleep(300*time.Millisecond),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse up failed: %v", err)), nil
		}
		_ = runWithTimeout(pageCtx, timeout,
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
