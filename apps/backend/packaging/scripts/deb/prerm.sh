#!/bin/sh
set -eu

case "${1:-}" in
    remove|purge)
        for unit in tako.service tako-sessiond.service tako.socket tako-sessiond.socket; do
            deb-systemd-invoke stop "$unit" >/dev/null 2>&1 || true
        done
        for unit in tako.service tako.socket tako-sessiond.socket; do
            deb-systemd-helper disable "$unit" >/dev/null 2>&1 || true
        done
        ;;
esac

exit 0
