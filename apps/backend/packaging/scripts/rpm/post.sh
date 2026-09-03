#!/bin/sh
set -eu

systemd-sysusers /usr/lib/sysusers.d/tako.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/tako.conf
systemctl daemon-reload >/dev/null 2>&1 || true

# Always ensure the sockets are enabled (unless masked). A reinstall must
# re-enable and an upgrade must not leave a stopped-but-enabled socket dead.
for unit in tako.socket tako-sessiond.socket; do
    if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
        systemctl enable "$unit" >/dev/null 2>&1 || true
    fi
done

if [ "${1:-0}" -eq 1 ]; then
    for unit in tako.socket tako-sessiond.socket; do
        if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
            systemctl start "$unit" >/dev/null 2>&1 || true
        fi
    done
else
    # Upgrade: restart sockets so they pick up the new binary and start
    # listening again even if stopped; only try-restart services so the
    # socket-activated gateway is never started directly.
    for unit in tako.socket tako-sessiond.socket; do
        if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
            systemctl restart "$unit" >/dev/null 2>&1 || systemctl start "$unit" >/dev/null 2>&1 || true
        fi
    done
    for unit in tako-sessiond.service tako.service; do
        if systemctl is-active --quiet "$unit"; then
            systemctl try-restart "$unit" >/dev/null 2>&1 || true
        fi
    done
fi

# The gateway and sessiond are socket-activated and must never be
# boot-enabled; only the sockets carry an [Install] section.
for unit in tako.service tako-sessiond.service; do
    if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
        systemctl disable "$unit" >/dev/null 2>&1 || true
    fi
done

exit 0
