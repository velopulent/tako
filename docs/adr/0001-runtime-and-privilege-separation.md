# ADR 0001: Runtime and privilege separation

Status: accepted

Tako separates network parsing from authentication and host access. `tako` runs as an unprivileged service account. A narrow, local, socket-activated `tako-sessiond` process performs PAM authentication, owns PAM session lifetime, and launches PTYs with the UNIX identity and supplementary groups of the operator. Administrative module mutations move to `tako-bridge` as each vertical slice gains its Polkit/systemd authorization adapter. They must never execute as the gateway service account.

Go executables are not installed setuid. This keeps the Go runtime outside a setuid process and makes the privileged entry point independently sandboxable and auditable through systemd.

Packaging under `apps/backend/packaging` uses one multicall executable with `serve`, `sessiond`, and `bridge` modes. This reduces installed artifacts without combining trust domains: systemd still launches each mode as a separate process with its own user, lifecycle, file descriptors, and sandbox policy.
