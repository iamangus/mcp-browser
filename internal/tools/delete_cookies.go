package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func deleteCookiesHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		_ = chromedp.Run(pageCtx, chromedp.Evaluate(`window.location.hostname`, &currentDomain))
		var results []string
		totalDeleted := 0
		for i, cookie := range rawCookies {
			name, _ := cookie["name"].(string)
			if name == "" {
				results = append(results, fmt.Sprintf("  [%d] SKIP: cookie name is required", i))
				continue
			}
			domain, _ := cookie["domain"].(string)
			if domain == "" {
				domain = currentDomain
			}
			if name == "*" {
				script := `(function(){
				var cookies = document.cookie.split('; ');
				var count = 0;
				cookies.forEach(function(c) {
					if (c.length > 0) {
						var name = c.split('=')[0];
						document.cookie = name + '=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; domain=' + window.location.hostname;
						count++;
					}
				});
				return count;
				})()`
				var count float64
				err := chromedp.Run(pageCtx,
					chromedp.Evaluate(script, &count),
				)
				if err != nil {
					results = append(results, fmt.Sprintf("  [%d] FAIL: delete all cookies: %v", i, err))
				} else {
					totalDeleted += int(count)
					results = append(results, fmt.Sprintf("  [%d] OK: deleted all %d cookie(s) for domain %s", i, int(count), domain))
				}
			} else {
				script := fmt.Sprintf(`document.cookie = %q`, name+"=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; domain="+domain)
				err := chromedp.Run(pageCtx,
					chromedp.Evaluate(script, nil),
				)
				if err != nil {
					results = append(results, fmt.Sprintf("  [%d] FAIL: %s: %v", i, name, err))
				} else {
					totalDeleted++
					results = append(results, fmt.Sprintf("  [%d] OK: deleted cookie '%s' for domain %s", i, name, domain))
				}
			}
		}
		msg := fmt.Sprintf("Deleted %d cookie(s) total:\n%s", totalDeleted, strings.Join(results, "\n"))
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(msg)},
		}, nil
	}
}
