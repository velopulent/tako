#!/bin/sh
set -eu

systemd-sysusers /usr/lib/sysusers.d/tako.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/tako.conf

if command -v deb-systemd-invoke >/dev/null 2>&1; then
    deb-systemd-invoke daemon-reload >/dev/null 2>&1 || systemctl daemon-reload >/dev/null 2>&1 || true
else
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

if [ "${1:-}" = configure ] || [ "${1:-}" = abort-upgrade ] || [ "${1:-}" = abort-deconfigure ] || [ "${1:-}" = abort-remove ]; then
    # Always ensure the sockets are enabled (unless masked) and running.
    # A remove+reinstall must re-enable (old packages disabled on remove),
    # and an upgrade must restart — try-restart would leave a
    # stopped-but-enabled socket dead.
    for unit in tako.socket tako-sessiond.socket; do
        if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
            deb-systemd-helper unmask "$unit" >/dev/null 2>&1 || true
            deb-systemd-helper enable "$unit" >/dev/null 2>&1 || true
            systemctl enable "$unit" >/dev/null 2>&1 || true
            deb-systemd-helper update-state "$unit" >/dev/null 2>&1 || true
        fi
    done

    if [ -z "${2:-}" ]; then
        _tako_action=start
    else
        _tako_action=restart
    fi
    for unit in tako.socket tako-sessiond.socket; do
        if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
            if command -v deb-systemd-invoke >/dev/null 2>&1; then
                deb-systemd-invoke "$_tako_action" "$unit" >/dev/null 2>&1 || true
            else
                systemctl "$_tako_action" "$unit" >/dev/null 2>&1 || true
            fi
        fi
    done
    # Socket-activated services must never be started by the package;
    # only pick up a new binary when already running.
    for unit in tako-sessiond.service tako.service; do
        if command -v deb-systemd-invoke >/dev/null 2>&1; then
            deb-systemd-invoke try-restart "$unit" >/dev/null 2>&1 || true
        else
            systemctl try-restart "$unit" >/dev/null 2>&1 || true
        fi
    done
fi

# The gateway and sessiond are socket-activated and must never be
# boot-enabled; only the sockets carry an [Install] section.
for unit in tako.service tako-sessiond.service; do
    if [ "$(systemctl is-enabled "$unit" 2>/dev/null || true)" != masked ]; then
        deb-systemd-helper disable "$unit" >/dev/null 2>&1 || true
    fi
done

exit 0
