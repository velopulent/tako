# Hacking on Tako

Linux only (Arch, Fedora, Debian/Ubuntu). Development mirrors production: an
unprivileged `tako serve` gateway, socket-activated `tako sessiond`, PAM login,
and a checkout dashboard overlay.

## Requirements

- Go 1.26+
- Bun 1.3+
- systemd
- pam development headers (`pam-devel` / `libpam0g-dev`)
- Optional: Polkit (Administrative elevation)

```bash
bun install
```

## Host development (default)

One-time bootstrap (idempotent):

```bash
sudo ./tools/tako-host setup
```

This creates the `tako` / `tako-session` accounts, installs PAM and systemd
units, writes `/etc/tako/config.toml`, and points the units at `bin/tako` in
this checkout. Dashboard assets are bind-mounted from
`apps/backend/internal/dashboard/dist` to `/run/tako/dashboard`.

Day-to-day UI work:

```bash
bun run dev
```

That builds `bin/tako`, starts the systemd stack, and runs
`vite build --watch`. Open **https://127.0.0.1:9090** and log in with your
normal UNIX username and password. Refresh the browser after Vite finishes a
rebuild (no separate Vite origin on :5173).

After Go or unit changes:

```bash
./tools/tako-host reload
```

Other helpers:

```bash
./tools/tako-host status
./tools/tako-host stop
sudo ./tools/tako-host uninstall
```

Do **not** run `sudo ./bin/tako sessiond` by hand. Socket activation owns
`/run/tako/session.sock`, matching production.

Logs:

```bash
journalctl -u tako.service -u tako-sessiond.service -f
TAKO_LOG_LEVEL=debug   # set in a drop-in or the environment if needed
```

### SELinux

Tree-built binaries are copied to `/usr/local/libexec/tako` for systemd
(`ProtectHome` hides ExecStart under `/home`). On enforcing SELinux hosts
(Fedora/RHEL), `tools/tako-host` labels that copy `bin_t`. If it still fails:

```bash
sudo setenforce 0
```

or re-label the installed host binary:

```bash
sudo chcon -t bin_t /usr/local/libexec/tako
```

### Overlay

Gateway serves SPA files from `$TAKO_DASHBOARD_DIR` when set, else
`/run/tako/dashboard` when that path has `index.html`, else the embedded
`go:embed` assets. Host-dev units set `TAKO_DASHBOARD_DIR=/run/tako/dashboard`
and bind-mount the checkout `dist` there so Vite watch updates appear without
restarting the gateway.

## UI-only lane (not the product)

Passwordless loopback mode for layout work only. No PAM, no PTY, no user
bridge:

```bash
bun run dev:ui
```

Open `http://127.0.0.1:5173`. Vite proxies `/api` to `serve --dev` on port
9090. Prefer `bun run dev` whenever you need terminals, admin elevation, or
anything that talks to sessiond.

## Packaging and VMs

Production layout, PAM variants, and the disposable-VM smoke seam live in
[`apps/backend/packaging/README.md`](apps/backend/packaging/README.md). Distro
coverage expectations are in [`docs/vm-release-matrix.md`](docs/vm-release-matrix.md).

## Common workspace commands

```bash
bun run build
bun run test
bun run lint
bun run typecheck
bun run race
bun run graph
```
