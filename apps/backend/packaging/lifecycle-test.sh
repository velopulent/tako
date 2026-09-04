#!/usr/bin/env bash
set -euo pipefail

if [[ "${TAKO_DISPOSABLE_HOST:-}" != 1 ]]; then
  echo "Refusing to modify this host. Run only in a disposable VM with TAKO_DISPOSABLE_HOST=1." >&2
  exit 2
fi
if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root inside the disposable VM." >&2
  exit 2
fi
if [[ $# -ne 2 ]]; then
  echo "usage: TAKO_DISPOSABLE_HOST=1 $0 OLD_PACKAGE NEW_PACKAGE" >&2
  exit 2
fi

old_package=$(realpath "$1")
new_package=$(realpath "$2")
units=(tako-sessiond.socket tako.socket tako-sessiond.service tako.service)

case "$new_package" in
  *.deb)
    [[ "$old_package" == *.deb ]] || { echo "Both packages must be .deb files." >&2; exit 2; }
    install_package() { dpkg --install "$1"; }
    remove_package() { dpkg --remove tako; }
    purge_package() { dpkg --purge tako >/dev/null 2>&1 || true; }
    ;;
  *.rpm)
    [[ "$old_package" == *.rpm ]] || { echo "Both packages must be .rpm files." >&2; exit 2; }
    install_package() { rpm --upgrade --verbose --hash "$1"; }
    remove_package() { rpm --erase tako; }
    purge_package() { rpm --erase tako >/dev/null 2>&1 || true; }
    ;;
  *)
    echo "NEW_PACKAGE must end in .deb or .rpm." >&2
    exit 2
    ;;
esac

reset_host() {
  purge_package
  systemctl unmask "${units[@]}" >/dev/null 2>&1 || true
  systemctl reset-failed "${units[@]}" >/dev/null 2>&1 || true
}

assert_socket_ready() {
  local unit=$1
  systemctl is-enabled --quiet "$unit"
  systemctl is-active --quiet "$unit"
  ! systemctl is-failed --quiet "$unit"
}

assert_default_install() {
  assert_socket_ready tako-sessiond.socket
  assert_socket_ready tako.socket
  ! systemctl is-active --quiet tako-sessiond.service
  ! systemctl is-active --quiet tako.service
}

activate_services() {
  local status
  status=$(curl --silent --show-error --insecure --output /dev/null --write-out '%{http_code}' \
    https://127.0.0.1:9090/api/v1/auth/session)
  [[ "$status" == 401 ]]
  python3 -c 'import socket; client=socket.socket(socket.AF_UNIX); client.connect("/run/tako/session.sock"); client.close()'
  systemctl is-active --quiet tako.service
  systemctl is-active --quiet tako-sessiond.service
}

assert_no_package_mask() {
  local unit target
  for unit in tako-sessiond.socket tako.socket; do
    target=$(readlink -f "/etc/systemd/system/$unit" 2>/dev/null || true)
    [[ "$target" != /dev/null ]] || { echo "$unit left masked after removal" >&2; exit 1; }
  done
}

trap reset_host EXIT

# Fresh install and remove/reinstall must need no manual enable --now.
reset_host
install_package "$new_package"
assert_default_install
activate_services
remove_package
assert_no_package_mask
install_package "$new_package"
assert_default_install

# Upgrade running services without leaving either socket failed.
reset_host
install_package "$old_package"
activate_services
journal_cursor=$(journalctl -u tako.socket -u tako-sessiond.socket -n 0 --show-cursor --no-pager \
  | sed -n 's/^-- cursor: //p')
install_package "$new_package"
assert_socket_ready tako-sessiond.socket
assert_socket_ready tako.socket
activate_services
if [[ -n "$journal_cursor" ]]; then
  ! journalctl -u tako.socket -u tako-sessiond.socket --after-cursor "$journal_cursor" --no-pager \
    | grep -Fq 'Trigger limit hit'
fi

# Upgrade must preserve administrator-stopped and disabled sockets.
reset_host
install_package "$old_package"
systemctl disable --now tako.socket tako-sessiond.socket
install_package "$new_package"
! systemctl is-active --quiet tako.socket
! systemctl is-active --quiet tako-sessiond.socket
[[ "$(systemctl is-enabled tako.socket 2>/dev/null || true)" == disabled ]]
[[ "$(systemctl is-enabled tako-sessiond.socket 2>/dev/null || true)" == disabled ]]

# Upgrade must preserve a real administrator mask.
reset_host
install_package "$old_package"
systemctl mask --now tako.socket
install_package "$new_package"
[[ "$(systemctl is-enabled tako.socket 2>/dev/null || true)" == masked ]]

reset_host
echo "Package lifecycle tests passed"
