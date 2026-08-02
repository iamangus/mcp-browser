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
| `STEALTH` | `true` | Hide automation fingerprints (see [Cloudflare-protected sites](#cloudflare-protected-sites)) |
| `STEALTH_USER_AGENT` | *(auto)* | Optional user-agent override used in stealth mode |
| `RATE_LIMIT_MAX` | `100` | Max requests per window |
| `RATE_LIMIT_WINDOW` | `15m` | Rate limit window |
| `CORS_ORIGIN` | `*` | Allowed CORS origin |
| `LIVE_INTERVAL` | `400ms` | Live-view screenshot cadence |
| `LIVE_QUALITY` | `60` | Live-view JPEG quality (1-100) |
| `MAX_SNAPSHOTS_PER_SESSION` | `50` | Screenshot history kept per session |
| `DEBUG_PPROF` | `false` | Expose Go pprof profiles at `/debug/pprof` (diagnostics only) |

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Server info (JSON) |
| `GET` | `/health` | Health check |
| `POST/GET/DELETE` | `/mcp` | MCP Streamable HTTP endpoint |

All `/mcp` requests require `Authorization: Bearer <API_KEY>` unless auth is disabled.

### Watch UI

`GET /watch` is a browser UI showing all agent sessions. Clicking a session opens a detail page with two tabs:

- **Live** — a live rendering of the session's browser tab. Frames are captured while
  at least one viewer is connected (polling at `LIVE_INTERVAL` plus immediate captures on
  page load/navigation events) and streamed over SSE; nothing is persisted.
- **Screenshots** — the history of explicit `browser_screenshot` calls for that session.

Supporting endpoints:

| Method | Path | Description |
|---|---|---|
| `GET` | `/watch` | Watch UI (HTML) |
| `GET` | `/watch/snapshots` | Session summaries (latest frame per session) |
| `GET` | `/watch/snapshots/{sessionId}` | Screenshot history for a session |
| `GET` | `/watch/sessions` | Active browser sessions |
| `GET` | `/watch/events` | SSE of new screenshots |
| `GET` | `/watch/live/{sessionId}` | SSE live frame stream |

The watch routes require auth by default. Because `EventSource` cannot set headers, the
watch UI also accepts the API key as `?api_key=<key>` (the `/mcp` endpoint does **not**
accept query-param keys). If auth is disabled, the key is not needed.

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

## Cloudflare-protected sites

By default (`STEALTH=true`) the browser hides the automation fingerprints that
bot detectors flag: the launch flags that expose `navigator.webdriver` are
countered, a pre-document script restores the usual `window.chrome`, `plugins`,
`languages`, `hardwareConcurrency` and permissions surfaces, and all mouse tools
dispatch **real CDP input events** (`isTrusted=true`) instead of synthetic JS
`MouseEvent`s — so `browser_click` moves the pointer like a human before
pressing/releasing.

Turnstile-style "click the checkbox" challenges still run a JS challenge around
the checkbox, so work the page like this:

1. `browser_screenshot` — find the checkbox.
2. `browser_click` on the checkbox selector.
3. **Wait 3-5 seconds** — the challenge resolves asynchronously; a screenshot
   taken immediately will still show the spinner. Do not click again.
4. `browser_screenshot` again to confirm it passed.

Notes:

- **Headed mode helps a lot.** Headless Chromium (even with the new `--headless`
  mode) is far easier to fingerprint. Run with `HEADLESS=false` on a machine
  with a display, or on a server use `xvfb-run -a ./mcp-browser`. In headed mode
  stealth launches a clean near-default flag set (the heavy suppression flags are
  dropped). In Docker, `HEADLESS=false` is handled automatically: the entrypoint
  starts a virtual display (`Xvfb`) plus a system/session D-Bus (headed Chromium
  stalls without one) and runs the browser headed at
  `XVFB_SCREEN=1280x720x24`.
- **Hardware acceleration is automatic when a GPU is exposed.** Software
  rendering (SwiftShader) is fragile and memory-hungry for heavy sites and can
  crash renderers mid-challenge. In Kubernetes, if the pod requests a GPU
  device (e.g. `gpu.intel.com/i915` from the Intel device plugin), the container
  receives `/dev/dri/renderD128` and the server detects it and launches Chromium
  with hardware ANGLE/Vulkan + VA-API flags instead of SwiftShader, falling back
  automatically if Vulkan initialization fails. The image ships the Intel
  userspace drivers (`mesa-vulkan-intel`, `intel-media-driver`).
- **IP reputation matters.** The browser may be clean but Cloudflare also scores
  the TLS/JA3 and egress IP. On a residential IP these changes usually resolve
  interactive challenges; on a flagged datacenter IP you will keep getting them
  no matter how clean the browser is.
- Stealth is best-effort against a moving target — it is not a guarantee, and
  Cloudflare updates its detectors over time. Set `STEALTH=false` to restore the
  original launch flags.
- Only automate sites you are authorized to use. These settings make *our own
  browser* look ordinary; they do not defeat or solve the challenge itself.

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
  watch/                   /watch UI, snapshot store, live frame hub
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
