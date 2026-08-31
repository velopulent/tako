#!/bin/sh
set -eu

systemctl daemon-reload >/dev/null 2>&1 || true

exit 0
