#!/bin/sh
set -eu

repo=${ACORN_REPO:-ycvk/acorn}
version=${ACORN_VERSION:-}
arch=${ACORN_ARCH:-}
unit_path=/etc/systemd/system/acorn.service
install_dir=/opt/acorn
bin_path=$install_dir/acorn
wrapper_path=/usr/local/bin/acorn
install_host_tools=${ACORN_INSTALL_HOST_TOOLS:-1}
start_service=${ACORN_START_SERVICE:-1}

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

log() {
	printf '%s\n' "$*"
}

service_user=$(id -un)
service_group=$(id -gn)
service_home=
if command -v getent >/dev/null 2>&1; then
	service_home=$(getent passwd "$service_user" | cut -d: -f6 || true)
fi
if [ -z "$service_home" ]; then
	service_home=${HOME:-}
fi
if [ -z "$service_home" ]; then
	die "could not determine home directory for user $service_user"
fi

workspace_dir=/srv/acorn/workspace
config_dir=$service_home/.acorn
config_path=$config_dir/acorn.yaml
env_path=$config_dir/acorn.env
skills_dir=$config_dir/skills

run_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	else
		sudo "$@"
	fi
}

need_command() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

install_debian_host_tools() {
	if [ "$install_host_tools" != "1" ]; then
		return
	fi
	if ! command -v apt-get >/dev/null 2>&1; then
		die "automatic host package installation requires apt-get; set ACORN_INSTALL_HOST_TOOLS=0 after installing curl tar sha256sum systemctl git ripgrep python3 make bash"
	fi
	run_root apt-get update
	run_root apt-get install -y ca-certificates curl git ripgrep python3 make bash
}

install_packaged_skills() {
	source_dir=$1
	target_dir=$2
	if [ ! -d "$source_dir" ]; then
		die "packaged skills directory not found: $source_dir"
	fi
	run_root install -d "$target_dir"
	run_root cp -R "$source_dir/." "$target_dir/"
	run_root chown -R "$service_user:$service_group" "$target_dir"
}

detect_arch() {
	raw=${ACORN_ARCH:-}
	if [ -n "$raw" ]; then
		case "$raw" in
			amd64|arm64) printf '%s\n' "$raw" ;;
			x86_64) printf 'amd64\n' ;;
			aarch64) printf 'arm64\n' ;;
			*) die "unsupported ACORN_ARCH: $raw" ;;
		esac
		return
	fi
	case "$(uname -m)" in
		x86_64) printf 'amd64\n' ;;
		aarch64|arm64) printf 'arm64\n' ;;
		*) die "could not detect architecture from $(uname -m)" ;;
	esac
}

resolve_version() {
	if [ -n "$version" ]; then
		printf '%s\n' "$version"
		return
	fi
	latest=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
	if [ -z "$latest" ]; then
		die "could not determine latest release version; set ACORN_VERSION"
	fi
	printf '%s\n' "$latest"
}

download_release_files() {
	url_base="$base_url/$package"
	curl -fsSL -o "$work_dir/$package.tar.gz" "$url_base.tar.gz"
	curl -fsSL -o "$work_dir/$package.tar.gz.sha256" "$url_base.tar.gz.sha256"
}

write_config_template() {
	target=$1
	cat > "$target" <<'EOF'
providers:
  - name: default
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
runtime:
  storage_dir: /srv/acorn/workspace
web:
  listen_addr: 127.0.0.1:8080
memory:
  search:
    memory_context_token_budget: 2000
context:
  window_tokens: 200000
  compact_margin_tokens: 13000
  mask_after_turns: 2
  preserve_recent_turns: 3
agent:
  name: acorn
  description: Self-hosted AI agent
  max_iterations: 30
tools:
  workspace:
    root_dir: /srv/acorn/workspace
  mutation:
    disabled: false
  run_command:
    disabled: false
    default_timeout: 120
EOF
}

write_service_template() {
	target=$1
	cat > "$target" <<EOF
[Unit]
Description=Acorn self-hosted agent backend
Documentation=https://github.com/ycvk/acorn
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$service_user
Group=$service_group
Environment=HOME=$service_home
EnvironmentFile=-$env_path
WorkingDirectory=$workspace_dir
ExecStart=$bin_path serve
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=$service_home/.acorn $workspace_dir

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
	test -f "skills/skill_creator/SKILL.md"
)

package_dir=$work_dir/$package
config_template=$work_dir/acorn.yaml
env_template=$work_dir/acorn.env
service_template=$work_dir/acorn.service
wrapper_template=$work_dir/acorn-wrapper
write_config_template "$config_template"
write_env_template "$env_template"
write_service_template "$service_template"
cat > "$wrapper_template" <<EOF
#!/bin/sh
set -eu

bin='$bin_path'
service_user='$service_user'
service_home='$service_home'
env_path='$env_path'

run_with_service_env() {
	exec env HOME="\$service_home" sh -c 'set -eu
env_path=\$1
bin=\$2
shift 2
set -a
[ -f "$env_path" ] && . "$env_path"
set +a
exec "\$bin" "\$@"' sh "\$env_path" "\$bin" "\$@"
}

has_config_flag() {
	for arg in "\$@"; do
		case "\$arg" in
			-c|-c=*)
				return 0
				;;
		esac
	done
	return 1
}

run_service_command() {
	if [ "\$(id -un)" = "\$service_user" ]; then
		run_with_service_env "\$@"
	fi

	if [ "\$(id -u)" -eq 0 ]; then
		run_with_service_env "\$@"
	fi

	if command -v sudo >/dev/null 2>&1; then
		exec sudo -u "\$service_user" env HOME="\$service_home" sh -c 'set -eu
env_path=\$1
bin=\$2
shift 2
set -a
[ -f "$env_path" ] && . "$env_path"
set +a
exec "\$bin" "\$@"' sh "\$env_path" "\$bin" "\$@"
	fi

	printf 'error: %s uses installer-owned state; run with sudo or as user %s\n' "\$1" "\$service_user" >&2
	exit 1
}

if [ "\$#" -gt 0 ]; then
	case "\$1" in
		decision|doctor|memory|pair|token|devices|skills|smoke)
			command_name=\$1
			shift
			if ! has_config_flag "\$@"; then
				run_service_command "\$command_name" "\$@"
			fi
			set -- "\$command_name" "\$@"
			;;
	esac
fi

exec "\$bin" "\$@"
EOF
run_root install -d "$install_dir"
run_root install -d -o "$service_user" -g "$service_group" "$config_dir"
run_root chmod 0700 "$config_dir"
run_root install -d -m 0755 -o "$service_user" -g "$service_group" "$workspace_dir"
run_root install -m 0755 "$package_dir/acorn" "$bin_path"
run_root install -m 0755 "$wrapper_template" "$wrapper_path"
install_packaged_skills "$package_dir/skills" "$skills_dir"

if [ ! -e "$config_path" ]; then
	run_root install -m 0644 -o "$service_user" -g "$service_group" "$config_template" "$config_path"
else
	log "Keeping existing config: $config_path"
fi

if [ ! -e "$env_path" ] || [ -n "${OPENAI_API_KEY:-}" ]; then
	run_root install -m 0600 -o "$service_user" -g "$service_group" "$env_template" "$env_path"
else
	log "Keeping existing environment file: $env_path"
fi

run_root install -m 0644 "$service_template" "$unit_path"
run_root systemctl daemon-reload

if [ "$start_service" != "1" ]; then
	log "Installed Acorn without starting service because ACORN_START_SERVICE=$start_service"
	log "Config: $config_path"
	log "Env:    $env_path"
	log "Verify, then start the service:"
	log "  acorn doctor"
	log '  acorn smoke "hello"'
	log "  sudo systemctl enable --now acorn"
	exit 0
fi

if [ -z "${OPENAI_API_KEY:-}" ]; then
	log "Installed Acorn but did not start the service because OPENAI_API_KEY was not provided."
	log "Set the key, verify, then start the service:"
	log "  sudoedit $env_path"
	log "  acorn doctor"
	log '  acorn smoke "hello"'
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
log "  acorn pair --server-url https://your-acorn.example.com --qr"
