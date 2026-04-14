package tools

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/angoo/mcp-browser/internal/watch"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func screenshotHandler(defaultQuality int, store *watch.Store) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		viewportW := int64(0)
		viewportH := int64(0)
		if w := request.GetFloat("width", 0); w > 0 {
			viewportW = int64(w)
		}
		if h := request.GetFloat("height", 0); h > 0 {
			viewportH = int64(h)
		}
		if viewportW > 0 || viewportH > 0 {
			vw := viewportW
			vh := viewportH
			if vw == 0 {
				vw = 1280
			}
			if vh == 0 {
				vh = 720
			}
			if err := chromedp.Run(pageCtx, chromedp.EmulateViewport(vw, vh)); err != nil {
				return mcpErrorResult(fmt.Sprintf("failed to set viewport: %v", err)), nil
			}
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
		saveSnapshot(ctx, store, buf, "browser_screenshot")
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewImageContent(b64, "image/png")},
		}, nil
	}
}
