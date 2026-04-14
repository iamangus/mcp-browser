package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func setCookiesHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cookiesStr, err := request.RequireString("cookies")
		if err != nil {
			return mcpErrorResult("cookies is required"), nil
		}
		var rawCookies []map[string]any
		if err := json.Unmarshal([]byte(cookiesStr), &rawCookies); err != nil {
			return mcpErrorResult("cookies must be a valid JSON array"), nil
		}
		if len(rawCookies) == 0 {
			return mcpErrorResult("cookies array must not be empty"), nil
		}
		pageCtx := getPageCtx(ctx)
		currentDomain := ""
		chromedp.Run(pageCtx, chromedp.Evaluate(`window.location.hostname`, &currentDomain))
		var results []string
		for i, cookie := range rawCookies {
			name, _ := cookie["name"].(string)
			value, _ := cookie["value"].(string)
			if name == "" {
				results = append(results, fmt.Sprintf("  [%d] SKIP: cookie name is required", i))
				continue
			}
			domain, _ := cookie["domain"].(string)
			if domain == "" {
				domain = currentDomain
			}
			path, _ := cookie["path"].(string)
			if path == "" {
				path = "/"
			}
			secure := true
			if s, ok := cookie["secure"].(bool); ok {
				secure = s
			}
			httpOnly := false
			if h, ok := cookie["httpOnly"].(bool); ok {
				httpOnly = h
			}
			sameSite, _ := cookie["sameSite"].(string)
			cookieStr := fmt.Sprintf("%s=%s; path=%s; domain=%s", name, value, path, domain)
			if secure {
				cookieStr += "; Secure"
			}
			if httpOnly {
				cookieStr += "; HttpOnly"
			}
			if sameSite != "" {
				cookieStr += fmt.Sprintf("; SameSite=%s", sameSite)
			}
			script := fmt.Sprintf(`document.cookie = %q`, cookieStr)
			var setVal string
			err := chromedp.Run(pageCtx,
				chromedp.Evaluate(script, &setVal),
			)
			if err != nil {
				results = append(results, fmt.Sprintf("  [%d] FAIL: %s = ***: %v", i, name, err))
			} else {
				results = append(results, fmt.Sprintf("  [%d] OK: %s (domain=%s, path=%s, secure=%v, httpOnly=%v)", i, name, domain, path, secure, httpOnly))
			}
		}
		msg := fmt.Sprintf("Set %d cookie(s):\n%s", len(rawCookies), strings.Join(results, "\n"))
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(msg)},
		}, nil
	}
}
