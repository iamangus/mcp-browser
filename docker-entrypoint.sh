#!/bin/sh
set -e

# Run the browser in headed mode under a virtual X display when HEADLESS=false.
# This makes the Chromium process far less distinguishable from a real browser,
# which materially improves reliability against Cloudflare-style challenges.
if [ "${HEADLESS}" = "false" ]; then
	exec xvfb-run -a -s "${XVFB_SCREEN:-1280x720x24}" -e /dev/stderr /usr/local/bin/mcp-browser "$@"
fi

exec /usr/local/bin/mcp-browser "$@"
