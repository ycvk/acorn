#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${VERSION:-}
goos=${RELEASE_GOOS:-linux}
goarch=${RELEASE_GOARCH:-amd64}
dist_dir=${DIST_DIR:-./dist}

case "$version" in
	"") version=$(cd "$root" && git describe --tags --dirty --always 2>/dev/null || echo dev) ;;
esac

case "$goos/$goarch" in
	linux/amd64|linux/arm64) ;;
	*) echo "unsupported release target: $goos/$goarch" >&2; exit 1 ;;
esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/acorn-release.XXXXXX")

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

case "$dist_dir" in
	/*) dist_path="$dist_dir" ;;
	*) dist_path="$root/$dist_dir" ;;
esac
package_name=acorn_${version}_${goos}_${goarch}

package_dir=$work_dir/$package_name
mkdir -p "$package_dir"

cd "$root"
GOOS=$goos GOARCH=$goarch CGO_ENABLED=0 \
	go build -trimpath -ldflags="-s -w" -o "$package_dir/acorn" ./cmd/acorn

if [ ! -d skills ]; then
	echo "skills seed pack not found" >&2
	exit 1
fi
skill_count=$(find skills -mindepth 2 -maxdepth 2 -name SKILL.md -type f | wc -l | tr -d ' ')
if [ "$skill_count" = "0" ]; then
	echo "skills seed pack is empty" >&2
	exit 1
fi
cp -R skills "$package_dir/skills"

install -m 0644 configs/acorn.selfhosted.example.yaml "$package_dir/acorn.yaml.example"
install -m 0644 deploy/systemd/acorn.service "$package_dir/acorn.service"
install -m 0600 deploy/systemd/acorn.env.example "$package_dir/acorn.env.example"
install -m 0644 docs/user/self-hosted-onboarding.md "$package_dir/self-hosted-onboarding.md"
install -m 0755 scripts/install-release.sh "$package_dir/install-release.sh"

commit="unknown"
if command -v git >/dev/null 2>&1; then
	commit=$(git rev-parse --verify HEAD 2>/dev/null || printf "unknown")
fi

cat > "$package_dir/RELEASE" <<EOF
name=acorn
version=$version
commit=$commit
goos=$goos
goarch=$goarch
cgo_enabled=0
EOF

hash_file() {
	( cd "$1" && sha256sum "$2" )
}

(
	cd "$package_dir"
	find . -type f | sort | while read -r f; do
		hash_file "$package_dir" "$f"
	done
) > "$package_dir/CHECKSUMS"

mkdir -p "$dist_path"
archive=$dist_path/$package_name.tar.gz
rm -f "$archive" "$archive.sha256"
tar -czf "$archive" -C "$work_dir" "$package_name"

(
	cd "$(dirname "$archive")"
	sha256sum "$(basename "$archive")"
) > "$archive.sha256"

printf "built %s\n" "$archive"
printf "wrote %s.sha256\n" "$archive"
