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
		var result struct {
			ScrollBefore struct {
				X int64 `json:"x"`
				Y int64 `json:"y"`
			} `json:"scrollBefore"`
			ScrollAfter struct {
				X int64 `json:"x"`
				Y int64 `json:"y"`
			} `json:"scrollAfter"`
		}
		err = runWithTimeout(pageCtx, getBrowserTimeout(ctx),
			chromedp.Evaluate(`({scrollBefore: {x: window.scrollX, y: window.scrollY}})`, &result),
			chromedp.ActionFunc(func(ctx context.Context) error {
				fromX, fromY := readMousePos(ctx)
				if err := moveMouse(ctx, fromX, fromY, xf, yf); err != nil {
					return err
				}
				return input.DispatchMouseEvent(input.MouseWheel, xf, yf).
					WithDeltaX(deltaX).WithDeltaY(deltaY).Do(ctx)
			}),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.Evaluate(`({scrollAfter: {x: window.scrollX, y: window.scrollY}})`, &result),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse wheel failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Mouse wheel at (%.0f, %.0f) deltaX=%.0f deltaY=%.0f\nScroll result: %+v", xf, yf, deltaX, deltaY, result))},
		}, nil
	}
}
