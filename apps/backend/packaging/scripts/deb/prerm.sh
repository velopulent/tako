#!/bin/sh
set -eu

case "${1:-}" in
    remove)
        for unit in tako.service tako-sessiond.service tako.socket tako-sessiond.socket; do
            deb-systemd-invoke stop "$unit" >/dev/null 2>&1 || true
        done
        # Mask (not disable) so a later reinstall restores the previous
        # enabled state via postinst unmask+enable. Plain disable would
        # destroy it and reinstalls would stay disabled.
        for unit in tako.socket tako-sessiond.socket; do
            deb-systemd-helper mask "$unit" >/dev/null 2>&1 || true
        done
        ;;
    upgrade|deconfigure|failed-upgrade)
        # restart-after-upgrade: leave units running; postinst restarts them.
        ;;
esac

exit 0
