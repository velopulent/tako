# Production packaging

The package installs one `/usr/bin/tako` multicall executable and keeps the trust domains separate:

- `tako.service` is the unprivileged HTTPS gateway (`tako:tako`) and writes only to `/var/lib/tako`.
- `tako-sessiond.socket` exposes a mode-`0660` UNIX socket to `tako-session`; `tako-sessiond.service` is the root-owned PAM/session boundary.
- `tmpfiles.d/tako.conf` creates `/run/tako` as `root:tako-session` with mode `0750` before socket activation; the gateway receives `tako-session` as a supplementary group.
- `tako bridge` is launched by the session service for an authenticated UNIX user and is never reachable from the network gateway.
- `tako-sessiond.service` is a login helper: it PAM-authenticates, then setuid-execs `tako bridge` as that user. The child inherits the unit sandbox, so sessiond is not locked down like `tako.service`. `tako serve --dev` skips PAM and the user bridge; it is not a production auth test.

Install the distribution-specific PAM file as `/etc/pam.d/tako`: use `pam/tako.debian` on Debian/Ubuntu and `pam/tako.redhat` on Fedora/RHEL-compatible systems. The generic `pam/tako` file is only a minimal development fixture; package builds should transform it to the host's standard stack.
Password changes and administrative resets use the host's standard `passwd` PAM service. They do not invoke a password helper with secrets in argv, stdin, or the environment.

Install `polkit/org.velopulent.tako.policy` when the deployment uses Polkit for Administrative access. The optional `sudoers.d/tako.example` documents the exact `NOPASSWD` probe; copy and edit it only for a dedicated local operator group. Do not grant the gateway service account unrestricted sudo.

For a reverse proxy, terminate public TLS at the proxy and forward only to a loopback Tako listener. Preserve WebSocket upgrade headers for `/api/v1/terminal/ws`, do not cache `/api/v1/*`, and restrict the proxy's upstream to `127.0.0.1:9090`. If Tako itself is internet-facing, use its generated/configured certificate and keep `allowed_origin` aligned with the browser origin (comma-separated list allowed).

Host-integrated development (`tools/tako-host`, dashboard overlay) is documented in [`HACKING.md`](../../../HACKING.md).

The disposable-VM smoke seam is `packaging/smoke-test.sh`. It checks binary presence, socket activation, service identities, TLS reachability, and (when `TAKO_SMOKE_USER`/`TAKO_SMOKE_PASSWORD` are provided by the VM harness) a real PAM login. `TAKO_SMOKE_USER` must be a **non-root** UNIX account; root-only success hides the user-bridge spawn path. It never changes host state or stores credentials.

The release image matrix and privileged-backend assertions are documented in
[`docs/vm-release-matrix.md`](../../../docs/vm-release-matrix.md). A packaging
run must record optional-backend degradation explicitly rather than treating a
missing SELinux, AppArmor, UFW, firewalld, Netplan, or PackageKit integration as
ready.
