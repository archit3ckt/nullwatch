#!/bin/sh
# nullwatch bootstrap installer.
#
# Review this script before piping it into a shell — it installs system
# packages (Docker, via the official get.docker.com script) and may write to
# your home directory (a local Go toolchain) and /usr/local/bin.
#
# Usage:
#   ./install.sh                  (run from inside a cloned nullwatch repo)
#   curl -fsSL <raw-url> | sh     (clones the repo into a temp dir first)
set -eu

GO_VERSION="1.23.4"
REPO_URL="https://github.com/archit3ckt/nullwatch.git"
GO_SDK_DIR="$HOME/go-sdk"
INSTALL_DIR="/usr/local/bin"

log() { printf '==> %s\n' "$1"; }

need_cmd() { command -v "$1" >/dev/null 2>&1; }

# --- Docker -------------------------------------------------------------

ensure_docker() {
	if need_cmd docker; then
		if docker compose version >/dev/null 2>&1; then
			log "docker + compose plugin already installed"
			return
		fi
		# Docker itself is already here — just fill the actual gap instead of
		# running the full convenience script, which touches package repo
		# config and doesn't know it's redundant.
		log "docker is installed but the compose plugin isn't — installing just the plugin"
		install_compose_plugin
		return
	fi

	log "docker not found — installing via the official convenience script"
	curl -fsSL https://get.docker.com | sh

	if ! id -nG "$USER" 2>/dev/null | grep -qw docker; then
		log "adding $USER to the docker group (log out/in for it to take effect)"
		sudo usermod -aG docker "$USER" || true
	fi
}

install_compose_plugin() {
	plugin_dir="$HOME/.docker/cli-plugins"
	mkdir -p "$plugin_dir"
	compose_url="https://github.com/docker/compose/releases/latest/download/docker-compose-linux-$(uname -m)"
	curl -fsSL "$compose_url" -o "$plugin_dir/docker-compose"
	chmod +x "$plugin_dir/docker-compose"

	if docker compose version >/dev/null 2>&1; then
		log "compose plugin installed to $plugin_dir"
	else
		echo "warning: installed the compose plugin to $plugin_dir but \`docker compose\` still doesn't work — see https://docs.docker.com/compose/install/linux/" >&2
	fi
}

# --- Go -------------------------------------------------------------------

detect_arch() {
	case "$(uname -m)" in
		x86_64) echo "amd64" ;;
		aarch64|arm64) echo "arm64" ;;
		*) echo "unsupported" ;;
	esac
}

ensure_go() {
	if need_cmd go; then
		log "go already installed ($(go version))"
		return
	fi

	arch="$(detect_arch)"
	if [ "$arch" = "unsupported" ]; then
		echo "error: unsupported architecture $(uname -m) — install Go manually from https://go.dev/dl/" >&2
		exit 1
	fi

	log "go not found — downloading Go ${GO_VERSION} into ${GO_SDK_DIR} (no sudo required)"
	tarball="go${GO_VERSION}.linux-${arch}.tar.gz"
	tmp="$(mktemp -d)"
	curl -fsSL "https://go.dev/dl/${tarball}" -o "${tmp}/${tarball}"
	mkdir -p "$GO_SDK_DIR"
	tar -C "$GO_SDK_DIR" --strip-components=1 -xzf "${tmp}/${tarball}"
	rm -rf "$tmp"

	export PATH="$GO_SDK_DIR/bin:$PATH"
	log "go installed. Add this to your shell rc to persist it:"
	echo "    export PATH=\"$GO_SDK_DIR/bin:\$PATH\""
}

# --- nullwatch itself -------------------------------------------------------

ensure_repo() {
	if [ -f "go.mod" ] && grep -q "module github.com/archit3ckt/nullwatch" go.mod 2>/dev/null; then
		echo "."
		return
	fi

	log "not inside the nullwatch repo — cloning it"
	tmp="$(mktemp -d)"
	git clone --depth 1 "$REPO_URL" "$tmp/nullwatch" >&2
	echo "$tmp/nullwatch"
}

main() {
	ensure_docker
	ensure_go

	repo_dir="$(ensure_repo)"
	log "building nullwatch"
	( cd "$repo_dir" && go build -o nullwatch ./cmd/nullwatch )

	log "installing to ${INSTALL_DIR}/nullwatch (requires sudo)"
	sudo install -m 0755 "$repo_dir/nullwatch" "${INSTALL_DIR}/nullwatch"

	log "launching nullwatch"
	exec "${INSTALL_DIR}/nullwatch"
}

main "$@"
