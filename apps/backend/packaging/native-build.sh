#!/usr/bin/env bash
set -Eeuo pipefail

# Build one target from targets.json inside its target distro image. The caller
# must provide a runner whose CPU matches the image; this script intentionally
# does not enable QEMU or cross-link against another distribution's libraries.
target="${TAKO_PACKAGE_TARGET:?TAKO_PACKAGE_TARGET is required}"
go_version="${TAKO_GO_VERSION:-1.26.7}"
goreleaser_version="${TAKO_GORELEASER_VERSION:-2.18.0}"
bun_version="${TAKO_BUN_VERSION:-1.4.0}"

machine_arch="$(uname -m)"
case "$machine_arch" in
  x86_64) go_arch=amd64; goreleaser_arch=x86_64 ;;
  aarch64) go_arch=arm64; goreleaser_arch=arm64 ;;
  *) echo "native-build: unsupported machine architecture $machine_arch" >&2; exit 2 ;;
esac

if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y --no-install-recommends ca-certificates curl gcc g++ git make pkg-config libpam0g-dev libsystemd-dev systemd systemd-sysv unzip tar gzip
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y ca-certificates curl gcc gcc-c++ git make pkgconf-pkg-config pam-devel systemd systemd-devel tar gzip unzip
elif command -v zypper >/dev/null 2>&1; then
  zypper --non-interactive refresh
  zypper --non-interactive install -y ca-certificates curl gcc gcc-c++ git make pkg-config pam-devel systemd systemd-devel tar gzip unzip
elif command -v pacman >/dev/null 2>&1; then
  pacman -Syu --noconfirm --needed ca-certificates curl gcc git make pkgconf pam systemd tar gzip unzip
else
  echo "native-build: no supported package manager in target image" >&2
  exit 2
fi

if [[ ! -x /opt/go/bin/go ]]; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  curl --fail --location --retry 3 --output "$tmp/go.tgz" "https://go.dev/dl/go${go_version}.linux-${go_arch}.tar.gz"
  rm -rf /opt/go
  mkdir -p /opt
  tar -C /opt -xzf "$tmp/go.tgz"
fi
export PATH="/opt/go/bin:/opt/bun/bin:/usr/local/bin:$PATH"
export GOCACHE="${GOCACHE:-/tmp/tako-go-cache}"
export GOTMPDIR="${GOTMPDIR:-/tmp/tako-go-tmp}"
mkdir -p "$GOCACHE" "$GOTMPDIR"

if ! command -v bun >/dev/null 2>&1; then
  mkdir -p /opt/bun
  curl --fail --location --retry 3 https://bun.sh/install | BUN_INSTALL=/opt/bun bash -s -- "bun-v${bun_version}"
fi

if ! command -v goreleaser >/dev/null 2>&1; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  curl --fail --location --retry 3 --output "$tmp/goreleaser.tgz" \
    "https://github.com/goreleaser/goreleaser/releases/download/v${goreleaser_version}/goreleaser_Linux_${goreleaser_arch}.tar.gz"
  tar -C /usr/local/bin -xzf "$tmp/goreleaser.tgz" goreleaser
  chmod 0755 /usr/local/bin/goreleaser
fi

# The mounted checkout belongs to the hosted runner, while this container runs as root.
git config --global --add safe.directory "$PWD"

go version
bun --version
goreleaser --version
bun install --frozen-lockfile
TAKO_PACKAGE_TARGET="$target" ./tools/package
