#!/bin/sh
set -eu

artifact_dir=${1:-}
if [ -z "$artifact_dir" ]; then
	echo "usage: scripts/run-with-faiss-artifacts.sh <faiss-artifact-dir> <command> [args...]" >&2
	exit 1
fi
shift
if [ "$#" -eq 0 ]; then
	echo "usage: scripts/run-with-faiss-artifacts.sh <faiss-artifact-dir> <command> [args...]" >&2
	exit 1
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "$artifact_dir" in
	/*)
		;;
	*)
		artifact_dir=$root/$artifact_dir
		;;
esac

goos=$(go env GOOS)
goarch=$(go env GOARCH)
case "$goos/$goarch" in
	linux/amd64|linux/arm64|darwin/arm64)
		;;
	*)
		echo "unsupported local FAISS dev target: $goos/$goarch" >&2
		exit 1
		;;
esac

target=${goos}_${goarch}
include_dir=$artifact_dir/include
lib_dir=$artifact_dir/lib/$target

if [ ! -f "$include_dir/faiss/c_api/Index_c.h" ]; then
	echo "missing FAISS C API header: $include_dir/faiss/c_api/Index_c.h" >&2
	exit 1
fi
if [ ! -f "$include_dir/faiss/c_api/IndexIVF_c_ex.h" ]; then
	echo "missing Bleve FAISS extension header: $include_dir/faiss/c_api/IndexIVF_c_ex.h" >&2
	exit 1
fi

case "$goos" in
	darwin)
		if [ ! -f "$lib_dir/libfaiss_c.dylib" ]; then
			echo "missing FAISS C API dynamic library: $lib_dir/libfaiss_c.dylib" >&2
			exit 1
		fi
		if [ ! -f "$lib_dir/libfaiss.dylib" ]; then
			echo "missing FAISS dynamic library: $lib_dir/libfaiss.dylib" >&2
			exit 1
		fi
		export DYLD_LIBRARY_PATH="$lib_dir${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"
		rpath_flag=
		;;
	*)
		if [ ! -f "$lib_dir/libfaiss_c.so" ]; then
			echo "missing FAISS C API shared library: $lib_dir/libfaiss_c.so" >&2
			exit 1
		fi
		if [ ! -f "$lib_dir/libfaiss.so" ]; then
			echo "missing FAISS shared library: $lib_dir/libfaiss.so" >&2
			exit 1
		fi
		export LD_LIBRARY_PATH="$lib_dir${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
		rpath_flag="-Wl,-rpath,$lib_dir -Wl,-rpath-link,$lib_dir"
		;;
esac

export CGO_ENABLED=1
export CGO_CFLAGS="-I$include_dir${CGO_CFLAGS:+ $CGO_CFLAGS}"
export CGO_LDFLAGS="-L$lib_dir${rpath_flag:+ $rpath_flag} -lfaiss_c -lfaiss${CGO_LDFLAGS:+ $CGO_LDFLAGS}"

exec "$@"
