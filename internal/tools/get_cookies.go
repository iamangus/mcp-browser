package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func getCookiesHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pageCtx := getPageCtx(ctx)
		var cookies []*network.Cookie
		err := runWithTimeout(pageCtx, getBrowserTimeout(ctx),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				cookies, err = network.GetCookies().Do(ctx)
				return err
			}),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("get cookies failed: %v", err)), nil
		}

		namesStr := request.GetString("names", "")
		var filterNames []string
		if namesStr != "" {
			filterNames = strings.Split(namesStr, ",")
			for i := range filterNames {
				filterNames[i] = strings.TrimSpace(filterNames[i])
			}
		}
		domainFilter := strings.TrimSpace(request.GetString("domain", ""))

		filtered := make([]cookieInfo, 0)
		for _, c := range cookies {
			if len(filterNames) > 0 && !containsString(filterNames, c.Name) {
				continue
			}
			if domainFilter != "" && !cookieDomainMatches(c.Domain, domainFilter) {
				continue
			}
			filtered = append(filtered, cookieInfo{
				Name:     c.Name,
				Value:    c.Value,
				Domain:   c.Domain,
				Path:     c.Path,
				HTTPOnly: c.HTTPOnly,
				Secure:   c.Secure,
				SameSite: string(c.SameSite),
				Size:     int(c.Size),
			})
		}
		data, _ := json.MarshalIndent(filtered, "", "  ")
		msg := fmt.Sprintf("Retrieved %d cookie(s)\n\n%s", len(filtered), string(data))
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

// cookieDomainMatches reports whether cookieDomain (which may carry a leading
// dot) belongs to the requested filter domain, matching the domain itself or
// any subdomain.
func cookieDomainMatches(cookieDomain, filter string) bool {
	cookieDomain = strings.TrimPrefix(cookieDomain, ".")
	filter = strings.TrimPrefix(filter, ".")
	if strings.EqualFold(cookieDomain, filter) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(cookieDomain), "."+strings.ToLower(filter))
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
