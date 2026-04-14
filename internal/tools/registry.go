package tools

import (
	"context"

	"github.com/angoo/mcp-browser/internal/browser"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type browserKey struct{}

var BrowserKey = browserKey{}

func RegisterTools(s *server.MCPServer, screenshotQuality int) {
	s.AddTool(mcp.NewTool("browser_navigate",
		mcp.WithDescription("Navigate to a URL in the browser. Returns the page title and final URL after navigation."),
		mcp.WithString("url", mcp.Description("The URL to navigate to"), mcp.Required()),
		mcp.WithString("waitUntil", mcp.Description("Navigation wait condition: 'load', 'domcontentloaded', or 'networkidle'"), mcp.DefaultString("networkidle")),
	), navigateHandler())

	s.AddTool(mcp.NewTool("browser_screenshot",
		mcp.WithDescription("Take a screenshot of the current page or a specific element. Returns the screenshot as a base64-encoded PNG image."),
		mcp.WithString("selector", mcp.Description("CSS selector of the element to screenshot. If omitted, takes a full page screenshot.")),
		mcp.WithNumber("quality", mcp.Description("Screenshot quality (1-100) for JPEG. Default: 80.")),
		mcp.WithNumber("width", mcp.Description("Override viewport width for the screenshot.")),
		mcp.WithNumber("height", mcp.Description("Override viewport height for the screenshot.")),
		mcp.WithBoolean("fullPage", mcp.Description("Whether to take a full page screenshot (scroll capture). Default: false.")),
	), screenshotHandler(screenshotQuality))

	s.AddTool(mcp.NewTool("browser_click",
		mcp.WithDescription("Click on an element matching a CSS selector. Scrolls the element into view if needed."),
		mcp.WithString("selector", mcp.Description("CSS selector of the element to click"), mcp.Required()),
	), clickHandler())

	s.AddTool(mcp.NewTool("browser_fill",
		mcp.WithDescription("Fill an input field with a value. Clears existing content and types the new value. Works with React-controlled inputs."),
		mcp.WithString("selector", mcp.Description("CSS selector of the input element"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The value to fill in"), mcp.Required()),
	), fillHandler())

	s.AddTool(mcp.NewTool("browser_select",
		mcp.WithDescription("Select an option from a dropdown element by value or visible text."),
		mcp.WithString("selector", mcp.Description("CSS selector of the select element"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The value or visible text of the option to select"), mcp.Required()),
	), selectHandler())

	s.AddTool(mcp.NewTool("browser_hover",
		mcp.WithDescription("Hover over an element. Detects tooltips, dropdowns, or popovers that appear after hovering."),
		mcp.WithString("selector", mcp.Description("CSS selector of the element to hover over"), mcp.Required()),
	), hoverHandler())

	s.AddTool(mcp.NewTool("browser_evaluate",
		mcp.WithDescription("Execute JavaScript code in the browser page. Returns the result and any console output. Dangerous patterns (eval, fetch, require, process) are blocked."),
		mcp.WithString("script", mcp.Description("JavaScript code to execute"), mcp.Required()),
	), evaluateHandler())

	s.AddTool(mcp.NewTool("browser_mouse_click",
		mcp.WithDescription("Click the mouse at specific x,y coordinates with configurable button and click count."),
		mcp.WithNumber("x", mcp.Description("X coordinate"), mcp.Required()),
		mcp.WithNumber("y", mcp.Description("Y coordinate"), mcp.Required()),
		mcp.WithString("button", mcp.Description("Mouse button: left, right, middle, back, forward (default: left)"), mcp.DefaultString("left")),
		mcp.WithNumber("clickCount", mcp.Description("Number of clicks (default: 1)")),
	), mouseClickHandler())

	s.AddTool(mcp.NewTool("browser_mouse_move",
		mcp.WithDescription("Move the mouse to specific x,y coordinates on the page."),
		mcp.WithNumber("x", mcp.Description("X coordinate"), mcp.Required()),
		mcp.WithNumber("y", mcp.Description("Y coordinate"), mcp.Required()),
		mcp.WithNumber("steps", mcp.Description("Number of interpolation steps for smooth movement (default: 1)")),
	), mouseMoveHandler())

	s.AddTool(mcp.NewTool("browser_mouse_down",
		mcp.WithDescription("Press and hold a mouse button at specific coordinates. Used for drag operations."),
		mcp.WithNumber("x", mcp.Description("X coordinate"), mcp.Required()),
		mcp.WithNumber("y", mcp.Description("Y coordinate"), mcp.Required()),
		mcp.WithString("button", mcp.Description("Mouse button: left, right, middle (default: left)"), mcp.DefaultString("left")),
	), mouseDownHandler())

	s.AddTool(mcp.NewTool("browser_mouse_up",
		mcp.WithDescription("Release a previously pressed mouse button at specific coordinates."),
		mcp.WithNumber("x", mcp.Description("X coordinate"), mcp.Required()),
		mcp.WithNumber("y", mcp.Description("Y coordinate"), mcp.Required()),
		mcp.WithString("button", mcp.Description("Mouse button: left, right, middle (default: left)"), mcp.DefaultString("left")),
	), mouseUpHandler())

	s.AddTool(mcp.NewTool("browser_mouse_drag",
		mcp.WithDescription("Perform a drag and drop operation from start coordinates to end coordinates."),
		mcp.WithNumber("startX", mcp.Description("Starting X coordinate"), mcp.Required()),
		mcp.WithNumber("startY", mcp.Description("Starting Y coordinate"), mcp.Required()),
		mcp.WithNumber("endX", mcp.Description("Ending X coordinate"), mcp.Required()),
		mcp.WithNumber("endY", mcp.Description("Ending Y coordinate"), mcp.Required()),
		mcp.WithNumber("steps", mcp.Description("Number of interpolation steps (default: 10)")),
		mcp.WithNumber("delay", mcp.Description("Delay in ms between steps (default: 10)")),
	), mouseDragHandler())

	s.AddTool(mcp.NewTool("browser_mouse_wheel",
		mcp.WithDescription("Scroll the mouse wheel at specific coordinates with configurable deltaX and deltaY."),
		mcp.WithNumber("x", mcp.Description("X coordinate"), mcp.Required()),
		mcp.WithNumber("y", mcp.Description("Y coordinate"), mcp.Required()),
		mcp.WithNumber("deltaX", mcp.Description("Horizontal scroll delta (default: 0)")),
		mcp.WithNumber("deltaY", mcp.Description("Vertical scroll delta (negative = scroll up, positive = scroll down, default: -120)")),
	), mouseWheelHandler())

	s.AddTool(mcp.NewTool("browser_get_cookies",
		mcp.WithDescription("Retrieve cookies from the current page. Can filter by cookie names or domain."),
		mcp.WithString("names", mcp.Description("Optional comma-separated list of cookie names to filter by")),
		mcp.WithString("domain", mcp.Description("Optional domain to filter cookies by")),
	), getCookiesHandler())

	s.AddTool(mcp.NewTool("browser_set_cookies",
		mcp.WithDescription("Set cookies on the current page. Useful for setting authentication tokens, session cookies, etc."),
		mcp.WithString("cookies", mcp.Description("JSON array of cookies to set. Each cookie has: name (required), value (required), domain, path, secure, httpOnly, sameSite"), mcp.Required()),
	), setCookiesHandler())

	s.AddTool(mcp.NewTool("browser_delete_cookies",
		mcp.WithDescription("Delete cookies from the current page. Use name '*' to delete all cookies for a domain."),
		mcp.WithString("cookies", mcp.Description("JSON array of cookies to delete. Each has: name (required, use '*' for all), domain (optional)"), mcp.Required()),
	), deleteCookiesHandler())
}

func BrowserContextMiddleware(bm *browser.BrowserManager) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			session := server.ClientSessionFromContext(ctx)
			var sessionID string
			if session != nil {
				sessionID = session.SessionID()
			} else {
				sessionID = "default"
			}
			pageCtx, err := bm.GetOrCreatePage(sessionID)
			if err != nil {
				return mcpErrorResult("failed to get browser page: " + err.Error()), nil
			}
			enrichedCtx := context.WithValue(ctx, BrowserKey, pageCtx)
			return next(enrichedCtx, request)
		}
	}
}

func getPageCtx(ctx context.Context) context.Context {
	v := ctx.Value(BrowserKey)
	if v == nil {
		return nil
	}
	pc, ok := v.(context.Context)
	if !ok {
		return nil
	}
	return pc
}
