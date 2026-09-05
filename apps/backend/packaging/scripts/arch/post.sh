#!/bin/sh
set -eu

systemd-sysusers tako.conf
systemd-tmpfiles --create tako.conf

systemd_runtime_dir="${TAKO_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"
if [ ! -d "$systemd_runtime_dir" ]; then
    exit 0
fi

run_systemctl() {
    if ! systemctl "$@" >/dev/null 2>&1; then
        echo "tako: warning: systemctl $* failed" >&2
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

install_action="${1:-install}"
case "$install_action" in
    install)
        run_systemctl daemon-reload
        run_systemctl reset-failed tako-sessiond.socket tako.socket tako-sessiond.service tako.service
        for unit in tako-sessiond.socket tako.socket; do
            if ! is_masked "$unit"; then
                run_systemctl enable "$unit"
                run_systemctl start "$unit"
            fi
        done
        ;;
    upgrade)
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
            run_systemctl stop tako.service
        fi
        if [ "$session_service_active" = true ]; then
            run_systemctl stop tako-sessiond.service
        fi

        run_systemctl daemon-reload
        run_systemctl reset-failed tako-sessiond.socket tako.socket tako-sessiond.service tako.service

        if { [ "$session_socket_active" = true ] || [ "$session_socket_failed" = true ]; } && ! is_masked tako-sessiond.socket; then
            run_systemctl restart tako-sessiond.socket
        fi
        if { [ "$gateway_socket_active" = true ] || [ "$gateway_socket_failed" = true ]; } && ! is_masked tako.socket; then
            run_systemctl restart tako.socket
        fi
        if [ "$session_service_active" = true ]; then
            run_systemctl start tako-sessiond.service
        fi
        if [ "$gateway_service_active" = true ]; then
            run_systemctl start tako.service
        fi
        ;;
esac
