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

Local packaging requires installed Bun dependencies, Go 1.26+, GoReleaser 2.x, and Linux amd64 PAM development headers and compiler support. CGO is required for PAM, so arm64 cross-builds remain deferred until an AArch64 PAM toolchain is available.

Run `bun run package` from repository root to build local snapshot packages in `dist/`. GoReleaser first builds dashboard assets, then builds `tako` with CGO for Linux amd64 and emits one `.deb`, one `.rpm`, and `checksums.txt`. Snapshot packages do not publish releases. Tagged release builds take version from the Git tag. No signing, archive, or GitHub workflow is configured.

Generated packages install full production runtime files: executable, four systemd units, sysusers and tmpfiles definitions, Polkit policy, the example TOML configuration under `/usr/share/doc/tako/`, read-only distro branding under `/usr/share/tako/branding/`, and a distribution-specific PAM stack at `/etc/pam.d/tako`. Debian includes Debian and Ubuntu art; RPM includes AlmaLinux, Fedora, RHEL, openSUSE, and Rocky Linux art. Arch Linux art is source-ready for host development or a future Arch package. Debian uses `pam/tako.debian`; RPM uses `pam/tako.redhat`. PAM files are package-managed as `config|noreplace`. Packages do not install a live `/etc/tako/config.toml`, sudoers example, smoke test, or operational README.

## Login branding

The public `GET /api/v1/branding` response resolves the exact `ID` from
`/etc/os-release` to an allowlisted image. It does not use `ID_LIKE`; unknown
IDs and installs without the matching external asset use the neutral login
background. Supported IDs are `almalinux`, `arch`, `debian`, `fedora`, `rhel`,
`ubuntu`, `rocky`/`rockylinux`, and IDs prefixed `opensuse`. Images are served
from `/usr/share/tako/branding/` through the gateway's fixed `/branding/*.png`
route. Production metadata revalidates with an ETag, while the selected
release-versioned image URL is immutable-cacheable.

Package installation requires systemd, PAM, D-Bus, Polkit, and PackageKit. Debian packages additionally require `packagekit-tools` for `pkcon` and `init-system-helpers` for Debian systemd maintainer helpers. NetworkManager, UDisks2, Netplan, UFW/firewalld, SELinux, AppArmor, and other host-specific integrations remain optional; capability degradation is reported explicitly. Package scripts create Tako users and runtime directories, reload systemd, and manage only Tako units. They never start or enable external D-Bus, Polkit, or PackageKit services. Installs and upgrades enable `tako.socket` and `tako-sessiond.socket` unless masked and start (fresh install) or restart (upgrade) them, so a stopped-but-enabled socket never stays dead; service processes otherwise start only on socket traffic and are restarted only when already active. The gateway service is never boot-enabled unless masked. Removal stops and masks Tako sockets so a reinstall restores them; only purge disables and clears helper state.

Removal stops and disables Tako units, reloads systemd, and leaves configuration, state, the `tako-session` group, and administrator drop-ins in place.

Install `polkit/org.velopulent.tako.policy` when deploying manually outside generated packages. The optional `sudoers.d/tako.example` documents the exact `NOPASSWD` probe; copy and edit it only for a dedicated local operator group. Do not grant the gateway service account unrestricted sudo.

For direct access, omit `allowed_origin`: Tako accepts same-origin requests based on the request scheme and host, including the server's IP address or hostname. For a reverse proxy, terminate public TLS at the proxy and forward only to a loopback Tako listener. Preserve WebSocket upgrade headers for `/api/v1/terminal/ws`, do not cache `/api/v1/*`, and restrict the proxy's upstream to `127.0.0.1:9090`. Configure `allowed_origin` with the exact public scheme, host, and port when the browser origin differs from the upstream request (comma-separated list allowed).

Host-integrated development (`tools/tako-host`, dashboard overlay) is documented in [`HACKING.md`](../../../HACKING.md).

The disposable-VM smoke seam is `packaging/smoke-test.sh`. It checks binary presence, socket activation, service identities, TLS reachability, and (when `TAKO_SMOKE_USER`/`TAKO_SMOKE_PASSWORD` are provided by the VM harness) a real PAM login. `TAKO_SMOKE_USER` must be a **non-root** UNIX account; root-only success hides the user-bridge spawn path. It never changes host state or stores credentials.
