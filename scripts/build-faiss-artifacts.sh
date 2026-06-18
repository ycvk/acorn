#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir=${1:-}
target_goos=${2:-linux}
target_goarch=${3:-amd64}

# Pinned FAISS source lives in deploy/faiss.version (single source of truth,
# shared by release packaging and local dev). Env vars override the file.
faiss_version_file="$root/deploy/faiss.version"
if [ -z "${FAISS_REPO:-}" ] && [ -f "$faiss_version_file" ]; then
	FAISS_REPO=$(grep '^FAISS_REPO=' "$faiss_version_file" | cut -d= -f2-)
fi
if [ -z "${FAISS_COMMIT:-}" ] && [ -f "$faiss_version_file" ]; then
	FAISS_COMMIT=$(grep '^FAISS_COMMIT=' "$faiss_version_file" | cut -d= -f2-)
fi
faiss_repo=${FAISS_REPO:-https://github.com/blevesearch/faiss.git}
faiss_commit=${FAISS_COMMIT:-ffd910a91f1acf49b9898a7e514e462db89ee7b3}

if [ -z "$output_dir" ]; then
	echo "usage: scripts/build-faiss-artifacts.sh <empty-output-dir> [target-goos target-goarch]" >&2
	exit 1
fi

case "$target_goos/$target_goarch" in
	linux/amd64|linux/arm64|darwin/arm64)
		;;
	*)
		echo "unsupported FAISS target: $target_goos/$target_goarch" >&2
		exit 1
		;;
esac

host_os=$(uname -s)
host_machine=$(uname -m)
case "$host_machine" in
	x86_64|amd64)
		host_goarch=amd64
		;;
	aarch64|arm64)
		host_goarch=arm64
		;;
	*)
		host_goarch=$host_machine
		;;
esac

if [ "$target_goos" = "linux" ] && [ "$host_os" != "Linux" ]; then
	echo "building Linux FAISS artifacts requires a Linux host; got $host_os" >&2
	exit 1
fi
if [ "$target_goos" = "darwin" ] && [ "$host_os" != "Darwin" ]; then
	echo "building Darwin FAISS artifacts requires a Darwin host; got $host_os" >&2
	exit 1
fi

linux_cross_arm64=false
case "$target_goos/$target_goarch/$host_goarch" in
	linux/amd64/amd64|linux/arm64/arm64)
		;;
	linux/arm64/amd64)
		linux_cross_arm64=true
		;;
	linux/*)
		echo "building $target_goos/$target_goarch FAISS artifacts from $host_machine is not supported" >&2
		exit 1
		;;
esac

case "$output_dir" in
	/*)
		;;
	*)
		output_dir=$root/$output_dir
		;;
esac

# Idempotent cache: if the output already holds artifacts built from this exact
# FAISS commit, skip the slow rebuild. A stale/partial dir is removed and rebuilt.
faiss_stamp="$output_dir/.faiss-commit"
if [ -f "$faiss_stamp" ] && [ "$(cat "$faiss_stamp" 2>/dev/null)" = "$faiss_commit" ]; then
	printf "FAISS %s artifacts already built at %s (commit %s); skipping rebuild\n" "$target_goos/$target_goarch" "$output_dir" "$faiss_commit"
	exit 0
fi
if [ -d "$output_dir" ]; then
	rm -rf "$output_dir"
fi

if [ -e "$output_dir" ] && [ ! -d "$output_dir" ]; then
	echo "FAISS artifact output path is not a directory: $output_dir" >&2
	exit 1
fi
if [ -d "$output_dir" ] && [ -n "$(find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
	echo "FAISS artifact output directory must be empty: $output_dir" >&2
	exit 1
fi
mkdir -p "$output_dir"

for tool in git cmake; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "missing tool required to build FAISS artifacts: $tool" >&2
		exit 1
	fi
done

case "$target_goos/$target_goarch" in
	linux/arm64)
		if [ "$linux_cross_arm64" = true ]; then
			for tool in aarch64-linux-gnu-gcc aarch64-linux-gnu-g++; do
				if ! command -v "$tool" >/dev/null 2>&1; then
					echo "missing tool required to build FAISS $target_goos/$target_goarch artifact: $tool" >&2
					exit 1
				fi
			done
		elif ! pkg-config --exists openblas; then
			echo "missing OpenBLAS development package required to build FAISS $target_goos/$target_goarch artifact" >&2
			exit 1
		fi
		;;
	linux/amd64)
		if ! pkg-config --exists openblas; then
			echo "missing OpenBLAS development package required to build FAISS $target_goos/$target_goarch artifact" >&2
			exit 1
		fi
		;;
	darwin/arm64)
		if ! command -v brew >/dev/null 2>&1; then
			echo "building Darwin FAISS artifacts requires Homebrew for llvm/openblas" >&2
			exit 1
		fi
		for formula in llvm openblas; do
			if ! brew --prefix "$formula" >/dev/null 2>&1; then
				echo "missing Homebrew formula required to build FAISS $target_goos/$target_goarch artifact: $formula" >&2
				echo "install with: brew install $formula" >&2
				exit 1
			fi
		done
		;;
esac

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/acorn-faiss-artifacts.XXXXXX")
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

source_dir=$tmpdir/faiss
build_dir=$source_dir/build
install_dir=$tmpdir/install
target_dir=$output_dir/lib/${target_goos}_${target_goarch}
source_cxx_flags="-I$source_dir${CXXFLAGS:+ $CXXFLAGS}"

git clone --filter=blob:none "$faiss_repo" "$source_dir"
git -C "$source_dir" checkout "$faiss_commit"

case "$target_goos/$target_goarch" in
	linux/arm64)
		if [ "$linux_cross_arm64" = true ]; then
			openblas_lib=${OPENBLAS_LIBRARIES:-}
			if [ -z "$openblas_lib" ]; then
				for candidate in \
					/usr/lib/aarch64-linux-gnu/openblas-pthread/libopenblas.so \
					/usr/lib/aarch64-linux-gnu/libopenblas.so \
					/usr/aarch64-linux-gnu/lib/libopenblas.so; do
					if [ -f "$candidate" ]; then
						openblas_lib=$candidate
						break
					fi
				done
			fi
			if [ -z "$openblas_lib" ]; then
				echo "missing arm64 OpenBLAS target library; install libopenblas-dev:arm64 or set OPENBLAS_LIBRARIES" >&2
				exit 1
			fi
			cmake -S "$source_dir" -B "$build_dir" \
				-DFAISS_ENABLE_GPU=OFF \
				-DFAISS_ENABLE_C_API=ON \
				-DBUILD_SHARED_LIBS=ON \
				-DFAISS_ENABLE_PYTHON=OFF \
				-DFAISS_ENABLE_EXTRAS=OFF \
				-DBUILD_TESTING=OFF \
				-DBLA_VENDOR=OpenBLAS \
				-DBLAS_LIBRARIES="$openblas_lib" \
				-DCMAKE_CXX_FLAGS="$source_cxx_flags" \
				-DCMAKE_INSTALL_PREFIX="$install_dir" \
				-DCMAKE_INSTALL_RPATH="\$ORIGIN" \
				-DCMAKE_BUILD_WITH_INSTALL_RPATH=ON \
				-DCMAKE_SYSTEM_NAME=Linux \
				-DCMAKE_SYSTEM_PROCESSOR=aarch64 \
				-DCMAKE_C_COMPILER=aarch64-linux-gnu-gcc \
				-DCMAKE_CXX_COMPILER=aarch64-linux-gnu-g++
		else
			cmake -S "$source_dir" -B "$build_dir" \
				-DFAISS_ENABLE_GPU=OFF \
				-DFAISS_ENABLE_C_API=ON \
				-DBUILD_SHARED_LIBS=ON \
				-DFAISS_ENABLE_PYTHON=OFF \
				-DFAISS_ENABLE_EXTRAS=OFF \
				-DBUILD_TESTING=OFF \
				-DBLA_VENDOR=OpenBLAS \
				-DCMAKE_CXX_FLAGS="$source_cxx_flags" \
				-DCMAKE_INSTALL_PREFIX="$install_dir" \
				-DCMAKE_INSTALL_RPATH="\$ORIGIN" \
				-DCMAKE_BUILD_WITH_INSTALL_RPATH=ON
		fi
		;;
	darwin/arm64)
		llvm_prefix=$(brew --prefix llvm)
		openblas_prefix=$(brew --prefix openblas)
		LDFLAGS="-L$llvm_prefix/lib -L$openblas_prefix/lib${LDFLAGS:+ $LDFLAGS}" \
			CPPFLAGS="-I$llvm_prefix/include -I$openblas_prefix/include${CPPFLAGS:+ $CPPFLAGS}" \
			CC="${CC:-$llvm_prefix/bin/clang}" \
			CXX="${CXX:-$llvm_prefix/bin/clang++}" \
			cmake -S "$source_dir" -B "$build_dir" \
			-DFAISS_ENABLE_GPU=OFF \
			-DFAISS_ENABLE_C_API=ON \
			-DBUILD_SHARED_LIBS=ON \
			-DFAISS_ENABLE_PYTHON=OFF \
			-DFAISS_ENABLE_EXTRAS=OFF \
			-DBUILD_TESTING=OFF \
			-DBLA_VENDOR=OpenBLAS \
			-DOpenBLAS_ROOT="$openblas_prefix" \
			-DCMAKE_PREFIX_PATH="$llvm_prefix;$openblas_prefix" \
			-DCMAKE_INSTALL_PREFIX="$install_dir" \
			-DCMAKE_INSTALL_RPATH="@loader_path" \
			-DCMAKE_BUILD_WITH_INSTALL_RPATH=ON \
			-DCMAKE_OSX_ARCHITECTURES=arm64
		;;
	*)
		cmake -S "$source_dir" -B "$build_dir" \
			-DFAISS_ENABLE_GPU=OFF \
			-DFAISS_ENABLE_C_API=ON \
			-DBUILD_SHARED_LIBS=ON \
			-DFAISS_ENABLE_PYTHON=OFF \
			-DFAISS_ENABLE_EXTRAS=OFF \
			-DBUILD_TESTING=OFF \
			-DBLA_VENDOR=OpenBLAS \
			-DCMAKE_CXX_FLAGS="$source_cxx_flags" \
			-DCMAKE_INSTALL_PREFIX="$install_dir" \
			-DCMAKE_INSTALL_RPATH="\$ORIGIN" \
			-DCMAKE_BUILD_WITH_INSTALL_RPATH=ON
		;;
esac

parallel_jobs=2
if command -v nproc >/dev/null 2>&1; then
	parallel_jobs=$(nproc)
fi
cmake --build "$build_dir" --target faiss --parallel "$parallel_jobs"
cmake --build "$build_dir" --target faiss_c --parallel "$parallel_jobs"
cmake --install "$build_dir"

install_lib_dir=$install_dir/lib
if [ ! -d "$install_lib_dir" ] && [ -d "$install_dir/lib64" ]; then
	install_lib_dir=$install_dir/lib64
fi
if [ ! -d "$install_dir/include/faiss/c_api" ]; then
	echo "built FAISS install is missing C API headers" >&2
	exit 1
fi
if [ ! -d "$install_lib_dir" ]; then
	echo "built FAISS install is missing lib directory" >&2
	exit 1
fi

mkdir -p "$output_dir/include" "$target_dir"
cp -R "$install_dir/include/." "$output_dir/include/"
case "$target_goos" in
	darwin)
		cp -P "$install_lib_dir"/libfaiss*.dylib* "$target_dir/"
		;;
	*)
		cp -P "$install_lib_dir"/libfaiss*.so* "$target_dir/"
		;;
esac
case "$target_goos" in
	darwin)
		if [ ! -f "$target_dir/libfaiss.dylib" ]; then
			if [ ! -f "$build_dir/faiss/libfaiss.dylib" ]; then
				echo "built FAISS target is missing $build_dir/faiss/libfaiss.dylib" >&2
				exit 1
			fi
			cp -P "$build_dir/faiss/libfaiss.dylib" "$target_dir/"
		fi
		;;
	*)
		if [ ! -f "$target_dir/libfaiss.so" ]; then
			if [ ! -f "$build_dir/faiss/libfaiss.so" ]; then
				echo "built FAISS target is missing $build_dir/faiss/libfaiss.so" >&2
				exit 1
			fi
			cp -P "$build_dir/faiss/libfaiss.so" "$target_dir/"
		fi
		;;
esac

if [ ! -f "$output_dir/include/faiss/c_api/Index_c.h" ]; then
	echo "FAISS artifact is missing include/faiss/c_api/Index_c.h" >&2
	exit 1
fi
if [ ! -f "$output_dir/include/faiss/c_api/IndexIVF_c_ex.h" ]; then
	echo "FAISS artifact is missing include/faiss/c_api/IndexIVF_c_ex.h" >&2
	exit 1
fi
case "$target_goos" in
	darwin)
		if [ ! -f "$target_dir/libfaiss_c.dylib" ]; then
			echo "FAISS artifact is missing $target_dir/libfaiss_c.dylib" >&2
			exit 1
		fi
		if [ ! -f "$target_dir/libfaiss.dylib" ]; then
			echo "FAISS artifact is missing $target_dir/libfaiss.dylib" >&2
			exit 1
		fi
		;;
	*)
		if [ ! -f "$target_dir/libfaiss_c.so" ]; then
			echo "FAISS artifact is missing $target_dir/libfaiss_c.so" >&2
			exit 1
		fi
		if [ ! -f "$target_dir/libfaiss.so" ]; then
			echo "FAISS artifact is missing $target_dir/libfaiss.so" >&2
			exit 1
		fi
		;;
esac

printf "%s" "$faiss_commit" > "$output_dir/.faiss-commit"
printf "built FAISS %s artifacts from %s to %s\n" "$target_goos/$target_goarch" "$faiss_commit" "$output_dir"
find "$target_dir" -maxdepth 1 -type f \( -name "libfaiss*.so*" -o -name "libfaiss*.dylib*" \) -print | sort
