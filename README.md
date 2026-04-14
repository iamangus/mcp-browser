# mcp-browser

A Go-based [Model Context Protocol](https://modelcontextprotocol.io) server that provides browser automation tools via [chromedp](https://github.com/chromedp/chromedp). Exposes 16 tools for navigation, interaction, screenshots, mouse control, and cookie management over Streamable HTTP.

## Quick Start

### Docker (recommended)

```bash
docker compose up -d
```

Requires no local Chromium install. The image bundles Alpine + Chromium.

### Go binary

Requires Chromium or Chrome installed locally.

```bash
go build -o mcp-browser ./cmd/server
API_KEY=your-key ./mcp-browser
```

## Configuration

All config is via environment variables. See [.env.example](.env.example) for defaults.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3000` | Listen port |
| `HOST` | `0.0.0.0` | Listen address |
| `API_KEY` | `test-api-key-12345` | Bearer token for authentication |
| `DISABLE_AUTH` | `false` | Disable auth entirely |
| `HEADLESS` | `true` | Run Chromium headless |
| `NO_SANDBOX` | `true` | Disable Chromium sandbox |
| `CHROMIUM_PATH` | *(auto)* | Path to Chromium binary |
| `SESSION_TIMEOUT` | `30m` | MCP session idle TTL |
| `MAX_CONCURRENT_PAGES` | `10` | Max browser tabs |
| `SCREENSHOT_QUALITY` | `80` | JPEG quality (1-100) |
| `SCREENSHOT_DEFAULT_WIDTH` | `1280` | Default viewport width |
| `SCREENSHOT_DEFAULT_HEIGHT` | `720` | Default viewport height |
| `RATE_LIMIT_MAX` | `100` | Max requests per window |
| `RATE_LIMIT_WINDOW` | `15m` | Rate limit window |
| `CORS_ORIGIN` | `*` | Allowed CORS origin |

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Server info (JSON) |
| `GET` | `/health` | Health check |
| `POST/GET/DELETE` | `/mcp` | MCP Streamable HTTP endpoint |

All `/mcp` requests require `Authorization: Bearer <API_KEY>` unless auth is disabled.

## Tools

16 MCP tools are registered. Each MCP session gets an isolated browser tab.

### Navigation

| Tool | Description |
|---|---|
| `browser_navigate` | Navigate to a URL. Returns page title and final URL. |
| `browser_screenshot` | Capture a screenshot (full page or element). Returns base64 PNG. |

### Interaction

| Tool | Description |
|---|---|
| `browser_click` | Click an element by CSS selector. |
| `browser_fill` | Fill an input field (clears first, works with React). |
| `browser_select` | Select a dropdown option by value or text. |
| `browser_hover` | Hover over an element (detects tooltips/popovers). |
| `browser_evaluate` | Execute JavaScript (dangerous patterns blocked). |

### Mouse Control

| Tool | Description |
|---|---|
| `browser_mouse_click` | Click at x,y coordinates with configurable button/count. |
| `browser_mouse_move` | Move mouse to x,y with optional smooth steps. |
| `browser_mouse_down` | Press and hold mouse button at coordinates. |
| `browser_mouse_up` | Release mouse button at coordinates. |
| `browser_mouse_drag` | Drag from start to end coordinates. |
| `browser_mouse_wheel` | Scroll at coordinates with configurable delta. |

### Cookies

| Tool | Description |
|---|---|
| `browser_get_cookies` | Get cookies, optionally filtered by name or domain. |
| `browser_set_cookies` | Set cookies (auth tokens, sessions, etc.). |
| `browser_delete_cookies` | Delete cookies by name (use `*` for all). |

## Security

- **Auth**: Bearer token required on all MCP requests (disable with `DISABLE_AUTH=true`)
- **SSRF protection**: Navigation blocks `localhost`, private IPs, and non-HTTP schemes
- **JS sandboxing**: `eval`, `fetch`, `require`, `process`, `import` are blocked in `browser_evaluate`
- **Rate limiting**: Sliding window per API key (falls back to IP)
- **Security headers**: `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, CSP, etc.

## Architecture

```
cmd/server/main.go          Entry point, wires everything together
internal/
  server/server.go          Chi router, middleware, route mounting
  browser/manager.go        Chromedp lifecycle, page pool per session
  tools/                    16 MCP tool handlers + registry
  config/config.go          Env-based configuration
  middleware/               Auth, rate limiting, security headers
  validation/               URL, JS, selector, coordinate validation
  logger/logger.go          Structured slog setup
```

Each MCP session maps to an isolated browser tab. Tabs are created on first tool call and cleaned up when the session expires.

## Development

```bash
go build ./...
go vet ./...
go test ./...
```
