#!/bin/sh
set -e

# Start the system D-Bus. Headed Chromium retries the connection in a tight
# loop when the bus is absent, which stalls chromedp's startup handshake and
# the HTTP server never comes up. The daemon needs root, hence this entrypoint
# runs as root and drops privileges to appuser below.
mkdir -p /run/dbus
dbus-daemon --system --fork

# Per-user session bus for Chromium. Run it as appuser so its socket stays
# accessible after we drop privileges.
su-exec appuser sh -c 'rm -f /home/appuser/.dbus-session; dbus-daemon --session --address=unix:path=/home/appuser/.dbus-session --fork' || true
export DBUS_SESSION_BUS_ADDRESS="unix:path=/home/appuser/.dbus-session"

# Make the GPU render node (exposed by the Kubernetes device plugin when the
# pod requests e.g. gpu.intel.com/i915) readable by the unprivileged appuser.
# Running as root, we chmod defensively since device plugin defaults vary.
if [ -e /dev/dri/renderD128 ]; then
	chmod 0666 /dev/dri/renderD128 2>/dev/null || true
fi
if [ -e /dev/dri/card0 ]; then
	chmod 0666 /dev/dri/card0 2>/dev/null || true
fi

run_app() {
	exec su-exec appuser env HOME=/home/appuser /usr/local/bin/mcp-browser "$@"
}

# Run the browser in headed mode under a virtual X display when HEADLESS=false.
# This makes the Chromium process far less distinguishable from a real browser,
# which materially improves reliability against Cloudflare-style challenges.
if [ "${HEADLESS}" = "false" ]; then
	DISPLAY_NUM=":99"
	SCREEN="${XVFB_SCREEN:-1280x720x24}"

	mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix
	rm -f "/tmp/.X${DISPLAY_NUM#:}-lock" "/tmp/.X11-unix/X${DISPLAY_NUM#:}" 2>/dev/null || true

	su-exec appuser sh -c 'exec Xvfb "$1" -screen 0 "$2" -nolisten tcp -ac' sh "${DISPLAY_NUM}" "${SCREEN}" &
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
	run_app "$@"
fi

run_app "$@"
