# Production packaging

The package installs one `/usr/bin/tako` multicall executable and keeps the trust domains separate:

- `tako.service` is the unprivileged HTTPS gateway and starts on demand from `tako.socket`. It uses systemd `DynamicUser=yes` (`User=tako-gateway`) with primary group `tako-session`, writes only to `/var/lib/tako` via `StateDirectory`, and exits after the configured idle interval when no sessions or work remain.
- `sysusers.d/tako.conf` creates the persistent `tako-session` group used for session-socket ACLs. There is no static `tako` login or system user.
- `tako-sessiond.socket` listens on `/run/tako/session.sock` as `root:tako-session` mode `0660` and starts the root-owned `tako-sessiond.service` on demand. Sessiond exits when no connections, PAM conversations, or grants remain for the configured idle interval.
- `tmpfiles.d/tako.conf` creates `/run/tako` as `root:tako-session` with mode `0750` before socket activation.
- `tako bridge` is launched by the session service for an authenticated UNIX user and is never reachable from the network gateway.
- `tako-sessiond.service` is a login and host-authority helper: it PAM-authenticates, resolves typed operations, then setuid-execs `tako bridge` as that user. The child inherits the unit sandbox, so sessiond is not locked down like `tako.service`. `tako serve --dev` uses a fake broker, skips PAM and the user bridge, and is not a production auth test.

Sessiond reads the same `/etc/tako/config.toml` as the gateway (`--config` is
included in the packaged unit). The gateway never opens a host D-Bus bus or
uses a privileged host operation as a fallback. The narrow public branding
endpoint reads only `/etc/os-release` and the hostname for the pre-auth login
screen. User-scoped services require a PAM-created
`XDG_RUNTIME_DIR`, a reachable user D-Bus socket, and a running systemd user
manager; Tako reports `user-manager-unavailable` when those prerequisites are
missing. Sessiond does not enable linger, start a missing manager, or
impersonate another user. External certificate paths are read by sessiond;
certificates in Tako's own state directory remain gateway-owned.

Set `server.service_idle_timeout` to a Go duration such as `10m`; `0` disables automatic service exit. Idle exit stops sessiond's in-memory metric sampler, so metric history contains gaps while Tako is stopped.

Hosts must resolve systemd dynamic users. `nsswitch.conf` `passwd` (and typically `group`) must include `systemd`, for example `passwd: files systemd`. Without that, `DynamicUser` fails with status 217/USER.

Install the distribution-specific PAM file as `/etc/pam.d/tako`: use `pam/tako.debian` on Debian/Ubuntu and `pam/tako.redhat` on Fedora/RHEL-compatible systems. The generic `pam/tako` file is only a minimal development fixture; package builds should transform it to the host's standard stack.
Password changes and administrative resets use the host's standard `passwd` PAM service. They do not invoke a password helper with secrets in argv, stdin, or the environment.

## GoReleaser packages

Local packaging requires installed Bun dependencies, Go 1.26+, GoReleaser 2.x, a C compiler, and the host's PAM development headers and linker files. `bun run package:check` validates those capabilities without assuming a distribution package manager. The default package command cross-packages every manifest distro target matching the host architecture; use `TAKO_PACKAGE_TARGET` to select one native manifest entry. The complete beta matrix and its external VM prerequisites are documented in [`RELEASE.md`](RELEASE.md).

Run `bun run package` from repository root to validate packaging and emit one package and binary per manifest distro for the host architecture, plus `artifacts.json` and `checksums.txt` in `dist/`. `TAKO_PACKAGE_TARGET=<manifest-id> bun run package` emits one native package and binary. GoReleaser's embedded nFPM packager creates packages without requiring a separate package-builder tool. Snapshot packages do not publish releases. The beta workflow builds each manifest target in its own target image, signs the final checksum manifest, records a dependency inventory, and emits provenance only after the CI and VM evidence gates pass. Local cross-packages use the host's CGO, glibc, and PAM toolchain; native matrix builds provide target-runtime compatibility validation.

Generated packages install full production runtime files: executable, four systemd units, sysusers and tmpfiles definitions, Polkit policy, the example TOML configuration under `/usr/share/doc/tako/`, exactly one matching distro branding asset under `/usr/share/tako/branding/`, and a distribution-specific PAM stack at `/etc/pam.d/tako`. Debian and Ubuntu use `pam/tako.debian`; Fedora, RHEL, Rocky, and AlmaLinux use `pam/tako.redhat`; openSUSE uses `pam/tako.opensuse`; Arch Linux uses the generic `pam/tako` policy. PAM files are package-managed as `config|noreplace`. Packages do not install a live `/etc/tako/config.toml`, sudoers example, smoke test, or operational README.

## Login branding

The public `GET /api/v1/branding` response resolves the exact `ID` from
`/etc/os-release` to an allowlisted image. It does not use `ID_LIKE`; unknown
IDs and installs without the matching external asset use the neutral login
background. Supported IDs are `almalinux`, `arch`, `debian`, `fedora`, `rhel`,
`ubuntu`, `rocky`/`rockylinux`, and IDs prefixed `opensuse`. Images are served
from `/usr/share/tako/branding/` through the gateway's fixed `/branding/*.png`
route. Production metadata revalidates with an ETag, while the selected
release-versioned image URL is immutable-cacheable.

Package installation requires systemd, PAM, D-Bus, Polkit, and the native package manager selected when building. Debian and Ubuntu use APT; Fedora, RHEL, Rocky, and AlmaLinux use DNF; Arch Linux uses Pacman plus `pacman-contrib`; openSUSE uses Zypper. NetworkManager, UDisks2, Netplan, UFW/firewalld, SELinux, AppArmor, and other host-specific integrations remain optional; capability degradation is reported explicitly.

Build one distro at a time:

```bash
cd apps/backend
go build -tags debian ./cmd/tako
go build -tags ubuntu ./cmd/tako
go build -tags fedora ./cmd/tako
go build -tags rhel ./cmd/tako
go build -tags rocky ./cmd/tako
go build -tags almalinux ./cmd/tako
go build -tags archlinux ./cmd/tako
go build -tags opensuse ./cmd/tako
```

Nx commands detect `/etc/os-release`; set `TAKO_DISTRO` to override detection. Raw builds without exactly one distro-family tag fail compilation.

Fresh installs enable and start `tako-sessiond.socket` before the public `tako.socket`; services remain socket-activated. Upgrades preserve administrator-disabled, stopped, and explicitly masked units. When units were running, scripts stop services before restarting sockets, reload systemd, clear failed/start-limit state, restore the private socket before the public socket, then restore only services that were previously active. Debian uses `deb-systemd-helper` state and `deb-systemd-invoke`, including offline-root and `policy-rc.d` behavior. Upgrades from older Tako packages remove only masks previously created by `deb-systemd-helper`; administrator masks remain intact.

Removal stops Tako units and never creates masks. RPM final removal disables the sockets. Debian removal keeps helper enablement state so reinstall restores package-managed defaults; purge clears that helper state. Configuration, application state, the `tako-session` group, and administrator drop-ins remain in place.

Install `polkit/org.velopulent.tako.policy` when deploying manually outside generated packages. The optional `sudoers.d/tako.example` documents the exact `NOPASSWD` probe; copy and edit it only for a dedicated local operator group. Do not grant the gateway service account unrestricted sudo.

For direct access, omit `allowed_origin`: Tako accepts same-origin requests based on the request scheme and host, including the server's IP address or hostname. For a reverse proxy, terminate public TLS at the proxy and forward only to a loopback Tako listener. Preserve WebSocket upgrade headers for `/api/v1/terminal/ws`, do not cache `/api/v1/*`, and restrict the proxy's upstream to `127.0.0.1:9090`. Configure `allowed_origin` with the exact public scheme, host, and port when the browser origin differs from the upstream request (comma-separated list allowed).

Host-integrated development (`tools/tako-host`, dashboard overlay) is documented in [`HACKING.md`](../../../HACKING.md).

The installed-system smoke seam is `packaging/smoke-test.sh`. It checks binary presence, socket activation, static service enablement, service identities, TLS reachability, and (when `TAKO_SMOKE_USER`/`TAKO_SMOKE_PASSWORD` are provided) a real PAM login. `TAKO_SMOKE_USER` must be a **non-root** UNIX account; root-only success hides the user-bridge spawn path. It stops activated service processes at completion while leaving sockets running.

Run `packaging/lifecycle-test.sh OLD_PACKAGE NEW_PACKAGE` only inside a disposable VM with `TAKO_DISPOSABLE_HOST=1`. It destructively exercises fresh install, active upgrade, disabled/stopped preservation, administrator masks, removal, reinstall, purge, and trigger-limit health for two `.deb`, two `.rpm`, or two Arch `.pkg.tar.zst` files. Dependencies must already be available in the VM. `packaging/vm-test.sh` records evidence only after this harness or the installed-system smoke test actually succeeds.
