package tools

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func mcpErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(msg)},
		IsError: true,
	}
}

// resultText extracts the concatenated text from an MCP tool result so the
// middleware can log the actual error message an IsError result carries.
func resultText(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(mcp.TextContent); ok {
			if sb.Len() > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

func runWithTimeout(parentCtx context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	return chromedp.Run(ctx, actions...)
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out")
}
