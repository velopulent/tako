<div align="center">

<img src="assets/TakoMascot-1.webp" width="300" alt="Tako mascot, an octopus">

# Tako

**A Linux administration dashboard you host yourself. Log in with your UNIX account and check system health, services, logs, and updates from a browser.**

[Quick start](#quick-start) · [What you can do](#what-you-can-do) · [How it works](#how-it-works) · [Configuration](#configuration) · [Development](./HACKING.md)

</div>

> :warning: Tako is under active development and not yet at v1. Expect breaking changes. See [status and scope](#status-and-scope).

Tako is one Go binary plus an embedded React dashboard. It runs on the machine it manages and exposes that machine over HTTPS. You log in with your normal UNIX username and password through PAM. There is no separate Tako account database and no cloud component.

The gateway that faces the browser is unprivileged. Anything that needs host authority goes through a small privileged helper over a local socket, and per-user work runs as your own UID. The code keeps those three roles in separate processes so a bug in the web layer does not become root access.

Linux only. Packages cover Debian, Ubuntu, Fedora, RHEL, Rocky Linux, AlmaLinux, openSUSE, and Arch Linux.

## Why Tako

SSH plus `systemctl`, `journalctl`, and your distro package manager already do everything Tako does. Tako does not replace them. It puts the read paths and the common safe writes behind one login so you can check a server from a phone or a borrowed laptop without remembering flags.

If you manage one or a few Linux hosts and want a quick visual overview with the option to restart a service, follow logs, or preview updates, Tako fits. If you need fleet management, config enforcement, or an external monitoring service, something else is a better match.

## What you can do

All of this is in the dashboard after login. Read paths work for any authenticated user. Writes need administrative access, which you elevate from inside the session with your password.

* Overview: host info, current CPU, memory, disk and network samples, storage summary, recent logs and processes.
* Metrics: 24-hour in-memory history with charts, plus live updates over server-sent events at intervals from 1s to 5m.
* Logs: journal browser with filters for boot, unit, priority, executable, and text, live tail, and CSV or JSON export.
* Services: system and user units of type service, target, socket, timer, and path. Inspect status, relationships, and raw unit config. Start, stop, restart, reload, enable, disable, mask, and unmask through allowlisted actions with a preview step.
* Processes: full process list from procfs with detail views. Optional per-process network accounting where the eBPF helper is present.
* Terminal: browser PTY over WebSocket running as your UID and GID.
* Accounts: local users and groups, group membership, administrator role, SSH authorized keys, and password change or reset. Login history comes straight from the journal. Destructive operations show a preview before they apply.
* Updates: distro-native update inventory using apt, dnf, pacman, or zypper depending on the host. Preview changes, refresh metadata, and run updates as a serialized background job with live progress. RHEL hosts also get kernel live-patch status.
* Storage: mounted filesystems and physical devices, with UDisks2 operations behind preview and confirm.
* Network: interface inventory with NetworkManager operations, plus firewall rules (UFW or firewalld) behind preview and confirm.
* Host: hostname, timezone, NTP state, reboot and shutdown with inhibitor checks, and boot history.
* Files: browse, search, read, and upload in bounded chunks, scoped to what your session is allowed to see.
* Timers and overrides: create structured systemd timer pairs and manage Tako's own service drop-ins, both with preview.
* Incidents, notifications, and jobs: a correlated incident timeline, a small built-in notification list, and background diagnostic jobs such as host inventory.
* Security posture: SELinux and AppArmor status with findings.

Capability reporting is honest about degraded hosts. If the user bus, user manager, NetworkManager, or another integration is missing, the affected page reports it instead of pretending.

## Quick start

You need a Linux host with systemd, Go 1.26+, Bun 1.3+, and PAM development headers (`pam-devel` on Fedora/Arch, `libpam0g-dev` on Debian/Ubuntu). Development mirrors production, so the steps below install real systemd units and a PAM policy pointed at your checkout.

```bash
bun install
sudo ./tools/tako-host setup   # one-time host setup
bun run dev
```

Open `https://127.0.0.1:9090` and log in with your UNIX username and password.

`bun run dev` builds `bin/tako`, starts the gateway and session helper through systemd socket activation, and proxies the browser to Vite so you get hot reload on the gateway origin. After Go changes, reload without a full restart:

```bash
./tools/tako-host reload
```

For layout work without login or terminals:

```bash
bun run dev:ui
```

This starts `serve --dev` with a fake broker on port 9090 and Vite on `http://127.0.0.1:5173`. It skips PAM and disables the terminal. Use `bun run dev` for anything involving auth, elevation, or sessiond.

Common commands:

```bash
bun run build      # dashboard build, then Go binary at bin/tako
bun run test       # Go tests plus dashboard typecheck
bun run lint       # go vet plus dashboard checks
bun run race       # Go race suite
./tools/tako-host status
./tools/tako-host stop
sudo ./tools/tako-host uninstall
```

Logs go to the journal in readable console form:

```bash
journalctl -u tako.service -u tako-sessiond.service -f
```

Set `TAKO_LOG_LEVEL=debug` for caller locations and extra detail.

## How it works

One binary, three modes:

```
browser --HTTPS--> tako serve --unix socket--> tako sessiond --framed RPC--> tako bridge (as you)
```

* `tako serve` is the gateway. It runs as a systemd `DynamicUser` (`tako-gateway`), serves the embedded dashboard, terminates TLS, holds sessions in memory, and enforces CSRF and origin checks. It never touches host D-Bus or the filesystem as root.
* `tako sessiond` is the host-authority boundary. It owns the session socket at `/run/tako/session.sock` (`root:tako-session`, mode `0660`), does PAM authentication, and resolves typed operations.
* `tako bridge` runs once per login as the authenticated UID and GID. User-scoped reads and writes happen here. Administrative system changes go back through root sessiond adapters.

Both services are socket-activated and exit after the configured idle interval when there is nothing left to do. Sockets stay listening so the next connection starts them again.

The API contract is at [apps/backend/api/openapi.yaml](./apps/backend/api/openapi.yaml). It is versioned as an internal dashboard contract and may change before v1.

## Installing on a server

Releases are built with GoReleaser for amd64 and arm64 (Arch is amd64 only) as `.deb`, `.rpm`, and Arch Linux packages. Each package installs the same layout:

* `/usr/bin/tako`, the multicall binary
* `tako.service` and `tako.socket` (unprivileged gateway)
* `tako-sessiond.service` and `tako-sessiond.socket` (privileged helper)
* `sysusers.d` entry for the persistent `tako-session` group, `tmpfiles.d` entry for `/run/tako`
* Polkit policy, an example config at `/usr/share/doc/tako/config.example.toml`, one distro branding image under `/usr/share/tako/branding/`, and a distro-specific PAM stack at `/etc/pam.d/tako`

Fresh installs enable and start `tako-sessiond.socket` before the public `tako.socket`. Nothing listens until the first connection.

Details on PAM variants, Polkit, sudoers guidance for the exact `NOPASSWD` probe, reverse-proxy setup, and the disposable-VM test harness are in [apps/backend/packaging/README.md](./apps/backend/packaging/README.md).

To build a package locally:

```bash
bun run package
TAKO_PACKAGE_TARGET=<manifest-id> bun run package   # one distro only
```

`bun run package:check` validates the toolchain without building. Local builds need Go 1.26+, GoReleaser 2.x, a C compiler, and the host PAM headers.

## Configuration

Copy [apps/backend/config.example.toml](./apps/backend/config.example.toml) to `/etc/tako/config.toml` and edit. Both the gateway and sessiond read the same file.

```toml
[server]
address = ":9090"
data_dir = "/var/lib/tako"
session_socket = "/run/tako/session.sock"
service_idle_timeout = "10m"
# allowed_origin = "https://console.example.com"
# certificate = "/etc/tako/tako.crt"
# certificate_key = "/etc/tako/tako.key"

[monitoring]
default_interval = "1m"
history_retention = "24h"

[admin]
idle_timeout = "5m"
```

Things worth knowing:

* Omit `allowed_origin` for direct same-origin use, including access by server IP or hostname. Set it to the exact public scheme, host, and port when running behind a reverse proxy with a different browser origin.
* For a reverse proxy, terminate public TLS at the proxy, forward only to loopback `127.0.0.1:9090`, preserve WebSocket upgrade headers for the terminal endpoint, and do not cache `/api/v1/*`.
* `service_idle_timeout` accepts any Go duration. `0` disables idle exit. When the service is idle and exits, the in-memory metric sampler stops too, so history shows gaps.
* Certificates in Tako's own state directory stay gateway-owned. External certificate paths are read by sessiond.
* Hosts must resolve systemd dynamic users: `passwd` (and usually `group`) in `nsswitch.conf` must include `systemd`.

Certificates bootstrap on first start when no paths are configured, so `bun run dev` works without extra setup.

## Project layout

```
apps/backend/            Go gateway, sessiond, bridge, platform adapters
apps/backend/api/        OpenAPI contract for the dashboard
apps/backend/packaging/  systemd units, PAM stacks, sysusers, packaging scripts
apps/dashboard/          React dashboard (Vite, TanStack Router and Query)
apps/web/                marketing site and docs placeholder (Astro)
tools/tako-host          host-integrated dev setup for systemd and PAM
```

The backend keeps platform logic in small module-owned adapters under `apps/backend/internal/`. The dashboard keeps reusable controls in `apps/dashboard/src/components/ui/` and pages in `apps/dashboard/src/routes/`. Production dashboard builds embed into the Go binary through `go:embed`.

## Status and scope

Tako is under active development and not yet at v1. The API explicitly offers no third-party compatibility promise during this phase.

What holds today: login and sessions, dashboard and metrics, logs, services, processes, terminal, accounts, updates, storage, network, firewall, host config and power, files, timers, incidents, and notifications, all behind the gateway/sessiond/bridge split described above.

What Tako will not do: run privileged work in the gateway process, fall back to the gateway's own runtime bus or filesystem for host authority, or perform process, account, package, storage, or network mutations outside narrow authenticated sessiond operations. New destructive actions arrive as small adapters with distribution VM tests, not as gateway code.

## Contributing

The working flow is the host-integrated setup in [HACKING.md](./HACKING.md): `bun install`, `sudo ./tools/tako-host setup`, `bun run dev`, and `./tools/tako-host reload` after Go changes.

Before opening a pull request, run the checks CI runs:

```bash
bun run test
bun run lint
bun run race
bunx --bun @redocly/cli@1.34.0 lint --config .redocly.yaml apps/backend/api/openapi.yaml
```

API changes should include handler tests and an update to `apps/backend/api/openapi.yaml`. UI changes should cover loading, error, empty, keyboard, mobile, and dark-mode states where they apply. Keep commits scoped with Conventional Commit prefixes such as `feat:`, `fix:`, or `docs:`, and note behavior and security impact in the pull request.

Issues live on GitHub. For anything security sensitive, do not open a public issue. Contact the maintainers privately and give them time to fix before disclosing.

## License

Tako is licensed under the GNU Affero General Public License v3.0 or later. See [LICENSE](./LICENSE) for the full text. Running a modified version on a network server requires offering users the corresponding source.
