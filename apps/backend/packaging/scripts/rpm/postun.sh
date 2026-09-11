#!/bin/sh
set -eu

systemd_runtime_dir="${TAKO_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"
if [ -d "$systemd_runtime_dir" ]; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
