# Beta release process

`targets.json` is the source of truth for the beta support matrix. It currently
contains the 15 native combinations below:

| Family | Release | Architectures |
| --- | --- | --- |
| Debian | 13 | amd64, arm64 |
| Ubuntu | 26.04 LTS | amd64, arm64 |
| Fedora | 44 | amd64, arm64 |
| RHEL | 10.2 | amd64, arm64 |
| Rocky Linux | 10.2 | amd64, arm64 |
| AlmaLinux | 10.2 | amd64, arm64 |
| openSUSE Leap | 16.0 | amd64, arm64 |
| Arch Linux | rolling snapshot | amd64 |

The table is descriptive; scripts and workflows read `targets.json` so adding a
target does not require changing artifact counts in a shell assertion. Every
target has its own GoReleaser build ID and is built with `CGO_ENABLED=1` inside
the target distribution image on a runner with the matching CPU architecture.
The release build lane deliberately does not use QEMU or cross-link against
another distribution's libc and PAM libraries.

For local cross-distro packaging, install the host's PAM development package
and the tools listed by `bun run package:check`, then run:

```bash
bun run package
```

This emits every manifest distro target matching host `GOARCH` (eight packages
on amd64, seven on arm64 with the current manifest). These are cross-CGO
packages built with the host's libc and PAM libraries. For one native local
build, select a manifest ID:

```bash
TAKO_PACKAGE_TARGET=fedora44-amd64 bun run package
```

The selected native target must match `/etc/os-release` and `GOARCH`. The
resulting `dist/` directory contains one package, one binary, `artifacts.json`,
and a checksum file for that target.

The `native-packages.yml` workflow expands the manifest and builds each target
in its declared image. The arm64 entries require an arm64 GitHub runner; an
amd64 runner cannot silently satisfy them. The RHEL image is the public UBI
image named by the manifest. If an image tag or hosted runner is unavailable,
the target remains pending or fails and the beta release stays blocked.

The reusable `vm-validation.yml` workflow consumes native artifacts from the
same beta run (use the beta workflow for manual end-to-end validation). It runs `packaging/vm-test.sh` on a disposable
VM runner carrying the `vm_label` from the manifest. These labels describe an
operator-provided runner contract; this repository does not contain VM hosts,
credentials, or evidence that a VM has run. Each successful invocation writes
one release input record containing the tested package digest. A failed or
missing record is a hard failure.

PR and push CI run on GitHub-hosted runners. Privileged VM integration runs
on its schedule, by manual dispatch, and as a required beta release gate; it
is not a PR gate because it requires operator-provided disposable runners.

The beta workflow publishes only after all required CI jobs, VM integration, all native package
jobs, and every manifest target's VM evidence pass. It verifies each package digest against its VM evidence, then assembles packages,
writes `dependency-inventory.json`, creates `checksums.txt`, signs that checksum
manifest with keyless Sigstore signing, and emits GitHub build provenance. The
workflow uses the repository's ephemeral `GITHUB_TOKEN`; no package registry,
VM host, signing key, password, or deployment credential is stored in this
repository.

Run the Arch lifecycle harness inside a disposable Arch VM when both package
versions are available:

```bash
TAKO_DISPOSABLE_HOST=1 \
  apps/backend/packaging/lifecycle-test.sh old.pkg.tar.zst new.pkg.tar.zst
```

The harness exercises fresh install, socket activation, active upgrade,
disabled and masked socket preservation, removal, and reinstall. Debian and
RPM paths use their native package managers in the same harness. The unit-level
maintainer-hook tests also exercise Arch's install, upgrade, and removal hooks.

`systemd-sysusers tako.conf` is intentional. `systemd-sysusers` treats a
basename as a lookup through the `sysusers.d` search path, and `tako.conf` is a
valid vendor filename. The package installs it under `/usr/lib/sysusers.d/`,
which leaves `/etc/sysusers.d/` available for administrator overrides.

Tag builds preserve the release version in package metadata and the binary.
Local untagged builds retain GoReleaser snapshot versions. Release checksums
use relative filenames and can be checked from the downloaded asset directory.
