package tools

import (
	"context"
	"fmt"

	"github.com/angoo/mcp-browser/internal/validation"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func evaluateHandler() func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		script, err := request.RequireString("script")
		if err != nil {
			return mcpErrorResult("script is required"), nil
		}
		if err := validation.ValidateScript(script); err != nil {
			return mcpErrorResult(err.Error()), nil
		}
		pageCtx := getPageCtx(ctx)
		wrapper := fmt.Sprintf(`(function(){
		var __logs = [];
		var __origConsole = {};
		['log','warn','error','info','debug'].forEach(function(level){
			__origConsole[level] = console[level];
			console[level] = function(){
				var args = Array.from(arguments).map(function(a){
					if (typeof a === 'object') try { return JSON.stringify(a); } catch(e) { return String(a); }
					return String(a);
				}).join(' ');
				__logs.push({level: level, message: args});
			};
		});
		try {
			var __result = (function(){ %s })();
			return JSON.stringify({result: __result !== undefined ? __result : null, logs: __logs, error: null});
		} catch(__e) {
			return JSON.stringify({result: null, logs: __logs, error: String(__e.message || __e)});
		} finally {
			['log','warn','error','info','debug'].forEach(function(level){
				console[level] = __origConsole[level];
			});
		}
		})()`, script)
		var raw string
		err = chromedp.Run(pageCtx,
			chromedp.Evaluate(wrapper, &raw),
		)
		if err != nil {
			return mcpErrorResult(fmt.Sprintf("evaluate failed: %v", err)), nil
		}
		var result struct {
			Result any              `json:"result"`
			Logs   []map[string]any `json:"logs"`
			Error  string           `json:"error"`
		}
		if err := jsonUnmarshal([]byte(raw), &result); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(raw)},
			}, nil
		}
		msg := fmt.Sprintf("Result: %v", result.Result)
		if result.Error != "" {
			msg = fmt.Sprintf("Error: %s\nResult: %v", result.Error, result.Result)
		}
		if len(result.Logs) > 0 {
			msg += "\n\nConsole output:"
			for _, log := range result.Logs {
				msg += fmt.Sprintf("\n  [%s] %v", log["level"], log["message"])
			}
		}
		isError := result.Error != ""
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(msg)},
			IsError: isError,
		}, nil
	}
}
