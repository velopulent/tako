#!/bin/sh
set -eu

if command -v deb-systemd-invoke >/dev/null 2>&1; then
    deb-systemd-invoke daemon-reload >/dev/null 2>&1 || systemctl daemon-reload >/dev/null 2>&1 || true
else
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

case "${1:-}" in
    remove)
        # Keep the prerm mask so a reinstall unmasks cleanly.
        for unit in tako.socket tako-sessiond.socket; do
            deb-systemd-helper mask "$unit" >/dev/null 2>&1 || true
        done
        ;;
    purge|disappear)
        for unit in tako.socket tako-sessiond.socket tako.service tako-sessiond.service; do
            deb-systemd-helper purge "$unit" >/dev/null 2>&1 || true
            deb-systemd-helper unmask "$unit" >/dev/null 2>&1 || true
        done
        ;;
esac

exit 0
