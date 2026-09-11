#!/bin/sh
set -eu

distro=${TAKO_DISTRO:-}
if [ -z "$distro" ]; then
  distro=$(sed -n 's/^ID=//p' /etc/os-release | head -n 1 | tr -d '"')
fi

case "$distro" in
  debian|ubuntu|fedora|rhel|rocky|almalinux|archlinux|opensuse)
    tag=$distro
    ;;
  arch)
    tag=archlinux
    ;;
  opensuse-leap|opensuse-tumbleweed|sles)
    tag=opensuse
    ;;
  *)
    echo "unsupported TAKO_DISTRO or /etc/os-release ID: $distro" >&2
    exit 2
    ;;
esac

command=$1
shift
tags=$tag
if [ -n "${TAKO_EXTRA_GO_TAGS:-}" ]; then
  tags="$tags,$TAKO_EXTRA_GO_TAGS"
fi
exec go "$command" -tags "$tags" "$@"
