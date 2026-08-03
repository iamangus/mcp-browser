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
		steps := int(request.GetFloat("steps", 0))
		if steps < 0 {
			steps = 0
		}
		pageCtx := getPageCtx(ctx)
		var result struct {
			X     float64 `json:"x"`
			Y     float64 `json:"y"`
			Steps int     `json:"steps"`
		}
		err = runWithTimeout(pageCtx, getBrowserTimeout(ctx),
			moveToSteps(func() (float64, float64) { return xf, yf }, steps),
			chromedp.Evaluate(`({x: window._lastMouseX, y: window._lastMouseY})`, &result),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("mouse move failed: %v", err)), nil
		}
		if result.Steps == 0 {
			result.Steps = steps
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Mouse moved to (%v, %v) in %v steps", result.X, result.Y, result.Steps))},
		}, nil
	}
}
