package tools

import (
	"context"
	"fmt"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func selectHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, err := request.RequireString("selector")
		if err != nil {
			return mcpErrorResult("selector is required"), nil
		}
		value, err := request.RequireString("value")
		if err != nil || value == "" {
			return mcpErrorResult("value is required"), nil
		}
		if err := validation.ValidateSelector(selector); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		pageCtx := getPageCtx(ctx)
		timeout := getBrowserTimeout(ctx)
		script := fmt.Sprintf(`(function(){
		var sel = document.querySelector(%q);
		if (!sel) throw new Error('Select element not found: ' + %q);
		var options = Array.from(sel.options).map(function(o) {
			return {value: o.value, text: o.text};
		});
		var matched = options.find(function(o) { return o.value === %q || o.text === %q; });
		if (!matched) {
			throw new Error('Option not found. Available: ' + JSON.stringify(options.map(function(o) { return o.value + ' (' + o.text + ')'; })));
		}
		sel.value = matched.value;
		sel.dispatchEvent(new Event('change', {bubbles: true}));
		return {selected: matched.value, text: matched.text, allOptions: options};
		})()`, selector, selector, value, value)
		var result map[string]any
		err = runWithTimeout(pageCtx, timeout,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.Evaluate(script, &result),
		)
		if err != nil {
			if isTimeoutError(err) {
				return mcpErrorResult(fmt.Sprintf("Timeout after %v: element '%s' not found or not visible. Try taking a screenshot to see the current page state.", timeout, selector)), nil
			}
			return mcpErrorResult(fmt.Sprintf("select failed: %v", err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Selected value: %v\nAll options: %v", result["selected"], result["allOptions"]))},
		}, nil
	}
}
