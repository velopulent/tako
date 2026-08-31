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

This installs the `tako-session` group (`sysusers.d`), PAM, systemd units,
`/etc/tako/config.toml`, and a copy of `bin/tako` at `/usr/local/libexec/tako`
(file context `bin_t`). The gateway runs as a systemd `DynamicUser`
(`tako-gateway`) with primary group `tako-session`. Dashboard assets are
**copied** to `/run/tako/dashboard` (not bind-mounted from `/home`) so SELinux
sees `var_run_t` rather than `user_home_t`. While `bun run dev` is running, a
user-space watcher recopies `dist/` after Vite rebuilds (systemd does not
inotify `/home`).

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

Host-dev installs `/usr/local/libexec/tako` with a persistent `bin_t` file
context (`semanage fcontext` + `restorecon`) and serves the SPA from a copy
under `/run/tako/dashboard`. Bind-mounting the checkout would keep
`user_home_t` and generate AVC alerts; do not do that.

A confined `tako_t` domain belongs with distro packaging. Until then, default
`unconfined_service_t` plus correct labels is enough for enforcing mode.

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
[`apps/backend/packaging/README.md`](apps/backend/packaging/README.md).

## Common workspace commands

```bash
bun run build
bun run test
bun run lint
bun run typecheck
bun run race
bun run graph
```
