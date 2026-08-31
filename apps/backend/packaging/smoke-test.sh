#!/usr/bin/env bash
set -euo pipefail

# Run inside a disposable, already-installed VM. The script never prints the
# optional password and does not modify host configuration.
binary="${TAKO_BINARY:-/usr/bin/tako}"
gateway_unit="${TAKO_GATEWAY_UNIT:-tako.service}"
gateway_socket_unit="${TAKO_GATEWAY_SOCKET_UNIT:-tako.socket}"
session_socket_unit="${TAKO_SESSION_SOCKET_UNIT:-tako-sessiond.socket}"
session_service_unit="${TAKO_SESSION_SERVICE_UNIT:-tako-sessiond.service}"
base_url="${TAKO_BASE_URL:-https://127.0.0.1:9090}"

test -x "$binary"
systemctl is-enabled "$gateway_unit" >/dev/null
systemctl is-enabled "$gateway_socket_unit" >/dev/null
systemctl is-enabled "$session_socket_unit" >/dev/null
systemctl is-active "$gateway_socket_unit" >/dev/null
systemctl is-active "$session_socket_unit" >/dev/null
if systemctl is-active --quiet "$gateway_unit"; then
    echo "$gateway_unit unexpectedly active before socket traffic" >&2
    exit 1
fi
if systemctl is-active --quiet "$session_service_unit"; then
    echo "$session_service_unit unexpectedly active before socket traffic" >&2
    exit 1
fi

# HTTPS traffic activates only the gateway.
status="$(curl --silent --show-error --insecure --output /dev/null --write-out '%{http_code}' "$base_url/api/v1/auth/session")"
test "$status" = 401
systemctl is-active "$gateway_unit" >/dev/null
if systemctl is-active --quiet "$session_service_unit"; then
    echo "$session_service_unit activated without privileged socket traffic" >&2
    exit 1
fi

# A Unix-socket connection activates sessiond without attempting PAM.
python3 -c 'import socket; client=socket.socket(socket.AF_UNIX); client.connect("/run/tako/session.sock"); client.close()'
systemctl is-active "$session_service_unit" >/dev/null
# Starting sessiond must not remove its socket-activation pathname. This catches
# accidental RuntimeDirectory ownership of /run/tako by the service unit.
test -S /run/tako/session.sock
test "$(stat -c '%a %U %G' /run/tako)" = "750 root tako-session"
test "$(stat -c '%a %U %G' /run/tako/session.sock)" = "660 root tako-session"

gateway_dynamic="$(systemctl show -p DynamicUser --value "$gateway_unit")"
gateway_user="$(systemctl show -p User --value "$gateway_unit")"
gateway_group="$(systemctl show -p Group --value "$gateway_unit")"
session_user="$(systemctl show -p User --value "$session_service_unit")"
test "$gateway_dynamic" = yes
test "$gateway_user" = tako-gateway
test "$gateway_group" = tako-session
test "$session_user" = root

if [[ -n "${TAKO_SMOKE_USER:-}" && -n "${TAKO_SMOKE_PASSWORD:-}" ]]; then
  if [[ "${TAKO_SMOKE_USER}" == "root" ]]; then
    echo "TAKO_SMOKE_USER must be a non-root UNIX account" >&2
    exit 1
  fi
  response="$(printf '%s' "$(TAKO_USER="$TAKO_SMOKE_USER" TAKO_PASSWORD="$TAKO_SMOKE_PASSWORD" python3 -c 'import json,os; print(json.dumps({"username":os.environ["TAKO_USER"],"password":os.environ["TAKO_PASSWORD"]}))')" | curl --silent --show-error --insecure --fail --header 'Origin: https://127.0.0.1:9090' --header 'Content-Type: application/json' --data-binary @- "$base_url/api/v1/auth/login")"
  printf '%s' "$response" | python3 -c 'import json,sys; value=json.load(sys.stdin); assert value.get("user",{}).get("username")'
  echo "PAM login smoke test passed"
else
  echo "PAM login smoke test skipped (set TAKO_SMOKE_USER and TAKO_SMOKE_PASSWORD in the disposable VM)"
fi

systemctl stop "$gateway_unit" "$session_service_unit"
systemctl is-active "$gateway_socket_unit" >/dev/null
systemctl is-active "$session_socket_unit" >/dev/null

echo "Tako packaging smoke test passed"
