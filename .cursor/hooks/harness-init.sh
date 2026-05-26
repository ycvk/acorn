#!/bin/bash
# harness-init.sh
# Session start: remind AI to load harness project state

# Consume stdin (sessionStart provides minimal JSON)
cat > /dev/null 2>/dev/null

STATE_FILE=".acorn/harness/state/current.md"

if [ ! -f "$STATE_FILE" ]; then
  echo '{"continue": true}'
  exit 0
fi

# Do NOT pass full content via JSON. Instead, give a short prompt so the AI
# reads the file itself via the Read tool. This avoids all JSON escaping bugs.
AGENT_MSG="[Acorn Harness] Session initialized. Please read .acorn/harness/state/current.md to load the current sprint status, active tasks, blockers, and risks before responding."

# Use printf to emit clean JSON
printf '{"continue": true, "agent_message": "%s"}\n' "$AGENT_MSG"
