package tools

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/cdproto/input"
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
		timeout := getBrowserTimeout(ctx)
		var result struct {
			Steps int     `json:"steps"`
			Delay int     `json:"delay"`
			FromX float64 `json:"fromX"`
			FromY float64 `json:"fromY"`
			ToX   float64 `json:"toX"`
			ToY   float64 `json:"toY"`
		}
		err = runWithTimeout(pageCtx, timeout,
			chromedp.ActionFunc(func(ctx context.Context) error {
				fromX, fromY := readMousePos(ctx)
				if err := moveMouse(ctx, fromX, fromY, startX, startY); err != nil {
					return err
				}
				if err := input.DispatchMouseEvent(input.MousePressed, startX, startY).
					WithButton(input.Left).WithClickCount(1).Do(ctx); err != nil {
					return err
				}
				for i := 1; i <= steps; i++ {
					t := float64(i) / float64(steps)
					e := 1 - math.Pow(1-t, 3)
					x := startX + (endX-startX)*e
					y := startY + (endY-startY)*e
					if err := input.DispatchMouseEvent(input.MouseMoved, x, y).
						WithButton(input.Left).WithButtons(1).Do(ctx); err != nil {
						return err
					}
					if i < steps {
						time.Sleep(time.Duration(delay) * time.Millisecond)
					}
				}
				if err := input.DispatchMouseEvent(input.MouseReleased, endX, endY).
					WithButton(input.Left).WithClickCount(1).Do(ctx); err != nil {
					return err
				}
				writeMousePos(ctx, endX, endY)
				result.Steps = steps
				result.Delay = delay
				result.FromX = startX
				result.FromY = startY
				result.ToX = endX
				result.ToY = endY
				return nil
			}),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse drag failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Drag completed: %+v", result))},
		}, nil
	}
}
