# Tako

Tako is a lightweight, modern Linux administration dashboard written in Go and React. The repository is an Nx monorepo managed with Bun.

## Development

Requirements:

- Go 1.26+
- Bun 1.3+
- systemd, Linux procfs
- pam-devel (or distro equivalent) and optional system D-Bus / Polkit services

Linux only (Arch, Fedora, Debian/Ubuntu). Full workflow is documented in [`HACKING.md`](HACKING.md).

```bash
bun install
sudo ./tools/tako-host setup   # once per machine
bun run dev
```

Open **https://127.0.0.1:9090** and log in with your UNIX username and password.
`bun run dev` builds `bin/tako`, runs the real systemd gateway + sessiond stack,
and watches the dashboard into the checkout overlay. That is the same process
model as packaging (PAM, PTY, bridge grants). Refresh the browser after Vite
rebuilds.

For passwordless loopback UI-only work (no PAM, no terminals):

```bash
bun run dev:ui
```

Common workspace commands:

```bash
bun run build
bun run test
bun run lint
bun run typecheck
bun run race
bun run graph
./tools/tako-host reload   # after Go changes
```

`bun run build` builds `apps/dashboard` first, embeds its output in `apps/backend`, and writes the multicall executable to `bin/tako`.

Logs are JSON on the service journal (`journalctl -u tako.service -u tako-sessiond.service -f`). Set `TAKO_LOG_LEVEL=debug` for more detail.

## Implemented

The gateway includes HTTPS certificate bootstrapping, systemd socket activation, PAM authentication, in-memory sessions, CSRF/origin protections, capability discovery, an embedded responsive SPA, and bounded live metrics over SSE.

Dashboard, metrics, journal logs, systemd services, processes, users, mounted storage, network interfaces, PackageKit readiness, hardware, boot history, and restart status expose real host snapshots. Metrics retain a configurable 24-hour in-memory window and adapt collection to active browser intervals. Production PAM sessions receive a short-lived opaque bridge grant and can open a binary WebSocket PTY running under the authenticated UNIX UID/GID. The UI-only `serve --dev` lane deliberately disables terminals.

The Services page inventories service, target, socket, timer, and path units, exposes unit relationships and journal entries, and supports allowlisted lifecycle actions after time-bounded Administrative access. System actions are audited by the local privileged session service; the HTTPS gateway remains unprivileged. Current elevation uses password-backed PAM. Sudo `NOPASSWD`, interactive MFA, and fully functional user-manager actions remain follow-up compatibility work.

Ubuntu LTS and current Fedora are the primary integration targets. Network metadata auto-detects NetworkManager and systemd-networkd. Per-process network byte accounting is represented as an optional eBPF capability; hosts without the helper retain CPU, memory, threads, command, and disk-I/O process data.

Other destructive administration actions remain adapter boundaries: Tako does not perform process, account, package, storage, or network mutations from the unprivileged gateway. Those actions must be added through narrow authenticated bridge methods with distribution VM tests.

## Production layout

Install the single `bin/tako` multicall executable, systemd units from `apps/backend/packaging/systemd`, and PAM policy from `apps/backend/packaging/pam`. Systemd launches `tako serve` as the unprivileged network gateway and `tako sessiond` as the socket-activated PAM/PTY boundary. `tako bridge` is launched per user when module mutations need it. These remain isolated processes with distinct users and sandboxes; no gateway process retains root privileges.

Distribution-specific PAM variants, the optional Polkit action, exact-command sudoers guidance, reverse-proxy notes, and the disposable-VM packaging smoke seam are documented in [`apps/backend/packaging/README.md`](apps/backend/packaging/README.md).
