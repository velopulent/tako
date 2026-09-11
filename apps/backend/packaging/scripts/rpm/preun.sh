#!/bin/sh
set -eu

systemd_runtime_dir="${TAKO_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"
if [ "${1:-0}" -eq 0 ] && [ -d "$systemd_runtime_dir" ]; then
    systemctl stop tako.service tako-sessiond.service tako.socket tako-sessiond.socket >/dev/null 2>&1 || true
    systemctl disable tako-sessiond.socket tako.socket >/dev/null 2>&1 || true
fi

exit 0
