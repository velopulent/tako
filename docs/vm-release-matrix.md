# Tako release validation matrix

Tako keeps privileged integration validation in disposable virtual machines. A
release candidate is not considered certified until the packaging smoke seam
and the Go integration suite pass for every image in the matrix below.

| Family | Vendor/version | Architecture | Required privileged checks |
| --- | --- | --- | --- |
| Debian | Debian 12, 13 | amd64, arm64 | PAM login, user bridge UID/groups, systemd, NetworkManager or networkd, UFW, AppArmor, file authority |
| Ubuntu | 22.04 LTS, 24.04 LTS | amd64, arm64 | PAM login, systemd, NetworkManager, Netplan, UFW, AppArmor, file authority |
| Fedora | Fedora 40, 41 | amd64, arm64 | PAM login, systemd, NetworkManager, firewalld, SELinux, PackageKit, file authority |
| RHEL-compatible | Rocky Linux 9, AlmaLinux 9 | amd64 | PAM login, systemd, NetworkManager, firewalld, SELinux, PackageKit fallback, file authority |

The matrix is intentionally explicit about backend variants. A green Debian
run does not certify SELinux, and a green Fedora run does not certify UFW or
Netplan. The harness must record the image digest, kernel, architecture, and
which optional backend was active; an unavailable optional backend is a
degraded assertion, not a false ready result.

## Commands

Run the host-independent suites first:

```sh
GOCACHE=/tmp/tako-go-cache go test -race ./...
GOCACHE=/tmp/tako-go-cache go test -tags integration ./internal/platform ./internal/sessiond
```

Inside each installed VM, run:

```sh
TAKO_SMOKE_USER=operator TAKO_SMOKE_PASSWORD='(injected by the VM harness)' \
  apps/backend/packaging/smoke-test.sh
```

The harness must inject the password only for the process lifetime, redact it
from logs, destroy the VM after the run, and retain only structured pass/fail
results. It must also exercise the network checkpoint/rollback, firewall
access-warning, SELinux/AppArmor narrow-remediation, support-report warning,
certificate inspection, and logout/expiry cleanup seams when the corresponding
backend is present.
