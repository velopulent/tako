#!/usr/bin/env bash
set -euo pipefail

# Run inside a disposable, already-installed VM. The script never prints the
# optional password and does not modify host configuration.
binary="${TAKO_BINARY:-/usr/bin/tako}"
gateway_unit="${TAKO_GATEWAY_UNIT:-tako.service}"
session_socket_unit="${TAKO_SESSION_SOCKET_UNIT:-tako-sessiond.socket}"
base_url="${TAKO_BASE_URL:-https://127.0.0.1:9090}"

test -x "$binary"
systemctl is-enabled "$gateway_unit" >/dev/null
systemctl is-enabled "$session_socket_unit" >/dev/null
systemctl is-active "$gateway_unit" >/dev/null
systemctl is-active "$session_socket_unit" >/dev/null
test -S /run/tako/session.sock

gateway_user="$(systemctl show -p User --value "$gateway_unit")"
session_user="$(systemctl show -p User --value tako-sessiond.service)"
test "$gateway_user" = tako
test "$session_user" = root

# An unauthenticated request proves TLS/socket reachability without attempting
# PAM. The production gateway must not expose a dashboard without a session.
status="$(curl --silent --show-error --insecure --output /dev/null --write-out '%{http_code}' "$base_url/api/v1/auth/session")"
test "$status" = 401

if [[ -n "${TAKO_SMOKE_USER:-}" && -n "${TAKO_SMOKE_PASSWORD:-}" ]]; then
  response="$(printf '%s' "$(TAKO_USER="$TAKO_SMOKE_USER" TAKO_PASSWORD="$TAKO_SMOKE_PASSWORD" python3 -c 'import json,os; print(json.dumps({"username":os.environ["TAKO_USER"],"password":os.environ["TAKO_PASSWORD"]}))')" | curl --silent --show-error --insecure --fail --header 'Origin: https://127.0.0.1:9090' --header 'Content-Type: application/json' --data-binary @- "$base_url/api/v1/auth/login")"
  printf '%s' "$response" | python3 -c 'import json,sys; value=json.load(sys.stdin); assert value.get("user",{}).get("username")'
  echo "PAM login smoke test passed"
else
  echo "PAM login smoke test skipped (set TAKO_SMOKE_USER and TAKO_SMOKE_PASSWORD in the disposable VM)"
fi

echo "Tako packaging smoke test passed"
