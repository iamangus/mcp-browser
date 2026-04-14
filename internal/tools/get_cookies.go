package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func getCookiesHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pageCtx := getPageCtx(ctx)
		script := `(function(){
		return document.cookie.split('; ').filter(function(c) { return c.length > 0; }).map(function(c) {
			var parts = c.split('=');
			return {name: parts[0], value: parts.slice(1).join('=')};
		});
	})()`
		var rawCookies []map[string]string
		err := chromedp.Run(pageCtx,
			chromedp.Evaluate(script, &rawCookies),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("get cookies failed: %v", err)), nil
		}
		currentDomain := ""
		_ = chromedp.Run(pageCtx, chromedp.Evaluate(`window.location.hostname`, &currentDomain))

		namesStr := request.GetString("names", "")
		var filterNames []string
		if namesStr != "" {
			filterNames = strings.Split(namesStr, ",")
			for i := range filterNames {
				filterNames[i] = strings.TrimSpace(filterNames[i])
			}
		}

		filtered := make([]cookieInfo, 0)
		for _, c := range rawCookies {
			if len(filterNames) > 0 && !containsString(filterNames, c["name"]) {
				continue
			}
			cookie := cookieInfo{
				Name:   c["name"],
				Value:  c["value"],
				Domain: currentDomain,
				Path:   "/",
				Size:   len(c["name"]) + len(c["value"]),
			}
			filtered = append(filtered, cookie)
		}
		data, _ := json.MarshalIndent(filtered, "", "  ")
		msg := fmt.Sprintf("Retrieved %d cookie(s) for domain: %s\n\n%s", len(filtered), currentDomain, string(data))
		if len(filtered) > 0 {
			var authCookies []string
			for _, c := range filtered {
				lower := strings.ToLower(c.Name)
				if strings.Contains(lower, "session") || strings.Contains(lower, "token") || strings.Contains(lower, "auth") {
					authCookies = append(authCookies, c.Name)
				}
			}
			if len(authCookies) > 0 {
				msg += fmt.Sprintf("\n\nAuth-related cookies detected: %s", strings.Join(authCookies, ", "))
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(msg)},
		}, nil
	}
}

type cookieInfo struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	HTTPOnly bool   `json:"httpOnly"`
	Secure   bool   `json:"secure"`
	SameSite string `json:"sameSite,omitempty"`
	Size     int    `json:"size"`
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
