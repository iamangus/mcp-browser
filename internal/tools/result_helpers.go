package tools

import "github.com/mark3labs/mcp-go/mcp"

func mcpErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(msg)},
		IsError: true,
	}
}
