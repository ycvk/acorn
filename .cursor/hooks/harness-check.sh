#!/bin/bash
# harness-check.sh
# After agent response: detect PROJECT-ONLY file changes THIS TURN and prompt harness update
# Excludes harness metadata (.acorn/, .cursor/) from change detection

cat > /dev/null

CHECKPOINT_FILE=".acorn/harness/.last-check"
COOLDOWN_SECONDS=60

mkdir -p "$(dirname "$CHECKPOINT_FILE")"

NOW=$(date +%s)
if [ -f "$CHECKPOINT_FILE" ]; then
  LAST_TIME=$(head -n1 "$CHECKPOINT_FILE" 2>/dev/null || echo 0)
  ELAPSED=$((NOW - LAST_TIME))
  if [ "$ELAPSED" -lt "$COOLDOWN_SECONDS" ]; then
    echo '{"continue": true}'
    exit 0
  fi
fi

if command -v git >/dev/null 2>&1 && [ -d .git ]; then
  # Exclude harness metadata paths: .acorn/ and .cursor/
  CURRENT_FINGERPRINT=$(git status --porcelain 2>/dev/null | grep -v '^[ ?][?] \.acorn/' | grep -v '^[ ?][?] \.cursor/' | sha256sum | awk '{print $1}')
else
  echo '{"continue": true}'
  exit 0
fi

PREV_FINGERPRINT=""
if [ -f "$CHECKPOINT_FILE" ]; then
  PREV_FINGERPRINT=$(sed -n '2p' "$CHECKPOINT_FILE" 2>/dev/null)
fi

if [ "$CURRENT_FINGERPRINT" = "$PREV_FINGERPRINT" ]; then
  echo '{"continue": true}'
  exit 0
fi

printf '%s\n%s\n' "$NOW" "$CURRENT_FINGERPRINT" > "$CHECKPOINT_FILE"

# Count project-only changed files
CHANGED_COUNT=$(git status --short 2>/dev/null | grep -v '^[ ?][?] \.acorn/' | grep -v '^[ ?][?] \.cursor/' | wc -l | tr -d ' ')

AGENT_MSG="[Harness Update Check] $CHANGED_COUNT new project file change(s) detected this turn. Please update .acorn/harness/state/current.md if progress was made, blockers encountered, or new risks identified. Also run harness-update and generate a reflexion if appropriate."

printf '{"continue": true, "agent_message": "%s"}\n' "$AGENT_MSG"