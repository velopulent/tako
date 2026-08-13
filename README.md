# Tako

Tako is a lightweight, modern Linux administration dashboard written in Go and React. The repository is an Nx monorepo managed with Bun.

## Development

Requirements: Go 1.26+, Bun 1.3+, Linux procfs, and optional system D-Bus services.

```bash
bun install
bun run dev
```

Open `http://127.0.0.1:5173`. Nx runs the Vite dashboard and Go backend together; Vite proxies `/api` requests to the backend on `http://127.0.0.1:9090`. Development mode accepts a local UNIX username without a password and is restricted to loopback HTTP. Production mode uses HTTPS and `/run/tako/session.sock` for PAM authentication.

Common workspace commands:

```bash
bun run build
bun run test
bun run lint
bun run typecheck
bun run race
bun run graph
```

`bun run build` builds `apps/dashboard` first, embeds its output in `apps/backend`, and writes the multicall executable to `bin/tako`.

For local production-mode PAM testing, run both process modes in separate terminals:

```bash
sudo -g "$(id -gn)" ./bin/tako sessiond
./bin/tako serve --config ./local-config.toml
```

Set `data_dir` in `local-config.toml` to a directory writable only by your user; other server values can follow `apps/backend/config.example.toml`. Running `sudo ./bin/tako serve` alone does not start the privileged PAM service. Keep the network gateway unprivileged. Logs are JSON on stderr; set `TAKO_LOG_LEVEL=debug` for more detail.

## Implemented

The gateway includes HTTPS certificate bootstrapping, systemd socket activation, PAM authentication, in-memory sessions, CSRF/origin protections, capability discovery, an embedded responsive SPA, and bounded live metrics over SSE.

Dashboard, metrics, journal logs, systemd services, processes, users, mounted storage, network interfaces, and PackageKit readiness expose real host snapshots. Metrics retain a configurable 24-hour in-memory window and adapt collection to active browser intervals. Production PAM sessions receive a short-lived opaque bridge grant and can open a binary WebSocket PTY running under the authenticated UNIX UID/GID. Development mode deliberately disables terminals.

The Services page inventories service, target, socket, timer, and path units, exposes unit relationships and journal entries, and supports allowlisted lifecycle actions after time-bounded Administrative access. System actions are audited by the local privileged session service; the HTTPS gateway remains unprivileged. Current elevation uses password-backed PAM. Sudo `NOPASSWD`, interactive MFA, and fully functional user-manager actions remain follow-up compatibility work.

Ubuntu LTS and current Fedora are the primary integration targets. Network metadata auto-detects NetworkManager and systemd-networkd. Per-process network byte accounting is represented as an optional eBPF capability; hosts without the helper retain CPU, memory, threads, command, and disk-I/O process data.

Other destructive administration actions remain adapter boundaries: Tako does not perform process, account, package, storage, or network mutations from the unprivileged gateway. Those actions must be added through narrow authenticated bridge methods with distribution VM tests.

## Production layout

Install the single `bin/tako` multicall executable, systemd units from `apps/backend/packaging/systemd`, and PAM policy from `apps/backend/packaging/pam`. Systemd launches `tako serve` as the unprivileged network gateway and `tako sessiond` as the socket-activated PAM/PTY boundary. `tako bridge` is launched per user when module mutations need it. These remain isolated processes with distinct users and sandboxes; no gateway process retains root privileges.
