#!/bin/bash
set -eu

# Generate Kotlin client from OpenAPI spec.
# Usage: ./tool/generate_openapi_client.sh [--check]
#   --check : verify the checked-in client is up to date (CI gate); exits non-zero if stale.

# This script lives at mobile-kotlin/tool/, so the repo root is two levels up.
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OPENAPI="$ROOT/docs/openapi.yaml"
OUTPUT="/tmp/openapi-kotlin-gen"
DEST="$ROOT/mobile-kotlin/app/src/main/java/io/ycvk/acorn/api"

export JAVA_HOME="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home}"
export PATH="$JAVA_HOME/bin:$PATH"

# openapi-generator 7.23's --silent flag aborts the run, so we discard logs via redirection instead.
openapi-generator generate \
  -g kotlin \
  -i "$OPENAPI" \
  -o "$OUTPUT" \
  --additional-properties=packageName=io.ycvk.acorn.api,library=jvm-okhttp4,serializationLibraryName=moshi,omitGradleWrapper=true,omitBuildFiles=true \
  > /dev/null 2>&1

# The generator emits build.gradle/settings.gradle/docs/tests even with omit* flags; trim them.
rm -f "$OUTPUT/build.gradle" "$OUTPUT/settings.gradle" "$OUTPUT/README.md"
rm -rf "$OUTPUT/src/test" "$OUTPUT/docs" "$OUTPUT/.openapi-generator" "$OUTPUT/.openapi-generator-ignore"

GEN_SRC="$OUTPUT/src/main/kotlin/io/ycvk/acorn/api"

if [ "${1:-}" = "--check" ]; then
    if diff -rq "$GEN_SRC" "$DEST" >/dev/null 2>&1; then
        echo "Generated Kotlin client is up to date"
        rm -rf "$OUTPUT"
        exit 0
    else
        echo "ERROR: Generated Kotlin client is out of date. Run ./tool/generate_openapi_client.sh to update." >&2
        diff -rq "$GEN_SRC" "$DEST" || true
        rm -rf "$OUTPUT"
        exit 1
    fi
fi

# Copy generated source into the app.
rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
cp -r "$GEN_SRC" "$DEST"
rm -rf "$OUTPUT"

echo "Generated Kotlin client at $DEST"
