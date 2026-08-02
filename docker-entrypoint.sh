#!/bin/sh
set -e

# Run the browser in headed mode under a virtual X display when HEADLESS=false.
# This makes the Chromium process far less distinguishable from a real browser,
# which materially improves reliability against Cloudflare-style challenges.
if [ "${HEADLESS}" = "false" ]; then
	DISPLAY_NUM=":99"
	SCREEN="${XVFB_SCREEN:-1280x720x24}"

	rm -f "/tmp/.X${DISPLAY_NUM#:}-lock" "/tmp/.X11-unix/X${DISPLAY_NUM#:}" 2>/dev/null || true

	Xvfb "${DISPLAY_NUM}" -screen 0 "${SCREEN}" -nolisten tcp -ac >/dev/null 2>&1 &
	XVFB_PID=$!

	i=0
	while [ ! -e "/tmp/.X11-unix/X${DISPLAY_NUM#:}" ]; do
		i=$((i + 1))
		if [ "$i" -gt 50 ]; then
			echo "Xvfb failed to start on ${DISPLAY_NUM}" >&2
			kill "${XVFB_PID}" 2>/dev/null || true
			exit 1
		fi
		sleep 0.1
	done

	export DISPLAY="${DISPLAY_NUM}"
	exec /usr/local/bin/mcp-browser "$@"
fi

exec /usr/local/bin/mcp-browser "$@"
