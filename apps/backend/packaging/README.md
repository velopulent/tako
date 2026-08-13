# Production packaging

The package installs one `/usr/bin/tako` multicall executable and keeps the trust domains separate:

- `tako.service` is the unprivileged HTTPS gateway (`tako:tako`) and writes only to `/var/lib/tako`.
- `tako-sessiond.socket` exposes a mode-`0660` UNIX socket to `tako-session`; `tako-sessiond.service` is the root-owned PAM/session boundary.
- `tako bridge` is launched by the session service for an authenticated UNIX user and is never reachable from the network gateway.

Install the distribution-specific PAM file as `/etc/pam.d/tako`: use `pam/tako.debian` on Debian/Ubuntu and `pam/tako.redhat` on Fedora/RHEL-compatible systems. The generic `pam/tako` file is only a minimal development fixture; package builds should transform it to the host's standard stack.
Password changes and administrative resets use the host's standard `passwd` PAM service. They do not invoke a password helper with secrets in argv, stdin, or the environment.

Install `polkit/org.velopulent.tako.policy` when the deployment uses Polkit for Administrative access. The optional `sudoers.d/tako.example` documents the exact `NOPASSWD` probe; copy and edit it only for a dedicated local operator group. Do not grant the gateway service account unrestricted sudo.

For a reverse proxy, terminate public TLS at the proxy and forward only to a loopback Tako listener. Preserve WebSocket upgrade headers for `/api/v1/terminal/ws`, do not cache `/api/v1/*`, and restrict the proxy's upstream to `127.0.0.1:9090`. If Tako itself is internet-facing, use its generated/configured certificate and keep `allowed_origin` aligned with the browser origin.

The disposable-VM smoke seam is `packaging/smoke-test.sh`. It checks binary presence, socket activation, service identities, TLS reachability, and (when `TAKO_SMOKE_USER`/`TAKO_SMOKE_PASSWORD` are provided by the VM harness) a real PAM login. It never changes host state or stores credentials.
