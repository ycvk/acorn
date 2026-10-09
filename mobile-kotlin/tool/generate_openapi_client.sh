#!/bin/bash
set -eu

# Generate Kotlin client from OpenAPI spec.
# Usage: ./tool/generate_openapi_client.sh [--check]
#   --check : verify the checked-in client is up to date (CI gate); exits non-zero if stale.

# This script lives at mobile-kotlin/tool/, so the repo root is two levels up.
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OPENAPI="$ROOT/docs/openapi.yaml"
OUTPUT=$(mktemp -d "${TMPDIR:-/tmp}/acorn-openapi.XXXXXX")
trap 'rm -rf "$OUTPUT"' EXIT
DEST="$ROOT/mobile-kotlin/app/src/main/java/io/ycvk/acorn/api"

export JAVA_HOME="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home}"
export PATH="$JAVA_HOME/bin:$PATH"

# The checked-in CLI configuration pins one generator version on every host.
if ! command -v openapi-generator-cli >/dev/null 2>&1; then
    echo "ERROR: openapi-generator-cli not found. Install with npm install -g @openapitools/openapi-generator-cli." >&2
    exit 1
fi

if ! openapi-generator-cli --openapitools "$ROOT/mobile-kotlin/openapitools.json" generate \
  -g kotlin \
  -i "$OPENAPI" \
  -o "$OUTPUT" \
  --additional-properties=packageName=io.ycvk.acorn.api,library=jvm-okhttp4,serializationLibraryName=moshi,omitGradleWrapper=true,omitBuildFiles=true \
  > "$OUTPUT/generator.log" 2>&1; then
    cat "$OUTPUT/generator.log" >&2
    exit 1
fi

# The generator emits build.gradle/settings.gradle/docs/tests even with omit* flags; trim them.
rm -f "$OUTPUT/build.gradle" "$OUTPUT/settings.gradle" "$OUTPUT/README.md"
rm -rf "$OUTPUT/src/test" "$OUTPUT/docs" "$OUTPUT/.openapi-generator" "$OUTPUT/.openapi-generator-ignore"

GEN_SRC="$OUTPUT/src/main/kotlin/io/ycvk/acorn/api"

if [ "${1:-}" = "--check" ]; then
    # Compare each generated file against the checked-in copy.
    stale=0
    while IFS= read -r -d '' genfile; do
        rel="${genfile#$GEN_SRC/}"
        destfile="$DEST/$rel"
        if [ ! -f "$destfile" ]; then
            echo "ERROR: $rel is missing from checked-in client" >&2
            stale=1
        elif ! diff -q "$genfile" "$destfile" >/dev/null 2>&1; then
            echo "ERROR: $rel differs from generated version" >&2
            diff -u "$genfile" "$destfile" || true
            stale=1
        fi
    done < <(find "$GEN_SRC" -type f -name '*.kt' -print0)

    if [ "$stale" -eq 0 ]; then
        echo "Generated Kotlin client is up to date"
        exit 0
    else
        echo "ERROR: Generated Kotlin client is out of date. Run ./tool/generate_openapi_client.sh to update." >&2
        exit 1
    fi
fi

# Copy generated source into the app.
rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
cp -r "$GEN_SRC" "$DEST"


echo "Generated Kotlin client at $DEST"
