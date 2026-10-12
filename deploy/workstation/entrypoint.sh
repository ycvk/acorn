#!/bin/bash
# Starts the workstation API and keeps its companions running: a virtual
# display, Chromium on it with the BrowserSkill extension, and the bsk daemon
# that drives it. Chromium runs with a display because sites challenge or
# block headless browsers. The browser's DevTools port stays inside the
# container; the API uses it for the owner's view of the browser.
set -euo pipefail

home=/home/agent
profile=$home/.config/acorn-browser
display=:99
socket=/tmp/.X11-unix/X99
# A rebuilt container has a new hostname, and Chromium refuses a profile whose
# singleton lock names another host. Only this browser uses the profile.
args=(
	workstation serve --home "$home" --chromium /usr/bin/chromium --devtools http://127.0.0.1:9222
	--keep "browser-daemon=exec bsk daemon start --foreground --daemon-idle 8760h"
	--keep "display=rm -f /tmp/.X99-lock $socket; exec Xvfb $display -screen 0 1280x900x24 -nolisten tcp"
	--keep "browser=until [ -S $socket ]; do sleep 0.2; done; rm -f $profile/SingletonLock $profile/SingletonSocket $profile/SingletonCookie; DISPLAY=$display exec chromium --no-sandbox --disable-dev-shm-usage --no-first-run --no-default-browser-check --hide-crash-restore-bubble --window-position=0,0 --window-size=1280,900 --remote-debugging-port=9222 --user-data-dir=$profile --load-extension=/opt/browserskill/extension about:blank"
)

exec /usr/local/bin/acorn "${args[@]}"
