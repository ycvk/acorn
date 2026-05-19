#!/bin/sh
set -eu

repo=${ACORN_REPO:-ycvk/acorn}
version=${ACORN_VERSION:-}
arch=${ACORN_ARCH:-}
service_user=acorn
service_home=/var/lib/acorn
workspace_dir=/srv/acorn/workspace
config_dir=$service_home/.acorn
config_path=$config_dir/acorn.yaml
env_path=$config_dir/acorn.env
unit_path=/etc/systemd/system/acorn.service
install_dir=/opt/acorn
bin_path=$install_dir/acorn
wrapper_path=/usr/local/bin/acorn
install_host_tools=${ACORN_INSTALL_HOST_TOOLS:-1}
start_service=${ACORN_START_SERVICE:-1}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

log() {
	printf '%s\n' "$*"
}

run_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
		return
	fi
	if ! command -v sudo >/dev/null 2>&1; then
		die "sudo is required when the installer is not run as root"
	fi
	sudo "$@"
}

need_command() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

select_openblas_runtime_package() {
	if ! command -v apt-cache >/dev/null 2>&1; then
		die "apt-cache is required to select the OpenBLAS runtime package"
	fi
	if apt-cache show libopenblas0-pthread >/dev/null 2>&1; then
		printf '%s\n' "libopenblas0-pthread"
		return
	fi
	if apt-cache show libopenblas0 >/dev/null 2>&1; then
		printf '%s\n' "libopenblas0"
		return
	fi
	die "could not find an OpenBLAS runtime package; expected libopenblas0-pthread or libopenblas0"
}

install_debian_host_tools() {
	if [ "$install_host_tools" != "1" ]; then
		return
	fi
	if ! command -v apt-get >/dev/null 2>&1; then
		die "automatic host package installation requires apt-get; set ACORN_INSTALL_HOST_TOOLS=0 after installing curl tar sha256sum systemctl git ripgrep python3 make bash and libopenblas.so.0"
	fi
	run_root apt-get update
	openblas_package=$(select_openblas_runtime_package)
	run_root apt-get install -y ca-certificates curl git ripgrep python3 make bash "$openblas_package"
}

verify_runtime_link() {
	target=$1
	link_dir=${2:-}
	need_command ldd
	if [ -n "$link_dir" ]; then
		output=$(LD_LIBRARY_PATH="$link_dir${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" ldd "$target" 2>&1) || {
			printf '%s\n' "$output" >&2
			die "could not inspect runtime shared libraries for $target"
		}
	else
		output=$(ldd "$target" 2>&1) || {
			printf '%s\n' "$output" >&2
			die "could not inspect runtime shared libraries for $target"
		}
	fi
	if printf '%s\n' "$output" | grep -q "not found"; then
		printf '%s\n' "$output" >&2
		die "missing runtime shared library for $target"
	fi
}

verify_package_runtime_links() {
	package_root=$1
	package_runtime_lib_dir=$package_root/lib/linux_${arch}
	verify_runtime_link "$package_root/acorn"
	verify_runtime_link "$package_runtime_lib_dir/libfaiss_c.so" "$package_runtime_lib_dir"
	verify_runtime_link "$package_runtime_lib_dir/libfaiss.so" "$package_runtime_lib_dir"
}

detect_arch() {
	if [ -n "$arch" ]; then
		case "$arch" in
			amd64|arm64)
				printf '%s\n' "$arch"
				return
				;;
			*)
				die "unsupported ACORN_ARCH: $arch"
				;;
		esac
	fi

	machine=$(uname -m)
	case "$machine" in
		x86_64|amd64)
			printf 'amd64\n'
			;;
		aarch64|arm64)
			printf 'arm64\n'
			;;
		*)
			die "unsupported machine architecture: $machine"
			;;
	esac
}

resolve_version() {
	if [ -n "$version" ]; then
		printf '%s\n' "$version"
		return
	fi

	if latest_url=$(curl -fsSLo /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest" 2>/dev/null); then
		resolved=${latest_url##*/}
		case "$resolved" in
			v*) printf '%s\n' "$resolved"; return ;;
		esac
	fi

	if command -v gh >/dev/null 2>&1; then
		log "Direct latest-release resolution failed; trying GitHub CLI" >&2
		if resolved=$(gh release view --repo "$repo" --json tagName --jq .tagName 2>/dev/null); then
			case "$resolved" in
				v*) printf '%s\n' "$resolved"; return ;;
			esac
		fi
	fi

	die "could not resolve latest release tag for $repo"
}

download_release_files() {
	log "Downloading release assets with curl"
	if curl -fL --retry 3 --retry-delay 2 -o "$work_dir/$package.tar.gz" "$base_url/$package.tar.gz" &&
		curl -fL --retry 3 --retry-delay 2 -o "$work_dir/$package.tar.gz.sha256" "$base_url/$package.tar.gz.sha256"; then
		return
	fi
	log "Direct release asset download failed; trying GitHub CLI"

	if command -v gh >/dev/null 2>&1; then
		if gh release download "$version" --repo "$repo" --pattern "$package.tar.gz" --pattern "$package.tar.gz.sha256" --dir "$work_dir" --clobber; then
			return
		fi
	fi

	die "could not download $package release assets from $repo@$version"
}

write_config_template() {
	target=$1
	cat > "$target" <<'EOF'
runtime:
  storage_dir: ~/.acorn
  run_timeout_seconds: 900

providers:
  - name: primary
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    timeout_seconds: 30
    temperature: 0.1
    max_completion_tokens: 2048
    enabled: true

context:
  window_tokens: 200000
  compact_margin_tokens: 13000
  preserve_recent_turns: 3
  summary_max_tokens: 2048

web:
  listen_addr: 127.0.0.1:8080

agent:
  name: coordinator
  description: Self-hosted operator agent for files, shell tasks, and external MCP tools.
  max_iterations: 90
  system_prompt: |
    You are Acorn's coordinator agent running in a self-hosted backend.

    Rules:
    - Treat /srv/acorn/workspace as the operator workspace.
    - Prefer read_file, list_files, search_text, inspect_git_status, and inspect_git_diff before mutation or shell execution.
    - Use run_command only when native tools cannot answer or execute the task directly.
    - Never pretend a tool, MCP provider, command, or push notification succeeded when it did not run.
    - Keep answers concrete and short.

tools:
  run_command:
    default_timeout: 30
    work_dir: /srv/acorn/workspace
    env_whitelist:
      - PATH
      - HOME
      - GIT_EDITOR
  workspace:
    root_dir: /srv/acorn/workspace
  mutation:
    root_dir: /srv/acorn/workspace
    denylist: []

mcp:
  providers: []

memory:
  search:
    token_budget: 2000
  semantic:
    bleve:
      path: ""
      index_name: memory_records
    embedding:
      provider: openai_compatible
      model: text-embedding-3-small
      base_url: https://api.openai.com/v1
      api_key: ${OPENAI_API_KEY}
      dimensions: 1536
      timeout_seconds: 30
      batch_size: 64
EOF
}

write_service_template() {
	target=$1
	cat > "$target" <<'EOF'
[Unit]
Description=Acorn self-hosted agent backend
Documentation=https://github.com/ycvk/acorn
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=acorn
Group=acorn
Environment=HOME=/var/lib/acorn
EnvironmentFile=-/var/lib/acorn/.acorn/acorn.env
WorkingDirectory=/srv/acorn/workspace
ExecStart=/opt/acorn/acorn serve -c /var/lib/acorn/.acorn/acorn.yaml
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/acorn /srv/acorn/workspace

[Install]
WantedBy=multi-user.target
EOF
}

write_env_template() {
	target=$1
	if [ -n "${OPENAI_API_KEY:-}" ]; then
		printf 'OPENAI_API_KEY=%s\n' "$OPENAI_API_KEY" > "$target"
	else
		printf 'OPENAI_API_KEY=replace-with-your-provider-key\n' > "$target"
	fi
}

case "$repo" in
	*/*) ;;
	*) die "ACORN_REPO must be owner/name, got: $repo" ;;
esac

install_debian_host_tools
need_command curl
need_command tar
need_command sha256sum
need_command systemctl

arch=$(detect_arch)
version=$(resolve_version)
case "$version" in
	v*) ;;
	*) die "release version must start with v, got: $version" ;;
esac
case "$version" in
	*[!A-Za-z0-9._-]*)
		die "release version contains unsupported characters: $version"
		;;
esac

package=acorn_${version}_linux_${arch}
base_url=https://github.com/$repo/releases/download/$version
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/acorn-install.XXXXXX")

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

log "Installing Acorn $version for linux/$arch"
download_release_files

(
	cd "$work_dir"
	sha256sum -c "$package.tar.gz.sha256"
	tar -xzf "$package.tar.gz"
	cd "$package"
	sha256sum -c CHECKSUMS
	test -x acorn
	test -f "lib/linux_${arch}/libfaiss_c.so"
	test -f "lib/linux_${arch}/libfaiss.so"
	verify_package_runtime_links "$PWD"
)

package_dir=$work_dir/$package
config_template=$work_dir/acorn.yaml
env_template=$work_dir/acorn.env
service_template=$work_dir/acorn.service
wrapper_template=$work_dir/acorn-wrapper
write_config_template "$config_template"
write_env_template "$env_template"
write_service_template "$service_template"
cat > "$wrapper_template" <<'EOF'
#!/bin/sh
exec /opt/acorn/acorn "$@"
EOF

if ! id -u "$service_user" >/dev/null 2>&1; then
	run_root useradd --system --home "$service_home" --create-home --shell /usr/sbin/nologin "$service_user"
fi
run_root install -d "$install_dir"
run_root install -d -o "$service_user" -g "$service_user" "$service_home" "$config_dir" "$workspace_dir"
run_root install -m 0755 "$package_dir/acorn" "$bin_path"
run_root rm -rf "$install_dir/lib"
run_root mkdir -p "$install_dir/lib"
run_root cp -R "$package_dir/lib/linux_${arch}" "$install_dir/lib/"
run_root chown -R root:root "$install_dir/lib"
run_root install -m 0755 "$wrapper_template" "$wrapper_path"

if [ ! -e "$config_path" ]; then
	run_root install -m 0644 -o "$service_user" -g "$service_user" "$config_template" "$config_path"
else
	log "Keeping existing config: $config_path"
fi

if [ ! -e "$env_path" ] || [ -n "${OPENAI_API_KEY:-}" ]; then
	run_root install -m 0600 -o "$service_user" -g "$service_user" "$env_template" "$env_path"
else
	log "Keeping existing environment file: $env_path"
fi

run_root install -m 0644 "$service_template" "$unit_path"
run_root systemctl daemon-reload

if [ "$start_service" != "1" ]; then
	log "Installed Acorn without starting service because ACORN_START_SERVICE=$start_service"
	log "Config: $config_path"
	log "Env:    $env_path"
	exit 0
fi

if [ -z "${OPENAI_API_KEY:-}" ]; then
	log "Installed Acorn but did not start the service because OPENAI_API_KEY was not provided."
	log "Edit $env_path, then run:"
	log "  sudo systemctl enable --now acorn"
	exit 0
fi

run_root systemctl enable --now acorn
sleep 2
if ! run_root systemctl is-active --quiet acorn; then
	run_root journalctl -u acorn --no-pager -n 80
	die "acorn service did not become active"
fi
curl -fsS http://127.0.0.1:8080/healthz >/dev/null

log "Acorn is installed and running."
log "Binary: $bin_path"
log "Command: $wrapper_path"
log "Config: $config_path"
log "Env:    $env_path"
log "Pair mobile with:"
log "  sudo -u acorn HOME=/var/lib/acorn acorn pair -c /var/lib/acorn/.acorn/acorn.yaml --server-url https://your-acorn.example.com --qr"
