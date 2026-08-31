#!/bin/sh
set -eu

if [ "${1:-0}" -eq 0 ]; then
    for unit in tako.service tako-sessiond.service tako.socket tako-sessiond.socket; do
        systemctl stop "$unit" >/dev/null 2>&1 || true
    done
    for unit in tako.service tako.socket tako-sessiond.socket; do
        systemctl disable "$unit" >/dev/null 2>&1 || true
    done
fi

exit 0
