#!/bin/sh
set -eu

case "${1:-}" in
    configure|abort-upgrade|abort-deconfigure|abort-remove) ;;
    *) exit 0 ;;
esac

systemd_runtime_dir="${TAKO_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"
default_package_state_dir="${DPKG_ROOT:-}/var/lib/tako"
package_state_dir="${TAKO_PACKAGE_STATE_DIR:-$default_package_state_dir}"
reinstall=false
if [ -f "$package_state_dir/package-removed" ]; then
    reinstall=true
fi

# Remember masks before repairing helper-owned masks. If an older Tako removal
# created one, this installation must behave like a fresh install.
session_socket_was_masked=false
gateway_socket_was_masked=false
if [ -z "${DPKG_ROOT:-}" ] && [ -d "$systemd_runtime_dir" ]; then
    [ "$(systemctl is-enabled tako-sessiond.socket 2>/dev/null || true)" = masked ] && session_socket_was_masked=true
    [ "$(systemctl is-enabled tako.socket 2>/dev/null || true)" = masked ] && gateway_socket_was_masked=true
fi

if [ -n "${DPKG_ROOT:-}" ]; then
    systemd-sysusers --root="$DPKG_ROOT" tako.conf
    systemd-tmpfiles --root="$DPKG_ROOT" --create tako.conf
else
    systemd-sysusers tako.conf
    systemd-tmpfiles --create tako.conf
fi

# Mirror dh_installsystemd state handling. unmask removes masks made by
# deb-systemd-helper (including masks left by older Tako packages), but leaves
# administrator-created systemctl masks alone.
for unit in tako-sessiond.socket tako.socket; do
    deb-systemd-helper unmask "$unit" >/dev/null || true
    if deb-systemd-helper --quiet was-enabled "$unit"; then
        deb-systemd-helper enable "$unit" >/dev/null || true
    else
        deb-systemd-helper update-state "$unit" >/dev/null || true
    fi
done

if [ -n "${DPKG_ROOT:-}" ] || [ ! -d "$systemd_runtime_dir" ]; then
    exit 0
fi

run_systemctl() {
    if ! systemctl "$@" >/dev/null 2>&1; then
        echo "tako: warning: systemctl $* failed" >&2
    fi
}

run_invoke() {
    if ! deb-systemd-invoke "$@" >/dev/null 2>&1; then
        echo "tako: warning: deb-systemd-invoke $* failed" >&2
    fi
}

is_active() {
    systemctl is-active --quiet "$1"
}

is_failed() {
    systemctl is-failed --quiet "$1"
}

is_masked() {
    [ "$(systemctl is-enabled "$1" 2>/dev/null || true)" = masked ]
}

if [ "$session_socket_was_masked" = true ] && ! is_masked tako-sessiond.socket; then
    reinstall=true
fi
if [ "$gateway_socket_was_masked" = true ] && ! is_masked tako.socket; then
    reinstall=true
fi

if [ -z "${2:-}" ] || [ "$reinstall" = true ]; then
    run_systemctl daemon-reload
    run_systemctl reset-failed tako-sessiond.socket tako.socket tako-sessiond.service tako.service
    for unit in tako-sessiond.socket tako.socket; do
        if ! is_masked "$unit"; then
            run_invoke start "$unit"
        fi
    done
    rm -f "$package_state_dir/package-removed"
    exit 0
fi

# Preserve runtime state across upgrades. Stop services before touching their
# listening sockets, bring the private dependency socket back first, then
# restore only processes that were active before the upgrade.
session_socket_active=false
gateway_socket_active=false
session_service_active=false
gateway_service_active=false
session_socket_failed=false
gateway_socket_failed=false
is_active tako-sessiond.socket && session_socket_active=true
is_active tako.socket && gateway_socket_active=true
is_active tako-sessiond.service && session_service_active=true
is_active tako.service && gateway_service_active=true
is_failed tako-sessiond.socket && session_socket_failed=true
is_failed tako.socket && gateway_socket_failed=true

if [ "$gateway_service_active" = true ]; then
    run_invoke stop tako.service
fi
if [ "$session_service_active" = true ]; then
    run_invoke stop tako-sessiond.service
fi

run_systemctl daemon-reload
run_systemctl reset-failed tako-sessiond.socket tako.socket tako-sessiond.service tako.service

if { [ "$session_socket_active" = true ] || [ "$session_socket_failed" = true ]; } && ! is_masked tako-sessiond.socket; then
    run_invoke restart tako-sessiond.socket
fi
if { [ "$gateway_socket_active" = true ] || [ "$gateway_socket_failed" = true ]; } && ! is_masked tako.socket; then
    run_invoke restart tako.socket
fi
if [ "$session_service_active" = true ]; then
    run_invoke start tako-sessiond.service
fi
if [ "$gateway_service_active" = true ]; then
    run_invoke start tako.service
fi

rm -f "$package_state_dir/package-removed"

exit 0
