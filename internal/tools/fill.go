package tools

import (
	"context"
	"fmt"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func fillHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, err := request.RequireString("selector")
		if err != nil {
			return mcpErrorResult("selector is required"), nil
		}
		value, err := request.RequireString("value")
		if err != nil {
			return mcpErrorResult("value is required"), nil
		}
		if err := validation.ValidateSelector(selector); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		pageCtx := getPageCtx(ctx)
		script := fmt.Sprintf(`(function(){
		var el = document.querySelector(%q);
		if (!el) throw new Error('Element not found: ' + %q);
		el.focus();
		el.select();
		var nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set ||
			Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value')?.set;
		if (nativeSetter) { nativeSetter.call(el, %q); }
		else { el.value = %q; }
		el.dispatchEvent(new Event('input', {bubbles: true}));
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return el.value;
		})()`, selector, selector, value, value)
		var actualValue string
		err = chromedp.Run(pageCtx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Evaluate(script, &actualValue),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("fill failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Filled %s with value: %s", selector, actualValue))},
		}, nil
	}
}
