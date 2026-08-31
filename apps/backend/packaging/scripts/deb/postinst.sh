#!/bin/sh
set -eu

systemd-sysusers /usr/lib/sysusers.d/tako.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/tako.conf

if command -v deb-systemd-invoke >/dev/null 2>&1; then
    deb-systemd-invoke daemon-reload >/dev/null 2>&1 || systemctl daemon-reload >/dev/null 2>&1 || true
else
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

if [ "${1:-}" = configure ] && [ -z "${2:-}" ]; then
    for unit in tako-sessiond.socket tako.socket tako.service; do
        deb-systemd-helper enable "$unit" >/dev/null 2>&1 || true
        deb-systemd-invoke start "$unit" >/dev/null 2>&1 || true
    done
else
    for unit in tako-sessiond.service tako-sessiond.socket tako.socket tako.service; do
        deb-systemd-invoke try-restart "$unit" >/dev/null 2>&1 || true
    done
fi

exit 0
