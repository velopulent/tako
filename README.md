# Tako

Tako is a lightweight, modern Linux administration dashboard written in Go and React.

## Development

Requirements: Go 1.26+, Bun, Linux procfs, and optional system D-Bus services.

```bash
cd web && bun install
cd ..
make build-web
go run ./cmd/tako serve --dev
```

Open `http://127.0.0.1:9090`. Development mode accepts a local UNIX username without a password and is restricted to loopback HTTP. Production mode uses HTTPS and `/run/tako/session.sock` for PAM authentication.

For local production-mode PAM testing, run both process modes in separate terminals:

```bash
sudo -g "$(id -gn)" ./bin/tako sessiond
./bin/tako serve --config ./local-config.toml
```

Set `data_dir` in `local-config.toml` to a directory writable only by your user; other server values can follow `config.example.toml`. Running `sudo ./bin/tako serve` alone does not start the privileged PAM service. Keep the network gateway unprivileged. Logs are JSON on stderr; set `TAKO_LOG_LEVEL=debug` for more detail.

## Implemented

The gateway includes HTTPS certificate bootstrapping, systemd socket activation, PAM authentication, in-memory sessions, CSRF/origin protections, capability discovery, an embedded responsive SPA, and bounded live metrics over SSE.

Dashboard, metrics, journal logs, systemd services, processes, users, mounted storage, network interfaces, and PackageKit readiness expose real host snapshots. Production PAM sessions receive a short-lived opaque bridge grant and can open a binary WebSocket PTY running under the authenticated UNIX UID/GID. Development mode deliberately disables terminals.

Destructive administration actions remain adapter boundaries: Tako does not perform service, process, account, package, storage, or network mutations from the unprivileged gateway. Those actions must be added to the per-user bridge with Polkit/systemd authorization and distribution VM tests.

## Production layout

Install the single `bin/tako` multicall executable, systemd units from `packaging/systemd`, and PAM policy from `packaging/pam`. Systemd launches `tako serve` as the unprivileged network gateway and `tako sessiond` as the socket-activated PAM/PTY boundary. `tako bridge` is launched per user when module mutations need it. These remain isolated processes with distinct users and sandboxes; no gateway process retains root privileges.
