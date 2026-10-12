#!/bin/bash
# Starts the workstation API and keeps its companions running: a headless
# Chromium with the BrowserSkill extension, the local bsk daemon that drives
# it, and, when ACORN_BROWSER_PUBLIC_URL is set, a bsk server daemon that the
# owner's own browser connects to through the reverse proxy.
set -euo pipefail

home=/home/agent
profile=$home/.config/acorn-browser
# A rebuilt container has a new hostname, and Chromium refuses a profile whose
# singleton lock names another host. Only this browser uses the profile.
args=(
	workstation serve --home "$home" --chromium /usr/bin/chromium
	--keep "browser-daemon=exec bsk daemon start --foreground --daemon-idle 8760h"
	--keep "browser=rm -f $profile/SingletonLock $profile/SingletonSocket $profile/SingletonCookie; exec chromium --headless=new --no-sandbox --disable-gpu --disable-dev-shm-usage --no-first-run --no-default-browser-check --window-size=1280,900 --user-data-dir=$profile --load-extension=/opt/browserskill/extension about:blank"
)

public_url=${ACORN_BROWSER_PUBLIC_URL:-}
if [ -n "$public_url" ]; then
	if ! [[ $public_url =~ ^wss://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._~/-]*)?$ ]]; then
		echo "ACORN_BROWSER_PUBLIC_URL must be a wss:// URL, got: $public_url" >&2
		exit 1
	fi
	owner_home=$home/.bsk-owner
	mkdir -p "$owner_home"
	# The reverse proxy talks TLS to this listener; the owner's browser only
	# sees the proxy's certificate.
	if [ ! -f "$owner_home/tls.crt" ]; then
		openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 \
			-subj /CN=acorn-workstation -keyout "$owner_home/tls.key" -out "$owner_home/tls.crt" 2>/dev/null
		chmod 600 "$owner_home/tls.key"
	fi
	args+=(--keep "owner-browser-daemon=BSK_HOME=$owner_home exec bsk daemon start --mode server --listen 0.0.0.0 --port 52801 --tls-cert $owner_home/tls.crt --tls-key $owner_home/tls.key --public-url $public_url")
fi

exec /usr/local/bin/acorn "${args[@]}"
