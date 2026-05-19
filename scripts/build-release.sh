#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${VERSION:-}
goos=${RELEASE_GOOS:-linux}
goarch=${RELEASE_GOARCH:-amd64}
dist_dir=${DIST_DIR:-./dist}
faiss_artifact_dir=${FAISS_ARTIFACT_DIR:-}
host_goos=$(go env GOHOSTOS 2>/dev/null || go env GOOS)
host_goarch=$(go env GOHOSTARCH 2>/dev/null || go env GOARCH)

case "$version" in
	"")
		echo "VERSION is required" >&2
		exit 1
		;;
	*[!A-Za-z0-9._-]*)
		echo "VERSION contains unsupported characters: $version" >&2
		exit 1
		;;
esac

case "$goos/$goarch" in
	linux/amd64|linux/arm64)
		;;
	*)
		echo "unsupported release target: $goos/$goarch" >&2
		exit 1
		;;
esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/acorn-release.XXXXXX")

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

if [ -z "$faiss_artifact_dir" ]; then
	faiss_artifact_dir=$work_dir/faiss-native
	sh "$root/scripts/build-faiss-artifacts.sh" "$faiss_artifact_dir" "$goos" "$goarch"
fi

case "$faiss_artifact_dir" in
	/*)
		;;
	*)
		faiss_artifact_dir=$root/$faiss_artifact_dir
		;;
esac

faiss_target=${goos}_${goarch}
faiss_lib_dir=$faiss_artifact_dir/lib/$faiss_target
if [ ! -f "$faiss_artifact_dir/include/faiss/c_api/Index_c.h" ]; then
	echo "missing FAISS C API header: $faiss_artifact_dir/include/faiss/c_api/Index_c.h" >&2
	exit 1
fi
if [ ! -f "$faiss_lib_dir/libfaiss_c.so" ]; then
	echo "missing FAISS C API shared library: $faiss_lib_dir/libfaiss_c.so" >&2
	exit 1
fi
if [ ! -f "$faiss_lib_dir/libfaiss.so" ]; then
	echo "missing FAISS shared library: $faiss_lib_dir/libfaiss.so" >&2
	exit 1
fi

case "$dist_dir" in
	/*)
		dist_path=$dist_dir
		;;
	*)
		dist_path=$root/$dist_dir
		;;
esac
package_name=acorn_${version}_${goos}_${goarch}

package_dir=$work_dir/$package_name
mkdir -p "$package_dir/lib/$faiss_target"

cd "$root"
case "$goos/$goarch" in
	linux/arm64)
		if [ "$host_goos/$host_goarch" != "linux/arm64" ]; then
			export CC=${CC:-aarch64-linux-gnu-gcc}
			export CXX=${CXX:-aarch64-linux-gnu-g++}
		fi
		;;
esac

binary_rpath="\$ORIGIN/lib/$faiss_target"
LD_LIBRARY_PATH="$faiss_lib_dir${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
	CGO_CFLAGS="-I$faiss_artifact_dir/include" \
	CGO_LDFLAGS="-L$faiss_lib_dir -Wl,-rpath,$binary_rpath -Wl,-rpath-link,$faiss_lib_dir -lfaiss_c -lfaiss" \
	GOOS=$goos GOARCH=$goarch CGO_ENABLED=1 \
	go build -tags "bleve_faiss vectors" -trimpath -ldflags="-s -w" -o "$package_dir/acorn" ./cmd/acorn

cp -P "$faiss_lib_dir"/libfaiss*.so* "$package_dir/lib/$faiss_target/"

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
target=$goos/$goarch
commit=$commit
binary=acorn
config_example=acorn.yaml.example
env_example=acorn.env.example
systemd_unit=acorn.service
installer=install-release.sh
semantic_index_backend=bleve_faiss
native_lib_dir=lib/$faiss_target
EOF

hash_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi
}

(
	cd "$package_dir"
	find . -type f ! -name CHECKSUMS -print | sed "s#^\./##" | sort | while IFS= read -r file; do
		hash_file "$file"
	done
) > "$package_dir/CHECKSUMS"

mkdir -p "$dist_path"
archive=$dist_path/$package_name.tar.gz
rm -f "$archive" "$archive.sha256"
tar -czf "$archive" -C "$work_dir" "$package_name"

(
	cd "$dist_path"
	hash_file "$package_name.tar.gz" > "$package_name.tar.gz.sha256"
)

printf "built %s\n" "$archive"
printf "wrote %s.sha256\n" "$archive"
