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

# Files that are hand-written, not generated. They live alongside generated code
# but must survive generation runs and be excluded from --check drift detection.
HANDWRIFT_FILES=(
    "models/MessagePartAdapter.kt"
)

# Serializer.kt needs a patch after generation to register the hand-written
# MessagePartAdapter (the generator output doesn't know about it).
patch_serializer() {
    local file="$1"
    # Add import for MessagePartAdapter after the KotlinJsonAdapterFactory import.
    sed -i '' 's|import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory|import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory\
import io.ycvk.acorn.api.models.MessagePartAdapter|' "$file"
    # Register the adapter factory as the first entry in moshiBuilder.
    sed -i '' 's|val moshiBuilder: Moshi.Builder = Moshi.Builder()|val moshiBuilder: Moshi.Builder = Moshi.Builder()\
        .add(MessagePartAdapter.FACTORY)|' "$file"
}

if [ "${1:-}" = "--check" ]; then
    # Compare each generated file against the checked-in copy.
    # Skip hand-written files (they don't exist in the generator output).
    # Skip Serializer.kt (it's patched after generation — compare a patched temp copy instead).
    stale=0
    while IFS= read -r -d '' genfile; do
        rel="${genfile#$GEN_SRC/}"
        destfile="$DEST/$rel"
        case "$rel" in
            infrastructure/Serializer.kt)
                # Patch the generated copy and compare against checked-in version.
                patch_serializer "$genfile"
                if ! diff -q "$genfile" "$destfile" >/dev/null 2>&1; then
                    echo "ERROR: $rel differs from generated (patched) version" >&2
                    diff -u "$genfile" "$destfile" || true
                    stale=1
                fi
                ;;
            *)
                if [ ! -f "$destfile" ]; then
                    echo "ERROR: $rel is missing from checked-in client" >&2
                    stale=1
                elif ! diff -q "$genfile" "$destfile" >/dev/null 2>&1; then
                    echo "ERROR: $rel differs from generated version" >&2
                    diff -u "$genfile" "$destfile" || true
                    stale=1
                fi
                ;;
        esac
    done < <(find "$GEN_SRC" -type f -name '*.kt' -print0)

    # Verify hand-written files exist in the checked-in tree.
    for hw in "${HANDWRIFT_FILES[@]}"; do
        if [ ! -f "$DEST/$hw" ]; then
            echo "ERROR: hand-written file $hw is missing" >&2
            stale=1
        fi
    done

    if [ "$stale" -eq 0 ]; then
        echo "Generated Kotlin client is up to date"
        rm -rf "$OUTPUT"
        exit 0
    else
        echo "ERROR: Generated Kotlin client is out of date. Run ./tool/generate_openapi_client.sh to update." >&2
        rm -rf "$OUTPUT"
        exit 1
    fi
fi

# Copy generated source into the app.
rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
cp -r "$GEN_SRC" "$DEST"

# Patch Serializer.kt to register the hand-written MessagePartAdapter.
patch_serializer "$DEST/infrastructure/Serializer.kt"

# Restore hand-written files from git (they were wiped by rm -rf above).
for hw in "${HANDWRIFT_FILES[@]}"; do
    git -C "$ROOT" checkout -- "mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/$hw"
done

rm -rf "$OUTPUT"

echo "Generated Kotlin client at $DEST"
echo "Patched Serializer.kt with MessagePartAdapter registration"
echo "Restored hand-written files: ${HANDWRIFT_FILES[*]}"
