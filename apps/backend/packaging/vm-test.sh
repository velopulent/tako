#!/usr/bin/env bash
set -Eeuo pipefail

# This is the only script that writes a VM validation result. It is intended to
# run as root in a disposable, booted target VM. The release workflow consumes
# the result and refuses to publish when a target has no passing evidence.
target_id="${TAKO_VM_TARGET:-}"
new_package="${TAKO_VM_PACKAGE:-}"
old_package="${TAKO_VM_OLD_PACKAGE:-}"
evidence_dir="${TAKO_VM_EVIDENCE_DIR:-vm-evidence}"
manifest="${TAKO_VM_MANIFEST:-apps/backend/packaging/targets.json}"
evidence_file="$evidence_dir/${target_id:-unknown}.json"
status=failed
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
log_file=""

write_evidence() {
  local exit_code=$?
  mkdir -p "$evidence_dir"
  python3 - "$evidence_file" "$target_id" "$status" "$exit_code" "$started_at" "$new_package" "$old_package" "$log_file" <<'PY'
import hashlib
import json
import os
import platform
import sys
from datetime import datetime, timezone

path, target, status, exit_code, started, new_package, old_package, log_file = sys.argv[1:]

def digest(value):
    if not value:
        return None
    try:
        with open(value, "rb") as stream:
            return hashlib.sha256(stream.read()).hexdigest()
    except OSError:
        return None

payload = {
    "schema": 1,
    "target": target,
    "status": status,
    "exit_code": int(exit_code),
    "started_at": started,
    "finished_at": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "commit": os.environ.get("GITHUB_SHA") or os.environ.get("TAKO_VM_COMMIT"),
    "host": {
        "machine": platform.machine(),
        "system": platform.system(),
        "release": platform.release(),
    },
    "package": {
        "new": os.path.basename(new_package) if new_package else None,
        "new_sha256": digest(new_package),
        "old": os.path.basename(old_package) if old_package else None,
        "old_sha256": digest(old_package),
    },
    "log": os.path.basename(log_file) if log_file else None,
}
with open(path, "w", encoding="utf-8") as stream:
    json.dump(payload, stream, sort_keys=True, indent=2)
    stream.write("\n")
PY
  return "$exit_code"
}
trap write_evidence EXIT

if [[ "$EUID" -ne 0 ]]; then
  echo "vm-test: run as root in a disposable target VM" >&2
  exit 2
fi
if [[ -z "$target_id" || -z "$new_package" ]]; then
  echo "usage: TAKO_VM_TARGET=TARGET TAKO_VM_PACKAGE=PACKAGE $0" >&2
  exit 2
fi
if [[ ! -f "$manifest" || ! -f "$new_package" ]]; then
  echo "vm-test: manifest and package must be present in the VM" >&2
  exit 2
fi

mkdir -p "$evidence_dir"
log_file="$evidence_dir/${target_id}.log"
: >"$log_file"
exec > >(tee -a "$log_file") 2>&1

target_json=$(python3 - "$manifest" "$target_id" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as stream:
    manifest = json.load(stream)
for target in manifest["targets"]:
    if target["id"] == sys.argv[2]:
        print(json.dumps(target))
        break
else:
    raise SystemExit("unknown target")
PY
)

read_target() {
  python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$target_json" "$1"
}

expected_distro=$(read_target distribution)
expected_release=$(read_target release)
expected_arch=$(read_target goarch)
package_format=$(read_target package_format)
host_arch=$(go env GOARCH 2>/dev/null || true)
[[ -n "$host_arch" ]] || host_arch=$(case "$(uname -m)" in x86_64) echo amd64;; aarch64) echo arm64;; *) echo unknown;; esac)
[[ "$host_arch" == "$expected_arch" ]] || { echo "vm-test: host arch $host_arch does not match $expected_arch" >&2; exit 1; }

declare -A os_release=()
while IFS='=' read -r key value; do
  value=${value%\"}
  value=${value#\"}
  os_release["$key"]="$value"
done < /etc/os-release
host_distro=${os_release[ID]:-}
case "$host_distro" in
  arch) host_distro=archlinux ;;
  opensuse-leap|opensuse-tumbleweed|sles) host_distro=opensuse ;;
esac
[[ "$host_distro" == "$expected_distro" ]] || { echo "vm-test: host distro $host_distro does not match $expected_distro" >&2; exit 1; }
if [[ "$expected_release" != rolling ]]; then
  host_release=${os_release[VERSION_ID]:-}
  [[ "$host_release" == "$expected_release" || "$host_release" == "$expected_release."* ]] || {
    echo "vm-test: host release $host_release does not match $expected_release" >&2
    exit 1
  }
fi

if [[ -n "$old_package" ]]; then
  [[ -f "$old_package" ]] || { echo "vm-test: old package does not exist" >&2; exit 1; }
  TAKO_DISPOSABLE_HOST=1 apps/backend/packaging/lifecycle-test.sh "$old_package" "$new_package"
else
  case "$package_format" in
    deb) dpkg --install "$new_package" ;;
    rpm) rpm --upgrade --verbose --hash "$new_package" ;;
    archlinux) pacman --noconfirm --upgrade "$new_package" ;;
    *) echo "vm-test: unsupported package format $package_format" >&2; exit 1 ;;
  esac
  TAKO_BINARY=/usr/bin/tako apps/backend/packaging/smoke-test.sh
fi

status=passed
echo "VM validation passed for $target_id"
