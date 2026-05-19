#!/bin/sh
set -eu

repo_root="${1:-.}"

printf 'cmd entrypoints\n'
find "$repo_root/cmd" -maxdepth 2 -type f 2>/dev/null | sort

printf '\ninternal hotspots\n'
find "$repo_root/internal" -maxdepth 2 -type f \( \
  -path '*/cli/*' -o \
  -path '*/runtime/*' -o \
  -path '*/store/*' \
\) 2>/dev/null | sort
