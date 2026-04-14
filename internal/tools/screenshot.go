package tools

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func screenshotHandler(defaultQuality int) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		quality := defaultQuality
		if q := request.GetFloat("quality", float64(defaultQuality)); q > 0 {
			quality = int(q)
		}
		if quality < 1 {
			quality = 1
		}
		if quality > 100 {
			quality = 100
		}
		pageCtx := getPageCtx(ctx)
		if w := request.GetFloat("width", 0); w > 0 {
			chromedp.Run(pageCtx, chromedp.EmulateViewport(int64(w), 720))
		}
		if h := request.GetFloat("height", 0); h > 0 {
			chromedp.Run(pageCtx, chromedp.EmulateViewport(1280, int64(h)))
		}
		var buf []byte
		var err error
		selector := request.GetString("selector", "")
		fullPage := request.GetBool("fullPage", false)
		if selector != "" {
			err = chromedp.Run(pageCtx,
				chromedp.WaitVisible(selector, chromedp.ByQuery),
				chromedp.Screenshot(selector, &buf, chromedp.ByQuery),
			)
		} else if fullPage {
			err = chromedp.Run(pageCtx, chromedp.FullScreenshot(&buf, quality))
		} else {
			err = chromedp.Run(pageCtx, chromedp.Screenshot(`body`, &buf, chromedp.ByQuery))
		}
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("screenshot failed: %v", err)), nil
		}
		b64 := base64.StdEncoding.EncodeToString(buf)
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewImageContent(b64, "image/png")},
		}, nil
	}
}
