#!/bin/sh
set -eu

if command -v deb-systemd-invoke >/dev/null 2>&1; then
    deb-systemd-invoke daemon-reload >/dev/null 2>&1 || systemctl daemon-reload >/dev/null 2>&1 || true
else
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
