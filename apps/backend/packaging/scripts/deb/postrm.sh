#!/bin/sh
set -eu

if [ "${1:-}" = purge ]; then
    deb-systemd-helper purge tako-sessiond.socket tako.socket tako-sessiond.service tako.service >/dev/null || true
    default_package_state_dir="${DPKG_ROOT:-}/var/lib/tako"
    package_state_dir="${TAKO_PACKAGE_STATE_DIR:-$default_package_state_dir}"
    rm -f "$package_state_dir/package-removed"
fi

systemd_runtime_dir="${TAKO_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"
if [ -z "${DPKG_ROOT:-}" ] && [ -d "$systemd_runtime_dir" ]; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
