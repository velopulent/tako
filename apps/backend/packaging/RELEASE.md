# Release process

`targets.json` is the source of truth for the support matrix. It currently
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
the target remains pending or fails and the release stays blocked.

The `release.yml` workflow runs CI, builds every manifest target with
`native-packages.yml` on GitHub-hosted runners, assembles one package per
target, and writes `checksums.txt`. It publishes a GitHub release for any
`v*` tag; manual dispatch builds and validates without publishing. The tag
is the release type: a hyphenated suffix (`v0.3.0-beta1`, `v0.3.0-alpha2`,
`v0.3.0-rc1`) publishes as a prerelease, a clean version (`v1.2.0`)
publishes as a stable release. Malformed tags fail fast before any packages
build. There is no VM evidence gate, no signing, and no provenance
attestation; validation of installed systems happens separately through the
lifecycle harness inside a disposable VM you provide.

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
