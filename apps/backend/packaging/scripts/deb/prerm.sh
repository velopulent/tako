#!/bin/sh
set -eu

systemd_runtime_dir="${TAKO_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"
if [ "${1:-}" = remove ] && [ -z "${DPKG_ROOT:-}" ] && [ -d "$systemd_runtime_dir" ]; then
    deb-systemd-invoke stop tako.service tako-sessiond.service tako.socket tako-sessiond.socket >/dev/null || true
fi

if [ "${1:-}" = remove ]; then
    default_package_state_dir="${DPKG_ROOT:-}/var/lib/tako"
    package_state_dir="${TAKO_PACKAGE_STATE_DIR:-$default_package_state_dir}"
    mkdir -p "$package_state_dir"
    : >"$package_state_dir/package-removed"
fi

# Never mask or disable units here. Removal keeps deb-systemd-helper state so
# reinstall can restore package-managed enablement; purge clears that state.
exit 0
