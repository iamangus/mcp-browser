package tools

import (
	"context"
	"log/slog"
	"time"

	"github.com/angoo/mcp-browser/internal/browser"
	"github.com/angoo/mcp-browser/internal/watch"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type browserKey struct{}

var BrowserKey = browserKey{}

func RegisterTools(s *server.MCPServer, screenshotQuality int, snapshotStore *watch.Store) {
	s.AddTool(mcp.NewTool("browser_navigate",
		mcp.WithDescription("Navigate to a URL in the browser. Returns the page title and final URL after navigation. Only http/https URLs are allowed; localhost and private-network addresses are rejected."),
		mcp.WithString("url", mcp.Description("The URL to navigate to"), mcp.Required()),
		mcp.WithString("waitUntil", mcp.Description("Navigation wait condition: 'load', 'domcontentloaded', or 'networkidle'"), mcp.DefaultString("domcontentloaded")),
	), navigateHandler())

	s.AddTool(mcp.NewTool("browser_screenshot",
		mcp.WithDescription("Take a screenshot of the current page or a specific element. Returns the screenshot as an image. When both selector and fullPage are set, selector wins. Note: width/height permanently resize the session viewport, which changes the coordinate space used by the mouse tools."),
		mcp.WithString("selector", mcp.Description("CSS selector of the element to screenshot. If omitted, takes a full page screenshot.")),
		mcp.WithNumber("quality", mcp.Description("Screenshot quality (1-100) for JPEG. Default: 80.")),
		mcp.WithNumber("width", mcp.Description("Resize the session viewport to this width. Persists for the session and affects mouse-tool coordinates.")),
		mcp.WithNumber("height", mcp.Description("Resize the session viewport to this height. Persists for the session and affects mouse-tool coordinates.")),
		mcp.WithBoolean("fullPage", mcp.Description("Whether to take a full page screenshot (scroll capture). Default: false.")),
	), screenshotHandler(screenshotQuality, snapshotStore))

	s.AddTool(mcp.NewTool("browser_click",
		mcp.WithDescription("Click on an element matching a CSS selector. Preferred over browser_mouse_click whenever the target has a stable, unique selector: this tool waits for the element to be visible, scrolls it into view, clicks its center with a human-like pointer path, and reports title/URL changes. Use browser_mouse_click instead for exact-pixel clicks, canvas/WebGL content, shadow DOM, cross-origin iframes, elements covered by overlays, ambiguous selectors that match multiple elements, or right/middle clicks and double-clicks."),
		mcp.WithString("selector", mcp.Description("CSS selector of the element to click"), mcp.Required()),
	), clickHandler())

	s.AddTool(mcp.NewTool("browser_fill",
		mcp.WithDescription("Set the value of an input or textarea by CSS selector. Clears existing content and works with React-controlled inputs by dispatching input/change events. This does not simulate keystrokes, so it will not work for contenteditable fields or inputs with custom keydown/mask handling (use browser_evaluate for those)."),
		mcp.WithString("selector", mcp.Description("CSS selector of the input element"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The value to fill in"), mcp.Required()),
	), fillHandler())

	s.AddTool(mcp.NewTool("browser_select",
		mcp.WithDescription("Select an option from a native <select> element by value or visible text. Returns all options in the result. Does not work on custom dropdowns (react-select, comboboxes) — use browser_mouse_click or browser_fill for those."),
		mcp.WithString("selector", mcp.Description("CSS selector of the select element"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The value or visible text of the option to select"), mcp.Required()),
	), selectHandler())

	s.AddTool(mcp.NewTool("browser_hover",
		mcp.WithDescription("Move the pointer over an element by CSS selector and wait briefly so tooltips/dropdowns/popovers can appear. It does not read what appeared — take a screenshot afterwards to see it."),
		mcp.WithString("selector", mcp.Description("CSS selector of the element to hover over"), mcp.Required()),
	), hoverHandler())

	s.AddTool(mcp.NewTool("browser_evaluate",
		mcp.WithDescription("Execute JavaScript in the page and return the result plus console output. The script runs inside a function body, so use \"return <expr>\" to yield a value and it must be JSON-serializable (functions and DOM nodes are not); var declarations do not persist across calls. Blocked tokens: eval, Function, fetch, require, process, import, export, __proto__, constructor, prototype."),
		mcp.WithString("script", mcp.Description("JavaScript code to execute"), mcp.Required()),
	), evaluateHandler())

	s.AddTool(mcp.NewTool("browser_mouse_click",
		mcp.WithDescription("Click the mouse at specific x,y coordinates with configurable button and click count. Prefer browser_click when a CSS selector addresses the element — it is more robust (waits for visibility, scrolls into view, reports navigation). Use this for coordinate clicks: a specific pixel from a screenshot, canvas/WebGL content, shadow DOM, cross-origin iframes, elements covered by overlays, ambiguous selectors, and right/middle clicks or double-clicks."),
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
		mcp.WithDescription("Retrieve cookies for the current page, optionally filtered by cookie names or domain. Includes HttpOnly cookies."),
		mcp.WithString("names", mcp.Description("Optional comma-separated list of cookie names to filter by")),
		mcp.WithString("domain", mcp.Description("Optional domain to filter cookies by")),
	), getCookiesHandler())

	s.AddTool(mcp.NewTool("browser_set_cookies",
		mcp.WithDescription("Set cookies on the current page via document.cookie. Useful for authentication tokens and session cookies. Cannot set HttpOnly cookies, and Secure cookies are only applied on HTTPS pages."),
		mcp.WithString("cookies", mcp.Description("JSON array of cookies to set. Each cookie has: name (required), value (required), domain, path, secure, httpOnly, sameSite"), mcp.Required()),
	), setCookiesHandler())

	s.AddTool(mcp.NewTool("browser_delete_cookies",
		mcp.WithDescription("Delete cookies from the current page. Use name '*' to delete all cookies for a domain."),
		mcp.WithString("cookies", mcp.Description("JSON array of cookies to delete. Each has: name (required, use '*' for all), domain (optional)"), mcp.Required()),
	), deleteCookiesHandler())
}

type timeoutKey struct{}

var TimeoutKey = timeoutKey{}

func BrowserContextMiddleware(bm *browser.BrowserManager, browserTimeout time.Duration) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			sessionID := getSessionID(ctx)
			toolName := request.Params.Name
			pageCtx, err := bm.GetOrCreatePage(sessionID)
			if err != nil {
				slog.Default().Warn("tool call failed: no browser page",
					"tool", toolName,
					"session", sessionID,
					"duration", time.Since(start).Round(time.Millisecond),
					"error", err,
				)
				return mcpErrorResult("failed to get browser page: " + err.Error()), nil
			}
			enrichedCtx := context.WithValue(ctx, BrowserKey, pageCtx)
			enrichedCtx = context.WithValue(enrichedCtx, TimeoutKey, browserTimeout)
			// Take an exclusive lease on the page for the whole tool call so the
			// /watch live streamer skips its screenshot loop while the tool
			// (especially a navigation) is using the renderer, and so two tools
			// cannot use the same page concurrently. The streamer yields to a
			// waiting tool, so the wait here is bounded by one screenshot.
			release, lockWait, err := bm.BeginPageOp(ctx, sessionID)
			if err != nil {
				slog.Default().Warn("tool call failed: page busy",
					"tool", toolName,
					"session", sessionID,
					"duration", time.Since(start).Round(time.Millisecond),
					"error", err,
				)
				return mcpErrorResult("browser page busy: " + err.Error()), nil
			}
			defer release()
			result, err := next(enrichedCtx, request)
			if err != nil {
				slog.Default().Warn("tool call failed",
					"tool", toolName,
					"session", sessionID,
					"duration", time.Since(start).Round(time.Millisecond),
					"page_lock_wait", lockWait.Round(time.Millisecond),
					"error", err,
				)
			} else if result != nil && result.IsError {
				slog.Default().Warn("tool call returned error",
					"tool", toolName,
					"session", sessionID,
					"duration", time.Since(start).Round(time.Millisecond),
					"page_lock_wait", lockWait.Round(time.Millisecond),
					"result", resultText(result),
				)
			} else {
				slog.Default().Info("tool call",
					"tool", toolName,
					"session", sessionID,
					"duration", time.Since(start).Round(time.Millisecond),
					"page_lock_wait", lockWait.Round(time.Millisecond),
				)
			}
			return result, err
		}
	}
}

func getBrowserTimeout(ctx context.Context) time.Duration {
	v := ctx.Value(TimeoutKey)
	if v == nil {
		return 30 * time.Second
	}
	t, ok := v.(time.Duration)
	if !ok {
		return 30 * time.Second
	}
	return t
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

func getSessionID(ctx context.Context) string {
	session := server.ClientSessionFromContext(ctx)
	if session != nil {
		return session.SessionID()
	}
	return "default"
}

func saveSnapshot(ctx context.Context, store *watch.Store, image []byte, toolName string) {
	if store == nil {
		return
	}
	pageCtx := getPageCtx(ctx)
	if pageCtx == nil {
		return
	}
	var title, url string
	_ = runWithTimeout(pageCtx, getBrowserTimeout(ctx),
		chromedp.Title(&title),
		chromedp.Evaluate(`window.location.href`, &url),
	)
	store.Save(&watch.Snapshot{
		SessionID: getSessionID(ctx),
		URL:       url,
		Title:     title,
		Image:     image,
		ToolName:  toolName,
		Timestamp: time.Now(),
	})
}
