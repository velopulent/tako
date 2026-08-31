#!/bin/sh
set -eu

systemd-sysusers /usr/lib/sysusers.d/tako.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/tako.conf
systemctl daemon-reload >/dev/null 2>&1 || true

if [ "${1:-0}" -eq 1 ]; then
    for unit in tako-sessiond.socket tako.socket; do
        if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
            systemctl enable "$unit" >/dev/null 2>&1 || true
            systemctl start "$unit" >/dev/null 2>&1 || true
        fi
    done
else
    for unit in tako-sessiond.service tako-sessiond.socket tako.socket tako.service; do
        if systemctl is-active --quiet "$unit"; then
            systemctl try-restart "$unit" >/dev/null 2>&1 || true
        fi
    done
fi

if [ "$(systemctl is-enabled tako.service 2>/dev/null || true)" != masked ]; then
    systemctl disable tako.service >/dev/null 2>&1 || true
fi

exit 0
